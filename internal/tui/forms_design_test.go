package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestFormsDesignDatePolicyAndLiteralInput(t *testing.T) {
	for _, palette := range []Palette{darkPalette(), lightPalette()} {
		p := newDateJumpPrompt("2026-10-31", 80, 24)
		p.update(privatePasteMsg("2024-02-29"))
		p.update(privatePasteMsg("\n"))
		view := ansi.Strip(p.view(palette))
		for _, want := range []string{"Jump to date", "2026-10-31", "  Date · YYYY-MM-DD", "2024-02-29", "Input rejected", "ASCII", "Selection only; no note is saved.", "Enter: select date", "F1: help"} {
			if !strings.Contains(view, want) {
				t.Fatalf("date form missing %q:\n%s", want, view)
			}
		}
		if date, close := p.update(tea.KeyMsg{Type: tea.KeyEnter}); date != "" || close {
			t.Fatal("rejected paste authorized an older candidate")
		}
		p.update(tea.KeyMsg{Type: tea.KeyF1})
		p.update(tea.KeyMsg{Type: tea.KeyCtrlU})
		p.update(privatePasteMsg("2099-01-01"))
		p.update(tea.KeyMsg{Type: tea.KeyEnter})
		p.update(tea.KeyMsg{Type: tea.KeyEsc})
		if p.keyHelp != nil || p.input.Value() != "2024-02-29" || !p.rejected {
			t.Fatal("help changed the date or rejected-input guard")
		}
		for _, size := range layoutSizes {
			p.resize(size.Width, size.Height)
			assertFrameFits(t, p.view(palette), size)
		}
		p.update(tea.KeyMsg{Type: tea.KeyCtrlU})
		p.update(privatePasteMsg("2024-02-29"))
		if date, close := p.update(privacyKey("enter")); date != "" || close {
			t.Fatal("literal Enter text submitted the date")
		}
		p.update(tea.KeyMsg{Type: tea.KeyCtrlU})
		p.update(privatePasteMsg("2024-02-29"))
		if date, close := p.update(tea.KeyMsg{Type: tea.KeyEnter}); date != "2024-02-29" || !close {
			t.Fatal("physical Enter did not select the exact date")
		}
	}
}

func TestFormsDesignTemplatePolicyAndOutcomeAtStandardSize(t *testing.T) {
	const date = "2026-10-31"
	diagnostic := strings.Repeat("synthetic storage detail ", 80) + "END_OF_ERROR"
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		app := templateWorkflowApp(t, mode, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
			return TemplateApplyResult{Published: true, Applied: []string{"one"}}, fmt.Errorf("%s: %w", diagnostic, scratchpad.ErrDurabilityUnconfirmed)
		})
		app.templateChooser.date = date
		app.appliedTemplates[date] = map[string]bool{"one": true}
		app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		view := ansi.Strip(app.View())
		for _, want := range []string{"Templates", date, "  SAVE NOW: saves before editing", "Editor cancel does not undo insertion", "[applied] = history", "another copy", "[ ] [applied] Section one", "Space: select", "Enter: SAVE NOW", "?/F1: help"} {
			if !strings.Contains(view, want) {
				t.Fatalf("template chooser missing %q:\n%s", want, view)
			}
		}
		app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if !strings.Contains(app.View(), "Nothing selected") || app.templateChooser.applying {
			t.Fatal("empty selection did not stay in the form with feedback")
		}
		app.Update(privacyKey(" "))
		app.Update(templateWorkflowStart(t, app)())
		view = strings.Join(strings.Fields(ansi.Strip(app.View())), " ")
		for _, want := range []string{"Saved; durability unconfirmed", "Check storage before retrying", "Enter appends another copy", "[✓] [applied] [append again] Section one", "F1: full details"} {
			if !strings.Contains(view, want) {
				t.Fatalf("published outcome missing %q:\n%s", want, view)
			}
		}
		for _, size := range layoutSizes {
			app.Update(size)
			assertFrameFits(t, app.View(), size)
			if !app.templateChooser.selected["one"] || !strings.Contains(app.templateChooser.feedback, diagnostic) {
				t.Fatal("resize lost selection or complete error")
			}
		}
		app.Update(tea.KeyMsg{Type: tea.KeyF1})
		if !strings.Contains(app.templateChooser.keyHelp.content, diagnostic) {
			t.Fatal("F1 lost the full diagnostic")
		}
	}
}

func TestFormsDesignTemplateCancellationDoesNotClaimRollback(t *testing.T) {
	app := templateWorkflowApp(t, ModeCalendar, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		t.Fatal("view-only fixture must not apply templates")
		return TemplateApplyResult{}, nil
	})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.templateChooser.applying = true // View-only fixture; no owned work.
	app.templateChooser.cancelRequested = true
	view := ansi.Strip(app.View())
	for _, want := range []string{"Cancellation requested", "waiting for outcome", "Saved insertion is not undone", "Esc: request cancel", "?/F1: help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("pending cancellation missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Enter: SAVE NOW") || strings.Contains(view, "Space: select") {
		t.Fatal("pending work advertises unavailable selection actions")
	}
}
