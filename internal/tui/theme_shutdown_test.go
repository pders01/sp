package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestThemeWaitRetainsItsSubscriptionAcrossClose(t *testing.T) {
	for _, stopFirst := range []bool{true, false} {
		w := newThemeWatcher(ThemePrefLight)
		cmd := w.wait()
		if stopFirst {
			w.stop()
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		if !stopFirst {
			w.stop()
		}
		select {
		case msg := <-done:
			if msg != nil {
				t.Fatal("closed theme subscription emitted an event")
			}
		case <-time.After(time.Second):
			t.Fatal("late theme command was stranded after close")
		}
		if w.wait() != nil {
			t.Fatal("closed watcher created another blocking command")
		}
		w.stop()
	}
}
