package tui

import "strings"

func (n *Notebook) renderMarkdown(text string) string {
	// renderDocument owns the two-cell margin; give it the full viewport.
	return renderDocument(text, n.theme.Style(), max(1, n.width))
}

func insetNotebookBody(body string) string {
	return "  " + strings.ReplaceAll(body, "\n", "\n  ")
}

// Only the personal document belongs in the primary viewport. Shared text is
// rendered independently in shared_updates.go, never combined with this buffer.
func (n *Notebook) renderPrivateAreas(page PrivatePage) string {
	switch {
	case page.LoadError != "":
		return insetNotebookBody("Personal note unavailable: " + safeMarkdownDisplay(page.LoadError))
	case page.Locked && page.HasPrivate:
		return insetNotebookBody("Note locked. Press u to unlock.")
	case !page.HasPrivate && page.Private == "":
		return insetNotebookBody("No personal note for this day. Press e to create a note.")
	case page.Locked:
		return insetNotebookBody("Note locked. Press u to unlock.")
	case page.Private == "":
		return insetNotebookBody("This note is empty. Press e to write.")
	default:
		return n.renderMarkdown(page.Private)
	}
}
