//go:build windows

package scanner

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}

// Windows requires Job Objects to guarantee descendant cleanup after Agent
// termination. The production container support matrix is Linux/amd64.
func scannerParentEnvironment(env []string) []string { return env }

func configureIdentityProcessGroup(cmd *exec.Cmd) func() {
	configureProcessGroup(cmd)
	return func() {}
}
