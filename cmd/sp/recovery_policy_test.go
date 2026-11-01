package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pders01/sp/internal/templates"
	"github.com/pders01/sp/internal/tui"

	"github.com/pders01/sp/internal/scratchpad"
)

func TestRecoverySavePolicyTracksEnabledAndDisabledStore(t *testing.T) {
	mgr, managerErr := scratchpad.NewManagerAt(t.TempDir())
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	defer mgr.Lock()
	if got, err := recoverySavePolicy(mgr); err != nil || got != "Saving as plaintext" {
		t.Fatalf("legacy policy %q %v", got, err)
	}
	secret := []byte("synthetic recovery policy passphrase")
	defer clear(secret)
	if err := mgr.EnablePrivacy(secret); err != nil {
		t.Fatal(err)
	}
	if got, err := recoverySavePolicy(mgr); err != nil || got != "Saving encrypted" {
		t.Fatalf("enabled policy %q %v", got, err)
	}
	privateSave := makePrivacyHooks(mgr).Save
	privateTemplates := makeTemplateApplier(mgr, []templates.Definition{{ID: "one", Name: "One", File: filepath.Join(t.TempDir(), "must-not-read.md")}}, true)
	if err := mgr.DisablePrivacy(secret); err != nil {
		t.Fatal(err)
	}
	if got, err := recoverySavePolicy(mgr); err != nil || got != "Saving as plaintext" {
		t.Fatalf("disabled policy %q %v", got, err)
	}
	if err := privateSave("2026-01-01", "", "PRIVATE_MUST_NOT_BECOME_PLAINTEXT"); !errors.Is(err, scratchpad.ErrPrivacyPolicyChanged) {
		t.Fatalf("private save followed disabled policy: %v", err)
	}
	if _, err := privateTemplates(context.Background(), "2026-01-01", []tui.TemplateSelection{{ID: "one"}}); !errors.Is(err, scratchpad.ErrPrivacyPolicyChanged) {
		t.Fatalf("private template action ran under changed policy: %v", err)
	}
	note, managerErr := mgr.GetByDate("2026-01-01")
	if managerErr != nil || note.Content != "" {
		t.Fatal("private draft was published as plaintext")
	}
}
