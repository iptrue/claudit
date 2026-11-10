//go:build !windows

package ui

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// bangShell is the shell a ! command runs in, picked as for claude's own commands:
// CLAUDE_CODE_SHELL or SHELL when bash or zsh, else zsh or bash, else sh.
func bangShell() (shell, flag string) {
	for _, sh := range []string{os.Getenv("CLAUDE_CODE_SHELL"), os.Getenv("SHELL")} {
		if strings.Contains(sh, "bash") || strings.Contains(sh, "zsh") {
			if fi, err := os.Stat(sh); err == nil && fi.Mode()&0o111 != 0 {
				return sh, "-c"
			}
		}
	}
	for _, sh := range []string{"zsh", "bash"} {
		if p, err := exec.LookPath(sh); err == nil {
			return p, "-c"
		}
	}
	return "/bin/sh", "-c"
}

// bangProcess runs cmd in a session of its own, without the terminal, so that it cannot draw
// over the UI or read the keys; stopping it stops what it started too.
func bangProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
}
