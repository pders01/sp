package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// The application owns one appearance subscription. Views retain independent
// status banners, but preference/resolution is session-wide and never persisted.
func (a *App) syncAppearance(acknowledge bool) {
	a.cal.theme.applyResolved()
	a.nb.theme.SetPref(a.cal.theme.Pref())
	a.nb.updateViewportContent()
	if acknowledge {
		a.SetNotice(fmt.Sprintf("Theme: %s (%s); session only, configuration unchanged.", a.cal.theme.Pref(), a.cal.theme.Style()))
	}
}

func (a *App) cycleAppearance() {
	a.cal.theme.SetPref(nextThemePref(a.cal.theme.Pref()))
	a.syncAppearance(true)
}

func (a *App) appearanceChanged() (tea.Model, tea.Cmd) {
	a.syncAppearance(true)
	return a, a.cal.theme.wait()
}
