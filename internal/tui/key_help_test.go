package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestViewerHelpOwnsKeysBeforePrivacyAndTemplateShortcuts(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		app := workflowApp(t, mode, map[string]string{"2026-01-01": "shared"})
		loads, unlocks, applies := 0, 0, 0
		app.SetPrivacy(PrivacyHooks{
			Load:   func(string) (PrivatePage, error) { loads++; return PrivatePage{Locked: true, HasPrivate: true}, nil },
			Unlock: func([]byte) error { unlocks++; return nil },
		})
		app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil,
			func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
				applies++
				return TemplateApplyResult{}, nil
			})
		beforeLoads := loads
		beforeDate := app.privateDate()
		app.Update(privacyKey("?"))
		for _, key := range []tea.KeyMsg{privacyKey("u"), privacyKey("a"), privacyKey("e"), privacyKey("d"), rawPhysical(tea.KeyEnter), rawPhysical(tea.KeyDown)} {
			app.Update(key)
		}
		if app.privateDate() != beforeDate || loads != beforeLoads || unlocks != 0 || applies != 0 || app.privacy.prompt != nil || app.privacy.editor != nil || app.templateChooser != nil {
			t.Fatal("help forwarded an action to the underlying view")
		}
		if !strings.Contains(app.View(), "keys") {
			t.Fatal("viewer did not show its key reference")
		}
		app.Update(rawPhysical(tea.KeyEsc))
		if app.Mode() != mode || app.IsQuitting() || app.cal.keyHelp != nil || app.nb.keyHelp != nil {
			t.Fatal("closing help navigated or quit instead of returning to the same view")
		}
	}
}

func TestKeyHelpIsContextualAndLabelsHalfPageScrolling(t *testing.T) {
	cal := NewCalendar(nil)
	defer cal.Close()
	cal.Update(privacyKey("?"))
	if strings.Contains(cal.keyHelp.content, "unlock private") || strings.Contains(cal.keyHelp.content, "choose template") {
		t.Fatal("unavailable capabilities appeared in help")
	}
	cal.Update(rawPhysical(tea.KeyEsc))
	cal.privacyEnabled, cal.templatesAvailable = true, true
	cal.Update(privacyKey("y"))
	cal.Update(rawPhysical(tea.KeyF1))
	if !strings.Contains(cal.keyHelp.content, "open selected month") || strings.Contains(cal.keyHelp.content, "edit selected day") || strings.Contains(cal.keyHelp.content, "choose template") {
		t.Fatal("year help describes month-only actions")
	}
	nb := NewNotebook([]string{"2026-01-01"})
	defer nb.Close()
	nb.Update(rawPhysical(tea.KeyF1))
	for _, want := range []string{"half a page", "full page", "g/G", "newer/older", "no parent view"} {
		if !strings.Contains(nb.keyHelp.content, want) {
			t.Fatalf("notebook help missing %q", want)
		}
	}
}

func TestKeyHelpResizesScrollsAndRetainsFullStatus(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": strings.Repeat("note\n\n", 60)})
	app.nb.viewport.SetYOffset(5)
	status := strings.Repeat("recovery/path/", 50) + " RECOVERY_END"
	app.nb.theme.SetStatus(status, 0)
	app.Update(rawPhysical(tea.KeyF1))
	if !strings.Contains(app.nb.keyHelp.content, status) {
		t.Fatal("expanded help lost the full status/recovery path")
	}
	for _, size := range layoutSizes {
		app.Update(size)
		assertFrameFits(t, app.View(), size)
	}
	app.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	app.Update(privacyKey("G"))
	if !strings.Contains(ansi.Strip(app.View()), "RECOVERY_END") || app.nb.keyHelp.viewport.YOffset == 0 {
		t.Fatal("expanded details cannot be reached by scrolling")
	}
	app.Update(rawPhysical(tea.KeyEsc))
	if app.nb.viewport.YOffset != 5 || app.nb.theme.StatusText() != status {
		t.Fatal("help changed reading position or discarded status details")
	}
}

func TestPrivateEditorHelpDoesNotConsumeLiteralQuestionMarksOrSave(t *testing.T) {
	calls := 0
	e := newPrivateEditor("2026-01-01", "draft", func(_, _, _ string) error { calls++; return nil }, nil)
	e.Update(rawPhysical(tea.KeyCtrlEnd))
	e.Update(privacyKey("?F1"))
	if e.keyHelp != nil || e.input.Value() != "draft?F1" {
		t.Fatal("literal text was interpreted as a help key")
	}
	e.Update(rawPhysical(tea.KeyEsc))
	if !e.confirm {
		t.Fatal("fixture did not arm cancellation")
	}
	e.Update(rawPhysical(tea.KeyF1))
	if e.keyHelp == nil || e.confirm || strings.Contains(e.keyHelp.content, "draft?F1") {
		t.Fatal("help exposed the draft or retained an armed cancellation")
	}
	e.Update(rawPhysical(tea.KeyCtrlS))
	e.Update(privatePasteMsg("paste must stay out of the draft"))
	for _, size := range layoutSizes {
		e.Update(size)
		assertFrameFits(t, e.View(), size)
	}
	e.Update(rawPhysical(tea.KeyEsc))
	if calls != 0 || e.done || e.input.Value() != "draft?F1" {
		t.Fatal("help saved, cancelled, or modified the draft")
	}
	e.Update(rawPhysical(tea.KeyEsc))
	if e.done || !e.confirm {
		t.Fatal("closing help accidentally confirmed cancellation")
	}
}

func TestUnlockHelpNeverEchoesOrSubmitsSecret(t *testing.T) {
	const secret = "SYNTHETIC secret ?"
	app := newTestApp(ModeNotebook)
	defer app.Close()
	calls := 0
	app.SetPrivacy(PrivacyHooks{Unlock: func(value []byte) error {
		calls++
		if string(value) != secret {
			t.Fatal("help changed the passphrase")
		}
		return nil
	}})
	app.Update(privacyKey("u"))
	app.Update(privacyKey(secret))
	app.Update(rawPhysical(tea.KeyF1))
	if app.privacy.keyHelp == nil || strings.Contains(app.privacy.keyHelp.content, secret) {
		t.Fatal("unlock help did not open safely")
	}
	app.Update(rawPhysical(tea.KeyEnter))
	app.Update(privatePasteMsg("paste must stay out of the secret"))
	for _, size := range layoutSizes {
		app.Update(size)
		view := app.View()
		assertFrameFits(t, view, size)
		if strings.Contains(view, "SYNTHETIC") || app.privacy.prompt.Value() != secret {
			t.Fatal("help exposed or changed the secret")
		}
	}
	app.Update(rawPhysical(tea.KeyEsc))
	if calls != 0 || app.privacy.prompt == nil {
		t.Fatal("closing help cancelled or submitted the underlying prompt")
	}
	app.Update(rawPhysical(tea.KeyEnter))
	if calls != 1 || app.privacy.prompt != nil {
		t.Fatal("unlock did not resume after closing help")
	}
}

func TestTemplateHelpPreservesSelectionsAndDescribesPendingActions(t *testing.T) {
	app := workflowApp(t, ModeCalendar, map[string]string{"2026-01-01": ""})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil,
		func(ctx context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
			<-ctx.Done()
			return TemplateApplyResult{}, ctx.Err()
		})
	app.Update(privacyKey("a"))
	app.Update(rawPhysical(tea.KeySpace))
	app.Update(rawPhysical(tea.KeyF1))
	app.Update(rawPhysical(tea.KeyEnter))
	if !app.templateChooser.selected["one"] || app.templateChooser.applying {
		t.Fatal("template help changed selection or started application")
	}
	app.Update(rawPhysical(tea.KeyEsc))
	_, applyCmd := app.Update(rawPhysical(tea.KeyEnter))
	if applyCmd == nil || !app.templateChooser.applying {
		t.Fatal("fixture did not enter pending state")
	}
	app.Update(rawPhysical(tea.KeyF1))
	if app.templateChooser.keyHelp == nil || strings.Contains(app.templateChooser.keyHelp.content, "cancel chooser") {
		t.Fatal("pending help describes a cancellation action that is currently unavailable")
	}
	app.Update(rawPhysical(tea.KeyEsc))
	app.Update(privacyKey("q")) // Cancel the owned operation; Close waits for it.
}
