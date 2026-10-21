//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !windows

package scratchpad

import (
	"errors"
	"os"
)

func lockFile(_ *os.File) error {
	return errors.New("scratchpad locking is not supported on this platform")
}
