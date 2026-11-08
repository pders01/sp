package editorterminal

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestTerminalNeverStartedInputRemainsOwnedAfterExit(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	cmd := apply(m, physical('x'))
	m.jobs.stopAndWait()
	if cmd() == nil && len(e.typed) == 0 {
		pending := unsettledInput(m, m.jobs.lastResult())
		if len(pending) != 1 || pending[0].Text != "x" || pending[0].Partial {
			t.Fatal("unstarted input disappeared or became ambiguous")
		}
	} else {
		t.Fatal("command started after exit")
	}
}

func TestTerminalLateCompletedInputSettlesExactlyOnce(t *testing.T) {
	for _, fail := range []bool{false, true} {
		m := fixtureModel()
		if fail {
			m.editor = failedInputEditor{m.editor.(*fakeEditor)}
		}
		cmd := apply(m, physical('x'))
		result := cmd().(jobMsg) // Completed, deliberately never delivered to Update.
		m.jobs.stopAndWait()
		pending := unsettledInput(m, m.jobs.lastResult())
		if fail {
			if len(pending) != 1 || pending[0].Text != "x" || !pending[0].Partial {
				t.Fatal("late failure lost/duplicated input")
			}
		} else if len(pending) != 0 || result.err != nil {
			t.Fatal("settled input was retained for replay")
		}
	}
}

func TestTerminalFaultWhileBusyCannotStartAnotherOperation(t *testing.T) {
	m := fixtureModel()
	m.busy = true
	apply(m, actionMsg{epoch: m.epoch, err: editorcore.ErrProtocol})
	for _, key := range []rune{'n', 'k', 'y', 'r', 'c'} {
		if apply(m, physical(key)) != nil || !m.busy {
			t.Fatal("fault prompt bypassed active operation ownership")
		}
	}
	if m.start("known", Input{}) != nil {
		t.Fatal("start bypassed busy admission")
	}
}

func TestOwnedJobsRejectConcurrentExecution(t *testing.T) {
	j := &jobOwner{accepting: true}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	first := j.command(func() jobMsg { close(started); <-release; return jobMsg{kind: "first"} })
	go func() { first(); close(done) }()
	<-started
	defer func() { close(release); <-done; j.stopAndWait() }()
	called := false
	if j.command(func() jobMsg { called = true; return jobMsg{kind: "second"} })() != nil || called {
		t.Fatal("concurrent backend operation admitted")
	}
}

func TestTerminalTinyShutdownDisarmsQueuedFinish(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	m.busy = true
	apply(m, tea.KeyMsg{Type: tea.KeyF2})
	apply(m, tea.WindowSizeMsg{Width: 2, Height: 2})
	apply(m, stopMsg{"signal"})
	m.busy = false
	runJob(t, m, m.next())
	if len(e.keys) != 1 || e.keys[0] != "Cancel" {
		t.Fatal("tiny shutdown stalled or preserved save permission")
	}
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "cancel"}))
	if !e.Retained().Editor.Closed || e.finishes != 0 {
		t.Fatal("tiny shutdown did not encrypt/cancel")
	}
}

func TestTerminalFailedShutdownRecoveryParksUntilExplicitRetry(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	m.stopping = true
	m.actionKind = "cancel"
	e.r.Editor.Mirror.Frozen = true
	e.cancelErr = editorcore.ErrRemote
	runJob(t, m, m.next())
	if m.phase != "error" || !m.shutdownFailed || e.cancels != 1 {
		t.Fatal("failed recovery not parked")
	}
	for _, event := range []tea.Msg{tea.WindowSizeMsg{Width: 100, Height: 30}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true}, stopMsg{"signal"}} {
		apply(m, event)
		if m.busy || m.phase != "error" || e.cancels != 1 {
			t.Fatal("incidental event retried failed recovery")
		}
	}
	if apply(m, physical('n')) != nil {
		t.Fatal("shutdown failure silently resumed editing")
	}
	runJob(t, m, apply(m, physical('r')))
	if e.cancels != 2 || m.phase != "error" {
		t.Fatal("explicit recovery retry missing")
	}
}

func TestTerminalConflictRetainsEarlierPublicationTruth(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	e.r.Result.Published = true
	e.r.Result.DurabilityUnconfirmed = true
	e.finishErr = scratchpad.ErrConflict
	m.actionKind = "finish"
	runJob(t, m, m.next())
	if !strings.Contains(m.status, "Published; durability unconfirmed") || strings.Contains(m.status, "not saved") {
		t.Fatal("latest conflict negated prior publication")
	}
}
