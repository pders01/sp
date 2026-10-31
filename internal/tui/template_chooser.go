package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/scratchpad"
)

// DayTemplate is one named section shown in the day-template chooser.
type DayTemplate struct {
	ID   string
	Name string
}

// TemplateApplyResult refreshes the active day after selected sections have
// been rendered and persisted.
type TemplateApplyResult struct {
	Content   string
	Applied   []string
	Published bool // A replacement was visible despite an error; it must not be treated as rolled back.
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
	date            string
	cursor          int
	selected        map[string]bool
	applying        bool
	cancel          context.CancelFunc
	cancelRequested bool
	attempt         *templateApplyAttempt
	feedback        string // Persistent until another attempt or the chooser is dismissed.
	emptySelection  bool
	keyHelp         *keyHelp
}

// A non-zero-sized identity distinguishes both newer choosers and retries. Only
// the UI goroutine reads/writes cancellation state; commands carry the identity.
type templateApplyAttempt struct {
	identity byte
	done     chan struct{}
	result   templateAppliedMsg // Written before done closes; never read earlier.
}

type templateAppliedMsg struct {
	date    string
	result  TemplateApplyResult
	err     error
	attempt *templateApplyAttempt
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
	if key.Type == tea.KeyRunes && (len(key.Runes) != 1 || key.Alt) {
		return a, nil // Literal/batched key names cannot publish or cancel work.
	}
	if chooser.keyHelp != nil {
		if chooser.keyHelp.update(key) {
			chooser.keyHelp = nil
		}
		return a, nil
	}
	if helpRequested(key, true) {
		details := "SAVE NOW persists selected sections before opening the editor. Cancelling the editor does not undo insertion. Append again adds another copy; existing writing stays unchanged. Escape during application requests cancellation and waits; already-published insertion is not undone."
		if feedback := chooser.feedbackText(); feedback != "" {
			details += "\n\nLast outcome\n" + feedback
		}
		chooser.keyHelp = newKeyHelp("Templates", chooser.bindings(), details,
			a.cal.width, a.cal.height)
		return a, nil
	}
	if chooser.applying && key.String() != "ctrl+c" && key.String() != "q" && key.String() != "esc" {
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
		if chooser.applying {
			chooser.cancelRequested = true
			if chooser.cancel != nil {
				chooser.cancel()
			}
			return a, nil // Wait for the actual persistence outcome before leaving.
		}
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
		if chooser.cursor < 0 || chooser.cursor >= len(a.templates) {
			return a, nil
		}
		option := a.templates[chooser.cursor]
		chooser.selected[option.ID] = !chooser.selected[option.ID]
		chooser.emptySelection = false
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
			chooser.emptySelection = true
			return a, nil
		}
		date := chooser.date
		apply := a.applyTemplates
		ctx, cancel := context.WithCancel(context.Background())
		chooser.applying = true
		chooser.cancel = cancel
		chooser.cancelRequested = false
		chooser.feedback = ""
		chooser.emptySelection = false
		a.setTemplatePersistentStatus("")
		attempt := &templateApplyAttempt{done: make(chan struct{})}
		chooser.attempt = attempt
		// Own work from acceptance, even if a returned command is never run.
		go func() {
			defer close(attempt.done)
			result, err := apply(ctx, date, selections)
			attempt.result = templateAppliedMsg{date: date, result: result, err: err, attempt: attempt}
		}()
		return a, func() tea.Msg {
			<-attempt.done
			return attempt.result
		}
	}
	return a, nil
}

func (a *App) finishTemplateApply(msg templateAppliedMsg) (tea.Model, tea.Cmd) {
	if a.quitting {
		return a, nil
	}
	chooser := a.templateChooser
	if msg.attempt != nil {
		if chooser == nil || !chooser.applying || chooser.attempt != msg.attempt || chooser.date != msg.date {
			return a, nil // A late/duplicate result cannot mutate a newer workflow.
		}
	} else if chooser != nil {
		// Legacy callers may inject an untagged result without a chooser. Never
		// let an unidentifiable result interfere with an interactive workflow.
		return a, nil
	}
	cancelRequested := chooser != nil && chooser.cancelRequested
	if chooser != nil {
		if chooser.cancel != nil {
			chooser.cancel()
		}
		chooser.cancel = nil
		chooser.applying = false
		chooser.attempt = nil
	}

	published := msg.err == nil || msg.result.Published
	if published {
		if a.privacy != nil {
			// Template output is private; never feed it into shared calendar caches.
			a.noteChanged(msg.date, "")
		} else {
			a.noteChanged(msg.date, msg.result.Content)
		}
		// Private refreshes can reload metadata; record the published outcome last.
		if a.appliedTemplates == nil {
			a.appliedTemplates = make(map[string]map[string]bool)
		}
		if a.appliedTemplates[msg.date] == nil {
			a.appliedTemplates[msg.date] = make(map[string]bool)
		}
		for _, id := range msg.result.Applied {
			a.appliedTemplates[msg.date][id] = true
		}
	}
	if msg.err != nil {
		var text string
		switch {
		case published && errors.Is(msg.err, scratchpad.ErrDurabilityUnconfirmed):
			text = fmt.Sprintf("Templates saved, but durability is unconfirmed: %v. Check storage before retrying; Enter appends another copy, Esc closes.", msg.err)
		case published:
			text = fmt.Sprintf("Templates published, but application reported an error: %v. Check saved content before retrying; Enter appends another copy, Esc closes.", msg.err)
		case errors.Is(msg.err, context.Canceled):
			text = "Template application cancelled before publication. Selections retained; Enter retries, Space changes selection, Esc closes."
		default:
			text = fmt.Sprintf("Templates not saved: %v. Resolve the error, then Enter retries; Space changes selection, Esc closes.", msg.err)
		}
		if cancelRequested && published {
			text = "Cancellation after commit: insertion was not undone. Editor not opened. " + text
		}
		if chooser != nil {
			chooser.feedback = text
			chooser.keyHelp = nil // The outcome must not remain hidden behind help.
		}
		a.setTemplatePersistentStatus(text)
		return a, nil
	}

	a.templateChooser = nil
	if cancelRequested {
		a.setTemplatePersistentStatus("Cancellation after commit: templates saved; insertion was not undone. Editor not opened.")
		return a, nil
	}
	if a.privacy != nil {
		return a, a.startPrivateEdit(msg.date)
	}
	var editorCmd tea.Cmd
	if a.mode == ModeCalendar {
		editorCmd = a.cal.startEdit(msg.date)
	} else {
		editorCmd = a.nb.startEdit(msg.date)
	}
	if editorCmd != nil {
		return a, editorCmd
	}
	cmd := a.templateStatus("Templates saved", false)
	return a, cmd
}

func (a *App) setTemplatePersistentStatus(text string) {
	watcher := a.cal.theme
	if a.mode == ModeNotebook {
		watcher = a.nb.theme
	}
	watcher.SetStatus(text, 0)
}

// Keep chooser-specific help here: pending work offers task cancellation, not
// only application exit. Selection controls never double as history indicators.
func (c *templateChooser) bindings() []helpEntry {
	return []helpEntry{
		{keys: "↑/↓", label: "move", visible: !c.applying},
		{keys: "Space", label: "select section", visible: !c.applying},
		{keys: "Enter", label: "SAVE NOW, then edit", visible: !c.applying},
		{keys: "Esc", label: "close chooser", visible: !c.applying},
		{keys: "Esc", label: "request cancellation; wait for outcome", visible: c.applying},
		{keys: "?/F1", label: "help", visible: true},
		{keys: "q/Ctrl+C", label: "quit application", visible: true},
	}
}

func (c *templateChooser) feedbackText() string {
	if c.emptySelection {
		text := "Nothing selected. Use Space to select sections, then Enter to SAVE NOW."
		if c.feedback != "" {
			text += "\n" + c.feedback
		}
		return text
	}
	return c.feedback
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

	if chooser.keyHelp != nil {
		return chooser.keyHelp.view(palette)
	}
	innerWidth := max(1, width-4)
	footer := wrappedHelp(chooser.bindings(), innerWidth)
	if chooser.applying {
		phase := "Saving templates…"
		if chooser.cancelRequested {
			phase = "Cancellation requested; waiting for outcome. Saved insertion is not undone."
		}
		footer = ansi.Wrap(phase, innerWidth, "") + "\n" + footer
	}
	boundary := ansi.Wrap("SAVE NOW: saves before editing. Editor cancel does not undo insertion.", innerWidth, "")
	hint := ansi.Wrap("[applied] = history; select to append again (another copy).", innerWidth, "")
	// Padding, header, instructions, gap, footer. Always reserve one option row.
	chromeHeight := 4 + viewHeight(boundary) + viewHeight(hint) + viewHeight(footer)
	minHeight := chromeHeight + 1
	feedbackText := chooser.feedbackText()
	if feedbackText != "" {
		minHeight++
	}
	if width < 30 || height < minHeight {
		return smallTerminal("Templates · SAVE NOW", width, height, 30, minHeight, "F1: details/help · Esc: close/request cancel")
	}
	lines := []string{
		palette.Header.Render(truncate(fmt.Sprintf("Templates · %s · %d/%d", chooser.date, chooser.cursor+1, len(a.templates)), innerWidth)),
		palette.MutedText.Render(boundary),
		palette.MutedText.Render(hint),
	}
	feedbackHeight := 0
	if feedbackText != "" {
		feedback := strings.Split(ansi.Wrap(displayText(feedbackText), innerWidth, ""), "\n")
		feedbackHeight = min(len(feedback), min(3, height-chromeHeight-1))
		if feedbackHeight < len(feedback) {
			feedback[feedbackHeight-1] = truncate(feedback[feedbackHeight-1], max(0, innerWidth-19)) + " … F1: full details"
		}
		lines = append(lines, palette.MutedText.Render(strings.Join(feedback[:feedbackHeight], "\n")))
	}
	start, end := chooser.visibleRange(len(a.templates), height-chromeHeight-feedbackHeight)
	for i := start; i < end; i++ {
		option := a.templates[i]
		marker := "[ ]"
		style := lipgloss.NewStyle().Foreground(palette.Text)
		history := ""
		if a.templateApplied(chooser.date, option.ID) {
			history = "[applied] "
			style = palette.MutedText
		}
		if chooser.selected[option.ID] {
			marker = "[✓]"
			style = lipgloss.NewStyle().Foreground(palette.Accent).Bold(true)
			if history != "" {
				history += "[append again] "
			}
		}
		cursor := "  "
		if i == chooser.cursor {
			cursor = "▌ "
			style = style.Foreground(palette.Highlight).Bold(true)
		}
		lines = append(lines, cursor+style.Render(truncate(marker+" "+history+oneLine(option.Name), innerWidth-2)))
	}
	lines = append(lines, "", palette.Help.Render(footer))
	return fitView(lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n")), width, height)
}

func (c *templateChooser) visibleRange(total, height int) (start, end int) {
	capacity := max(height, 1) // Caller has reserved chrome and wrapped controls.
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
