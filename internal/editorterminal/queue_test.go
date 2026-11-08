package editorterminal

import (
	"context"
	"strconv"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
)

func fillOverflow(m *model) {
	m.busy = true
	for i := 0; i <= maxQueueEvents; i++ {
		m.enqueue(Input{Text: strconv.Itoa(i)})
	}
	m.busy = false
}

func TestTerminalOverflowStaysAtFIFOTail(t *testing.T) {
	m := fixtureModel()
	e := m.editor.(*fakeEditor)
	fillOverflow(m)
	if !m.inputPaused || len(m.queue) != maxQueueEvents+1 {
		t.Fatal("overflow ownership missing")
	}
	m.enqueue(Input{Text: "rejected-while-paused"})
	cmd := m.next()
	for cmd != nil {
		cmd = runJob(t, m, cmd)
	}
	if len(e.typed) != maxQueueEvents+1 || m.inputPaused {
		t.Fatal("overflow did not drain exactly once")
	}
	for i, text := range e.typed {
		if text != strconv.Itoa(i) {
			t.Fatal("input reordered at queue boundary")
		}
	}
}

type failedInputEditor struct{ *fakeEditor }

func (e failedInputEditor) Type(context.Context, string) (int, error) { return 0, editorcore.ErrRemote }

func TestTerminalOverflowSurvivesEarlierInputFailure(t *testing.T) {
	m := fixtureModel()
	m.editor = failedInputEditor{m.editor.(*fakeEditor)}
	fillOverflow(m)
	runJob(t, m, m.next())
	if m.held == nil || m.held.Text != "0" || len(m.queue) != maxQueueEvents || m.queue[len(m.queue)-1].Text != strconv.Itoa(maxQueueEvents) {
		t.Fatal("earlier failure overwrote or reordered overflow")
	}
}

func TestTerminalNamedInputTimeoutRetainsExactEventUntilReceipt(t *testing.T) {
	for _, accepted := range []int{0, 3, 6} {
		t.Run(strconv.Itoa(accepted), func(t *testing.T) {
			m := fixtureModel()
			e := m.editor.(*fakeEditor)
			m.jobs.sequence = 1 // Match the deliberately injected completed job.
			apply(m, jobMsg{sequence: 1, kind: "input", input: Input{Key: "Enter"}, before: 4, err: context.DeadlineExceeded})
			if m.held == nil || m.held.Key != "Enter" || m.lastOp != "resolve-input" {
				t.Fatal("named input lost pending ownership")
			}
			e.receipt = editorcore.InputReceipt{RequestID: 5, RequestedBytes: 6, AcceptedBytes: accepted, Known: true}
			runJob(t, m, apply(m, physical('r')))
			if accepted == 6 {
				if m.held != nil || m.phase != "editing" {
					t.Fatal("complete receipt did not settle exact event")
				}
			} else {
				if m.held == nil || m.held.Partial != (accepted != 0) || len(e.keys) != 0 {
					t.Fatal("partial/zero receipt replayed input or lost event")
				}
				if accepted != 0 && apply(m, physical('r')) != nil {
					t.Fatal("ambiguous suffix was replayed")
				}
			}
		})
	}
}
