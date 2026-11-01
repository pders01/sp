package scratchpad

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pders01/sp/internal/templates"
)

func TestReviewedRecoveryPolicyIsCheckedForMissingDates(t *testing.T) {
	m, managerErr := NewManagerAt(t.TempDir())
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	defer m.Lock()
	note := &Scratchpad{Date: "2026-01-01", Content: "synthetic recovered draft", Created: time.Now()}
	secret := []byte("synthetic save policy passphrase")
	defer clear(secret)
	if err := m.EnablePrivacy(secret); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveWithPrivacyPolicy(note, false); !errors.Is(err, ErrPrivacyPolicyChanged) {
		t.Fatalf("changed policy = %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.storageDir, note.Date+".json")); !os.IsNotExist(err) {
		t.Fatal("policy mismatch created a note")
	}
	if err := m.SaveWithPrivacyPolicy(note, true); err != nil {
		t.Fatal(err)
	}
	if err := m.DisablePrivacy(secret); err != nil {
		t.Fatal(err)
	}
	current, managerErr := m.GetByDate(note.Date)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	current.Content = "must not replace plaintext"
	if err := m.SaveWithPrivacyPolicy(current, true); !errors.Is(err, ErrPrivacyPolicyChanged) {
		t.Fatalf("disabled policy = %v", err)
	}
	if _, err := m.ApplyTemplateSectionsWithPrivacyPolicy("2026-01-02", []templates.Section{{ID: "one", Title: "One", Body: "must remain private"}}, true); !errors.Is(err, ErrPrivacyPolicyChanged) {
		t.Fatalf("template policy changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.storageDir, "2026-01-02.json")); !os.IsNotExist(err) {
		t.Fatal("template policy mismatch created plaintext date")
	}
	loaded, managerErr := m.GetByDate(note.Date)
	if managerErr != nil || loaded.Content != "synthetic recovered draft" {
		t.Fatal("policy mismatch replaced current note")
	}
}
