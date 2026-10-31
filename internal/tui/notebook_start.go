package tui

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
	footer := screenFooter([]helpEntry{
		{keys: "Enter/e", label: "create today", visible: true},
		{keys: "Esc", label: back, visible: true},
		{keys: "?/F1", label: "help", visible: true},
	}, palette, n.width)
	title := "Notebook · " + n.today + privacyBadge(n.privacyEnabled, n.privacyState)
	body := "Start today's note\n\nNo personal note yet. Press Enter or e to begin."
	minHeight := viewHeight(viewHeader(title, n.theme.StatusText(), palette, n.width)) + viewHeight(footer) + viewHeight(body)
	if n.width < 24 || n.height < minHeight {
		return smallTerminal("Notebook · "+n.today, n.width, n.height, 24, minHeight, "Enter/e: create · F1: help · q: quit")
	}
	return screenView(title, n.theme.StatusText(), insetNotebookBody(body), footer, palette, n.width, n.height)
}
