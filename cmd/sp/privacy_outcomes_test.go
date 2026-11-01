package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

func TestPrivacyStatusBoundedAndOptInDates(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scratchpad.NewManagerAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Lock()
	for day := 1; day <= 28; day++ {
		if err := mgr.Save(&scratchpad.Scratchpad{Date: fmt.Sprintf("2025-02-%02d", day), Content: "synthetic private status sentinel"}); err != nil {
			t.Fatal(err)
		}
	}
	factory := func() (*scratchpad.Manager, error) { return mgr, nil }
	for _, retained := range []bool{false, true} {
		if retained {
			pass := []byte("synthetic retained status passphrase")
			if err := mgr.EnablePrivacy(pass); err != nil {
				t.Fatal(err)
			}
			if err := mgr.DisablePrivacy(pass); err != nil {
				t.Fatal(err)
			}
			mgr.Lock()
		}
		output, diagnostic, err := testCLI(factory, "privacy", "status")
		if err != nil || diagnostic != "" {
			t.Fatalf("status = %q, %v", diagnostic, err)
		}
		if strings.Contains(output, "2025-02-") || strings.Contains(output, "Remaining plaintext") || strings.Contains(output, "Migration is incomplete") || strings.Contains(output, "sentinel") {
			t.Fatalf("disabled status not bounded/policy-aware: %q", output)
		}
		if !strings.Contains(output, "Plaintext dates (normal; privacy disabled): 28") || len(strings.Split(output, "\n")) > 9 {
			t.Fatalf("unexpected summary: %q", output)
		}
		if retained && (!strings.Contains(output, "Wrapped key: retained") || !strings.Contains(output, "keep privacy.json and the existing passphrase") || !strings.Contains(output, "encrypted backups and recovery files")) {
			t.Fatalf("missing retained-key obligation: %q", output)
		}
		listed, _, err := testCLI(factory, "privacy", "status", "--dates")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(listed, "  2025-02-") != 28 || strings.Contains(listed, "sentinel") {
			t.Fatalf("date listing = %q", listed)
		}
	}
}

func TestPrivacyEnableRetainedIdentityWrongPassphraseAndDecrypting(t *testing.T) {
	dir := t.TempDir()
	mgr, managerErr := scratchpad.NewManagerAt(dir)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	defer mgr.Lock()
	pass := []byte("synthetic retained identity passphrase")
	if err := mgr.EnablePrivacy(pass); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DisablePrivacy(pass); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	path := filepath.Join(dir, "privacy.json")
	before, managerErr := os.ReadFile(path)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	prompt := &scriptedPrivacyPrompt{line: "ENABLE PRIVACY", passwords: []string{"synthetic wrong passphrase"}}
	var output bytes.Buffer
	if err := enablePrivacy(mgr, prompt, &output); err == nil {
		t.Fatal("wrong existing passphrase accepted")
	}
	after, managerErr := os.ReadFile(path)
	if managerErr != nil || !bytes.Equal(before, after) {
		t.Fatal("wrong passphrase changed retained identity")
	}
	if prompt.passwordCalls != 1 || prompt.passwordPrompts[0] != "Existing passphrase: " || strings.Contains(output.String(), "Privacy enabled.") {
		t.Fatalf("wrong-passphrase flow: %+v, %q", prompt, output.String())
	}

	// Re-enable cannot reverse an incomplete decryption or start key creation.
	var metadata map[string]any
	if err := json.Unmarshal(before, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata["version"], metadata["state"] = 2, "decrypting"
	data, managerErr := json.Marshal(metadata)
	if managerErr != nil {
		t.Fatal(managerErr)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	prompt = &scriptedPrivacyPrompt{}
	output.Reset()
	if err := enablePrivacy(mgr, prompt, &output); !errors.Is(err, scratchpad.ErrPrivacyDecrypting) {
		t.Fatalf("decrypting enable = %v", err)
	}
	if prompt.passwordCalls != 0 || prompt.lineCalls != 0 || output.Len() != 0 {
		t.Fatal("incomplete decryption entered enable/setup flow")
	}
}

func TestSharedJSONOutcomesAndRawDefaults(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Lock()
	factory := func() (*scratchpad.Manager, error) { return mgr, nil }
	for _, date := range []string{"2025-01-02", "2025-01-03"} {
		if date == "2025-01-03" {
			if err := mgr.Save(&scratchpad.Scratchpad{Date: date, Content: "synthetic private JSON sentinel", AppliedTemplates: []string{"private-template-sentinel"}}); err != nil {
				t.Fatal(err)
			}
		}
		output, diagnostic, operationErr := testCLI(factory, "shared", date, "--json")
		if operationErr != nil || diagnostic != "" {
			t.Fatalf("JSON read = %q, %v", diagnostic, operationErr)
		}
		var result struct{ Date, Status, Content string }
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		want := "missing"
		if date == "2025-01-03" {
			want = "no_shared"
		}
		if result.Date != date || result.Status != want || result.Content != "" || strings.Contains(output, "sentinel") {
			t.Fatalf("shared result = %q", output)
		}
		raw, diagnostic, operationErr := testCLI(factory, "shared", date)
		if operationErr != nil || raw != "" || diagnostic != "" {
			t.Fatalf("raw read = %q, %q, %v", raw, diagnostic, operationErr)
		}
	}
	if err := mgr.EnablePrivacy([]byte("synthetic JSON outcomes passphrase")); err != nil {
		t.Fatal(err)
	}
	mgr.Lock()
	for i, bullets := range [][]string{{"- one", "two", "one"}, {"one", "- two"}, {"two", "three", "three"}} {
		args := append([]string{"append", "2025-01-03", "--json", "--section", "Updates", "--"}, bullets...)
		output, diagnostic, err := testCLI(factory, args...)
		if err != nil || diagnostic != "" {
			t.Fatalf("append JSON = %q, %v", diagnostic, err)
		}
		var result scratchpad.AppendResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(err)
		}
		wantAdded := []string{"one,two", "", "three"}[i]
		wantDup := []string{"one", "one,two", "two,three"}[i]
		if strings.Join(result.Added, ",") != wantAdded || strings.Join(result.Duplicates, ",") != wantDup || result.Source != "sp" || result.Section != "Updates" || result.Date != "2025-01-03" {
			t.Fatalf("append result = %q", output)
		}
		if strings.Contains(output, "sentinel") || strings.Contains(output, ":null") || mgr.IsUnlocked() {
			t.Fatalf("unsafe result = %q", output)
		}
	}
	output, diagnostic, err := testCLI(factory, "append", "2025-01-03", "--section", "Updates", "--", "one")
	if err != nil || output != "" || diagnostic != "" {
		t.Fatalf("quiet duplicate = %q, %q, %v", output, diagnostic, err)
	}
	raw, _, err := testCLI(factory, "shared", "2025-01-03")
	if err != nil {
		t.Fatal(err)
	}
	output, diagnostic, err = testCLI(factory, "shared", "2025-01-03", "--json")
	var result struct{ Date, Status, Content string }
	if err != nil || diagnostic != "" {
		t.Fatalf("shared JSON = %q, %v", diagnostic, err)
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || result.Content != raw || strings.Contains(output, "sentinel") || mgr.IsUnlocked() {
		t.Fatalf("unsafe shared result = %q", output)
	}
}

func TestSharedJSONValidationBeforeStorage(t *testing.T) {
	factory := func() (*scratchpad.Manager, error) {
		t.Fatal("invalid shared operation opened storage")
		return nil, errors.New("unexpected storage")
	}
	cases := [][]string{
		{"shared", "2025-02-30", "--json"},
		{"shared", "../invalid", "--json"},
		{"shared", "2025-01-02", "--json=invalid"},
		{"shared", "2025-01-02", "extra", "--json"},
		{"append", "2025-01-02", "--json", "--section", "Updates", "--source", "bad\nsource", "--", "one"},
		{"append", "2025-01-02", "--json", "--section", "# injection", "--", "one"},
		{"append", "2025-01-02", "--json=invalid", "--section", "Updates", "--", "one"},
		{"append", "2025-01-02", "--json", "--section", "Updates", "--", "one\nprivate"},
		{"append", "2025-01-02", "--json", "--section", "Updates", "--"},
		{"append", "2025-01-02", "--json", "--section", "Updates", "--private", "--", "one"},
	}
	for _, args := range cases {
		output, diagnostic, err := testCLI(factory, args...)
		if err == nil || output != "" || diagnostic != "" {
			t.Fatalf("invalid %q = %q, %q, %v", args, output, diagnostic, err)
		}
	}
}
