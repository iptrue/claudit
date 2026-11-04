//go:build !windows

package claude

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func helperWatchHangup() {
	// The group gets HUP together; keep the parent alive long enough to reap its child.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
}

func assertProcessIsolated(t *testing.T, pid int) {
	t.Helper()
	if pgid, err := syscall.Getpgid(pid); err != nil || pgid != pid {
		t.Errorf("getpgid(%d) = %d, %v; want a group of its own", pid, pgid, err)
	}
}

func processExists(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func killTestProcess(pid int) {
	syscall.Kill(pid, syscall.SIGKILL)
}
