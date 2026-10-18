//go:build windows

package sourceidentity

import (
	"os/exec"
	"time"
)

func configureGitProcess(cmd *exec.Cmd) { cmd.WaitDelay = time.Second }
