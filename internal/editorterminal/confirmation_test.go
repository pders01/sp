package editorterminal

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
)

type knownRetryEditor struct {
	*fakeEditor
	knownCalls int
}

func (e *knownRetryEditor) RecoverKnown(context.Context, bool) (privateedit.Result, error) {
	e.knownCalls++
	return e.Retained().Result, editorcore.ErrRemote
}

func TestTerminalKnownRetryRenewsConsentAndKeepsModalOwnership(t *testing.T) {
	for _, stopping := range []bool{false, true} {
		m := fixtureModel()
		e := &knownRetryEditor{fakeEditor: m.editor.(*fakeEditor)}
		m.editor = e
		m.phase = "error"
		m.lastOp = "known"
		m.stopping = stopping
		m.shutdownFailed = stopping
		if apply(m, physical('r')) != nil || m.phase != "known-confirm" || e.knownCalls != 0 {
			t.Fatal("retry bypassed renewed possible-loss consent")
		}
		for _, event := range []tea.Msg{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true}, tea.WindowSizeMsg{Width: 100, Height: 30}} {
			if apply(m, event) != nil || m.phase != "known-confirm" || e.knownCalls != 0 || e.cancels != 0 {
				t.Fatal("incidental event stole confirmation or authorized release")
			}
		}
		if stopping {
			apply(m, stopMsg{"signal"})
			if m.phase != "known-confirm" || m.busy {
				t.Fatal("repeat signal bypassed failed-shutdown consent")
			}
		}
		runJob(t, m, apply(m, physical('y')))
		if e.knownCalls != 1 || m.phase != "error" || m.lastOp != "known" {
			t.Fatal("explicit known-copy attempt not retained on error")
		}
		apply(m, physical('r'))
		if m.phase != "known-confirm" || e.knownCalls != 1 {
			t.Fatal("failed attempt reused old consent")
		}
		apply(m, physical('n'))
		if m.phase != "error" || e.knownCalls != 1 {
			t.Fatal("declining retry changed lifecycle")
		}
	}
}

func TestTerminalRelockCanLeaveCancellationConfirmation(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	m.phase = "confirm"
	m.actionKind = "cancel"
	e.r.Editor.Mirror.Frozen = true
	runJob(t, m, apply(m, tea.KeyMsg{Type: tea.KeyCtrlL}))
	if !e.r.Editor.Closed || e.cancels != 1 || !m.relock {
		t.Fatal("modal guard blocked explicit encrypted relock")
	}
}

func TestTerminalConfirmationWarningsFitMinimumSupportedGeometry(t *testing.T) {
	for _, phase := range []string{"confirm", "known-confirm"} {
		for _, width := range []int{40, 41, 80} {
			m := fixtureModel()
			m.phase = phase
			m.width = width
			m.height = 10
			view := m.View()
			lines := strings.Split(view, "\n")
			if len(lines) > m.height || strings.Contains(view, testPrivate) {
				t.Fatal("confirmation scrolled or exposed the draft")
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > width-1 {
					t.Fatal("confirmation warning/choices clipped")
				}
			}
			prose := strings.Join(strings.Fields(view), " ") // App-owned prose only.
			for _, required := range []string{"Physical Y: recover encrypted + close", "Physical N:", "Pasted text never confirms."} {
				if !strings.Contains(prose, required) {
					t.Fatal("physical choices hidden")
				}
			}
			if phase == "known-confirm" && (!strings.Contains(prose, "Latest edits may be missing.") || !strings.Contains(prose, "Missing edits can be lost.")) {
				t.Fatal("possible-loss warning hidden")
			}
		}
	}
}
