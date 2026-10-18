//go:build !windows

package scanner

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	cmd.WaitDelay = 5 * time.Second
}

func scannerParentEnvironment(env []string) []string {
	result := make([]string, 0, len(env)+2)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "AXIOM_SCANNER_PARENT_PID=") && !strings.HasPrefix(entry, "AXIOM_SCANNER_OWN_PROCESS_GROUP=") {
			result = append(result, entry)
		}
	}
	return append(result, "AXIOM_SCANNER_PARENT_PID="+strconv.Itoa(os.Getpid()), "AXIOM_SCANNER_OWN_PROCESS_GROUP=1")
}

// Give a native device-code handle a bounded opportunity to cancel before
// stopping the entire process group. The lease still bounds all descendants.
func configureIdentityProcessGroup(cmd *exec.Cmd) func() {
	configureProcessGroup(cmd)
	done := make(chan struct{})
	var canceled, closed sync.Once
	cmd.Cancel = func() error {
		var err error
		canceled.Do(func() {
			err = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			go func() {
				select {
				case <-done:
					return
				case <-time.After(3 * time.Second):
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			}()
		})
		return err
	}
	return func() { closed.Do(func() { close(done) }) }
}
