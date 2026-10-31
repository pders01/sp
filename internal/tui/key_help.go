package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// keyHelp is a read-only, scrollable reference. It never forwards keys to an
// editor, unlock operation, or template action underneath it.
type keyHelp struct {
	title, content string
	viewport       viewport.Model
	width, height  int
}

func newKeyHelp(title string, entries []helpEntry, details string, width, height int) *keyHelp {
	var lines []string
	for _, entry := range entries {
		if entry.visible {
			lines = append(lines, entry.keys+"  "+entry.label)
		}
	}
	if details != "" {
		lines = append(lines, "", "Details", details)
	}
	h := &keyHelp{title: title + " · keys", content: strings.Join(lines, "\n"), viewport: viewport.New(1, 1)}
	h.resize(width, height)
	return h
}

func (h *keyHelp) footer() string {
	return wrappedHelp([]helpEntry{
		{keys: "↑/↓ PgUp/Dn", label: "scroll", visible: true},
		{keys: "Esc/F1/q", label: "close", visible: true},
	}, h.width)
}

func (h *keyHelp) resize(width, height int) {
	h.width, h.height = max(0, width), max(0, height)
	h.viewport.Width = max(1, width)
	h.viewport.Height = max(1, height-1-viewHeight(h.footer()))
	h.viewport.SetContent(ansi.Wrap(displayText(h.content), max(1, width), ""))
}

func helpRequested(key tea.KeyMsg, allowQuestion bool) bool {
	return key.Type == tea.KeyF1 || (allowQuestion && key.Type == tea.KeyRunes && !key.Alt && len(key.Runes) == 1 && key.Runes[0] == '?')
}

// update reports when the reference should close. Escape closes only this
// layer; drafts, selections, and the underlying reading position are untouched.
func (h *keyHelp) update(key tea.KeyMsg) bool {
	if helpRequested(key, true) || key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC || key.String() == "q" {
		return true
	}
	switch key.String() {
	case "up", "k":
		h.viewport.LineUp(1)
	case "down", "j":
		h.viewport.LineDown(1)
	case "pgup", "b":
		h.viewport.SetYOffset(h.viewport.YOffset - h.viewport.Height)
	case "pgdown", "f":
		h.viewport.SetYOffset(h.viewport.YOffset + h.viewport.Height)
	case "ctrl+u":
		h.viewport.SetYOffset(h.viewport.YOffset - max(1, h.viewport.Height/2))
	case "ctrl+d":
		h.viewport.SetYOffset(h.viewport.YOffset + max(1, h.viewport.Height/2))
	case "g", "home":
		h.viewport.GotoTop()
	case "G", "end":
		h.viewport.GotoBottom()
	}
	return false
}

func (h *keyHelp) view(palette Palette) string {
	if h.width < 24 || h.height < 6 {
		return smallTerminal(h.title, h.width, h.height, 24, 6, "Esc/F1/q: close help")
	}
	return fitView(lipgloss.JoinVertical(lipgloss.Left,
		palette.Header.Render(truncate(h.title, h.width)), h.viewport.View(), palette.Help.Render(h.footer()),
	), h.width, h.height)
}

func (c *Calendar) keyBindings() []helpEntry {
	year := c.view == ViewYear
	unit, row, period := "day", "week", "month"
	if year {
		unit, row, period = "month", "row", "year"
	}
	return []helpEntry{
		{keys: "←/h →/l", label: "previous/next " + unit, visible: true},
		{keys: "↑/k ↓/j", label: "previous/next " + row, visible: true},
		{keys: "H/L", label: "previous/next " + period + " (clamped)", visible: true},
		{keys: "Enter", label: "open selected day in notebook", visible: !year},
		{keys: "Enter", label: "open selected month", visible: year},
		{keys: "e/i", label: c.editLabel() + " selected day directly", visible: !year},
		{keys: "u", label: "unlock private notes for this process", visible: c.privacyEnabled && !privacyIsUnlocked(c.privacyState)},
		{keys: "Ctrl+L", label: "lock session (seal open drafts first); not secure memory erasure", visible: c.canRelock},
		{keys: "a", label: "choose template sections", visible: !year && c.templatesAvailable},
		{keys: "m/y", label: "month/year view", visible: true},
		{keys: "t/T", label: "return to today", visible: true},
		{keys: "d", label: "jump to a YYYY-MM-DD date (focus month)", visible: c.dateJumpAvailable},
		{keys: "Ctrl+T", label: "cycle theme", visible: true},
		{keys: "?/F1", label: "toggle key help", visible: true},
		{keys: "Esc", label: "return to month view", visible: year},
		{keys: "Esc", label: "quit (root month view)", visible: !year},
		{keys: "q/Ctrl+C", label: "quit", visible: true},
	}
}

func (n *Notebook) keyBindings() []helpEntry {
	back := "quit (no parent view)"
	if n.canPop {
		back = "return to calendar"
	}
	return []helpEntry{
		{keys: "←/h →/l", label: "newer/older page", visible: len(n.pages) > 0},
		{keys: "↑/k ↓/j", label: "scroll one line", visible: len(n.pages) > 0},
		{keys: "PgUp/b PgDn/f", label: "scroll a full page", visible: len(n.pages) > 0},
		{keys: "Ctrl+U/D", label: "scroll half a page", visible: len(n.pages) > 0},
		{keys: "g/G", label: "top/bottom of note", visible: len(n.pages) > 0},
		{keys: "Enter/e/i", label: n.editLabel() + " current page", visible: len(n.pages) > 0},
		{keys: "Enter/e/i", label: "create today's note (" + n.today + ")", visible: len(n.pages) == 0},
		{keys: "u", label: "unlock private notes for this process", visible: n.privacyEnabled && !privacyIsUnlocked(n.privacyState)},
		{keys: "Ctrl+L", label: "lock session (seal open drafts first); not secure memory erasure", visible: n.canRelock},
		{keys: "a", label: "choose template sections", visible: n.templatesAvailable && len(n.pages) > 0},
		{keys: "d", label: "jump to a YYYY-MM-DD date (open day)", visible: n.dateJumpAvailable},
		{keys: "Esc/Backspace", label: back, visible: true},
		{keys: "Ctrl+T", label: "cycle theme", visible: true},
		{keys: "?/F1", label: "toggle key help", visible: true},
		{keys: "q/Ctrl+C", label: "quit", visible: true},
	}
}

func privateEditorBindings() []helpEntry {
	return []helpEntry{
		{keys: "Ctrl+S", label: "save and return", visible: true},
		{keys: "F2", label: "after conflict: compare saved text/draft, reload or explicitly select replacement", visible: true},
		{keys: "Esc/Ctrl+C", label: "cancel; changed text requires confirmation and encrypted recovery", visible: true},
		{keys: "←/→ ↑/↓", label: "move cursor", visible: true},
		{keys: "Home/Ctrl+A", label: "start of line", visible: true},
		{keys: "End/Ctrl+E", label: "end of line", visible: true},
		{keys: "Backspace/Ctrl+H", label: "delete previous character", visible: true},
		{keys: "Delete/Ctrl+D", label: "delete next character", visible: true},
		{keys: "Ctrl+K/U", label: "delete after/before cursor on this line", visible: true},
		{keys: "Tab/Enter", label: "insert tab/newline", visible: true},
		{keys: "F1", label: "toggle key help; ? remains literal text", visible: true},
	}
}

func unlockBindings() []helpEntry {
	return []helpEntry{
		{keys: "Enter", label: "unlock and continue", visible: true},
		{keys: "Esc/Ctrl+C", label: "cancel unlock", visible: true},
		{keys: "←/Ctrl+B →/Ctrl+F", label: "move within passphrase", visible: true},
		{keys: "Alt+←/→", label: "start/end of passphrase", visible: true},
		{keys: "Home/Ctrl+A", label: "start of passphrase", visible: true},
		{keys: "End/Ctrl+E", label: "end of passphrase", visible: true},
		{keys: "Backspace/Ctrl+H", label: "delete previous character", visible: true},
		{keys: "Delete/Ctrl+D", label: "delete next character", visible: true},
		{keys: "Ctrl+U/W", label: "delete before cursor", visible: true},
		{keys: "Ctrl+K", label: "delete after cursor", visible: true},
		{keys: "F1", label: "toggle key help; ? remains part of the passphrase", visible: true},
	}
}

func templateBindings(applying bool) []helpEntry {
	return []helpEntry{
		{keys: "↑/k ↓/j", label: "move selection", visible: !applying},
		{keys: "Space", label: "toggle section; applied sections can be appended again", visible: !applying},
		{keys: "Enter", label: "save selected sections, then open editor", visible: !applying},
		{keys: "Esc", label: "cancel chooser", visible: !applying},
		{keys: "?/F1", label: "toggle key help", visible: true},
		{keys: "q/Ctrl+C", label: "quit application; request cancellation of pending work", visible: true},
	}
}
