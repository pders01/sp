package tui

import "os"

// IconSet holds glyphs used across the TUI. Two backends are supported:
// "nerd" assumes the user's terminal font is patched with Nerd Font glyphs
// (https://www.nerdfonts.com); "unicode" uses geometric Unicode that renders
// in any monospace font. An empty field means render the human-readable
// label without a leading glyph. Selected via SP_ICONS env var.
type IconSet struct {
	Notebook string
	Calendar string
	Article  string
	Empty    string
	Prev     string
	Next     string
	Sep      string
}

var nerdIcons = IconSet{
	Notebook: "",
	Calendar: "",
	Article:  "",
	Empty:    "",
	Prev:     "",
	Next:     "",
	Sep:      "│",
}

// unicodeIcons leaves most fields empty so headers and list items render
// as clean text on terminals without Nerd Fonts. Only purely-geometric
// glyphs that survive any monospace font are kept.
var unicodeIcons = IconSet{
	Prev: "‹",
	Next: "›",
	Sep:  "│",
}

// NewIconSet returns the icon set for the given mode. Unknown modes fall
// back to the unicode set.
func NewIconSet(mode string) IconSet {
	if mode == "nerd" {
		return nerdIcons
	}
	return unicodeIcons
}

// DefaultIconSet returns the icon set selected via the SP_ICONS env var,
// defaulting to unicode when unset.
func DefaultIconSet() IconSet {
	return NewIconSet(os.Getenv("SP_ICONS"))
}

// withIcon prefixes name with glyph + space when glyph is non-empty,
// otherwise returns name unchanged.
func withIcon(glyph, name string) string {
	if glyph == "" {
		return name
	}
	return glyph + " " + name
}
