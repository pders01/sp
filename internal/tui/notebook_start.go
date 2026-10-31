package tui

import "strings"

// Dates are ISO strings sorted descending. Prefer today, then the newest past
// page, then the nearest future page. Startup never inserts or persists a page.
func notebookStartIndex(pages []string, today string) int {
	for i, date := range pages {
		if date <= today {
			return i
		}
	}
	return max(0, len(pages)-1)
}

func (n *Notebook) announceStart() {
	date := n.GetCurrentPage()
	switch {
	case date == "":
	case date < n.today:
		n.theme.SetStatus("No page for today; opened the newest earlier page.", 0)
	case date > n.today:
		n.theme.SetStatus("Only future pages saved; opened the nearest future date.", 0)
	}
}

// editDate creates only an in-memory selection after an explicit edit action.
// Opening/closing the empty viewer never manufactures a saved note.
func (n *Notebook) editDate() string {
	if n.GetCurrentPage() == "" {
		n.AddPage(n.today)
		n.SetCurrentDate(n.today)
	}
	return n.GetCurrentPage()
}

func (n *Notebook) emptyView() string {
	palette := n.theme.Palette()
	back := "quit"
	if n.canPop {
		back = "back"
	}
	controls := wrappedHelp([]helpEntry{
		{keys: "Enter/e/i", label: "create today", visible: true},
		{keys: "u", label: "unlock", visible: n.privacyEnabled && !privacyIsUnlocked(n.privacyState)},
		{keys: "Ctrl+L", label: "lock", visible: n.canRelock && privacyIsUnlocked(n.privacyState)},
		{keys: "d", label: "date", visible: n.dateJumpAvailable},
		{keys: "?/F1", label: "help", visible: true},
		{keys: "Esc", label: back, visible: true},
		{keys: "q", label: "quit", visible: true},
	}, n.width)
	view := strings.Join([]string{
		viewHeader("Notebook · "+n.today+privacyBadge(n.privacyEnabled, n.privacyState), n.theme.StatusText(), palette, n.width),
		palette.MutedText.Render("No scratchpad pages found."), controls,
	}, "\n")
	minHeight := viewHeight(view)
	if n.width < 24 || n.height < minHeight {
		return smallTerminal("Notebook · "+n.today, n.width, n.height, 24, minHeight, "Enter/e: create · F1: help · q: quit")
	}
	return fitView(view, n.width, n.height)
}
