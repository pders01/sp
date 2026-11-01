package scratchpad

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestBufferRecoveryIndexPersistsMetadataWithoutOpeningExternalPath(t *testing.T) {
	dir := t.TempDir()
	m, managerErr := NewManagerAt(dir)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	const date = "2026-01-01"
	path := filepath.Join(t.TempDir(), "sp-missing buffer.md")
	if err := m.RecordBufferRecovery(date, path); err != nil {
		t.Fatal(err)
	}
	reopened, managerErr := NewManagerAt(dir)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	got, managerErr := reopened.ListRecoveries()
	want := []RecoveryReference{{Date: date, Path: path}}
	if managerErr != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %+v, %v", got, managerErr)
	}
	if dates, err := reopened.ListDates(); err != nil || len(dates) != 0 {
		t.Fatal("recovery metadata became a daily note")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, recoveryIndexName(date, path)))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("recovery index permissions")
		}
	}
	if err := reopened.ForgetBufferRecovery(date, path); err != nil {
		t.Fatal(err)
	}
	got, managerErr = reopened.ListRecoveries()
	if managerErr != nil || len(got) != 0 {
		t.Fatalf("forgotten references = %+v, %v", got, managerErr)
	}
}

func TestRecoveryDiscoveryIsLockedMetadataOnlyAndReportsPartialErrors(t *testing.T) {
	dir := t.TempDir()
	m, managerErr2 := NewManagerAt(dir)
	if managerErr2 != nil {
		t.Fatal(managerErr2)
	}
	candidate := ".sp-recovery-2026-01-01-" + strings.Repeat("a", 32) + ".age"
	if err := os.WriteFile(filepath.Join(dir, candidate), []byte("not authenticated ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sp-buffer-recovery-broken.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".sp-recovery-2026-01-02-"+strings.Repeat("b", 32)+".age"), 0o700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, ".sp-recovery-2026-01-03-"+strings.Repeat("c", 32)+".age")); err != nil {
			t.Fatal(err)
		}
	}
	got, managerErr2 := m.ListRecoveries()
	if managerErr2 == nil || len(got) != 1 || !got[0].Encrypted || got[0].Date != "2026-01-01" {
		t.Fatalf("discovery = %+v, %v", got, managerErr2)
	}
	if m.IsUnlocked() {
		t.Fatal("listing unlocked the store")
	}
}

func TestRecoveryIndexRejectsMetadataTooLargeToDiscover(t *testing.T) {
	m, err := NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// This path is metadata only; neither recording nor listing opens it.
	// JSON escaping must not produce a record larger than its reader accepts.
	path := filepath.Join(t.TempDir(), strings.Repeat("<", 4000))
	if err := m.RecordBufferRecovery("2026-01-01", path); err == nil {
		t.Fatal("recorded oversized metadata that cannot be discovered")
	}
}

func TestRecoveryIndexRejectsInvalidInputAndNeverDeletesBuffer(t *testing.T) {
	m, managerErr3 := NewManagerAt(t.TempDir())
	if managerErr3 != nil {
		t.Fatal(managerErr3)
	}
	for _, ref := range []RecoveryReference{{Date: "../bad", Path: filepath.Join(t.TempDir(), "sp.md")}, {Date: "2026-01-01", Path: "relative.md"}} {
		if err := m.RecordBufferRecovery(ref.Date, ref.Path); err == nil {
			t.Fatal("accepted invalid reference")
		}
	}
	path := filepath.Join(t.TempDir(), "sp-buffer.md")
	if err := os.WriteFile(path, []byte("synthetic retained draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordBufferRecovery("2026-01-01", path); err != nil {
		t.Fatal(err)
	}
	if err := m.ForgetBufferRecovery("2026-01-01", path); err != nil {
		t.Fatal(err)
	}
	data, managerErr3 := os.ReadFile(path)
	if managerErr3 != nil || string(data) != "synthetic retained draft" {
		t.Fatal("forgetting metadata touched buffer contents")
	}
}
