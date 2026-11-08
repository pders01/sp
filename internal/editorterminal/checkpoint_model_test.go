package editorterminal

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/privateedit"
	"github.com/pders01/sp/internal/scratchpad"
)

// Optional checkpoint support lives on this wrapper, not the common fake or the
// required editor interface. All tests drive only headless owned jobs.
type checkpointModelEditor struct {
	*fakeEditor
	saves           [][2]string
	saveErrs        []error
	resumeErr       error
	freezeOnEnter   bool
	watchActionKind string
}

func (e *checkpointModelEditor) Key(ctx context.Context, key string) (int, error) {
	n, err := e.fakeEditor.Key(ctx, key)
	if err == nil && key == "Enter" && e.freezeOnEnter {
		e.mu.Lock()
		e.r.Editor.Mirror.Frozen = true
		e.mu.Unlock()
	}
	return n, err
}
func (e *checkpointModelEditor) WaitAction(context.Context) (editorcore.Action, error) {
	return editorcore.Action{Kind: e.watchActionKind}, nil
}
func (e *checkpointModelEditor) Checkpoint(context.Context) (privateedit.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.r.Editor.Mirror.Frozen {
		return e.r.Result, editorcore.ErrNotFrozen
	}
	e.saves = append(e.saves, [2]string{e.r.Baseline, e.r.Editor.Mirror.Text})
	var err error
	if len(e.saveErrs) > 0 {
		err, e.saveErrs = e.saveErrs[0], e.saveErrs[1:]
	}
	if err == nil || errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
		e.r.Baseline = e.r.Editor.Mirror.Text
		e.r.Result.Published = true
		e.r.Result.DurabilityUnconfirmed = err != nil
	}
	if err != nil {
		return e.r.Result, err
	}
	if e.resumeErr != nil {
		return e.r.Result, e.resumeErr
	}
	e.resumes++
	e.r.Editor.Mirror.Frozen = false
	return e.r.Result, nil
}
func checkpointModel() (*model, *checkpointModelEditor) {
	m := fixtureModel()
	m.forwardEditorControls = true
	e := &checkpointModelEditor{fakeEditor: m.editor.(*fakeEditor)}
	m.editor = e
	return m, e
}

func TestCheckpointTypedCommandKeepsQueuedInputBarrierAndRevokesEpoch(t *testing.T) {
	m, e := checkpointModel()
	e.freezeOnEnter = true
	e.watchActionKind = "cancel"
	oldEpoch := m.epoch
	m.queue = []Input{{Key: "Enter"}, {Text: "must-not-replay-after-w"}}
	if cmd := runJob(t, m, m.next()); cmd != nil || !m.waitingFreeze || m.phase != "waiting" || len(m.queue) != 1 || len(e.typed) != 0 {
		t.Fatal("typed command acknowledgement bypassed action barrier")
	}
	if cmd := apply(m, physical('z')); cmd != nil || len(m.queue) != 1 {
		t.Fatal("input entered draft while save action was in flight")
	}
	cmd := apply(m, actionMsg{epoch: oldEpoch, kind: "save"})
	if cmd == nil || len(m.queue) != 0 || !m.busy || len(e.saves) != 0 {
		t.Fatal("save was not scheduled as a serialized owned job")
	}
	watch := runJob(t, m, cmd)
	if len(e.saves) != 1 || e.finishes != 0 || e.cancels != 0 || e.resumes != 1 || m.epoch != oldEpoch+1 || m.actionKind != "" || m.waitingFreeze || m.phase != "editing" || len(e.typed) != 0 || len(m.queue) != 0 || m.status != "Saved encrypted." {
		t.Fatal("checkpoint closed, replayed trailing input, or failed epoch revocation")
	}
	if watch == nil {
		t.Fatal("new action watch was not installed")
	}
	if msg, ok := watch().(actionMsg); !ok || msg.epoch != m.epoch {
		t.Fatal("resumed action watcher captured the wrong epoch")
	}
	if apply(m, actionMsg{epoch: oldEpoch, kind: "save"}) != nil || m.actionKind != "" || len(e.saves) != 1 {
		t.Fatal("old action watcher authorized another publication")
	}
	if !strings.Contains(m.View(), "Saved encrypted.") || strings.Contains(m.View(), "Editing resumed") {
		t.Fatal("checkpoint did not use only the transient saved message")
	}
	runJob(t, m, apply(m, physical('n')))
	if len(e.typed) != 1 || e.typed[0] != "n" || m.status != "" || strings.Contains(m.View(), "Saved encrypted.") {
		t.Fatal("save notice became permanent or new input was not literal")
	}
}

func TestCheckpointModelFailureRequiresExplicitRetryAndRetainsPublicationTruth(t *testing.T) {
	for _, injected := range []error{scratchpad.ErrConflict, scratchpad.ErrLocked, scratchpad.ErrDurabilityUnconfirmed} {
		t.Run(injected.Error(), func(t *testing.T) {
			m, e := checkpointModel()
			e.saveErrs = []error{injected, nil}
			e.r.Editor.Mirror.Frozen = true
			oldEpoch := m.epoch
			runJob(t, m, apply(m, actionMsg{epoch: oldEpoch, kind: "save"}))
			uncertain := errors.Is(injected, scratchpad.ErrDurabilityUnconfirmed)
			if m.phase != "error" || m.lastOp != "save" || m.epoch != oldEpoch || len(e.saves) != 1 || e.resumes != 0 || e.r.Result.Published != uncertain || e.r.Result.DurabilityUnconfirmed != uncertain || e.r.Editor.Closed || strings.Contains(m.status, "Saved encrypted.") {
				t.Fatal("failed save hid facts, resumed or closed")
			}
			if uncertain && !strings.Contains(m.status, "Published; durability unconfirmed") {
				t.Fatal("unconfirmed save was reported as unpublished or durable")
			}
			for _, event := range []tea.Msg{frameMsg{}, tea.WindowSizeMsg{Width: 90, Height: 25}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Paste: true}} {
				apply(m, event) // Watch commands are not run; no actual UI is launched.
				if len(e.saves) != 1 || m.busy || m.phase != "error" {
					t.Fatal("incidental event/paste silently retried checkpoint")
				}
			}
			retry := apply(m, physical('r'))
			if retry == nil || len(e.saves) != 1 {
				t.Fatal("physical retry did not schedule an owned save")
			}
			runJob(t, m, retry)
			wantBaseline := "baseline"
			if uncertain {
				wantBaseline = testPrivate
			}
			if len(e.saves) != 2 || e.saves[1] != [2]string{wantBaseline, testPrivate} || e.finishes != 0 || e.cancels != 0 || e.r.Result.DurabilityUnconfirmed || m.epoch != oldEpoch+1 || m.phase != "editing" || m.status != "Saved encrypted." {
				t.Fatal("explicit checkpoint retry used stale baseline or wrong lifecycle")
			}
		})
	}
}

func TestCheckpointModelFailureCanExplicitlyResumeOrRecover(t *testing.T) {
	for _, resume := range []bool{false, true} {
		m, e := checkpointModel()
		e.saveErrs = []error{scratchpad.ErrConflict}
		e.r.Editor.Mirror.Frozen = true
		oldEpoch := m.epoch
		runJob(t, m, apply(m, actionMsg{epoch: oldEpoch, kind: "save"}))
		choice := 'c'
		if resume {
			choice = 'n'
		}
		runJob(t, m, apply(m, physical(choice)))
		if len(e.saves) != 1 || e.finishes != 0 || e.r.Result.Published {
			t.Fatal("resume/cancel retried rejected publication")
		}
		if resume {
			if e.resumes != 1 || m.phase != "editing" || m.epoch != oldEpoch+1 || e.r.Baseline != "baseline" || e.r.Editor.Closed {
				t.Fatal("explicit resume reset baseline or lost epoch")
			}
		} else if e.cancels != 1 || !e.r.Editor.Closed || len(e.r.Result.Recoveries) != 1 || m.phase != "done" {
			t.Fatal("explicit encrypted cancellation no longer available")
		}
	}
}

func TestCheckpointModelResumeFailureDoesNotReportNotSaved(t *testing.T) {
	m, e := checkpointModel()
	e.r.Editor.Mirror.Frozen = true
	e.resumeErr = editorcore.ErrPending
	oldEpoch := m.epoch
	runJob(t, m, apply(m, actionMsg{epoch: oldEpoch, kind: "save"}))
	if m.phase != "error" || m.lastOp != "save" || m.epoch != oldEpoch || e.r.Editor.Closed || !e.r.Result.Published || e.r.Result.DurabilityUnconfirmed || e.r.Baseline != testPrivate || !strings.Contains(m.status, "Earlier note publication succeeded") || strings.Contains(m.status, "Saved encrypted.") {
		t.Fatal("failed auto-resume hid durable publication or claimed resumed editing")
	}
	runJob(t, m, apply(m, physical('n')))
	if m.epoch != oldEpoch+1 || m.phase != "editing" || !e.r.Result.Published || e.r.Baseline != testPrivate || len(e.saves) != 1 {
		t.Fatal("explicit resume after saved-but-failed-resume lost publication facts")
	}
}

func TestCheckpointModelConflictPreservesEarlierPublicationTruth(t *testing.T) {
	m, e := checkpointModel()
	e.r.Editor.Mirror.Frozen = true
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "save"}))
	first := e.r.Baseline
	runJob(t, m, apply(m, physical('x')))
	e.r.Editor.Mirror.Frozen = true
	e.saveErrs = []error{scratchpad.ErrConflict}
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "save"}))
	if m.phase != "error" || !e.r.Result.Published || e.r.Baseline != first || !strings.Contains(m.status, "Earlier note publication succeeded") || !strings.Contains(m.status, "latest save rejected") || strings.Contains(m.status, "Saved encrypted.") {
		t.Fatal("later conflict hid earlier checkpoint or claimed current draft saved")
	}
	runJob(t, m, apply(m, physical('c')))
	if !e.r.Result.Published || e.r.Result.Disposition != privateedit.Cancelled || len(e.saves) != 2 || e.cancels != 1 || len(e.r.Result.Recoveries) != 1 {
		t.Fatal("explicit recovery erased earlier publication or retried current draft")
	}
}

func TestCheckpointOptionalInterfaceNeverFallsBackToFinish(t *testing.T) {
	m := fixtureModel() // Common fake intentionally has no Checkpoint method.
	e := m.editor.(*fakeEditor)
	e.r.Editor.Mirror.Frozen = true
	runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "save"}))
	if m.phase != "error" || m.lastOp != "save" || e.finishes != 0 || e.cancels != 0 || e.r.Result.Published || e.r.Editor.Closed {
		t.Fatal("unsupported checkpoint fell back to finish/cancel")
	}
}

func TestCheckpointStoppingNeverImplicitlySaves(t *testing.T) {
	for _, retry := range []bool{false, true} {
		m, e := checkpointModel()
		e.r.Editor.Mirror.Frozen = true
		m.stopping = true
		if retry {
			m.phase, m.lastOp, m.actionKind = "error", "save", "save"
			m.shutdownFailed = true
			runJob(t, m, apply(m, physical('r')))
		} else {
			runJob(t, m, apply(m, actionMsg{epoch: m.epoch, kind: "save"}))
		}
		if len(e.saves) != 0 || e.finishes != 0 || e.cancels != 1 || !e.r.Editor.Closed || e.r.Result.Published {
			t.Fatal("stopping implicitly published a frozen save action")
		}
	}
}
