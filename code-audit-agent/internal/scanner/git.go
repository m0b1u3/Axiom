package scanner

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// A scan receives its own source copy. The shared frozen repository is never
// given to the SDK as a writable checkout, and the copy is checked before use.
func materializeComponentSource(ctx context.Context, source, output, expected string) (string, error) {
	dir := filepath.Join(output, "runtime")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	dest, e := os.MkdirTemp(dir, "git-source-")
	if e != nil {
		return "", e
	}
	var total int64
	count := 0
	e = filepath.Walk(source, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		rel, e := filepath.Rel(source, p)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		name := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.Mkdir(name, 0700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("Git 快照包含链接或特殊文件")
		}
		count++
		total += info.Size()
		if count > 200000 || total > 10<<30 || info.Size() > 100<<20 {
			return errors.New("Git 快照超过物化限制")
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		ce := out.Close()
		if e != nil {
			return e
		}
		return ce
	})
	if e != nil {
		_ = os.RemoveAll(dest)
		return "", e
	}
	hash, _, _, e := sourceidentity.TreeHash(ctx, dest)
	if e != nil || hash != expected {
		return "", errors.New("Git 执行副本与冻结源码哈希不匹配")
	}
	return dest, nil
}

func materializeGitSource(ctx context.Context, source, output, expected string, target contracts.GitDiffTarget) (string, error) {
	dest, e := materializeComponentSource(ctx, source, output, expected)
	if e != nil {
		return "", e
	}
	if e = sourceidentity.VerifyGitDiff(ctx, dest, target.BaseCommit, target.HeadCommit, target.BaseTree, target.HeadTree, string(target.DiffSHA256), string(target.HeadContentHash)); e != nil {
		return "", e
	}
	return dest, nil
}
