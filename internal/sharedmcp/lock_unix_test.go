//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package sharedmcp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRealBackendBlockingLocksAreProcessOwned(t *testing.T) {
	for _, lockName := range []string{".sp-privacy.lock", ".sp-" + testDate + ".lock"} {
		for _, action := range []string{"cancel", "eof", "deadline"} {
			t.Run(lockName+"/"+action, func(t *testing.T) {
				store := t.TempDir()
				before := fixture(t, store, testDate+".json", sharedFixture("existing\r\n"))
				lock, err := os.OpenFile(filepath.Join(store, lockName), os.O_CREATE|os.O_RDWR, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
					t.Fatal(err)
				}
				s := startSession(t, store, "real-lock", "")
				s.initialized(t)
				s.call(t, 10, true)
				s.waitStarted(t)
				s.call(t, 11, true)
				responseError(t, s.read(t), "BUSY", "not_attempted")
				switch action {
				case "cancel":
					s.send(t, map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 10}})
					responseError(t, s.read(t), "APPEND_UNCONFIRMED", "unknown")
				case "eof":
					_ = s.in.Close()
				}
				s.wait(t, false) // Held backend lock cannot finish: child MUST exit.
				if err = unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(filepath.Join(store, testDate+".json"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Fatal("lock-blocked call changed record")
				}
				// After exit, no storage goroutine can resume on unlock. A new
				// synthetic manager can acquire both released process-owned locks.
				r := execute(managerAt(t, store), toolCall{append: true, date: testDate, section: "Updates", source: "synthetic", bullets: []string{"fresh"}})
				if r.IsError {
					t.Fatal("shutdown left lock unavailable")
				}
				page := execute(managerAt(t, store), toolCall{date: testDate}).Structured.(readResult)
				if page.Content != "existing\r\n\n### Updates\n\n- fresh\n" {
					t.Fatalf("unexpected publication after exit: %q", page.Content)
				}
			})
		}
	}
}
