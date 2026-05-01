package tui

import (
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

const AppName = "sp"

// Palette holds the colors and pre-built lipgloss styles for one theme.
// Each rendered view pulls the active palette from its themeWatcher so a
// theme swap (Ctrl+T, SIGUSR1, macOS appearance change) instantly
// recolors every cell on the next View() call.
type Palette struct {
	// Raw colors. Exposed so views can compose ad-hoc styles.
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Highlight lipgloss.Color
	Text      lipgloss.Color
	Muted     lipgloss.Color
	Error     lipgloss.Color
	// CursorFg is the foreground color used inside the highlight-tinted
	// cursor cell so the day number stays legible against Highlight.
	CursorFg lipgloss.Color

	// Pre-built styles. Computed once per palette construction; views
	// access these instead of building styles inline.
	Header        lipgloss.Style
	Title         lipgloss.Style
	Help          lipgloss.Style
	Separator     lipgloss.Style
	MutedText     lipgloss.Style
	SelectedDate  lipgloss.Style
	ErrorMessage  lipgloss.Style
	WeekdayHeader lipgloss.Style
}

// darkPalette is the dark-mode brand palette aligned with fwrd's
// time-progression theme.
func darkPalette() Palette {
	p := Palette{
		Primary:   lipgloss.Color("#FF6B6B"),
		Secondary: lipgloss.Color("#4ECDC4"),
		Accent:    lipgloss.Color("#95E1D3"),
		Highlight: lipgloss.Color("#FFE66D"),
		Text:      lipgloss.Color("#EAEAEA"),
		Muted:     lipgloss.Color("#94A3B8"),
		Error:     lipgloss.Color("#EF4444"),
		CursorFg:  lipgloss.Color("#1A1A2E"),
	}
	return withStyles(p)
}

// lightPalette is the light-mode counterpart. Hues shift to deeper,
// higher-contrast variants that read on a light terminal background.
func lightPalette() Palette {
	p := Palette{
		Primary:   lipgloss.Color("#DC2626"),
		Secondary: lipgloss.Color("#0F766E"),
		Accent:    lipgloss.Color("#0D9488"),
		Highlight: lipgloss.Color("#B45309"),
		Text:      lipgloss.Color("#1A1A2E"),
		Muted:     lipgloss.Color("#64748B"),
		Error:     lipgloss.Color("#B91C1C"),
		CursorFg:  lipgloss.Color("#FFFBEB"),
	}
	return withStyles(p)
}

func withStyles(p Palette) Palette {
	p.Header = lipgloss.NewStyle().Foreground(p.Secondary).Bold(true)
	p.Title = lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	p.Help = lipgloss.NewStyle().Foreground(p.Muted)
	p.Separator = lipgloss.NewStyle().Foreground(p.Muted)
	p.MutedText = lipgloss.NewStyle().Foreground(p.Muted)
	p.SelectedDate = lipgloss.NewStyle().Foreground(p.Highlight).Bold(true)
	p.ErrorMessage = lipgloss.NewStyle().Foreground(p.Error).Bold(true)
	p.WeekdayHeader = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	return p
}

// paletteFor returns the palette matching a resolved glamour style.
// Light style maps to lightPalette; everything else (dark, NoTTY,
// custom values) falls back to darkPalette so the app stays readable.
func paletteFor(style string) Palette {
	if style == styles.LightStyle {
		return lightPalette()
	}
	return darkPalette()
}
