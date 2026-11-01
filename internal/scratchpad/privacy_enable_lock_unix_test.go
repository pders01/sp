//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package scratchpad

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEnablePrivacyDurabilityAndCompletionHoldStoreLock(t *testing.T) {
	m := privateTestManager(t)
	defer m.Lock()
	const date = "2024-01-01"
	writeJSON(t, filepath.Join(m.storageDir, date+".json"), legacyDocument{Date: date, Content: "synthetic migration note"})
	checkLock := func(name string, wantHeld bool) {
		t.Helper()
		file, err := os.OpenFile(filepath.Join(m.storageDir, ".sp-"+name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
			if wantHeld {
				t.Fatalf("%s lock was not held", name)
			}
		} else if !wantHeld || !errors.Is(err, unix.EWOULDBLOCK) {
			t.Fatalf("check %s lock: %v (want held = %v)", name, err, wantHeld)
		}
	}
	calls := 0
	complete := false
	err := m.enablePrivacyWithProgress(fixturePassphrase, func(dir string) error {
		calls++
		checkLock("privacy", true)
		if calls == 3 { // metadata confirmation, note publication, final confirmation
			checkLock(date, false)
		}
		return syncDirectory(dir)
	}, func(p PrivacyProgress) {
		if p.Stage == "complete" {
			complete = true
			if calls != 3 {
				t.Fatalf("completion preceded durability confirmation: %d sync calls", calls)
			}
			checkLock("privacy", true)
			checkLock(date, false)
		}
	})
	if err != nil || calls != 3 || !complete {
		t.Fatalf("enable = %v, sync calls = %d, complete = %v", err, calls, complete)
	}
	checkLock("privacy", false)
}
