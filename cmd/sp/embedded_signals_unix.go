//go:build darwin || linux

package main

import (
	"os"
	"syscall"
)

func memoryEditSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGPIPE}
}
