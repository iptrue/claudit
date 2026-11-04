//go:build !windows

package main

import (
	"os"
	"syscall"
)

var terminalSignals = []os.Signal{syscall.SIGHUP}
