// Package sourceidentity implements the snapshot digest shared by upload and execution.
package sourceidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

func TreeHash(ctx context.Context, root string) (string, int, int64, error) {
	return treeHash(ctx, root, false)
}

// WorktreeHash binds the checked-out source independently of Git's object store.
// TreeHash retains its historical byte-for-byte algorithm, including dotfiles.
func WorktreeHash(ctx context.Context, root string) (string, int, int64, error) {
	return treeHash(ctx, root, true)
}

func treeHash(ctx context.Context, root string, ignoreGit bool) (string, int, int64, error) {
	type entry struct {
		rel  string
		size int64
		sha  string
	}
	entries := []entry{}
	var total int64
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if info.IsDir() {
			if ignoreGit && p != root && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("snapshot contains a non-regular file")
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		entries = append(entries, entry{filepath.ToSlash(rel), info.Size(), hex.EncodeToString(h.Sum(nil))})
		total += info.Size()
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%d\t%s\t%s\n", e.size, e.rel, e.sha)
	}
	return hex.EncodeToString(h.Sum(nil)), len(entries), total, nil
}
