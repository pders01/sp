package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// AppMode picks the initial view the router renders. ModeCalendar is
// the only mode that supports back-navigation from the notebook; in
// ModeNotebook the notebook quits the program when popped.
type AppMode int

const (
	// ModeCalendar opens on the calendar; Enter on a day drills into
	// the notebook, and pressing Esc/q in the notebook pops back.
	ModeCalendar AppMode = iota
	// ModeNotebook opens directly on the notebook with no calendar
	// behind it; Esc/q quits.
	ModeNotebook
)

// App wraps the calendar and notebook into a single tea.Model so the
// two views can share one terminal session. Drilling and popping are
// just internal mode flips — no nested tea programs, no terminal
// re-init flicker between views.
type App struct {
	cal      *Calendar
	nb       *Notebook
	mode     AppMode
	canPop   bool
	quitting bool
}

// NewApp builds the router around an already-configured calendar and
// notebook. canPop is true only when starting in ModeCalendar — the
// notebook needs the calendar behind it to back-nav into.
func NewApp(cal *Calendar, nb *Notebook, mode AppMode) *App {
	return &App{
		cal:    cal,
		nb:     nb,
		mode:   mode,
		canPop: mode == ModeCalendar,
	}
}

// Init kicks off both sub-views' watchers up front. They only emit
// events when something signals them, so running both is cheap and
// avoids a re-init delay when popping back to the calendar.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.cal.Init(), a.nb.Init())
}

// Close releases resources held by the sub-views. Safe to call after
// the program exits.
func (a *App) Close() {
	a.cal.Close()
	a.nb.Close()
}

// Update routes the message to the active view, then inspects the
// view's state machine for drill / pop / quit signals and reacts.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if a.mode == ModeNotebook || a.mode == ModeCalendar {
		// Always forward window-size to both views so the inactive one
		// is rendered correctly the moment we swap.
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			a.cal.Update(ws)
			a.nb.Update(ws)
			return a, nil
		}
	}

	switch a.mode {
	case ModeCalendar:
		return a.updateCalendar(msg)
	case ModeNotebook:
		return a.updateNotebook(msg)
	}
	return a, nil
}

func (a *App) updateCalendar(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := a.cal.Update(msg)

	if a.cal.quitting {
		a.quitting = true
		return a, tea.Quit
	}
	if d := a.cal.GetSelectedDate(); d != "" {
		// Direct-edit (e key) is handled inline by the calendar via
		// tea.ExecProcess; the editor wiring runs the saver and clears
		// state on done. If we still see a directEdit selection here,
		// no editor was wired — fall through to quit so the bare-flow
		// orchestrator can run the editor itself.
		if a.cal.IsDirectEdit() {
			a.quitting = true
			return a, tea.Quit
		}
		a.drillToNotebook(d)
		a.cal.ClearSelection()
		return a, nil
	}
	return a, cmd
}

func (a *App) drillToNotebook(date string) {
	a.nb.AddPage(date)
	a.nb.SetCurrentDate(date)
	a.mode = ModeNotebook
}

func (a *App) updateNotebook(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := a.nb.Update(msg)

	switch {
	case a.nb.IsPopping():
		// Esc / Backspace: pop to calendar when one is behind us, else
		// quit. Sync any inline-edit changes into the calendar's data
		// so the cell paints correctly when we return, and move the
		// calendar cursor to wherever the user landed in the notebook
		// so the pop doesn't feel stale.
		date, content := a.nb.CurrentContent()
		if content != "" {
			a.cal.MarkDate(date, extractPreview(content))
		}
		if date != "" {
			a.cal.SetCursor(date)
		}
		a.nb.ClearState()
		if a.canPop {
			a.mode = ModeCalendar
			return a, nil
		}
		a.quitting = true
		return a, tea.Quit
	case a.nb.IsQuitting():
		// q / Ctrl+C: hard quit regardless of mode.
		a.quitting = true
		return a, tea.Quit
	}
	return a, cmd
}

// View renders whichever sub-view is in focus.
func (a *App) View() string {
	if a.quitting {
		return ""
	}
	switch a.mode {
	case ModeNotebook:
		return a.nb.View()
	default:
		return a.cal.View()
	}
}

// Mode returns the active sub-view. Useful for tests.
func (a *App) Mode() AppMode { return a.mode }

// IsQuitting reports whether the program is exiting.
func (a *App) IsQuitting() bool { return a.quitting }
