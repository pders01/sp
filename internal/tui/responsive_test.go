package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var layoutSizes = []tea.WindowSizeMsg{
	{Width: 0, Height: 0}, {Width: -1, Height: -1}, {Width: 1, Height: 1},
	{Width: 20, Height: 5}, {Width: 30, Height: 10}, {Width: 42, Height: 19},
	{Width: 56, Height: 20}, {Width: 80, Height: 24}, {Width: 120, Height: 40},
}

func assertFrameFits(t *testing.T, view string, size tea.WindowSizeMsg) {
	t.Helper()
	if size.Width <= 0 || size.Height <= 0 {
		if view != "" {
			t.Fatal("nonpositive dimensions produced visible output")
		}
		return
	}
	if lipgloss.Width(view) > size.Width || viewHeight(view) > size.Height {
		t.Fatalf("frame %dx%d exceeds terminal %dx%d", lipgloss.Width(view), viewHeight(view), size.Width, size.Height)
	}
}

func TestResponsiveCalendarBoundsAndSmallScreenGuidance(t *testing.T) {
	for _, year := range []bool{false, true} {
		t.Run(fmt.Sprint(year), func(t *testing.T) {
			cal := NewCalendar([]string{"2026-08-31"})
			defer cal.Close()
			cal.SetCursor("2026-08-31") // Six-week month exercises the maximum grid.
			cal.SetContents(map[string]string{"2026-08-31": strings.Repeat("笔记🙂", 100)})
			cal.privacyEnabled, cal.templatesAvailable = true, true
			cal.theme.SetStatus(strings.Repeat("very-long-status-", 30)+"\nnext line", 0)
			if year {
				cal.Update(privacyKey("y"))
			}
			for _, size := range layoutSizes {
				cal.Update(size)
				assertFrameFits(t, cal.View(), size)
			}
			cal.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
			if !strings.Contains(cal.View(), "Resize terminal") {
				t.Fatal("an unsupported grid size did not provide resize guidance")
			}
			minimum := tea.WindowSizeMsg{Width: 42, Height: 19}
			if year {
				minimum = tea.WindowSizeMsg{Width: 56, Height: 20}
			}
			cal.Update(minimum)
			if strings.Contains(cal.View(), "Resize terminal") {
				t.Fatal("a supported minimum grid size was rejected")
			}
			assertFrameFits(t, cal.View(), minimum)
			cal.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			view := ansi.Strip(cal.View())
			if strings.Contains(view, "Resize terminal") || !strings.Contains(view, "?/F1: help") || !strings.Contains(view, "q: quit") {
				t.Fatal("standard-size calendar lost essential controls")
			}
		})
	}
}

func TestResponsiveNotebookBoundsAndStableFocus(t *testing.T) {
	contents := make(map[string]string)
	for day := 1; day <= 28; day++ {
		contents[fmt.Sprintf("2026-02-%02d", day)] = "# 笔记🙂\n\n" + strings.Repeat("long note ", 80)
	}
	app := workflowApp(t, ModeCalendar, contents)
	app.nb.SetCurrentDate("2026-02-14")
	app.nb.templatesAvailable, app.nb.privacyEnabled = true, true
	app.nb.theme.SetStatus(strings.Repeat("recover-this-path-", 30), 0)
	for _, size := range layoutSizes {
		app.Update(size)
		assertFrameFits(t, app.nb.View(), size)
		if app.nb.GetCurrentPage() != "2026-02-14" || app.nb.viewport.Width < 1 || app.nb.viewport.Height < 1 {
			t.Fatal("resize lost focus or assigned a nonpositive viewport")
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := ansi.Strip(app.nb.View())
	for _, want := range []string{"2026-02-14", "enter/e: edit", "u: unlock", "a: templates", "esc: back", "?/F1: help", "q: quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("visible notebook action/date missing: %q", want)
		}
	}
	if got := app.nb.viewport.Height + viewHeight(app.nb.renderHeader()) + viewHeight(app.nb.renderFooter()); got != 24 {
		t.Fatalf("viewport budget totals %d rows instead of 24", got)
	}
}

func TestResponsiveSingleLineViewportStillSupportsHalfPageKeys(t *testing.T) {
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-01-01": strings.Repeat("line\n\n", 60)})
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	height := viewHeight(app.nb.renderHeader()) + viewHeight(app.nb.renderFooter()) + 1
	app.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	if app.nb.viewport.Height != 1 || strings.Contains(app.nb.View(), "Resize terminal") {
		t.Fatal("fixture is not a supported single-line viewport")
	}
	app.Update(rawPhysical(tea.KeyCtrlD))
	if app.nb.viewport.YOffset != 1 {
		t.Fatal("half-page down did not move a single-line viewport")
	}
	app.Update(rawPhysical(tea.KeyCtrlU))
	if app.nb.viewport.YOffset != 0 {
		t.Fatal("half-page up did not return to the top")
	}
}

func TestResponsiveNotebookKeepsLongCodeLinesReadable(t *testing.T) {
	const date = "2026-01-01"
	original := "```\n" + strings.Repeat("x", 300) + " END_CODE\n\tindented\n```\n"
	app := workflowApp(t, ModeNotebook, map[string]string{date: original})
	app.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	_ = app.View()
	found := false
	for offset := 0; offset < app.nb.viewport.TotalLineCount(); offset++ {
		app.nb.viewport.SetYOffset(offset)
		if strings.Contains(ansi.Strip(app.nb.viewport.View()), "END_CODE") {
			found = true
			break
		}
	}
	if !found || app.nb.contents[date] != original {
		t.Fatal("narrow rendering lost code text or mutated the stored document")
	}
}

func TestResponsivePrivateEditorRetainsExactDraft(t *testing.T) {
	const original = "original\r\n"
	draft := "PRIVATE_笔记🙂\t " + strings.Repeat("line\r\n", 140) + "trailing  \r\n\r\n"
	e := newPrivateEditor("2026-10-31", original, nil, nil)
	e.input.SetValue(draft)
	e.status = strings.Repeat("recovery/path/", 30)
	for _, size := range layoutSizes {
		e.Update(size)
		assertFrameFits(t, e.View(), size)
		if e.input.Value() != draft || e.original != original || e.done {
			t.Fatal("layout changed or discarded private text")
		}
	}
	e.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, want := range []string{"Ctrl+S: save/return", "Esc: cancel", "F1: help"} {
		if !strings.Contains(ansi.Strip(e.View()), want) {
			t.Fatalf("private control missing: %q", want)
		}
	}
}

func TestResponsiveUnlockMasksAndPreservesSecret(t *testing.T) {
	const secret = "SYNTHETIC secret ? 笔记"
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{})
	app.Update(privacyKey("u"))
	app.Update(privacyKey(secret))
	app.privacy.promptErr = strings.Repeat("synthetic unlock error ", 30)
	for _, size := range layoutSizes {
		app.Update(size)
		view := app.View()
		assertFrameFits(t, view, size)
		if strings.Contains(view, "SYNTHETIC") || app.privacy.prompt.Value() != secret {
			t.Fatal("resize exposed or changed the passphrase")
		}
	}
}

func TestResponsiveTemplateChooserPreservesSelection(t *testing.T) {
	app := newTestApp(ModeCalendar)
	defer app.Close()
	app.SetTemplates([]DayTemplate{{ID: "one", Name: strings.Repeat("笔记🙂", 50) + "\nline"}}, nil,
		func(_ context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
			t.Fatal("layout must not apply templates")
			return TemplateApplyResult{}, nil
		})
	app.Update(privacyKey("a"))
	app.Update(privacyKey(" "))
	for _, size := range layoutSizes {
		app.Update(size)
		assertFrameFits(t, app.View(), size)
		if !app.templateChooser.selected["one"] || app.templateChooser.cursor != 0 {
			t.Fatal("resize discarded the template selection")
		}
	}
}
