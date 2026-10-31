package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

func TestRecoveryListDoesNotNeedTerminalOrUnlock(t *testing.T) {
	mgr, err := scratchpad.NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sp-stale.md")
	if err := mgr.RecordBufferRecovery("2026-01-01", path); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCommand(func() (*scratchpad.Manager, error) { return mgr, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"privacy", "recover", "--list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sp-stale.md") || !strings.Contains(out.String(), "may be stale") || strings.Contains(out.String(), "Passphrase") {
		t.Fatalf("unexpected listing: %q", out.String())
	}
}

func TestPlaintextRecoveryRejectsInvalidModesBeforeOpeningStore(t *testing.T) {
	for _, args := range [][]string{
		{"privacy", "recover", "--plaintext-date", "bad", "file.md"},
		{"privacy", "recover", "--plaintext-date=", "file.md"},
		{"privacy", "recover", "--list", "--plaintext-date", "2026-01-01"},
	} {
		cmd := newRootCommand(func() (*scratchpad.Manager, error) {
			t.Fatal("invalid recovery arguments opened storage")
			return nil, nil
		})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted invalid recovery arguments %q", args)
		}
	}
}

func TestPlaintextRecoveryRefusesPrivacyBeforeResolvingEditorOrReadingBuffer(t *testing.T) {
	mgr, err := scratchpad.NewManagerAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Lock()
	if err := mgr.EnablePrivacy([]byte("synthetic plaintext-recovery guard passphrase")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "nonexistent-must-not-be-resolved-editor")
	cmd := newRootCommand(func() (*scratchpad.Manager, error) { return mgr, nil })
	cmd.SetArgs([]string{"privacy", "recover", "--plaintext-date", "2026-01-01", filepath.Join(t.TempDir(), "missing.md")})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "privacy is enabled") {
		t.Fatalf("privacy guard = %v", err)
	}
}

func TestRecoveryListRejectsPathArgumentBeforeOpeningStore(t *testing.T) {
	cmd := newRootCommand(func() (*scratchpad.Manager, error) { t.Fatal("invalid listing opened store"); return nil, nil })
	cmd.SetArgs([]string{"privacy", "recover", "--list", "file.age"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("accepted ambiguous listing/review")
	}
}
