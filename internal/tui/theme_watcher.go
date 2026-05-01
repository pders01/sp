package tui

import (
	"context"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// themeChangedMsg is dispatched whenever an external signal source
// (SIGUSR1, macOS plist watcher) or the in-session toggle has fired.
type themeChangedMsg struct{}

// statusExpireMsg clears a transient status banner.
type statusExpireMsg struct{}

// themeWatcher centralizes the theme-change plumbing shared by every
// view that wants to react to OS / signal-driven appearance shifts.
// Each model embeds one instance and forwards themeChangedMsg from its
// own Update loop.
type themeWatcher struct {
	pref         string
	resolved     string
	events       chan struct{}
	watchCancel  context.CancelFunc
	watchWG      sync.WaitGroup
	statusText   string
	statusExpiry time.Time
}

// newThemeWatcher returns a watcher seeded with the given preference.
// pref is normalized via resolveGlamourStyle for the initial style.
func newThemeWatcher(pref string) *themeWatcher {
	if pref == "" {
		pref = ThemePrefAuto
	}
	return &themeWatcher{
		pref:     pref,
		resolved: resolveGlamourStyle(pref),
		events:   make(chan struct{}, 1),
	}
}

// SetPref overrides the preference and re-resolves the style.
func (w *themeWatcher) SetPref(pref string) {
	if pref == "" {
		pref = ThemePrefAuto
	}
	w.pref = pref
	w.resolved = resolveGlamourStyle(pref)
}

// Pref returns the current user preference label.
func (w *themeWatcher) Pref() string { return w.pref }

// Style returns the resolved glamour style label.
func (w *themeWatcher) Style() string { return w.resolved }

// Palette returns the brand palette matching the resolved style.
func (w *themeWatcher) Palette() Palette { return paletteFor(w.resolved) }

// Cycle advances the preference auto -> light -> dark -> auto and
// schedules a re-resolve through the event channel.
func (w *themeWatcher) Cycle() {
	w.pref = nextThemePref(w.pref)
	w.signal()
}

// applyResolved re-runs resolution against the current preference and
// returns true when the resolved style actually changed.
func (w *themeWatcher) applyResolved() bool {
	next := resolveGlamourStyle(w.pref)
	if next == w.resolved {
		return false
	}
	w.resolved = next
	return true
}

// signal wakes the watcher reader without blocking. The channel is
// buffered to one slot so coalesced bursts collapse into a single
// re-resolve.
func (w *themeWatcher) signal() {
	if w.events == nil {
		return
	}
	select {
	case w.events <- struct{}{}:
	default:
	}
}

// wait returns a tea.Cmd that blocks on the next theme event and emits
// themeChangedMsg. Callers re-arm the cmd after each event so the
// watcher behaves like a long-lived subscription.
func (w *themeWatcher) wait() tea.Cmd {
	return func() tea.Msg {
		_, ok := <-w.events
		if !ok {
			return nil
		}
		return themeChangedMsg{}
	}
}

// start spawns the SIGUSR1 listener and the platform-specific system
// theme watcher, both feeding signal().
func (w *themeWatcher) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.watchCancel = cancel

	w.watchWG.Add(1)
	go func() {
		defer w.watchWG.Done()
		watchThemeSignal(ctx, w.signal)
	}()

	if err := watchSystemTheme(ctx, &w.watchWG, w.signal); err != nil {
		// Watcher unavailable on this platform; signal + Cycle still work.
		_ = err
	}
}

// stop cancels watchers and waits for goroutines to drain. Safe to
// call multiple times.
func (w *themeWatcher) stop() {
	if w.watchCancel != nil {
		w.watchCancel()
		w.watchCancel = nil
	}
	w.watchWG.Wait()
}

// SetStatus stores a transient banner with a TTL; pair with expireCmd
// to clear it from the model's Update loop.
func (w *themeWatcher) SetStatus(msg string, ttl time.Duration) {
	w.statusText = msg
	w.statusExpiry = time.Now().Add(ttl)
}

// StatusText returns the active banner string, or "" when none.
func (w *themeWatcher) StatusText() string { return w.statusText }

// expireStatusCmd returns a tea.Cmd that emits statusExpireMsg after ttl.
func (w *themeWatcher) expireStatusCmd(ttl time.Duration) tea.Cmd {
	return tea.Tick(ttl, func(time.Time) tea.Msg { return statusExpireMsg{} })
}

// HandleStatusExpire clears the banner if its TTL has elapsed.
func (w *themeWatcher) HandleStatusExpire() {
	if !w.statusExpiry.IsZero() && time.Now().After(w.statusExpiry) {
		w.statusText = ""
		w.statusExpiry = time.Time{}
	}
}
