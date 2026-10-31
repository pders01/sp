package tui

// Capture the date actually rendered: the selection may already have moved or
// changed index after insertion. Never replace a remembered offset with the
// clamp from a transient locked placeholder or a failed private load.
func (n *Notebook) captureReadingPosition() {
	if n.viewportDate != "" && n.viewportReady {
		n.positions[n.viewportDate] = n.viewport.YOffset
	}
}

func (n *Notebook) setReadingContent(content string) {
	date := n.GetCurrentPage()
	ready := date != ""
	if n.privacyEnabled {
		ready = ready && n.privatePage != nil && n.privateDate == date && n.privatePage.LoadError == "" && !n.privatePage.Locked
	}
	n.viewport.SetContent(content)
	n.layoutViewport()
	n.viewport.SetYOffset(n.positions[date])
	n.viewportDate, n.viewportReady = date, ready
	if ready {
		// Content edits and reflow can shorten a document. Remember the actual
		// clamped position only once the target page has finished loading.
		n.positions[date] = n.viewport.YOffset
	}
}
