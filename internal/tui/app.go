package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
)

// AppMode picks the initial view the router renders. ModeCalendar is
// the only mode that supports back-navigation from the notebook; in
// ModeNotebook the notebook quits the program when popped.
type AppMode int

const (
	// ModeCalendar opens on the calendar; Enter on a day drills into
	// the notebook, and pressing Esc/Backspace in the notebook pops back.
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
	cal              *Calendar
	nb               *Notebook
	mode             AppMode
	canPop           bool
	quitting         bool
	templates        []DayTemplate
	appliedTemplates map[string]map[string]bool
	applyTemplates   TemplateApplier
	templateResolver func() error
	templateChooser  *templateChooser
	privacy          *privacyState
	dateJump         *dateJumpPrompt
	recoveries       []scratchpad.RecoveryReference
	recoveryWarnings map[string]string
	recordRecovery   func(date, path string) error
	forgetRecovery   func(date, path string) error
}

// NewApp builds the router around an already-configured calendar and
// notebook. canPop is true only when starting in ModeCalendar — the
// notebook needs the calendar behind it to back-nav into.
func NewApp(cal *Calendar, nb *Notebook, mode AppMode) *App {
	app := &App{
		cal:              cal,
		nb:               nb,
		mode:             mode,
		canPop:           mode == ModeCalendar,
		appliedTemplates: make(map[string]map[string]bool),
	}
	nb.canPop = app.canPop
	cal.dateJumpAvailable, nb.dateJumpAvailable = true, true
	if mode == ModeNotebook {
		nb.announceStart()
	}
	nb.drafts = cal.drafts // A failed draft follows its date across both views.
	cal.onChange, nb.onChange = app.noteChanged, app.noteChanged
	cal.onLoad, nb.onLoad = app.noteLoaded, app.noteLoaded
	cal.onRecovery, nb.onRecovery = app.trackRecovery, app.trackRecovery
	cal.onConsumed, nb.onConsumed = app.consumedRecovery, app.consumedRecovery
	nb.theme.SetPref(cal.theme.Pref())
	return app
}

// noteChanged projects a persisted snapshot through every editing path. Private
// text must never enter the calendar or the notebook's shared content caches.
func (a *App) noteChanged(date, content string) {
	if a.privacy != nil {
		a.nb.AddPage(date)
		a.cal.hasData[date] = true
		a.cal.privateDates[date] = true
		a.refreshPublicPage(date)
		if a.mode == ModeNotebook && a.nb.GetCurrentPage() == date {
			a.refreshPrivatePage()
		}
		return
	}
	a.nb.SetPageContent(date, content)
	preview := content
	if preview == "" {
		preview = a.nb.sharedContents[date]
	}
	a.cal.MarkDate(date, preview)
}

// noteLoaded refreshes an existing plaintext snapshot without manufacturing an
// entry when an editor merely opens a new, empty date.
func (a *App) noteLoaded(date, content string) {
	if a.privacy == nil && (a.cal.HasData(date) || content != "") {
		a.noteChanged(date, content)
	}
}

// SetSharedContents projects shared contributions alongside legacy private
// writing without changing the text handed to the external editor.
func (a *App) SetSharedContents(contents map[string]string) {
	a.nb.sharedContents = make(map[string]string, len(contents))
	for date, content := range contents {
		a.nb.sharedContents[date] = content
		if a.cal.contents[date] == "" && content != "" {
			a.cal.MarkDate(date, content)
		}
	}
	a.nb.updateViewportContent()
}

// SetPersonalPresence supplies public presence metadata for plaintext pages.
// A saved empty note is not an absent note or a shared-only day.
func (a *App) SetPersonalPresence(dates map[string]bool) {
	a.nb.personalDates = make(map[string]bool, len(dates))
	for date, present := range dates {
		a.nb.personalDates[date] = present
	}
	a.nb.updateViewportContent()
}

// Init starts the one application-wide appearance subscription. Both views
// share its preference/resolution without sharing transient status banners.
func (a *App) Init() tea.Cmd {
	return a.cal.Init()
}

// Close releases resources held by the sub-views. Safe to call after
// the program exits.
func (a *App) Close() {
	a.closeTemplateWork()
	if err := a.closePrivacy(); err != nil {
		if a.recoveryWarnings == nil {
			a.recoveryWarnings = make(map[string]string)
		}
		a.recoveryWarnings["privacy-session"] = "Private draft recovery failed during shutdown: " + err.Error()
	}
	a.cal.Close()
	a.nb.Close()
}

// Update routes the message to the active view, then inspects the
// view's state machine for drill / pop / quit signals and reacts.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := a.routeTerminalInput(msg); handled {
		return a, cmd
	}
	if _, changed := msg.(themeChangedMsg); changed {
		return a.appearanceChanged()
	}
	if a.dateJump != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			a.cal.Update(size)
			a.nb.Update(size)
			a.dateJump.resize(size.Width, size.Height)
			return a, nil
		}
		_, key := msg.(tea.KeyMsg)
		_, paste := privatePasteText(msg)
		if key || paste {
			return a.updateDateJump(msg)
		}
	}
	if a.privacy != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			a.cal.Update(size)
			a.nb.Update(size)
		}
		if handled, cmd := a.updatePrivacySession(msg); handled {
			return a, cmd
		}
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlT && a.privacy != nil && a.privacy.editor != nil {
		e := a.privacy.editor
		if e.keyHelp == nil && e.review == nil && !e.input.Finding() {
			a.cycleAppearance()
			return a, nil
		}
	}
	// Shared updates are a read-only secondary context, not an editing route.
	// Give it keys before unlock/edit/template/date shortcuts can see them.
	if a.mode == ModeNotebook && a.nb.sharedActive() {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			a.cal.Update(size)
			a.nb.Update(size)
			return a, nil
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			if key.Type == tea.KeyCtrlT && a.nb.keyHelp == nil && a.nb.shared.help == nil {
				a.cycleAppearance()
				return a, nil
			}
			return a.updateNotebook(msg)
		}
		if _, paste := privatePasteText(msg); paste {
			return a, nil
		}
	}
	// Read-only help must own its keys before privacy shortcuts or the
	// application-level template shortcut can act on the underlying view.
	if _, key := msg.(tea.KeyMsg); key && a.templateChooser == nil &&
		(a.privacy == nil || (a.privacy.editor == nil && a.privacy.prompt == nil)) {
		if a.mode == ModeCalendar && (a.cal.keyHelp != nil || a.cal.review != nil) {
			return a.updateCalendar(msg)
		}
		if a.mode == ModeNotebook && (a.nb.keyHelp != nil || a.nb.review != nil) {
			return a.updateNotebook(msg)
		}
	}
	if a.privacy != nil && a.templateChooser == nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			a.cal.Update(size)
			a.nb.Update(size)
		}
		if handled, cmd := a.updatePrivacy(msg); handled {
			return a, cmd
		}
	}
	if applied, ok := msg.(templateAppliedMsg); ok {
		if a.quitting {
			return a, tea.Quit
		}
		return a.finishTemplateApply(applied)
	}
	if a.templateChooser != nil {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			a.cal.Update(ws)
			a.nb.Update(ws)
			if a.templateChooser.keyHelp != nil {
				a.templateChooser.keyHelp.resize(a.cal.width, a.cal.height)
			}
			return a, nil
		}
		return a.updateTemplateChooser(msg)
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlT {
		a.cycleAppearance()
		return a, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyRunes && !key.Alt && len(key.Runes) == 1 && key.Runes[0] == 'd' {
		current := a.privateDate()
		if current == "" {
			current = a.nb.today
		}
		a.dateJump = newDateJumpPrompt(current, a.cal.width, a.cal.height)
		return a, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "a" && a.openTemplateChooser() {
		return a, nil
	}

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
	a.refreshPrivatePage()
}

func (a *App) updateNotebook(msg tea.Msg) (tea.Model, tea.Cmd) {
	previousDate := a.nb.GetCurrentPage()
	_, cmd := a.nb.Update(msg)
	if a.nb.GetCurrentPage() != previousDate {
		a.refreshPrivatePage()
	}

	switch {
	case a.nb.IsPopping():
		// Saved state is already synchronized at publication. Back navigation
		// only moves focus; visiting a date must not manufacture a saved entry.
		date := a.nb.GetCurrentPage()
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
	if a.dateJump != nil {
		palette := a.cal.theme.Palette()
		if a.mode == ModeNotebook {
			palette = a.nb.theme.Palette()
		}
		return a.dateJump.view(palette)
	}
	if view, active := a.privacyView(); active {
		return view
	}
	if a.templateChooser != nil {
		return a.renderTemplateChooser()
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
