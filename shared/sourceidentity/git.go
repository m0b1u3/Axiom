package sourceidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

type boundedGitWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedGitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("Git output exceeds inspection limit")
	}
	n, e := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, e
}

// Git never inherits repository credentials, ambient Git config or helpers.
// Callers pass separate arguments, and revision inputs follow --end-of-options.
func GitTo(ctx context.Context, root string, limit int64, dest io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	argv := []string{"--no-replace-objects", "--no-optional-locks", "--literal-pathspecs", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.attributesFile=" + os.DevNull, "-c", "core.quotePath=true", "-C", root}
	cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
	configureGitProcess(cmd)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_ALLOW_PROTOCOL=", "LC_ALL=C", "LANG=C"}
	if v := os.Getenv("SystemRoot"); v != "" {
		cmd.Env = append(cmd.Env, "SystemRoot="+v)
	}
	cmd.Stdout = &boundedGitWriter{dest, limit}
	var stderr bytes.Buffer
	cmd.Stderr = &boundedGitWriter{&stderr, 8192}
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("Git %s failed or exceeded its resource limit: %w", args[0], e)
	}
	return nil
}
func Git(ctx context.Context, root string, limit int64, args ...string) ([]byte, error) {
	var out bytes.Buffer
	e := GitTo(ctx, root, limit, &out, args...)
	return out.Bytes(), e
}
func GitDiffArgs(base, head string) []string {
	return []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--full-index", "--src-prefix=a/", "--dst-prefix=b/", "--diff-algorithm=myers", "--no-indent-heuristic", "--find-renames=50%", "-l1000", base, head, "--"}
}
func VerifyGitDiff(ctx context.Context, root, base, head, baseTree, headTree, diffHash, headHash string) error {
	for _, pair := range [][2]string{{base + "^{tree}", baseTree}, {head + "^{tree}", headTree}, {"HEAD", head}} {
		v, e := Git(ctx, root, 256, "rev-parse", "--verify", "--end-of-options", pair[0])
		if e != nil || strings.TrimSpace(string(v)) != pair[1] {
			return errors.New("冻结的 Git 提交或树身份不匹配")
		}
	}
	h := sha256.New()
	if e := GitTo(ctx, root, 64<<20, h, GitDiffArgs(base, head)...); e != nil {
		return e
	}
	if hex.EncodeToString(h.Sum(nil)) != diffHash {
		return errors.New("冻结的 Git 差异摘要不匹配")
	}
	v, _, _, e := WorktreeHash(ctx, root)
	if e != nil || v != headHash {
		return errors.New("冻结的 HEAD 源码与选定提交不匹配")
	}
	return nil
}
