package tui

import (
	"fmt"
	"strings"
	"unicode"
)

// Note-controlled terminal controls cannot erase/spoof UI provenance labels.
// This is a display projection only; stored text/editor bytes remain untouched.
func safeMarkdownDisplay(text string) string {
	var out strings.Builder
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') || r == '\u061c' || r == '\u200e' || r == '\u200f' || r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069' {
			fmt.Fprintf(&out, "\\u{%X}", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

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
