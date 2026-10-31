package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/editor"
)

func TestViewerDependenciesResolveOnlyForRequestedActions(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": "saved"})
	calls := 0
	resolve := func() (*editor.Editor, error) { calls++; return nil, errors.New("synthetic missing editor") }
	app.nb.SetEditorProvider(resolve, func(string, string, string) error { t.Fatal("failed dependency saved a note"); return nil }, func(string) (string, error) { return "saved", nil })
	app.View()
	app.Update(privacyKey("?"))
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if calls != 0 {
		t.Fatal("browsing resolved editor")
	}
	app.Update(privacyKey("e"))
	if calls != 1 || app.IsQuitting() || app.nb.GetSelectedDate() != "" || !strings.Contains(app.nb.theme.StatusText(), "browsing remains available") {
		t.Fatal("missing editor exited the viewer")
	}
	app.Update(privacyKey("e"))
	if calls != 2 {
		t.Fatal("editing dependency could not be retried")
	}
	app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) { return PrivatePage{}, nil }})
	app.Update(privacyKey("e"))
	if calls != 2 || app.privacy.editor == nil {
		t.Fatal("private editing resolved an external dependency")
	}
}

func TestViewerTemplateResolutionAndHistoryAreActionScoped(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": "saved"})
	calls := 0
	app.SetTemplateResolver(func() error { calls++; return errors.New("command requires explicit trust") })
	app.View()
	if calls != 0 {
		t.Fatal("browsing validated template dependencies")
	}
	app.Update(privacyKey("a"))
	if calls != 1 || app.templateChooser != nil || app.IsQuitting() || !strings.Contains(app.nb.theme.StatusText(), "Templates unavailable") {
		t.Fatal("invalid template config prevented safe browsing")
	}
	app.appliedTemplates["2026-01-01"] = map[string]bool{"one": true}
	app.SetTemplateResolver(func() error {
		app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(_ context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
			return TemplateApplyResult{}, nil
		})
		return nil
	})
	app.Update(privacyKey("a"))
	if app.templateChooser == nil || !app.templateApplied("2026-01-01", "one") {
		t.Fatal("lazy template resolution lost loaded application history")
	}
}

func TestViewerConfigurationWarningsRemainInHelpAfterOtherStatus(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": "saved"})
	app.SetConfigurationNotices([]string{"unknown configuration key ui.typo; ignored"})
	app.SetNotice("Saved")
	app.Update(privacyKey("?"))
	if !strings.Contains(app.nb.keyHelp.content, "ui.typo") || !strings.Contains(app.nb.keyHelp.content, "still in effect") {
		t.Fatal("configuration warning disappeared behind a later status")
	}
}

func TestApplicationAppearanceIsSharedAndAcknowledgesUnchangedResolution(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "dark")
	app := workflowApp(t, ModeCalendar, map[string]string{"2026-01-01": "saved"})
	app.cal.theme.SetPref("dark")
	app.nb.theme.SetPref("dark")
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT}) // dark -> auto, still resolved dark
	if app.cal.theme.Pref() != "auto" || app.nb.theme.Pref() != "auto" || app.nb.theme.Style() != app.cal.theme.Style() || !strings.Contains(app.nb.theme.StatusText(), "session only") {
		t.Fatal("appearance preference/resolution or feedback differed between views")
	}
	app.cal.SetCursor("2026-01-01")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if app.cal.theme.Pref() != "light" || app.nb.theme.Pref() != "light" {
		t.Fatal("notebook theme did not update calendar")
	}
}

func TestNotebookMarkdownCannotConsumeSharedProvenanceBoundary(t *testing.T) {
	const private = "```text\nPRIVATE_CANARY\n\x1b[2J"
	const shared = "# Shared heading\n\n- public contribution"
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": ""})
	app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) {
		return PrivatePage{Private: private, Shared: shared, HasPrivate: true}, nil
	}})
	view := ansi.Strip(app.nb.viewport.View())
	if !strings.Contains(view, "Private notes · unlocked") || !strings.Contains(view, "Agent contributions · shared (self-reported)") || !strings.Contains(view, "public contribution") {
		t.Fatalf("Markdown absorbed UI boundaries: %q", view)
	}
	if strings.Contains(app.nb.viewport.View(), "\x1b[2J") {
		t.Fatal("note controls could erase UI labels")
	}
	for _, cache := range []map[string]string{app.nb.contents, app.cal.contents, app.cal.previews} {
		for _, text := range cache {
			if strings.Contains(text, "PRIVATE_CANARY") {
				t.Fatal("rendering polluted public caches")
			}
		}
	}
	app.Update(privacyKey("e"))
	if app.privacy.editor.input.Value() != private {
		t.Fatal("UI labels or rendering mutations reached the private editor")
	}
}
