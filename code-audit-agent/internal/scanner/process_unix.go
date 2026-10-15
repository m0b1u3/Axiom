//go:build !windows

package scanner

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
