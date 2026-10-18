package snapshot_test

import (
	"archive/zip"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/sourceidentity"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	out, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("Git fixture %v: %v %s", args, e, out)
	}
	return strings.TrimSpace(string(out))
}
func gitFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
}
func gitRepo(t *testing.T) (root, base, head string) {
	t.Helper()
	root = t.TempDir()
	gitCmd(t, root, "init", "-q", "--template=", "--initial-branch=main", "--object-format=sha1")
	gitFile(t, root, "fixture.ts", "// Fixtures / Mock only\nconst query = 'base-only';\ndatabase.execute(query);\n")
	gitFile(t, root, "deleted.ts", "// deleted in HEAD\n")
	gitFile(t, root, "old.ts", "// rename content remains exactly the same\n")
	gitCmd(t, root, "add", ".")
	gitCmd(t, root, "commit", "-qm", "base")
	base = gitCmd(t, root, "rev-parse", "HEAD")
	gitFile(t, root, "fixture.ts", "// Fixtures / Mock only\nconst query = request.query.q;\ndatabase.execute(query);\n")
	os.Remove(filepath.Join(root, "deleted.ts"))
	os.Rename(filepath.Join(root, "old.ts"), filepath.Join(root, "new.ts"))
	gitFile(t, root, "源码/新增.ts", "// Unicode fixture\n")
	gitFile(t, root, "binary.bin", "\x00\x01\x02")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "commit", "-qm", "head")
	head = gitCmd(t, root, "rev-parse", "HEAD")
	return
}
func gitZip(t *testing.T, root, prefix string) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "repository.zip")
	f, e := os.Create(name)
	if e != nil {
		t.Fatal(e)
	}
	w := zip.NewWriter(f)
	e = filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		header := &zip.FileHeader{Name: prefix + filepath.ToSlash(rel), Method: zip.Deflate}
		header.SetMode(info.Mode())
		out, e := w.CreateHeader(header)
		if e != nil {
			return e
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		ce := in.Close()
		if e != nil {
			return e
		}
		return ce
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	return name
}
func TestGitSnapshotFreezesCommittedTreesAndDiscardsAmbientRepositoryState(t *testing.T) {
	root, base, head := gitRepo(t)
	// A credential, executable hook, dirty tracked file and dangling object are
	// deliberately present in the submission, but never in the frozen snapshot.
	gitCmd(t, root, "config", "remote.origin.url", "https://fixture:private-token@example.invalid/repo")
	gitCmd(t, root, "config", "core.hooksPath", "/fixture/untrusted-hooks")
	gitFile(t, root, ".git/hooks/post-checkout", "#!/bin/sh\nexit 99\n")
	gitFile(t, root, "dirty-only.txt", "must disappear")
	gitFile(t, root, "fixture.ts", "dirty worktree must disappear")
	gitFile(t, root, ".git/unused-secret", "unused fixture")
	dangling := gitCmd(t, root, "hash-object", "-w", ".git/unused-secret")
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "loose", true: "packed"}[packed], func(t *testing.T) {
			if packed {
				gitCmd(t, root, "repack", "-ad")
				gitCmd(t, root, "pack-refs", "--all")
			}
			archive := gitZip(t, root, "repository/")
			dest := filepath.Join(t.TempDir(), "frozen")
			result, target, e := snapshot.ImportGitArchive(t.Context(), archive, "repository.zip", dest, "HEAD~1", "main", limits())
			if e != nil {
				t.Fatal(e)
			}
			if target.BaseCommit != base || target.HeadCommit != head {
				t.Fatal("refs not pinned", target)
			}
			if got := string(mustRead(t, filepath.Join(dest, "fixture.ts"))); !strings.Contains(got, "request.query.q") {
				t.Fatal("dirty source copied", got)
			}
			for _, name := range []string{"dirty-only.txt", "deleted.ts", ".git/hooks/post-checkout", ".git/unused-secret", ".git/refs/heads/main"} {
				if _, e := os.Stat(filepath.Join(dest, name)); !os.IsNotExist(e) {
					t.Fatal("unexpected uploaded state", name, e)
				}
			}
			config := string(mustRead(t, filepath.Join(dest, ".git/config")))
			if strings.Contains(config, "private-token") || strings.Contains(config, "untrusted-hooks") {
				t.Fatal("repository credentials/config survived")
			}
			if _, e := sourceidentity.Git(t.Context(), dest, 1024, "cat-file", "-e", dangling); e == nil {
				t.Fatal("dangling object survived")
			}
			if e = sourceidentity.VerifyGitDiff(t.Context(), dest, base, head, target.BaseTree, target.HeadTree, string(target.DiffSHA256), string(target.HeadContentHash)); e != nil {
				t.Fatal(e)
			}
			files, e := snapshot.GitDiffFiles(t.Context(), dest, *target)
			if e != nil || len(files) != 5 {
				t.Fatal(files, e)
			}
			status := map[string]int{}
			for _, f := range files {
				status[string(f.Status)]++
			}
			if status["renamed"] != 1 || status["deleted"] != 1 || status["added"] != 2 || status["modified"] != 1 {
				t.Fatal(status)
			}
			again, other, e := snapshot.ImportGitArchive(t.Context(), archive, "repository.zip", filepath.Join(t.TempDir(), "again"), base, head, limits())
			if e != nil || *target != *other || result.ArchiveSha256 != again.ArchiveSha256 || result.ContentHash != again.ContentHash {
				t.Fatal("equivalent resolved refs changed identity", again, e)
			}
			gitFile(t, dest, "fixture.ts", "tampered")
			if e = sourceidentity.VerifyGitDiff(t.Context(), dest, base, head, target.BaseTree, target.HeadTree, string(target.DiffSHA256), string(target.HeadContentHash)); e == nil {
				t.Fatal("source tamper accepted")
			}
		})
	}
}
func TestGitSnapshotRejectsIncompleteOrUnsafeInputsAndCleansFailures(t *testing.T) {
	for _, name := range []string{"missing-git", "shallow", "alternates", "promisor", "invalid-ref", "missing-ref", "same-ref", "symlink", "submodule", "corrupt", "canceled", "tree-limit", "backslash-path"} {
		t.Run(name, func(t *testing.T) {
			root, _, _ := gitRepo(t)
			base, head := "HEAD~1", "HEAD"
			lim := limits()
			ctx := t.Context()
			switch name {
			case "missing-git":
				os.RemoveAll(filepath.Join(root, ".git"))
			case "shallow":
				gitFile(t, root, ".git/shallow", gitCmd(t, root, "rev-parse", "HEAD")+"\n")
			case "alternates":
				gitFile(t, root, ".git/objects/info/alternates", "/fixture/external\n")
			case "promisor":
				gitFile(t, root, ".git/objects/pack/pack-"+strings.Repeat("f", 40)+".promisor", "")
			case "invalid-ref":
				head = "--upload-pack=evil"
			case "missing-ref":
				head = "missing-branch"
			case "same-ref":
				base = "HEAD"
			case "symlink":
				if e := os.Symlink("fixture.ts", filepath.Join(root, "link.ts")); e != nil {
					t.Skip(e)
				}
				gitCmd(t, root, "add", "link.ts")
				gitCmd(t, root, "commit", "-qm", "link")
			case "submodule":
				gitCmd(t, root, "update-index", "--add", "--cacheinfo", "160000,"+gitCmd(t, root, "rev-parse", "HEAD")+",vendor")
				gitCmd(t, root, "commit", "-qm", "submodule")
			case "corrupt":
				id := gitCmd(t, root, "rev-parse", "HEAD")
				os.Remove(filepath.Join(root, ".git/objects/"+id[:2]+"/"+id[2:]))
				gitFile(t, root, ".git/objects/"+id[:2]+"/"+id[2:], "not a git object")
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "backslash-path":
				gitFile(t, root, "unsafe\\name.ts", "// fixture\n")
				gitCmd(t, root, "add", ".")
				gitCmd(t, root, "commit", "-qm", "unsafe-path")
			case "tree-limit":
				lim.MaxFiles = 1
			}
			archive := gitZip(t, root, "")
			dest := filepath.Join(t.TempDir(), "rejected")
			if _, _, e := snapshot.ImportGitArchive(ctx, archive, "repository.zip", dest, base, head, lim); e == nil {
				t.Fatal("unsafe input accepted")
			}
			if _, e := os.Stat(dest); !os.IsNotExist(e) {
				t.Fatal("failed import retained a partial snapshot", e)
			}
		})
	}
}
