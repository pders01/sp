package scratchpad

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRecoveryRequiresUnlockAndPreservesCurrentNote(t *testing.T) {
	m := privateTestManager(t)
	const date = "2026-10-01"
	if err := m.Save(&Scratchpad{Date: date, Content: "current note"}); err != nil {
		t.Fatal(err)
	}
	path, err := m.SealRecovery(date, "recovered text")
	if err != nil {
		t.Fatal(err)
	}
	m.Lock()
	if _, readErr := m.ReadRecovery(path); !errors.Is(readErr, ErrLocked) {
		t.Fatalf("locked recovery read = %v", readErr)
	}
	if unlockErr := m.Unlock(fixturePassphrase); unlockErr != nil {
		t.Fatal(unlockErr)
	}
	for _, argument := range []string{path, filepath.Base(path)} {
		recovery, readErr := m.ReadRecovery(argument)
		if readErr != nil || recovery.Date != date || recovery.Content != "recovered text" {
			t.Fatalf("recovery round trip = %+v, %v", recovery, readErr)
		}
	}
	current, err := m.GetByDate(date)
	if err != nil || current.Content != "current note" {
		t.Fatal("reading recovery replaced the current note")
	}
}

func TestReadRecoveryRejectsRenamedDateOutsidePathAndCorruption(t *testing.T) {
	m := privateTestManager(t)
	path, err := m.SealRecovery("2026-10-01", "recovered text")
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)
	if _, err := m.ReadRecovery(filepath.Join(t.TempDir(), name)); err == nil {
		t.Fatal("accepted path outside note store")
	}
	wrongDate := strings.Replace(path, "2026-10-01", "2026-10-02", 1)
	if err := os.WriteFile(wrongDate, readTestFile(t, path), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadRecovery(wrongDate); err == nil {
		t.Fatal("accepted swapped recovery date")
	}
	data := readTestFile(t, path)
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadRecovery(path); err == nil {
		t.Fatal("accepted corrupted recovery ciphertext")
	}
}
