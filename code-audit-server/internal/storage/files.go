// Package storage confines maintenance to configured managed roots. It never
// follows artifact symlinks and never deletes a managed root itself.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const Trash = ".axiom-trash"
const MaxBytes int64 = 9007199254740991

var ErrBoundary = errors.New("目录边界、符号链接或特殊文件检查未通过，数据已保留")
var ErrChanged = errors.New("目录内容与预览不一致，请重新预览；已有隔离数据可继续清理")

// Resolve existing ancestors too: /var on macOS and restored mount paths may
// differ lexically while referring to the same configured root.
func Resolve(name string) (string, error) {
	if name == "" {
		return "", ErrBoundary
	}
	abs, e := filepath.Abs(name)
	if e != nil {
		return "", e
	}
	tail := []string{}
	at := abs
	for {
		_, e = os.Lstat(at)
		if e == nil {
			break
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(at)
		if parent == at {
			return "", ErrBoundary
		}
		tail = append(tail, filepath.Base(at))
		at = parent
	}
	real, e := filepath.EvalSymlinks(at)
	if e != nil {
		return "", e
	}
	for i := len(tail) - 1; i >= 0; i-- {
		real = filepath.Join(real, tail[i])
	}
	return real, nil
}
func Root(name string) (string, error) {
	p, e := Resolve(name)
	if e != nil {
		return "", e
	}
	if filepath.Dir(p) == p {
		return "", ErrBoundary
	}
	if info, e := os.Stat(p); e == nil && !info.IsDir() {
		return "", ErrBoundary
	} else if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	return p, nil
}
func Top(root, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	p, e := Resolve(name)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrBoundary
	}
	top := strings.Split(rel, string(os.PathSeparator))[0]
	if !validLeaf(top) || top == Trash {
		return "", ErrBoundary
	}
	return top, nil
}
func validLeaf(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}

// A source reference can be a parent directory or an artifact below this root.
func Related(root, name, reference string) (bool, error) {
	if !validLeaf(name) || name == Trash {
		return false, ErrBoundary
	}
	target := filepath.Join(root, name)
	ref, e := Resolve(reference)
	if e != nil {
		return false, e
	}
	contains := func(parent, child string) bool {
		rel, e := filepath.Rel(parent, child)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	return contains(target, ref) || contains(ref, target), nil
}

// Missing database linkage is not proof that an old report can be discarded.
// Portable execution markers are conservatively retained for investigation.
func HasEvidence(ctx context.Context, root, name string) (bool, error) {
	r, e := os.OpenRoot(root)
	if e != nil {
		return false, e
	}
	defer r.Close()
	found := false
	count := 0
	e = fs.WalkDir(r.FS(), name, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		count++
		if count > 200000 {
			return ErrBoundary
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrBoundary
		}
		if !entry.IsDir() {
			switch entry.Name() {
			case "result.json", "result.commit.json", "progress.json", "events.jsonl", "checkpoints.jsonl":
				found = true
				return fs.SkipAll
			}
		}
		return nil
	})
	return found, e
}
func Entries(root string) ([]os.DirEntry, error) {
	r, e := os.OpenRoot(root)
	if e != nil {
		if os.IsNotExist(e) {
			return []os.DirEntry{}, nil
		}
		return nil, e
	}
	defer r.Close()
	f, e := r.Open(".")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	out, e := f.ReadDir(-1)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, e
}

type counter struct {
	files int
	bytes int64
}

// Usage reports logical regular-file bytes, not a promise of reclaimed blocks.
func Usage(ctx context.Context, root, name string, digest bool) (int64, string, error) {
	r, e := os.OpenRoot(root)
	if e != nil {
		if os.IsNotExist(e) {
			return 0, "", nil
		}
		return 0, "", e
	}
	defer r.Close()
	return measure(ctx, r, name, digest)
}
func measure(ctx context.Context, r *os.Root, name string, digest bool) (int64, string, error) {
	device, e := r.Stat(".")
	if e != nil {
		return 0, "", e
	}
	h := sha256.New()
	count := counter{}
	var walk func(string) error
	walk = func(path string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		count.files++
		if count.files > 200000 {
			return errors.New("目录条目超过本次检查上限，保留数据")
		}
		info, e := r.Lstat(path)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return ErrBoundary
		}
		if !sameDevice(device, info) {
			return ErrBoundary
		}
		size := info.Size()
		if info.IsDir() {
			size = 0
		}
		fmt.Fprintf(h, "%d:%s:%d:%d\n", len(path), path, info.Mode(), size)
		if info.IsDir() {
			f, e := r.Open(path)
			if e != nil {
				return e
			}
			entries, e := f.ReadDir(-1)
			f.Close()
			if e != nil {
				return e
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
			for _, v := range entries {
				if e = walk(filepath.Join(path, v.Name())); e != nil {
					return e
				}
			}
			return nil
		}
		if info.Size() < 0 || count.bytes > MaxBytes-info.Size() {
			return ErrBoundary
		}
		count.bytes += info.Size()
		if digest {
			f, e := r.Open(path)
			if e != nil {
				return e
			}
			_, e = io.Copy(h, &contextReader{ctx, f})
			f.Close()
			return e
		}
		return nil
	}
	if e := walk(name); e != nil {
		if os.IsNotExist(e) {
			return 0, "", nil
		}
		return 0, "", e
	}
	return count.bytes, hex.EncodeToString(h.Sum(nil)), nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}

// Quarantine uses a root-relative, descriptor-based rename on certified Unix
// runtimes. The deterministic destination lets recovery detect a lost phase ACK.
func Quarantine(ctx context.Context, root, name, destination, expected string) (bool, error) {
	if !validLeaf(name) || name == Trash || !validLeaf(destination) {
		return false, ErrBoundary
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return false, e
	}
	defer r.Close()
	if e = r.Mkdir(Trash, 0700); e != nil && !os.IsExist(e) {
		return false, e
	}
	info, e := r.Lstat(Trash)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrBoundary
	}
	trash, e := r.OpenRoot(Trash)
	if e != nil {
		return false, e
	}
	defer trash.Close()
	if _, e = trash.Lstat(destination); e == nil {
		return true, nil
	} else if !os.IsNotExist(e) {
		return false, e
	}
	_, hash, e := measure(ctx, r, name, true)
	if e != nil {
		return false, e
	}
	if hash == "" {
		return false, nil
	}
	if hash != expected {
		return false, ErrChanged
	}
	if e = renameRoot(r, name, trash, destination); e != nil {
		return false, e
	}
	return true, nil
}
func Purge(ctx context.Context, root, destination string) error {
	if !validLeaf(destination) {
		return ErrBoundary
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return e
	}
	defer r.Close()
	info, e := r.Lstat(Trash)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrBoundary
	}
	q, e := r.OpenRoot(Trash)
	if e != nil {
		return e
	}
	defer q.Close()
	device, e := q.Stat(".")
	if e != nil {
		return e
	}
	var remove func(*os.Root, string) error
	remove = func(at *os.Root, name string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		info, e := at.Lstat(name)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !sameDevice(device, info) {
			return ErrBoundary
		}
		if info.IsDir() {
			child, e := at.OpenRoot(name)
			if e != nil {
				return e
			}
			f, e := child.Open(".")
			if e != nil {
				child.Close()
				return e
			}
			items, e := f.ReadDir(-1)
			f.Close()
			if e == nil {
				for _, item := range items {
					if e = remove(child, item.Name()); e != nil {
						break
					}
				}
			}
			child.Close()
			if e != nil {
				return e
			}
		}
		return at.Remove(name)
	}
	return remove(q, destination)
}

func Quarantined(root, destination string) (bool, error) {
	if !validLeaf(destination) {
		return false, ErrBoundary
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return false, e
	}
	defer r.Close()
	info, e := r.Lstat(Trash)
	if os.IsNotExist(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrBoundary
	}
	q, e := r.OpenRoot(Trash)
	if e != nil {
		return false, e
	}
	defer q.Close()
	_, e = q.Lstat(destination)
	if os.IsNotExist(e) {
		return false, nil
	}
	return e == nil, e
}
