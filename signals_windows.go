package main

import (
	"os"
	"syscall"
)

// Windows reports console close/logoff/shutdown as SIGTERM to os/signal.
var terminalSignals = []os.Signal{syscall.SIGTERM}
