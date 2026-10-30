package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func privacyKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPrivacyCalendarDoesNotLoadPrivateArchive(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	calls := 0
	app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) {
		calls++
		return PrivatePage{Private: "PRIVATE CANARY", Shared: "shared"}, nil
	}})
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if calls != 0 || strings.Contains(app.View(), "PRIVATE CANARY") {
		t.Fatal("calendar loaded or displayed private content")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if calls != 1 || !strings.Contains(app.View(), "PRIVATE CANARY") {
		t.Fatal("opening notebook should load just the active private page")
	}
	if strings.Contains(app.nb.contents[app.nb.GetCurrentPage()], "PRIVATE CANARY") {
		t.Fatal("private projection was copied into shared contents")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	for _, content := range app.cal.contents {
		if strings.Contains(content, "PRIVATE CANARY") {
			t.Fatal("popping notebook copied private text into calendar cache")
		}
	}
}

func TestPrivacyUnlockIsMaskedAndFailedAttemptCleared(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	unlocked := false
	app.SetPrivacy(PrivacyHooks{
		IsUnlocked: func() bool { return unlocked },
		Load: func(string) (PrivatePage, error) {
			return PrivatePage{Locked: !unlocked, HasPrivate: true}, nil
		},
		Unlock: func(secret []byte) error {
			if string(secret) != "test passphrase" {
				return errors.New("incorrect passphrase")
			}
			unlocked = true
			return nil
		},
	})
	app.Update(privacyKey("u"))
	app.Update(privacyKey("wrong secret"))
	if strings.Contains(app.View(), "wrong secret") {
		t.Fatal("unlock prompt echoed the secret")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.privacy.prompt == nil || app.privacy.prompt.Value() != "" {
		t.Fatal("failed unlock should keep an empty prompt")
	}
	app.Update(privacyKey("test passphrase"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !unlocked || app.privacy.prompt != nil {
		t.Fatal("successful unlock did not dismiss prompt")
	}
}

func TestPrivacyEditingUsesOnlyPrivateTextAndNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	app := newTestApp(ModeNotebook)
	defer app.Close()
	var original, saved string
	app.SetPrivacy(PrivacyHooks{
		Load: func(string) (PrivatePage, error) {
			return PrivatePage{Private: "private original", Shared: "PUBLIC BULLET"}, nil
		},
		Save: func(_, before, after string) error {
			original, saved = before, after
			return nil
		},
	})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.privacy.editor == nil || app.privacy.editor.input.Value() != "private original" {
		t.Fatal("private editor was not seeded with private-only content")
	}
	app.privacy.editor.input.SetValue("private edited")
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if original != "private original" || saved != "private edited" || app.privacy.editor != nil {
		t.Fatalf("save baseline/content = %q, %q", original, saved)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("private editing created temporary files: %v, %v", files, err)
	}
}

func TestPrivateEditorFailedRecoveryRetainsUnsavedText(t *testing.T) {
	e := newPrivateEditor("2026-10-30", "original", func(_, _, _ string) error {
		return errors.New("conflict")
	}, func(_, _ string) (string, error) {
		return "", errors.New("disk full")
	})
	e.input.SetValue("unsaved private")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.done || e.input.Value() != "unsaved private" || !strings.Contains(e.status, "recovery failed") {
		t.Fatal("failed save/recovery discarded unsaved content")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.done || !strings.Contains(e.status, "Text remains") {
		t.Fatal("cancellation discarded content after failed recovery")
	}
}

func TestPrivateEditorCancelUsesEncryptedRecovery(t *testing.T) {
	recovered := ""
	e := newPrivateEditor("2026-10-30", "original", nil, func(date, content string) (string, error) {
		if date != "2026-10-30" {
			t.Fatal("wrong recovery date")
		}
		recovered = content
		return "/synthetic/recovery.age", nil
	})
	e.input.SetValue("unsaved private")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if e.done {
		t.Fatal("first cancellation should require confirmation")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !e.done || recovered != "unsaved private" || e.err == nil || !strings.Contains(e.err.Error(), "recovery.age") {
		t.Fatal("confirmed cancellation did not preserve encrypted recovery")
	}
}

func TestPrivacyTemplateDoesNotPopulateSharedCaches(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{
		Load: func(string) (PrivatePage, error) {
			return PrivatePage{Private: "PRIVATE TEMPLATE", Shared: "shared"}, nil
		},
	})
	app.appliedTemplates = make(map[string]map[string]bool)
	app.finishTemplateApply(templateAppliedMsg{
		date:   app.nb.GetCurrentPage(),
		result: TemplateApplyResult{Content: "PRIVATE TEMPLATE", Applied: []string{"test"}},
	})
	if strings.Contains(app.nb.contents[app.nb.GetCurrentPage()], "PRIVATE TEMPLATE") {
		t.Fatal("private template copied to shared notebook cache")
	}
	for _, content := range app.cal.contents {
		if strings.Contains(content, "PRIVATE TEMPLATE") {
			t.Fatal("private template copied to calendar")
		}
	}
}

func TestLegacySharedProjectionDoesNotChangeEditablePrivateText(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	date := app.nb.GetCurrentPage()
	app.nb.SetContents(map[string]string{date: "legacy private text"})
	app.SetSharedContents(map[string]string{date: "shared contribution"})
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !strings.Contains(app.View(), "shared contribution") || !strings.Contains(app.View(), "legacy private text") {
		t.Fatal("legacy notebook did not project both areas")
	}
	if _, content := app.nb.CurrentContent(); content != "legacy private text" {
		t.Fatal("shared projection changed private editing baseline")
	}
}

func TestPrivacyLoadFailureDoesNotShowPriorPage(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	fail := false
	app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) {
		if fail {
			return PrivatePage{}, errors.New("damaged file")
		}
		return PrivatePage{Private: "PRIVATE CANARY"}, nil
	}})
	fail = true
	app.refreshPrivatePage()
	if strings.Contains(app.View(), "PRIVATE CANARY") {
		t.Fatal("load failure retained decrypted projection")
	}
}
