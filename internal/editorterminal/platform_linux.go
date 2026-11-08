//go:build linux

package editorterminal

import (
	"errors"
	"golang.org/x/sys/unix"
)

func terminalEndpoint(fd int) bool {
	// Different ptmx masters can have the same Rdev. Only slave/ordinary tty
	// endpoints are supported. An unexpected ioctl error fails closed, without
	// another endpoint, mutation, or fallback after a denial.
	_, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	return errors.Is(err, unix.ENOTTY)
}
