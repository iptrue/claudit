//go:build windows

package ui

import (
	"os/exec"
	"time"
)

// bangShell is the shell a ! command runs in: Git Bash when it is on PATH, as Claude Code
// prefers it on Windows, else cmd.
func bangShell() (shell, flag string) {
	if p, err := exec.LookPath("bash"); err == nil {
		return p, "-c"
	}
	return "cmd", "/C"
}

// bangProcess lets a stopped command's pipes go after a second, should a child hold them.
func bangProcess(cmd *exec.Cmd) {
	cmd.WaitDelay = time.Second
}
