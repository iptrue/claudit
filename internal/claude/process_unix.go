//go:build !windows

package claude

import (
	"os/exec"
	"syscall"
	"time"
)

const claudeProgram = "claude"

func startProcess(cmd *exec.Cmd) (func(), error) {
	// No controlling terminal: child commands cannot draw over the TUI or steal input.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP) }
	cmd.WaitDelay = time.Second
	return func() {}, cmd.Start()
}
