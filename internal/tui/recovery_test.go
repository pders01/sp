package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestRecoveryReportSurvivesStatusChangesAndQuit(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	date := app.nb.GetCurrentPage()
	path := filepath.Join(t.TempDir(), "sp-failed.md")
	if err := os.WriteFile(path, []byte("PRIVATE_DRAFT_CANARY"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorded := 0
	app.SetRecoveryRecorder(func(d, p string) error { recorded++; return errors.New("synthetic index failure") }, nil)
	app.nb.save = func(string, string, string) error { return scratchpad.ErrConflict }
	app.Update(editDoneMsg{date: date, original: "original", path: path})
	app.nb.theme.SetStatus("unrelated transient message", 0)
	app.Update(privacyKey("q"))
	var out bytes.Buffer
	app.ReportRecoveries(&out)
	if recorded != 1 || !strings.Contains(out.String(), path) || !strings.Contains(out.String(), "could not index") || strings.Contains(out.String(), "PRIVATE_DRAFT_CANARY") {
		t.Fatalf("lost or unsafe recovery report: %q", out.String())
	}
}

func TestPrivateRecoveryReportTracksPublishedButUnconfirmedPaths(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) { return PrivatePage{}, nil }, Recover: func(string, string) (string, error) {
		return "/synthetic/recovery.age", scratchpad.ErrDurabilityUnconfirmed
	}})
	app.Update(privacyKey("e"))
	app.Update(privacyKey("PRIVATE_DRAFT_CANARY"))
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	var out bytes.Buffer
	app.ReportRecoveries(&out)
	if !strings.Contains(out.String(), "/synthetic/recovery.age") || strings.Contains(out.String(), "PRIVATE_DRAFT_CANARY") {
		t.Fatalf("unsafe/missing report: %q", out.String())
	}
	if app.privacy.editor == nil || app.privacy.editor.input.Value() != "PRIVATE_DRAFT_CANARY" {
		t.Fatal("failed recovery discarded draft")
	}
}

func TestRecoveryIndexWarningsRetryAndClearWhenBufferIsConsumed(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	calls := 0
	app.SetRecoveryRecorder(func(string, string) error {
		calls++
		if calls == 1 {
			return errors.New("transient index error")
		}
		return nil
	}, func(string, string) error { return nil })
	ref := scratchpad.RecoveryReference{Date: "2026-01-01", Path: "/synthetic/sp-buffer.md"}
	app.trackRecovery(ref)
	app.trackRecovery(ref)
	if calls != 2 || len(app.recoveries) != 1 || len(app.recoveryWarnings) != 0 {
		t.Fatal("index retry duplicated artifact or retained an obsolete warning")
	}
	app.consumedRecovery(ref.Date, ref.Path)
	var out bytes.Buffer
	app.ReportRecoveries(&out)
	if out.Len() != 0 {
		t.Fatalf("consumed buffer was reported as retained: %q", out.String())
	}
}

func TestRecoveryReportQuotesControlCharacters(t *testing.T) {
	var out bytes.Buffer
	WriteRecoveryReport(&out, []scratchpad.RecoveryReference{{Date: "2026-01-01", Path: "/synthetic/\x1b[31m\nfile", Encrypted: true}})
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), `\x1b`) || !strings.Contains(out.String(), "--list") {
		t.Fatalf("unsafe recovery path: %q", out.String())
	}
}
