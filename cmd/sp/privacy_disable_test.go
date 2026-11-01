package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

func TestPrivacyDisableConfirmation(t *testing.T) {
	dir := t.TempDir()
	mgr, managerErr := scratchpad.NewManagerAt(dir)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	defer mgr.Lock()
	const passphrase = "synthetic disable passphrase"
	const date = "2024-01-01"
	if err := mgr.Save(&scratchpad.Scratchpad{Date: date, Content: "private sentinel"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnablePrivacy([]byte(passphrase)); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	before, managerErr := os.ReadFile(filepath.Join(dir, date+".json"))
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	for _, tc := range []struct {
		name, line string
		passwords  []string
	}{
		{"cancel", "yes", nil},
		{"wrong", "DECRYPT ALL NOTES", []string{"wrong"}},
		{"empty", "DECRYPT ALL NOTES", []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prompt := &scriptedPrivacyPrompt{line: tc.line, passwords: tc.passwords}
			var output bytes.Buffer
			if err := disablePrivacy(mgr, prompt, &output); err == nil {
				t.Fatal("disable accepted missing confirmation or invalid passphrase")
			}
			if tc.name == "cancel" && prompt.passwordCalls != 0 {
				t.Fatal("requested passphrase before confirmation")
			}
			after, err := os.ReadFile(filepath.Join(dir, date+".json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed disable modified the note")
			}
			if strings.Contains(output.String(), "private sentinel") || strings.Contains(output.String(), "Privacy disabled.") {
				t.Fatal("failed disable leaked content or claimed completion")
			}
		})
	}
	var output bytes.Buffer
	prompt := &scriptedPrivacyPrompt{line: "DECRYPT ALL NOTES", passwords: []string{passphrase}}
	if err := disablePrivacy(mgr, prompt, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ALL daily private notes", "plaintext", "Shared contributions are preserved", "recovery files", "wrapped key", "Stop ALL sp instances", "Privacy: disabled", "Encrypted dates: 0"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing warning/result %q in %q", want, output.String())
		}
	}
	if strings.Contains(output.String(), passphrase) || strings.Contains(output.String(), "private sentinel") {
		t.Fatal("disable output disclosed a secret")
	}
	mgr.Lock()
	if note, err := mgr.GetByDate(date); err != nil || note.Content != "private sentinel" {
		t.Fatalf("plaintext read = %+v, %v", note, err)
	}
	alreadyDisabled := &scriptedPrivacyPrompt{}
	if err := disablePrivacy(mgr, alreadyDisabled, &output); err != nil {
		t.Fatal(err)
	}
	if alreadyDisabled.lineCalls != 0 || alreadyDisabled.passwordCalls != 0 {
		t.Fatal("idempotent disable prompted for secrets")
	}
	// Re-enabling is explicit and uses the existing passphrase/key.
	path := filepath.Join(dir, "privacy.json")
	retainedData, managerErr := os.ReadFile(path)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	var retained map[string]any
	if err := json.Unmarshal(retainedData, &retained); err != nil {
		t.Fatal(err)
	}
	reenable := &scriptedPrivacyPrompt{line: "ENABLE PRIVACY", passwords: []string{passphrase}}
	if err := enablePrivacy(mgr, reenable, &output); err != nil {
		t.Fatal(err)
	}
	if reenable.lineCalls != 1 || reenable.passwordCalls != 1 || reenable.passwordPrompts[0] != "Existing passphrase: " {
		t.Fatalf("re-enable should authenticate the existing identity once: %+v", reenable)
	}
	// Simulate the durable transition marker left by an interrupted reversal.
	data, managerErr := os.ReadFile(path)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["recipient"] != retained["recipient"] || metadata["wrapped_identity"] != retained["wrapped_identity"] {
		t.Fatal("re-enable rotated or rewrapped the retained identity")
	}
	metadata["version"], metadata["state"] = 2, "decrypting"
	data, managerErr = json.Marshal(metadata)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	output.Reset()
	if err := writePrivacyStatus(mgr, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Privacy: decrypting (incomplete)", "Plaintext dates (decryption in progress)", "Wrapped key: retained", "Normal access is blocked", "sp privacy disable"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("partial status missing %q: %q", want, output.String())
		}
	}
	if strings.Contains(output.String(), "sp privacy enable") {
		t.Fatal("partial reversal suggested enabling instead of resuming decryption")
	}
	if err := disablePrivacy(mgr, &scriptedPrivacyPrompt{line: "DECRYPT ALL NOTES", passwords: []string{passphrase}}, &output); err != nil {
		t.Fatalf("CLI resume = %v", err)
	}
}
