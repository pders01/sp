package tui

import "github.com/charmbracelet/x/ansi"

// truncate bounds one line by terminal cells without splitting ANSI sequences,
// wide glyphs, or grapheme clusters. No columns means no visible content.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}
