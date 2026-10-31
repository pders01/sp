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

func TestRecoveryListRejectsPathArgumentBeforeOpeningStore(t *testing.T) {
	cmd := newRootCommand(func() (*scratchpad.Manager, error) { t.Fatal("invalid listing opened store"); return nil, nil })
	cmd.SetArgs([]string{"privacy", "recover", "--list", "file.age"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("accepted ambiguous listing/review")
	}
}
