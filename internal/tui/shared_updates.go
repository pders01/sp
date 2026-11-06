package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// sharedUpdates is a strictly read-only projection. It owns no private text,
// loader, editor, publication callback, or private diagnostics.
type sharedUpdates struct {
	date     string
	viewport viewport.Model
	ready    bool
	help     *keyHelp
}

// sharedActive lets App route this layer before private/date/template actions.
// Session lock and appearance may still be handled by the parent.
func (n *Notebook) sharedActive() bool { return n.shared != nil }

func (n *Notebook) sharedText(date string) string {
	if n.privacyEnabled {
		if available, inspected := n.sharedInspections[date]; inspected {
			if !available {
				return ""
			}
			return n.contents[date] // Fresh public inspection wins over private-page caches.
		}
		if n.privatePage != nil && n.privateDate == date && strings.TrimSpace(n.privatePage.Shared) != "" {
			return n.privatePage.Shared
		}
		// In privacy mode contents is the public-only projection from Inspect.
		return n.contents[date]
	}
	return n.sharedContents[date]
}

func (n *Notebook) hasShared() bool {
	return n.GetCurrentPage() != "" && strings.TrimSpace(n.sharedText(n.GetCurrentPage())) != ""
}

func (n *Notebook) openShared() {
	if n.sharedActive() || !n.hasShared() {
		return
	}
	n.captureReadingPosition()
	n.shared = &sharedUpdates{date: n.GetCurrentPage(), viewport: viewport.New(1, 1)}
	n.refreshShared()
}

func (n *Notebook) closeShared() {
	if n.shared == nil {
		return
	}
	if n.shared.ready {
		n.sharedPositions[n.shared.date] = n.shared.viewport.YOffset
	}
	n.shared = nil // Drop the rendered shared projection on return.
}

func (n *Notebook) sharedTitle() string {
	return "Shared updates · " + n.shared.date + " · Read-only"
}

func (n *Notebook) sharedStatus() string {
	return "Not encrypted · Sources are self-reported"
}

func (n *Notebook) sharedFooter() string {
	return n.renderDateStrip() + "\n" + screenFooter([]helpEntry{
		{keys: "←/→", label: "earlier/later", visible: true},
		{keys: "↑/↓", label: "scroll", visible: true},
		{keys: "Esc/c", label: "note", visible: true},
		{keys: "?/F1", label: "help", visible: true},
		{keys: "q", label: "quit", visible: true},
	}, n.theme.Palette(), n.width)
}

func (n *Notebook) refreshShared() {
	if n.shared == nil {
		return
	}
	s := n.shared
	if date := n.GetCurrentPage(); s.date != date {
		if s.ready {
			n.sharedPositions[s.date] = s.viewport.YOffset
		}
		s.date, s.ready = date, false
	}
	text := n.sharedText(s.date)
	offset := n.sharedPositions[s.date]
	if s.ready {
		offset = s.viewport.YOffset
		n.sharedPositions[s.date] = offset // An unavailable refresh must not erase the last reading position.
	}
	s.viewport.Width = max(1, n.width)
	s.viewport.Height = max(1, n.height-viewHeight(viewHeader(n.sharedTitle(), n.sharedStatus(), n.theme.Palette(), n.width))-viewHeight(n.sharedFooter()))
	content := n.renderMarkdown(text)
	ready := strings.TrimSpace(text) != ""
	if !ready {
		message := "No shared updates for this day."
		if available, inspected := n.sharedInspections[s.date]; inspected && !available {
			message = "Shared updates unavailable for this day."
		}
		content = insetNotebookBody(message)
	}
	s.viewport.SetContent(content)
	s.viewport.SetYOffset(offset)
	s.ready = ready
	if ready {
		n.sharedPositions[s.date] = s.viewport.YOffset
	}
	n.resizeSharedHelp()
}

func (n *Notebook) sharedHelpFooter() string {
	return screenFooter([]helpEntry{
		{keys: "↑/↓", label: "scroll", visible: true},
		{keys: "Esc/F1/q", label: "close help", visible: true},
	}, n.theme.Palette(), n.width)
}

func (n *Notebook) resizeSharedHelp() {
	if n.shared == nil || n.shared.help == nil {
		return
	}
	h := n.shared.help
	offset := h.viewport.YOffset
	h.width, h.height = n.width, n.height
	h.viewport.Width = max(1, n.width-4)
	h.viewport.Height = max(1, n.height-viewHeaderRows-viewHeight(n.sharedHelpFooter()))
	h.viewport.SetContent(ansi.Wrap(displayText(h.content), h.viewport.Width, ""))
	h.viewport.SetYOffset(offset)
}

func (n *Notebook) sharedKeyBindings() []helpEntry {
	return []helpEntry{
		{keys: "←/h →/l", label: "earlier/later saved date (stay in shared updates)", visible: true},
		{keys: "↑/k ↓/j", label: "scroll one line", visible: true},
		{keys: "PgUp/b PgDn/f", label: "scroll a full page", visible: true},
		{keys: "Ctrl+U/D", label: "scroll half a page", visible: true},
		{keys: "g/G", label: "top/bottom of shared updates", visible: true},
		{keys: "Esc/c", label: "return to personal note", visible: true},
		{keys: "Ctrl+L", label: "lock session", visible: n.canRelock},
		{keys: "Ctrl+T", label: "cycle theme", visible: true},
		{keys: "?/F1", label: "toggle key help", visible: true},
		{keys: "q/Ctrl+C", label: "quit application", visible: true},
	}
}

func (n *Notebook) updateShared(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Paste || (msg.Type == tea.KeyRunes && (msg.Alt || len(msg.Runes) != 1)) {
		return n, nil
	}
	if n.shared.help != nil {
		if n.shared.help.update(msg) {
			n.shared.help = nil
		}
		return n, nil
	}
	switch msg.String() {
	case "q", "ctrl+c":
		n.quitting = true
		return n, nil
	case "esc", "c":
		n.closeShared()
		return n, nil
	}
	if helpRequested(msg, true) {
		n.shared.help = newKeyHelp("Shared updates", n.sharedKeyBindings(), n.sharedStatus(), n.width, n.height)
		n.resizeSharedHelp()
		return n, nil
	}
	v := &n.shared.viewport
	switch msg.String() {
	case "ctrl+t":
		n.theme.Cycle()
	case "left", "h":
		if n.current < len(n.pages)-1 {
			n.SetCurrentDate(n.pages[n.current+1])
		}
	case "right", "l":
		if n.current > 0 {
			n.SetCurrentDate(n.pages[n.current-1])
		}
	case "up", "k":
		v.LineUp(1)
	case "down", "j":
		v.LineDown(1)
	case "pgup", "b":
		v.SetYOffset(v.YOffset - v.Height)
	case "pgdown", "f":
		v.SetYOffset(v.YOffset + v.Height)
	case "ctrl+u":
		v.SetYOffset(v.YOffset - max(1, v.Height/2))
	case "ctrl+d":
		v.SetYOffset(v.YOffset + max(1, v.Height/2))
	case "g", "home":
		v.GotoTop()
	case "G", "end":
		v.GotoBottom()
	}
	// Every other key is consumed, notably editing, unlock, date prompts,
	// templates and publication shortcuts.
	return n, nil
}

func (n *Notebook) sharedView() string {
	if h := n.shared.help; h != nil {
		footer := n.sharedHelpFooter()
		minimum := viewHeaderRows + viewHeight(footer) + 2
		if n.width < 24 || n.height < minimum {
			return smallTerminal("Shared updates · Help", n.width, n.height, 24, minimum, "F1: close help · Esc/c: note · q: quit")
		}
		return screenView(h.title, n.sharedStatus(), insetNotebookBody(h.viewport.View()), footer, n.theme.Palette(), n.width, n.height)
	}
	footer := n.sharedFooter()
	minHeight := viewHeaderRows + viewHeight(footer) + 2
	// Keep both provenance sentences visible; never truncate the policy into
	// an ambiguous status at narrow sizes.
	if n.width < 48 || n.height < minHeight {
		return smallTerminal("Shared updates · Read-only · Not encrypted", n.width, n.height, 48, minHeight, "Esc/c: note · q: quit · F1: help")
	}
	return screenView(n.sharedTitle(), n.sharedStatus(), n.shared.viewport.View(), footer, n.theme.Palette(), n.width, n.height)
}
