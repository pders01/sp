//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package sharedmcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestExecutableProductionDeadlineUnderBackendLock(t *testing.T) {
	if testing.Short() {
		t.Skip("production ten-second deadline")
	}
	exe := buildExecutable(t)
	store := t.TempDir()
	lock, err := os.OpenFile(filepath.Join(store, ".sp-privacy.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	s := startSession(t, store, "", exe, "--store="+store, "--read-dates=", "--append-dates="+testDate)
	s.initialized(t)
	start := time.Now()
	s.call(t, 10, true)
	s.call(t, 11, true)
	responseError(t, s.read(t), "BUSY", "not_attempted")
	s.send(t, rpcRequest(12, "ping"))
	if s.read(t)["error"] != nil {
		t.Fatal("locked worker blocked ping")
	}
	s.waitWithin(t, false, operationBudget+3*time.Second)
	if time.Since(start) < operationBudget-time.Second {
		t.Fatal("did not exercise production operation budget")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store, testDate+".json")); !os.IsNotExist(err) {
		t.Fatal("deadline-blocked append published")
	}
	if r := execute(managerAt(t, store), toolCall{append: true, date: testDate, section: "Updates", source: "synthetic", bullets: []string{"fresh"}}); r.IsError {
		t.Fatal("process exit did not release backend locks")
	}
}
