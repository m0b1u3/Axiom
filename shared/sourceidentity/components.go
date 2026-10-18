package sourceidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Component struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}
type ComponentOverlap struct {
	LeftComponent  int    `json:"leftComponent"`
	LeftPath       string `json:"leftPath"`
	RightComponent int    `json:"rightComponent"`
	RightPath      string `json:"rightPath"`
}
type ComponentCoverage struct {
	InventoryHash    string             `json:"inventoryHash"`
	FileCount        int                `json:"fileCount"`
	CoveredFileCount int                `json:"coveredFileCount"`
	UncoveredPaths   []string           `json:"uncoveredPaths"`
	OverlapCount     int                `json:"overlapCount"`
	Overlaps         []ComponentOverlap `json:"overlaps"`
	EmptyComponents  []int              `json:"emptyComponents"`
	DuplicateNames   []string           `json:"duplicateNames"`
	Valid            bool               `json:"valid"`
}

// CheckComponents inventories the entire frozen source, including dotfiles and
// ignored source files. Git metadata is never a component or coverage evidence.
func CheckComponents(ctx context.Context, root string, components []Component) (ComponentCoverage, error) {
	out := ComponentCoverage{UncoveredPaths: []string{}, Overlaps: []ComponentOverlap{}, EmptyComponents: []int{}, DuplicateNames: []string{}}
	files := []string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if p != root && info.Name() == ".git" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return errors.New("源码包含外部 Git worktree")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("源码包含链接或特殊文件")
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		files = append(files, filepath.ToSlash(rel))
		if len(files) > 200000 {
			return errors.New("源码文件数量超过组件检查限制")
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	out.InventoryHash = hex.EncodeToString(h.Sum(nil))
	out.FileCount = len(files)
	type selected struct {
		index int
		path  string
	}
	paths := []selected{}
	owners := map[string][]int{}
	counts := make([]int, len(components))
	names := map[string]bool{}
	for i, c := range components {
		name := TrimComponentName(c.Name)
		if names[name] {
			out.DuplicateNames = append(out.DuplicateNames, name)
		}
		names[name] = true
		for _, p := range c.Paths {
			if !componentPath(p) {
				return out, errors.New("组件路径必须是规范相对路径，且不能选择 Git 元数据")
			}
			info, e := os.Lstat(filepath.Join(root, filepath.FromSlash(p)))
			if e != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
				return out, errors.New("组件路径不存在或不是普通源码")
			}
			paths = append(paths, selected{i, p})
			owners[p] = append(owners[p], i)
		}
	}
	for i, a := range paths {
		for _, b := range paths[i+1:] {
			if containsComponentPath(a.path, b.path) || containsComponentPath(b.path, a.path) {
				out.OverlapCount++
				if len(out.Overlaps) < 200 {
					out.Overlaps = append(out.Overlaps, ComponentOverlap{a.index, a.path, b.index, b.path})
				}
			}
		}
		if i%64 == 0 && ctx.Err() != nil {
			return out, ctx.Err()
		}
	}
	for _, file := range files {
		if e := ctx.Err(); e != nil {
			return out, e
		}
		matched := false
		for p := file; ; {
			for _, i := range owners[p] {
				counts[i]++
				matched = true
			}
			if p == "." {
				break
			}
			at := strings.LastIndexByte(p, '/')
			if at < 0 {
				p = "."
			} else {
				p = p[:at]
			}
		}
		if matched {
			out.CoveredFileCount++
		} else {
			out.UncoveredPaths = append(out.UncoveredPaths, file)
		}
	}
	for i, n := range counts {
		if n == 0 {
			out.EmptyComponents = append(out.EmptyComponents, i)
		}
	}
	out.Valid = out.FileCount > 0 && out.CoveredFileCount == out.FileCount && out.OverlapCount == 0 && len(out.EmptyComponents) == 0 && len(out.DuplicateNames) == 0
	return out, nil
}
func containsComponentPath(a, b string) bool {
	return a == "." || a == b || strings.HasPrefix(b, a+"/")
}

// TrimComponentName matches ECMAScript String.trim used by the pinned SDK.
// Go TrimSpace additionally trims U+0085 and does not trim U+FEFF.
func TrimComponentName(name string) string {
	return strings.TrimFunc(name, func(r rune) bool {
		return r >= 0x9 && r <= 0xd || r == 0x20 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}
func componentPath(p string) bool {
	if p == "." {
		return true
	}
	if p == "" || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." || s == ".git" {
			return false
		}
	}
	return true
}
