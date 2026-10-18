package scanner

import (
	"axiom/contracts"
	"axiom/sourceidentity"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitFixture(t *testing.T) (string, string, contracts.GitDiffTarget) {
	t.Helper()
	source := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		// Commits must finish all fixture writes before hashing or copying .git;
		// detached maintenance can otherwise remove locks during the snapshot walk.
		c := exec.Command("git", append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0", "-C", source}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatal(e, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "--template=", "--initial-branch=main", "--object-format=sha1")
	file := filepath.Join(source, "fixture.ts")
	os.WriteFile(file, []byte("// base\n"), 0600)
	git("add", ".")
	git("commit", "-qm", "base")
	base := git("rev-parse", "HEAD")
	os.WriteFile(file, []byte("// HEAD\n"), 0600)
	git("add", ".")
	git("commit", "-qm", "head")
	head := git("rev-parse", "HEAD")
	diff, e := sourceidentity.Git(t.Context(), source, 1<<20, sourceidentity.GitDiffArgs(base, head)...)
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(diff)
	work, _, _, e := sourceidentity.WorktreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	full, _, _, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	return source, full, contracts.GitDiffTarget{SchemaVersion: 1, Kind: "refs", ObjectFormat: "sha1", BaseCommit: base, HeadCommit: head, BaseTree: git("rev-parse", base+"^{tree}"), HeadTree: git("rev-parse", head+"^{tree}"), DiffSHA256: contracts.Digest(hex.EncodeToString(digest[:])), HeadContentHash: contracts.Digest(work)}
}
func TestGitSourceCopyProtectsSharedSnapshotAndChecksEveryIdentity(t *testing.T) {
	source, hash, target := gitFixture(t)
	dest, e := materializeGitSource(t.Context(), source, t.TempDir(), hash, target)
	if e != nil {
		t.Fatal(e)
	}
	if dest == source {
		t.Fatal("shared source passed to SDK")
	}
	os.WriteFile(filepath.Join(dest, "fixture.ts"), []byte("SDK changed its private checkout"), 0600)
	actual, _, _, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil || actual != hash {
		t.Fatal("shared immutable source changed", e)
	}
	for _, name := range []string{"hash", "head-tree", "diff", "head-source", "tamper"} {
		t.Run(name, func(t *testing.T) {
			v := target
			expected := hash
			switch name {
			case "hash":
				expected = strings.Repeat("f", 64)
			case "head-tree":
				v.HeadTree = strings.Repeat("f", 40)
			case "diff":
				v.DiffSHA256 = contracts.Digest(strings.Repeat("f", 64))
			case "head-source":
				v.HeadContentHash = contracts.Digest(strings.Repeat("f", 64))
			case "tamper":
				os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("tampered"), 0600)
			}
			if _, e := materializeGitSource(t.Context(), source, t.TempDir(), expected, v); e == nil {
				t.Fatal("unverified source reached SDK boundary")
			}
		})
	}
}
