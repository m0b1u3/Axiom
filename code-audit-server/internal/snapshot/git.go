package snapshot

import (
	"archive/tar"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var gitRevision = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./~^\-]{0,255}$`)
var gitLooseObject = regexp.MustCompile(`^[a-f0-9]{2}/[a-f0-9]{38}$`)
var gitPackObject = regexp.MustCompile(`^pack/pack-[a-f0-9]{40}\.(pack|idx|rev)$`)

// ImportGitArchive discards the submitted worktree and configuration. Only
// Git objects and refs are read to rebuild the selected, detached HEAD tree.
// No checkout filters, hooks, remote helpers or credential helpers are run.
func ImportGitArchive(ctx context.Context, archive, filename, destination, base, head string, limits Limits) (_ *Result, _ *contracts.GitDiffTarget, err error) {
	if !gitRevision.MatchString(base) || !gitRevision.MatchString(head) {
		return nil, nil, errors.New("base/head 须为单个提交、分支或标签，不能是范围、选项或路径")
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".git-import-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(stage)
	input := filepath.Join(stage, "incoming")
	if _, err = ExtractArchiveContext(ctx, archive, filename, input, limits); err != nil {
		return nil, nil, err
	}
	repository, err := archiveRepository(input)
	if err != nil {
		return nil, nil, err
	}
	if err = os.Mkdir(destination, 0700); err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(destination)
		}
	}()
	if _, err = sourceidentity.Git(ctx, destination, 4096, "init", "--quiet", "--template=", "--object-format=sha1"); err != nil {
		return nil, nil, err
	}
	if err = copyGitObjects(ctx, filepath.Join(repository, ".git"), filepath.Join(destination, ".git"), limits); err != nil {
		return nil, nil, err
	}
	resolve := func(value string) (string, error) {
		v, e := sourceidentity.Git(ctx, destination, 256, "rev-parse", "--verify", "--end-of-options", value+"^{commit}")
		if e != nil {
			return "", errors.New("选定的 Git 提交不存在或历史不完整；请上传含完整 .git 历史的仓库归档")
		}
		return strings.TrimSpace(string(v)), nil
	}
	baseID, err := resolve(base)
	if err != nil {
		return nil, nil, err
	}
	headID, err := resolve(head)
	if err != nil {
		return nil, nil, err
	}
	if baseID == headID {
		return nil, nil, errors.New("base/head 解析为同一提交，没有可审查的提交差异")
	}
	if _, err = sourceidentity.Git(ctx, destination, 1<<20, "fsck", "--strict", "--no-reflogs", "--no-dangling"); err != nil {
		return nil, nil, errors.New("Git 对象不完整或损坏；未创建扫描快照")
	}
	if err = os.RemoveAll(filepath.Join(destination, ".git", "refs")); err != nil {
		return nil, nil, err
	}
	if e := os.Remove(filepath.Join(destination, ".git", "packed-refs")); e != nil && !os.IsNotExist(e) {
		return nil, nil, e
	}
	if err = os.MkdirAll(filepath.Join(destination, ".git", "refs", "axiom"), 0700); err != nil {
		return nil, nil, err
	}
	for name, value := range map[string]string{"HEAD": headID, "refs/axiom/base": baseID, "refs/axiom/head": headID} {
		if err = os.WriteFile(filepath.Join(destination, ".git", filepath.FromSlash(name)), []byte(value+"\n"), 0600); err != nil {
			return nil, nil, err
		}
	}
	// Keep only objects reachable from the two frozen commits. Uploaded refs,
	// dangling objects and unrelated branch histories must not survive import.
	if _, err = sourceidentity.Git(ctx, destination, 4096, "repack", "-a", "-d", "--window=0", "--depth=0"); err != nil {
		return nil, nil, err
	}
	if _, err = sourceidentity.Git(ctx, destination, 4096, "prune", "--expire=now"); err != nil {
		return nil, nil, err
	}
	trees := []string{}
	for _, id := range []string{baseID, headID} {
		v, e := sourceidentity.Git(ctx, destination, 256, "rev-parse", "--verify", id+"^{tree}")
		if e != nil {
			return nil, nil, e
		}
		trees = append(trees, strings.TrimSpace(string(v)))
	}
	if err = materializeGitTree(ctx, destination, baseID, "", limits); err != nil {
		return nil, nil, err
	}
	if err = materializeGitTree(ctx, destination, headID, destination, limits); err != nil {
		return nil, nil, err
	}
	if _, err = sourceidentity.Git(ctx, destination, 4096, "read-tree", headID); err != nil {
		return nil, nil, err
	}
	headHash, _, _, err := sourceidentity.WorktreeHash(ctx, destination)
	if err != nil {
		return nil, nil, err
	}
	diff := sha256.New()
	if err = sourceidentity.GitTo(ctx, destination, 64<<20, diff, sourceidentity.GitDiffArgs(baseID, headID)...); err != nil {
		return nil, nil, err
	}
	target := &contracts.GitDiffTarget{SchemaVersion: 1, Kind: "refs", ObjectFormat: "sha1", BaseCommit: baseID, HeadCommit: headID, BaseTree: trees[0], HeadTree: trees[1], DiffSHA256: contracts.Digest(hex.EncodeToString(diff.Sum(nil))), HeadContentHash: contracts.Digest(headHash)}
	if raw, e := contracts.EncodeJSON(target); e != nil {
		return nil, nil, e
	} else if _, e = contracts.DecodeGitDiffTarget(raw, nil); e != nil {
		return nil, nil, e
	}
	content, count, size, err := sourceidentity.TreeHash(ctx, destination)
	if err != nil {
		return nil, nil, err
	}
	if count > limits.MaxFiles || size > limits.MaxExtractedBytes {
		return nil, nil, errors.New("重建后的 Git 对象与源码超过快照容量限制")
	}
	if err = filepath.Walk(destination, func(_ string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if !info.IsDir() && info.Size() > limits.MaxFileBytes {
			return errors.New("重建后的 Git 文件超过单文件容量限制")
		}
		return ctx.Err()
	}); err != nil {
		return nil, nil, err
	}
	archiveHash, err := canonicalGitArchiveHash(destination)
	if err != nil {
		return nil, nil, err
	}
	return &Result{ArchiveSha256: archiveHash, FileCount: count, TotalSize: size, ContentHash: content, SnapshotPath: destination}, target, nil
}

func archiveRepository(root string) (string, error) {
	if info, e := os.Lstat(filepath.Join(root, ".git")); e == nil && info.IsDir() {
		return root, nil
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		return "", e
	}
	if len(entries) == 1 && entries[0].IsDir() {
		nested := filepath.Join(root, entries[0].Name())
		if info, e := os.Lstat(filepath.Join(nested, ".git")); e == nil && info.IsDir() {
			return nested, nil
		}
	}
	return "", errors.New("归档需要一个含 .git 目录的完整仓库；普通源码 ZIP、外部 worktree 与浅克隆不能作为提交差异来源")
}
func copyGitObjects(ctx context.Context, source, destination string, limits Limits) error {
	if _, e := os.Stat(filepath.Join(source, "shallow")); e == nil {
		return errors.New("首版提交差异不接受浅克隆；请上传完整 Git 历史")
	}
	for _, name := range []string{"objects/info/alternates", "objects/info/http-alternates"} {
		if _, e := os.Stat(filepath.Join(source, filepath.FromSlash(name))); e == nil {
			return errors.New("Git 对象依赖外部目录，不能形成独立快照")
		}
	}
	copyFile := func(src, dst string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		in, e := os.Open(src)
		if e != nil {
			return e
		}
		defer in.Close()
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, io.LimitReader(in, limits.MaxFileBytes+1))
		closeErr := out.Close()
		if e != nil {
			return e
		}
		return closeErr
	}
	objects := filepath.Join(source, "objects")
	e := filepath.Walk(objects, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("Git 对象不是普通文件")
		}
		rel, _ := filepath.Rel(objects, p)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".promisor") {
			return errors.New("首版提交差异不接受需要远程补全的部分克隆")
		}
		if !gitLooseObject.MatchString(rel) && !gitPackObject.MatchString(rel) {
			return nil
		}
		return copyFile(p, filepath.Join(destination, "objects", filepath.FromSlash(rel)))
	})
	if e != nil {
		return e
	}
	refs := filepath.Join(source, "refs")
	if e = filepath.Walk(refs, func(p string, info os.FileInfo, e error) error {
		if os.IsNotExist(e) && p == refs {
			return nil
		}
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(refs, p)
		if !info.Mode().IsRegular() || info.Size() > 4096 {
			return errors.New("Git 引用不符合限制")
		}
		return copyFile(p, filepath.Join(destination, "refs", rel))
	}); e != nil {
		return e
	}
	for _, name := range []string{"HEAD", "packed-refs"} {
		p := filepath.Join(source, name)
		if info, e := os.Stat(p); e == nil {
			if !info.Mode().IsRegular() || info.Size() > 8<<20 {
				return errors.New("Git 引用超过限制")
			}
			if e = os.Remove(filepath.Join(destination, name)); e != nil && !os.IsNotExist(e) {
				return e
			}
			if e = copyFile(p, filepath.Join(destination, name)); e != nil {
				return e
			}
		}
	}
	return nil
}

func materializeGitTree(ctx context.Context, repository, commit, destination string, limits Limits) error {
	raw, e := sourceidentity.Git(ctx, repository, 32<<20, "ls-tree", "-r", "-l", "-z", commit)
	if e != nil {
		return e
	}
	state := &extractState{limits: limits, ctx: ctx}
	budget := limits.MaxExtractedBytes
	seenFiles := map[string]bool{}
	for _, record := range strings.Split(string(raw), "\x00") {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
			return errors.New("Git 提交包含符号链接、子模块或非普通文件；首版不接受这些目标")
		}
		rel, e := sanitize(name)
		if e != nil || rel == "" || rel != name || !utf8.ValidString(name) {
			return errors.New("Git 树包含不可安全物化的文件路径")
		}
		for _, part := range strings.Split(rel, "/") {
			if strings.EqualFold(part, ".git") {
				return errors.New("Git 树不能覆盖仓库元数据")
			}
		}
		size, e := strconv.ParseInt(fields[3], 10, 64)
		if e != nil || size < 0 {
			return errors.New("Git 文件大小无效")
		}
		if e = state.entry(size, 0); e != nil {
			return e
		}
		if destination == "" {
			continue
		}
		if e = ensureDirFor(destination, rel, seenFiles); e != nil {
			return e
		}
		name = filepath.Join(destination, filepath.FromSlash(rel))
		mode := os.FileMode(0644)
		if fields[0] == "100755" {
			mode = 0755
		}
		f, e := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			return e
		}
		e = sourceidentity.GitTo(ctx, repository, size+1, f, "cat-file", "blob", fields[2])
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if size > budget {
			return errors.New("Git 树超过快照容量")
		}
		budget -= size
		seenFiles[name] = true
	}
	return nil
}
func canonicalGitArchiveHash(root string) (string, error) {
	h := sha256.New()
	writer := tar.NewWriter(h)
	e := filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		header := &tar.Header{Name: filepath.ToSlash(rel), Mode: int64(info.Mode().Perm()), Size: info.Size(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if e = writer.WriteHeader(header); e != nil {
			return e
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(writer, f)
		ce := f.Close()
		if e != nil {
			return e
		}
		return ce
	})
	if e != nil {
		return "", e
	}
	if e = writer.Close(); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GitDiffFiles returns complete names (including tabs/newlines) using Git's NUL format.
func GitDiffFiles(ctx context.Context, root string, target contracts.GitDiffTarget) ([]contracts.GitDiffFile, error) {
	raw, e := sourceidentity.Git(ctx, root, 32<<20, "diff", "--raw", "--no-abbrev", "--no-ext-diff", "--no-textconv", "-z", "--find-renames=50%", "-l1000", target.BaseCommit, target.HeadCommit, "--")
	if e != nil {
		return nil, e
	}
	parts := strings.Split(string(raw), "\x00")
	files := []contracts.GitDiffFile{}
	for i := 0; i < len(parts) && parts[i] != ""; {
		fields := strings.Fields(parts[i])
		i++
		if len(fields) != 5 || i >= len(parts) {
			return nil, errors.New("Git 差异列表格式无效")
		}
		oldPath := parts[i]
		newPath := oldPath
		i++
		status := fields[4]
		if strings.HasPrefix(status, "R") {
			if i >= len(parts) {
				return nil, errors.New("Git 重命名记录缺失")
			}
			newPath = parts[i]
			i++
		}
		f := contracts.GitDiffFile{Index: int64(len(files)), OldPath: (*contracts.RelativePath)(&oldPath), NewPath: (*contracts.RelativePath)(&newPath), OldBlob: &fields[2], NewBlob: &fields[3]}
		switch status[0] {
		case 'A':
			f.Status = "added"
			f.OldPath = nil
			f.OldBlob = nil
		case 'D':
			f.Status = "deleted"
			f.NewPath = nil
			f.NewBlob = nil
		case 'M':
			f.Status = "modified"
		case 'R':
			f.Status = "renamed"
		case 'T':
			f.Status = "type_changed"
		default:
			return nil, fmt.Errorf("Git 差异类型不支持: %c", status[0])
		}
		files = append(files, f)
	}
	return files, nil
}
