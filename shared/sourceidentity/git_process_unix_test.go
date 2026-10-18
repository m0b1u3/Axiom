//go:build !windows

package sourceidentity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGitSubprocessHasNoAmbientCredentialsOrGitRedirection(t *testing.T) {
	dir := t.TempDir()
	program := "#!/bin/sh\nif [ -n \"$GIT_DIR$GIT_INDEX_FILE$AXIOM_MASTER_KEY\" ]; then exit 9; fi\nprintf 'fixture'\n"
	if e := os.WriteFile(filepath.Join(dir, "git"), []byte(program), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_DIR", "/fixture/outside")
	t.Setenv("GIT_INDEX_FILE", "/fixture/index")
	t.Setenv("AXIOM_MASTER_KEY", "fixture-only")
	out, e := Git(t.Context(), dir, 7, "status")
	if e != nil || string(out) != "fixture" {
		t.Fatal(string(out), e)
	}
	if _, e = Git(t.Context(), dir, 3, "status"); e == nil {
		t.Fatal("unbounded Git output accepted")
	}
}
func TestGitCancellationStopsHelpersInItsPrivateProcessGroup(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip(e)
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	script := fmt.Sprintf("#!%s\nimport subprocess,sys,time\np=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'])\nwith open(%q,'w') as f: f.write(str(p.pid))\ntime.sleep(30)\n", python, pidFile)
	if e = os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := Git(ctx, dir, 1024, "repack"); done <- e }()
	deadline := time.Now().Add(3 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		raw, e := os.ReadFile(pidFile)
		if e == nil {
			pid, e = strconv.Atoi(string(raw))
			if e == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		cancel()
		t.Fatal("Git helper did not start")
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancellation ignored")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation exceeded deadline")
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if e := syscall.Kill(pid, 0); errors.Is(e, syscall.ESRCH) {
			return
		}
		out, _ := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(pid)).Output()
		if strings.HasPrefix(strings.TrimSpace(string(out)), "Z") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Git helper survived request cancellation")
}
