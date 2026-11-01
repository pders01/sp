package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestRetainedExternalDraftFollowsDateAcrossViewsAndIsNotOverwritten(t *testing.T) {
	const date = "2026-01-01"
	tmp := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	oldPath := filepath.Join(tmp, "sp-original-failed.md")
	const draft = "\tdraft\r\n笔记\n"
	if err := os.WriteFile(oldPath, []byte(draft), 0o600); err != nil {
		t.Fatal(err)
	}
	app := workflowApp(t, ModeCalendar, map[string]string{date: "base"})
	current := "concurrent saved"
	load := func(string) (string, error) { return current, nil }
	save := func(string, string, string) error { return scratchpad.ErrConflict }
	app.SetTemplates([]DayTemplate{{ID: "test", Name: "Test"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		t.Fatal("comparison launched a template action")
		return TemplateApplyResult{}, nil
	})
	app.cal.SetEditor(&editor.Editor{}, save, load)
	app.nb.SetEditor(&editor.Editor{}, save, load)
	app.cal.finishEdit(editDoneMsg{date: date, original: "base", path: oldPath})
	app.drillToNotebook(date)
	if cmd := app.nb.startEdit(date); cmd == nil || app.nb.review == nil {
		t.Fatal("retained calendar draft was not offered in notebook")
	}
	if app.nb.review.current != current || app.nb.review.draft != draft {
		t.Fatal("comparison did not preserve snapshots")
	}
	app.Update(privacyKey("a"))
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	app.Update(privacyKey("d"))
	if app.templateChooser != nil || app.nb.review == nil {
		t.Fatal("review forwarded an underlying action")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.nb.review != nil || app.IsQuitting() {
		t.Fatal("confirmed draft did not resume editor")
	}
	if data, err := os.ReadFile(oldPath); err != nil || string(data) != draft {
		t.Fatal("resuming changed the retained recovery copy")
	}
	if len(app.recoveries) != 2 {
		t.Fatalf("prepared editor buffer was not tracked: %+v", app.recoveries)
	}
	newPath := app.recoveries[1].Path
	if newPath == oldPath {
		t.Fatal("retry reused the only recovery copy")
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != draft {
		t.Fatal("retry opened saved text instead of retained draft")
	}
	var baseline string
	app.nb.save = func(_ string, original, _ string) error { baseline = original; return nil }
	app.nb.finishEdit(editDoneMsg{date: date, original: current, path: newPath, cleanup: func() { _ = os.Remove(newPath) }})
	if baseline != current || app.cal.contents[date] != draft || len(app.recoveries) != 1 {
		t.Fatal("reviewed save did not use current baseline or sync views")
	}
}

func TestExplicitExternalRecoveryStartsWithReviewAndRefusesPrivateMode(t *testing.T) {
	const date = "2026-01-01"
	path := filepath.Join(t.TempDir(), "sp-old-session.md")
	if err := os.WriteFile(path, []byte("old session draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := workflowApp(t, ModeCalendar, map[string]string{date: "saved"})
	app.nb.SetEditor(&editor.Editor{}, func(string, string, string) error { return nil }, func(string) (string, error) { return "saved", nil })
	if err := app.ReviewExternalRecovery(date, path); err != nil || app.nb.review == nil || app.Mode() != ModeNotebook || app.canPop {
		t.Fatalf("explicit recovery did not start a standalone review: %v", err)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if data, err := os.ReadFile(path); err != nil || string(data) != "old session draft" {
		t.Fatal("closing initial recovery changed the draft")
	}
	app.SetPrivacy(PrivacyHooks{})
	if err := app.ReviewExternalRecovery(date, path); err == nil {
		t.Fatal("private mode accepted an external recovery path")
	}
}

func TestMissingExternalDraftCanReloadSavedTextWithoutDeletingReference(t *testing.T) {
	const date = "2026-01-01"
	tmp := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	app := workflowApp(t, ModeNotebook, map[string]string{date: "saved"})
	app.nb.SetEditor(&editor.Editor{}, func(string, string, string) error { return nil }, func(string) (string, error) { return "saved", nil })
	missing := filepath.Join(tmp, "sp-missing.md")
	app.nb.drafts[date] = editDoneMsg{date: date, original: "base", path: missing}
	app.nb.startEdit(date)
	app.Update(privacyKey("d"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.nb.review == nil {
		t.Fatal("missing draft authorized replacement")
	}
	app.Update(privacyKey("r"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.nb.review != nil || len(app.recoveries) != 1 {
		t.Fatal("saved-text reload failed")
	}
	if data, err := os.ReadFile(app.recoveries[0].Path); err != nil || string(data) != "saved" {
		t.Fatal("reload did not start with saved text")
	}
}

func TestExternalDurabilityRetryUsesPublishedBaselineAndActuallySaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sp-buffer.md")
	if err := os.WriteFile(path, []byte("published"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := NewNotebook([]string{"2026-01-01"})
	defer n.Close()
	n.save = func(string, string, string) error { return scratchpad.ErrDurabilityUnconfirmed }
	n.finishEdit(editDoneMsg{date: "2026-01-01", original: "base", path: path})
	pending := n.drafts["2026-01-01"]
	if pending.original != "published" || !pending.unconfirmed {
		t.Fatal("retry kept superseded baseline")
	}
	calls := 0
	result := completeExternalEdit(pending, func(_ string, original, content string) error {
		calls++
		if original != "published" || content != "published" {
			t.Fatal("wrong durability baseline")
		}
		return nil
	})
	if calls != 1 || result.err != nil || !result.published {
		t.Fatal("unchanged retry skipped durability confirmation")
	}
}
