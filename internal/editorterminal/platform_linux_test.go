//go:build linux

package editorterminal

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestTerminalRejectsUnrelatedLinuxPTYMasters(t *testing.T) {
	owned := func() *os.File {
		fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal("owned synthetic PTY master unavailable")
		}
		file := os.NewFile(uintptr(fd), "owned-synthetic-master")
		t.Cleanup(func() { _ = file.Close() })
		return file
	}
	a, b := owned(), owned()
	var left, right unix.Stat_t
	if unix.Fstat(int(a.Fd()), &left) != nil || unix.Fstat(int(b.Fd()), &right) != nil {
		t.Fatal("owned PTY metadata unavailable")
	}
	if left.Rdev != right.Rdev {
		t.Fatal("fixture did not reproduce shared master device identity")
	}
	if sameTerminal(a, b) || CheckTerminal(a, b) != ErrTerminal || terminalEndpoint(int(a.Fd())) {
		t.Fatal("unrelated master endpoints accepted")
	}
}
