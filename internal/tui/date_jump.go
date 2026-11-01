package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type dateJumpPrompt struct {
	current       string
	input         privateInput // Literal ASCII date only; no clipboard commands.
	err           string
	rejected      bool
	width, height int
	keyHelp       *keyHelp
}

func newDateJumpPrompt(current string, width, height int) *dateJumpPrompt {
	p := &dateJumpPrompt{current: current, input: newPrivateInput()}
	p.input.Focus()
	p.resize(width, height)
	return p
}

func (p *dateJumpPrompt) resize(width, height int) {
	p.width, p.height = max(0, width), max(0, height)
	p.input.SetWidth(max(1, min(11, width-12)))
	p.input.SetHeight(1)
	if p.keyHelp != nil {
		p.keyHelp.resize(width, height)
	}
}

func (p *dateJumpPrompt) insert(text string) {
	if text == "" {
		return
	}
	valid := len(text) <= 10-len(p.input.Value())
	for i := 0; valid && i < len(text); i++ {
		valid = text[i] == '-' || text[i] >= '0' && text[i] <= '9'
	}
	if !valid {
		p.err, p.rejected = "Input rejected: use ten ASCII characters, YYYY-MM-DD; edit or Ctrl+U to clear.", true
		return
	}
	p.input.insert(text)
	p.err, p.rejected = "", false
}

// Only a physical Enter submits. Rejected input must be edited/cleared rather
// than silently submitting an older valid candidate after a rejected paste.
func (p *dateJumpPrompt) update(msg tea.Msg) (date string, close bool) {
	if p.keyHelp != nil {
		if key, ok := msg.(tea.KeyMsg); ok && p.keyHelp.update(key) {
			p.keyHelp = nil
		}
		return "", false
	}
	if text, ok := privatePasteText(msg); ok {
		p.insert(text)
		return "", false
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return "", false
	}
	switch key.Type {
	case tea.KeyF1:
		p.keyHelp = newKeyHelp("Jump to date", dateJumpBindings(), p.err+"\nSelection only: no save or content search. Private loading is limited to the selected page. Calendar jumps focus the month; notebook jumps open the selected day.", p.width, p.height)
	case tea.KeyEsc, tea.KeyCtrlC:
		return "", true
	case tea.KeyEnter:
		if p.rejected {
			return "", false
		}
		value := p.input.Value()
		parsed, err := time.Parse(time.DateOnly, value)
		if err != nil || parsed.Format(time.DateOnly) != value {
			p.err = "Enter a valid date in exact YYYY-MM-DD format."
			return "", false
		}
		return value, true
	case tea.KeyRunes:
		p.insert(string(key.Runes))
	case tea.KeySpace, tea.KeyTab:
		p.insert(" ") // Rejected, never trimmed or silently normalized.
	case tea.KeyCtrlU:
		p.input.SetValue("")
		p.err, p.rejected = "", false
	case tea.KeyBackspace, tea.KeyCtrlH, tea.KeyDelete, tea.KeyCtrlD, tea.KeyCtrlK:
		p.input, _ = p.input.Update(msg)
		p.err, p.rejected = "", false
	case tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd, tea.KeyCtrlA, tea.KeyCtrlE:
		p.input, _ = p.input.Update(msg)
	}
	return "", false
}

func dateJumpBindings() []helpEntry {
	return []helpEntry{
		{keys: "Enter", label: "select a valid YYYY-MM-DD date; never saves a note", visible: true},
		{keys: "Esc/Ctrl+C", label: "cancel and return to the same view", visible: true},
		{keys: "←/→ Home/End", label: "move within the date", visible: true},
		{keys: "Backspace/Delete", label: "delete previous/next character", visible: true},
		{keys: "Ctrl+U", label: "clear date and rejected-input warning", visible: true},
		{keys: "F1", label: "key reference; other letters are not navigation shortcuts", visible: true},
	}
}

func (p *dateJumpPrompt) view(palette Palette) string {
	if p.keyHelp != nil {
		return p.keyHelp.view(palette)
	}
	innerWidth := max(1, p.width-4)
	footer := screenFooter([]helpEntry{
		{keys: "Enter", label: "select date", visible: true},
		{keys: "Esc/Ctrl+C", label: "cancel", visible: true},
		{keys: "F1", label: "help", visible: true},
	}, palette, p.width)
	lines := []string{palette.Header.Render("Date · YYYY-MM-DD"), p.input.View()}
	if p.err != "" {
		lines = append(lines, palette.ErrorMessage.Render(ansi.Wrap(p.err, innerWidth, "")))
	}
	lines = append(lines, "", palette.MutedText.Render(ansi.Wrap("Selection only; no note is saved.", innerWidth, "")))
	body := lipgloss.NewStyle().PaddingLeft(2).Render(strings.Join(lines, "\n"))
	minHeight := viewHeaderRows + viewHeight(body) + viewHeight(footer)
	if p.width < 24 || p.height < minHeight {
		return smallTerminal("Jump to date", p.width, p.height, 24, minHeight, "F1: help · Esc: cancel; no save")
	}
	return screenView("Jump to date · "+p.current, "", body, footer, palette, p.width, p.height)
}

func (a *App) updateDateJump(msg tea.Msg) (tea.Model, tea.Cmd) {
	date, close := a.dateJump.update(msg)
	if !close {
		return a, nil
	}
	a.dateJump = nil
	if date == "" {
		return a, nil
	}
	if a.mode == ModeCalendar {
		a.cal.SetCursor(date) // Explicit date selection resets preferred day.
	} else {
		a.nb.theme.SetStatus("Date selected. Enter/e: edit or create.", 0)
		a.drillToNotebook(date) // Loads only this selected private page, if enabled.
	}
	return a, nil
}
