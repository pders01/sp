package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DayTemplate is one named section shown in the day-template chooser.
type DayTemplate struct {
	ID   string
	Name string
}

// TemplateApplyResult refreshes the active day after selected sections have
// been rendered and persisted.
type TemplateApplyResult struct {
	Content string
	Applied []string
}

// TemplateSelection describes a chosen section. Force explicitly reapplies a
// template already recorded in the day's metadata.
type TemplateSelection struct {
	ID    string
	Force bool
}

// TemplateApplier renders and appends selected templates for a date.
type TemplateApplier func(ctx context.Context, date string, selections []TemplateSelection) (TemplateApplyResult, error)

type templateChooser struct {
	date     string
	cursor   int
	selected map[string]bool
	applying bool
	cancel   context.CancelFunc
}

type templateAppliedMsg struct {
	date   string
	result TemplateApplyResult
	err    error
}

func newTemplateChooser(date string) *templateChooser {
	return &templateChooser{date: date, selected: make(map[string]bool)}
}

func (a *App) SetTemplates(options []DayTemplate, applied map[string][]string, apply TemplateApplier) {
	a.templates = append([]DayTemplate(nil), options...)
	a.cal.templatesAvailable = len(options) > 0 && apply != nil
	a.nb.templatesAvailable = len(options) > 0 && apply != nil
	a.appliedTemplates = make(map[string]map[string]bool, len(applied))
	for date, ids := range applied {
		a.appliedTemplates[date] = make(map[string]bool, len(ids))
		for _, id := range ids {
			a.appliedTemplates[date][id] = true
		}
	}
	a.applyTemplates = apply
}

func (a *App) startTemplateChooser() bool {
	if len(a.templates) == 0 || a.applyTemplates == nil {
		return false
	}
	var date string
	switch a.mode {
	case ModeCalendar:
		date = a.cal.CursorDate()
	case ModeNotebook:
		date, _ = a.nb.CurrentContent()
	}
	if date == "" {
		return false
	}
	a.templateChooser = newTemplateChooser(date)
	return true
}

func (a *App) updateTemplateChooser(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return a, nil
	}
	chooser := a.templateChooser
	if chooser.applying && key.String() != "ctrl+c" && key.String() != "q" {
		return a, nil
	}
	switch key.String() {
	case "ctrl+c", "q":
		if chooser.cancel != nil {
			chooser.cancel()
		}
		a.quitting = true
		return a, tea.Quit
	case "esc":
		a.templateChooser = nil
	case "up", "k":
		if chooser.cursor > 0 {
			chooser.cursor--
		}
	case "down", "j":
		if chooser.cursor < len(a.templates)-1 {
			chooser.cursor++
		}
	case " ":
		option := a.templates[chooser.cursor]
		chooser.selected[option.ID] = !chooser.selected[option.ID]
	case "enter":
		selections := make([]TemplateSelection, 0, len(chooser.selected))
		for _, option := range a.templates {
			if chooser.selected[option.ID] {
				selections = append(selections, TemplateSelection{
					ID:    option.ID,
					Force: a.templateApplied(chooser.date, option.ID),
				})
			}
		}
		if len(selections) == 0 {
			return a, nil
		}
		date := chooser.date
		apply := a.applyTemplates
		ctx, cancel := context.WithCancel(context.Background())
		chooser.applying = true
		chooser.cancel = cancel
		return a, func() tea.Msg {
			result, err := apply(ctx, date, selections)
			return templateAppliedMsg{date: date, result: result, err: err}
		}
	}
	return a, nil
}

func (a *App) finishTemplateApply(msg templateAppliedMsg) (tea.Model, tea.Cmd) {
	if a.templateChooser != nil && a.templateChooser.cancel != nil {
		a.templateChooser.cancel()
	}
	a.templateChooser = nil
	if msg.err != nil {
		cmd := a.templateStatus(fmt.Sprintf("template: %v", msg.err), true)
		return a, cmd
	}
	if a.appliedTemplates[msg.date] == nil {
		a.appliedTemplates[msg.date] = make(map[string]bool)
	}
	for _, id := range msg.result.Applied {
		a.appliedTemplates[msg.date][id] = true
	}
	a.cal.MarkDate(msg.date, msg.result.Content)
	a.nb.SetPageContent(msg.date, msg.result.Content)

	var editorCmd tea.Cmd
	if a.mode == ModeCalendar {
		editorCmd = a.cal.startEdit(msg.date)
	} else {
		editorCmd = a.nb.startEdit(msg.date)
	}
	if editorCmd != nil {
		return a, editorCmd
	}
	cmd := a.templateStatus("Templates applied", false)
	return a, cmd
}

func (a *App) templateStatus(text string, isError bool) tea.Cmd {
	watcher := a.cal.theme
	if a.mode == ModeNotebook {
		watcher = a.nb.theme
	}
	if isError {
		watcher.SetStatus(text, 2*time.Second)
		return watcher.expireStatusCmd(2 * time.Second)
	}
	watcher.SetStatus(text, 1500*time.Millisecond)
	return watcher.expireStatusCmd(1500 * time.Millisecond)
}

func (a *App) templateApplied(date, id string) bool {
	return a.appliedTemplates[date] != nil && a.appliedTemplates[date][id]
}

func (a *App) renderTemplateChooser() string {
	chooser := a.templateChooser
	palette := a.cal.theme.Palette()
	width, height := a.cal.width, a.cal.height
	if a.mode == ModeNotebook {
		palette = a.nb.theme.Palette()
		width, height = a.nb.width, a.nb.height
	}

	lines := []string{
		palette.Header.Render(fmt.Sprintf(
			"Templates · %s · %d/%d",
			chooser.date, chooser.cursor+1, len(a.templates),
		)),
		palette.MutedText.Render("Choose sections; select an applied section to reapply it."),
		"",
	}
	start, end := chooser.visibleRange(len(a.templates), height)
	for i := start; i < end; i++ {
		option := a.templates[i]
		marker := "[ ]"
		style := lipgloss.NewStyle().Foreground(palette.Text)
		suffix := ""
		switch {
		case chooser.selected[option.ID] && a.templateApplied(chooser.date, option.ID):
			marker, suffix = "[↻]", "  reapply"
			style = lipgloss.NewStyle().Foreground(palette.Accent).Bold(true)
		case chooser.selected[option.ID]:
			marker, style = "[✓]", lipgloss.NewStyle().Foreground(palette.Accent).Bold(true)
		case a.templateApplied(chooser.date, option.ID):
			marker, suffix, style = "[✓]", "  applied", palette.MutedText
		}
		cursor := "  "
		if i == chooser.cursor {
			cursor = "▌ "
			style = style.Foreground(palette.Highlight).Bold(true)
		}
		lines = append(lines, cursor+style.Render(marker+" "+option.Name+suffix))
	}
	if chooser.applying {
		lines = append(lines, "", palette.MutedText.Render("Applying templates…"))
	} else {
		lines = append(lines, "", palette.Help.Render(renderHelp([]helpEntry{
			{keys: "↑/k ↓/j", label: "move", visible: true},
			{keys: "space", label: "toggle", visible: true},
			{keys: "enter", label: "apply and edit", visible: true},
			{keys: "esc", label: "cancel", visible: true},
			{keys: "q", label: "quit", visible: true},
		})))
	}
	return lipgloss.NewStyle().Width(width).Height(height).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (c *templateChooser) visibleRange(total, height int) (start, end int) {
	capacity := max(height-7, 1) // padding, heading, description, and help
	if total <= capacity {
		return 0, total
	}
	start = c.cursor - capacity/2
	if start < 0 {
		start = 0
	}
	if start+capacity > total {
		start = total - capacity
	}
	return start, start + capacity
}
