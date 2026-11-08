//go:build darwin || linux

package editorterminal

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformSupported() bool { return true }

// Catchable suspend/hangup requests are encrypted cancellation, not job suspension.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGTSTP, syscall.SIGHUP}
}

func foregroundTerminal(file *os.File) bool {
	group, err := unix.IoctlGetInt(int(file.Fd()), unix.TIOCGPGRP)
	return err == nil && group > 0 && group == unix.Getpgrp()
}
func sameTerminal(input, output *os.File) bool {
	var a, b unix.Stat_t
	if unix.Fstat(int(input.Fd()), &a) != nil || unix.Fstat(int(output.Fd()), &b) != nil {
		return false
	}
	return a.Mode&unix.S_IFMT == unix.S_IFCHR && b.Mode&unix.S_IFMT == unix.S_IFCHR && a.Rdev == b.Rdev && terminalEndpoint(int(input.Fd())) && terminalEndpoint(int(output.Fd()))
}
