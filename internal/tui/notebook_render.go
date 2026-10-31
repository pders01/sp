package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
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
	text = safeMarkdownDisplay(text)
	renderer, err := glamour.NewTermRenderer(glamour.WithStandardStyle(n.theme.Style()), glamour.WithWordWrap(max(n.width-4, 20)))
	if err == nil {
		if rendered, renderErr := renderer.Render(text); renderErr == nil {
			text = rendered
		}
	}
	return ansi.Wrap(displayText(text), max(n.width, 20), "")
}

// Every area gets its own Markdown renderer. A fence/HTML construct in private
// writing can never consume the shared area's UI label or change its provenance.
func (n *Notebook) renderPrivateAreas(page PrivatePage) string {
	palette := n.theme.Palette()
	var areas []string
	switch {
	case page.LoadError != "":
		areas = append(areas, "Private note unavailable: "+safeMarkdownDisplay(page.LoadError))
	case page.HasPrivate || page.Private != "":
		state, body := "unlocked", ""
		switch {
		case page.Locked:
			state, body = "locked", "Locked. Press u to unlock private notes."
		case page.Private == "":
			body = "(Private note is empty.)"
		default:
			body = n.renderMarkdown(page.Private)
		}
		if !n.privacyEnabled && !page.Locked {
			state = "plaintext"
		}
		areas = append(areas, palette.Header.Render("Private notes · "+state), body)
	}
	if strings.TrimSpace(page.Shared) != "" {
		areas = append(areas, palette.Header.Render("Agent contributions · shared (self-reported)"), n.renderMarkdown(page.Shared))
	}
	if len(areas) == 0 {
		return "No entry for this day. Press e to create a private note."
	}
	return strings.Join(areas, "\n")
}
