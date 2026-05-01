package tui

import "github.com/charmbracelet/lipgloss"

const AppName = "sp"

// Brand palette aligned with fwrd's time-progression theme so the two
// tools feel like part of one toolkit when used side by side.
var (
	PrimaryColor   = lipgloss.Color("#FF6B6B") // coral
	SecondaryColor = lipgloss.Color("#4ECDC4") // teal
	AccentColor    = lipgloss.Color("#95E1D3") // mint
	HighlightColor = lipgloss.Color("#FFE66D") // bright yellow
	TextColor      = lipgloss.Color("#EAEAEA") // soft white
	MutedColor     = lipgloss.Color("#94A3B8") // gray-blue
	ErrorColor     = lipgloss.Color("#EF4444") // red
)

var (
	HeaderStyle = lipgloss.NewStyle().
			Foreground(SecondaryColor).
			Bold(true)

	TitleStyle = lipgloss.NewStyle().
			Foreground(TextColor).
			Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	SeparatorStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	MutedStyle = lipgloss.NewStyle().
			Foreground(MutedColor)

	SelectedDateStyle = lipgloss.NewStyle().
				Foreground(HighlightColor).
				Bold(true)

	ErrorMessageStyle = lipgloss.NewStyle().
				Foreground(ErrorColor).
				Bold(true)
)
