//go:build !windows

package sourceidentity

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Git repack/fsck may launch helpers. Cancel the whole private group when the
// request deadline expires, including helpers holding temporary object files.
func configureGitProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return e
	}
	cmd.WaitDelay = time.Second
}
