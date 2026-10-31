package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func jumpToDate(t *testing.T, app *App, date string) {
	t.Helper()
	app.Update(privacyKey("d"))
	if app.dateJump == nil {
		t.Fatal("date prompt did not open")
	}
	app.Update(privatePasteMsg(date))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

func TestDateJumpRejectsInvalidAndNoncanonicalInputWithoutNavigation(t *testing.T) {
	for _, value := range []string{"2025-02-29", "2026-02-30", "2026-2-03", "2026/02/03", " 2026-02-03", "2026-02-03\n", "２０２６-02-03", "enter", "q", strings.Repeat("1", 100000)} {
		app := workflowApp(t, ModeCalendar, map[string]string{"2026-01-01": "saved"})
		before := app.cal.CursorDate()
		jumpToDate(t, app, value)
		if app.dateJump == nil || app.dateJump.err == "" || app.cal.CursorDate() != before || app.IsQuitting() || len(app.cal.hasData) != 1 {
			t.Fatalf("invalid date %q changed state or lacked feedback", value[:min(len(value), 40)])
		}
	}
}

func TestDateJumpCalendarSelectsExactDateAndResetsPeriodPreference(t *testing.T) {
	app := workflowApp(t, ModeCalendar, nil)
	app.cal.SetCursor("2026-01-31")
	app.cal.preferredDay = 31
	app.Update(privacyKey("y"))
	jumpToDate(t, app, "2024-02-29")
	if app.dateJump != nil || app.Mode() != ModeCalendar || app.cal.CursorDate() != "2024-02-29" || app.cal.view != ViewMonth || app.cal.preferredDay != 0 || len(app.cal.hasData) != 0 || len(app.nb.pages) != 0 {
		t.Fatal("calendar jump did not focus exact date or manufactured an entry")
	}
}

func TestDateJumpNotebookRestoresPositionAndAllowsUnsavedDates(t *testing.T) {
	const first, second = "2026-02-02", "2026-02-01"
	app := workflowApp(t, ModeNotebook, map[string]string{first: strings.Repeat("first\n\n", 120), second: strings.Repeat("second\n\n", 120)})
	app.nb.viewport.SetYOffset(12)
	jumpToDate(t, app, second)
	app.nb.viewport.SetYOffset(22)
	jumpToDate(t, app, first)
	if app.nb.viewport.YOffset != 12 {
		t.Fatal("date jump lost the first page's reading position")
	}
	jumpToDate(t, app, second)
	if app.nb.viewport.YOffset != 22 {
		t.Fatal("date jump lost the second page's reading position")
	}
	jumpToDate(t, app, "2099-12-31")
	if app.nb.GetCurrentPage() != "2099-12-31" || app.cal.HasData("2099-12-31") || app.IsQuitting() {
		t.Fatal("unsaved date could not be selected or appeared saved")
	}
}

func TestDateJumpOnlyLoadsSelectedPrivatePageAndDoesNotMaskLoadError(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		app := workflowApp(t, mode, map[string]string{"2026-01-01": "shared"})
		var dates []string
		fail := false
		app.SetPrivacy(PrivacyHooks{Load: func(date string) (PrivatePage, error) {
			dates = append(dates, date)
			if fail {
				return PrivatePage{}, errors.New("selected page unavailable")
			}
			return PrivatePage{Private: "PRIVATE_CANARY", HasPrivate: true}, nil
		}})
		dates = nil
		app.Update(privacyKey("d"))
		app.Update(privatePasteMsg("2024-02-29"))
		if len(dates) != 0 {
			t.Fatal("typing a date loaded private history")
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if mode == ModeCalendar {
			if len(dates) != 0 {
				t.Fatal("calendar date jump decrypted a private note")
			}
		} else {
			if len(dates) != 1 || dates[0] != "2024-02-29" {
				t.Fatalf("date jump loaded extra private pages: %v", dates)
			}
			fail = true
			jumpToDate(t, app, "2024-03-01")
			if !strings.Contains(app.nb.theme.StatusText(), "selected page unavailable") || strings.Contains(app.View(), "PRIVATE_CANARY") {
				t.Fatal("date selection hid a load error or left stale private text")
			}
		}
		for _, cache := range []map[string]string{app.nb.contents, app.cal.contents, app.cal.previews} {
			for _, text := range cache {
				if strings.Contains(text, "PRIVATE_CANARY") {
					t.Fatal("private jump text entered public caches")
				}
			}
		}
	}
}

func TestDatePromptOwnsActionsAndRequiresPhysicalConfirmation(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": "saved"})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		t.Fatal("date prompt applied a template")
		return TemplateApplyResult{}, nil
	})
	app.Update(privacyKey("d"))
	for _, text := range []string{"a", "e", "u", "q", "ctrl+c", "enter"} {
		app.Update(privacyKey(text))
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if app.dateJump == nil || app.templateChooser != nil || app.IsQuitting() || app.nb.GetSelectedDate() != "" {
		t.Fatal("date prompt forwarded an underlying action")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	app.Update(privatePasteMsg("2024-02-29"))
	app.Update(privatePasteMsg("\n"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.dateJump == nil || app.dateJump.input.Value() != "2024-02-29" {
		t.Fatal("rejected paste authorized an older candidate")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app.Update(privatePasteMsg("2026-01-01"))
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.dateJump == nil || app.dateJump.input.Value() != "2024-02-29" {
		t.Fatal("help forwarded Enter/paste or discarded input")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	app.Update(privatePasteMsg("2024-02-29"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.dateJump != nil || app.nb.GetCurrentPage() != "2024-02-29" {
		t.Fatal("valid deliberate submission did not navigate")
	}
}

func TestDatePromptEditingResizeAndCancelPreserveUnderlyingPosition(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": strings.Repeat("line\n\n", 120)})
	app.nb.viewport.SetYOffset(15)
	app.Update(privacyKey("d"))
	app.Update(privatePasteMsg("2024-02-28"))
	app.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	app.Update(privacyKey("9"))
	for _, size := range layoutSizes {
		app.Update(size)
		assertFrameFits(t, app.View(), size)
		if app.dateJump.input.Value() != "2024-02-29" {
			t.Fatal("resize changed entered date")
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if app.dateJump != nil || app.IsQuitting() || app.nb.GetCurrentPage() != "2026-01-01" || app.nb.viewport.YOffset != 15 {
		t.Fatal("cancel changed view/position or quit the app")
	}
}

func TestDateShortcutIsLiteralInPrivateInputAndOwnedByOtherModals(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	unlocked := false
	app.SetPrivacy(PrivacyHooks{IsUnlocked: func() bool { return unlocked }, Load: func(string) (PrivatePage, error) {
		return PrivatePage{Private: "base", Locked: !unlocked, HasPrivate: true}, nil
	}})
	app.Update(privacyKey("u"))
	app.Update(privacyKey("d"))
	if app.dateJump != nil || app.privacy.prompt.Value() != "d" {
		t.Fatal("date shortcut captured unlock input")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	unlocked = true
	app.Update(privacyKey("e"))
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlEnd})
	app.Update(privacyKey("d"))
	if app.dateJump != nil || app.privacy.editor.input.Value() != "based" {
		t.Fatal("date shortcut captured editor input")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	app.Update(privacyKey("d"))
	if app.dateJump != nil {
		t.Fatal("date shortcut captured editor help")
	}
}

func TestDateJumpDoesNotStealTemplateOrRecoveryReviewKeys(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": "saved"})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		return TemplateApplyResult{}, nil
	})
	app.Update(privacyKey("a"))
	app.Update(privacyKey("d"))
	if app.dateJump != nil || app.templateChooser == nil {
		t.Fatal("date shortcut stole a chooser key")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app.nb.review = &externalDraftReview{draftReview: newDraftReview("2026-01-01", "saved", "draft", 80, 24)}
	app.Update(privacyKey("d"))
	if app.dateJump != nil || app.nb.review.selected != 'd' {
		t.Fatal("date shortcut stole a recovery selection")
	}
}

func TestDateJumpCancelPreservesYearLayer(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.Update(privacyKey("y"))
	app.Update(privacyKey("d"))
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.dateJump == nil || app.dateJump.keyHelp != nil || app.cal.view != ViewYear {
		t.Fatal("help Escape closed the date prompt or year layer")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.dateJump != nil || app.cal.view != ViewYear || app.IsQuitting() {
		t.Fatal("date cancellation navigated out of year view")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.cal.view != ViewMonth || app.IsQuitting() {
		t.Fatal("year back navigation failed after date cancellation")
	}
}

func TestDateJumpOpensAndCancelsWithoutChangingView(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		app := workflowApp(t, mode, map[string]string{"2026-02-14": "saved"})
		before := app.privateDate()
		app.Update(privacyKey("d"))
		if !strings.Contains(app.View(), "Jump to date") {
			t.Fatal("viewer has no direct date prompt")
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if app.privateDate() != before || app.Mode() != mode || app.IsQuitting() {
			t.Fatal("cancelling date prompt changed navigation")
		}
	}
}
