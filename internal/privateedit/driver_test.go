package privateedit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/editorsandbox"
)

// A second, in-memory editor variant. It imports no Neovim code and never
// launches a process. Experiments are synthetic only, not sandbox receipts.
type memoryUI struct {
	mu      sync.Mutex
	state   editorcore.Observation
	receipt editorcore.InputReceipt
	changed chan struct{}
}

func newMemoryUI(options editorcore.Options, text string) *memoryUI {
	return &memoryUI{state: editorcore.Observation{Observed: editorcore.Snapshot{SessionID: "memory-synthetic-session", DocumentID: options.DocumentID, Revision: 1, Text: text}, Version: "memory-test"}, changed: make(chan struct{}, 1)}
}
func (m *memoryUI) Retained() editorcore.Observation {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.state
	if r.Action != nil {
		copyAction := *r.Action
		r.Action = &copyAction
	}
	return r
}
func (m *memoryUI) Frame() editorcore.Frame {
	return editorcore.Frame{Width: 1, Height: 1, Lines: []string{" "}}
}
func (m *memoryUI) Changed() <-chan struct{} { return m.changed }
func (m *memoryUI) Type(ctx context.Context, text string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Observed.Frozen {
		return 0, editorcore.ErrFrozen
	}
	m.state.Observed.Text += text
	m.state.Observed.Revision++
	m.receipt = editorcore.InputReceipt{RequestID: m.receipt.RequestID + 1, RequestedBytes: len(text), AcceptedBytes: len(text), Known: true}
	return len(text), nil
}
func (m *memoryUI) Key(ctx context.Context, key string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if key != "Finish" && key != "Cancel" {
		return 0, editorcore.ErrUnsupported
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Observed.Frozen {
		return 0, editorcore.ErrFrozen
	}
	m.state.Observed.Frozen = true
	m.state.Observed.Revision++
	kind := "finish"
	if key == "Cancel" {
		kind = "cancel"
	}
	m.state.Action = &editorcore.Action{Kind: kind, Snapshot: m.state.Observed}
	return 1, nil
}
func (m *memoryUI) InsertText(ctx context.Context, text string) (editorcore.Snapshot, error) {
	_, err := m.Type(ctx, text)
	return m.Retained().Observed, err
}
func (m *memoryUI) InputReceipt() editorcore.InputReceipt {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.receipt
}
func (m *memoryUI) Resize(context.Context, int, int) error { return nil }
func (m *memoryUI) WaitAction(context.Context) (editorcore.Action, error) {
	r := m.Retained()
	if r.Action == nil {
		return editorcore.Action{}, editorcore.ErrPending
	}
	return *r.Action, nil
}
func (m *memoryUI) Resolve(context.Context) error { return nil }
func (m *memoryUI) Resume(_ context.Context, s editorcore.Snapshot) (editorcore.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Action == nil || s != m.state.Action.Snapshot || s != m.state.Observed {
		return editorcore.Snapshot{}, editorcore.ErrNotFrozen
	}
	m.state.Action = nil
	m.state.Observed.Frozen = false
	return m.state.Observed, nil
}
func (m *memoryUI) Close(_ context.Context, s editorcore.Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Action == nil || s != m.state.Action.Snapshot || s != m.state.Observed || !s.Frozen {
		return editorcore.ErrNotFrozen
	}
	m.state.Closed = true
	return nil
}
func (m *memoryUI) Discard(_ context.Context, consent bool) (editorcore.Observation, error) {
	if !consent {
		return m.Retained(), editorcore.ErrLossConsent
	}
	m.mu.Lock()
	m.state.Closed = true
	m.mu.Unlock()
	return m.Retained(), nil
}

var _ editorcore.UI = (*memoryUI)(nil)

type spyProtectedDriver struct{ calls int }

func (*spyProtectedDriver) ID() string { return "synthetic-spy" }
func (d *spyProtectedDriver) OpenProtected(context.Context, editorcore.Options, string) (editorcore.UI, error) {
	d.calls++
	return nil, errors.New("must not run")
}

func TestProtectedDriverFailsBeforeManagerOrAdapterAccess(t *testing.T) {
	d := &spyProtectedDriver{}
	owner, err := OpenDriver(context.Background(), nil, editorcore.Options{DocumentID: fixtureDate}, d)
	if owner != nil || !errors.Is(err, editorsandbox.ErrUnavailable) || d.calls != 0 {
		t.Fatal("missing OS enforcement handed private data to a driver or fell back")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if owner, err = OpenDriver(ctx, nil, editorcore.Options{}, d); owner != nil || !errors.Is(err, context.Canceled) || d.calls != 0 {
		t.Fatal("cancelled protection gate invoked adapter")
	}
}

func TestMemoryVariantUsesSameEncryptedFinishAndSharedOwnership(t *testing.T) {
	manager, root := privateManager(t)
	var ui *memoryUI
	e, err := OpenExperimentWithDriver(context.Background(), manager, editorcore.Options{DocumentID: fixtureDate}, func(_ context.Context, options editorcore.Options, text string) (editorcore.UI, error) {
		ui = newMemoryUI(options, text)
		return ui, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	original := e.Retained().Baseline
	if _, err = e.InsertText(context.Background(), privateCanary); err != nil {
		t.Fatal(err)
	}
	if err = manager.Append(fixtureDate, "Updates", "synthetic-memory", []string{"public append with another editor variant"}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Key(context.Background(), "Cancel"); err != nil {
		t.Fatal(err)
	}
	if err = e.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Finish(context.Background()); !errors.Is(err, editorcore.ErrNotFrozen) {
		t.Fatal("old action survived generic resume")
	}
	if _, err = e.Key(context.Background(), "Finish"); err != nil {
		t.Fatal(err)
	}
	result, err := e.Finish(context.Background())
	if err != nil || !result.Published || !result.Closed || !ui.Retained().Closed {
		t.Fatal("generic editor did not use canonical encrypted finish")
	}
	note, err := manager.GetByDate(fixtureDate)
	if err != nil || note.Content != original+privateCanary || !strings.Contains(note.Agent.Content, "public append with another editor variant") {
		t.Fatal("generic editor changed private bytes or overwrote shared content")
	}
	assertNoPlaintextCanary(t, root)
}

func TestMemoryVariantRetainsPinnedRecoveryAfterLock(t *testing.T) {
	manager, root := privateManager(t)
	e, err := OpenExperimentWithDriver(context.Background(), manager, editorcore.Options{DocumentID: fixtureDate}, func(_ context.Context, options editorcore.Options, text string) (editorcore.UI, error) {
		return newMemoryUI(options, text), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Type(context.Background(), privateCanary); err != nil {
		t.Fatal(err)
	}
	want := e.Retained().Editor.Mirror.Text
	if _, err = e.Key(context.Background(), "Cancel"); err != nil {
		t.Fatal(err)
	}
	manager.Lock()
	if _, err = e.Cancel(context.Background(), false); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatal("generic changed draft bypassed consent")
	}
	result, err := e.Cancel(context.Background(), true)
	if err != nil || !result.Closed || len(result.Recoveries) != 1 || !result.Recoveries[0].Durable {
		t.Fatal("generic cancellation failed pinned encrypted recovery")
	}
	if err = manager.Unlock([]byte(fixtureSecret)); err != nil {
		t.Fatal(err)
	}
	recovery, err := manager.ReadRecovery(result.Recoveries[0].Path)
	if err != nil || recovery.Content != want {
		t.Fatal("generic recovery changed bytes or recipient")
	}
	assertNoPlaintextCanary(t, root)
}

func TestGenericAdapterStartupErrorRetainsOwner(t *testing.T) {
	manager, _ := privateManager(t)
	var ui *memoryUI
	e, err := OpenExperimentWithDriver(context.Background(), manager, editorcore.Options{DocumentID: fixtureDate}, func(_ context.Context, options editorcore.Options, text string) (editorcore.UI, error) {
		ui = newMemoryUI(options, text)
		ui.state.LatestUnknown = true
		ui.state.Fault = editorcore.ErrProtocol
		return ui, editorcore.ErrProtocol
	})
	if e == nil || !errors.Is(err, editorcore.ErrProtocol) || e.Retained().Editor.Closed {
		t.Fatal("generic startup error lost the owner")
	}
	if _, discardErr := ui.Discard(context.Background(), true); discardErr != nil {
		t.Fatal(discardErr)
	} // Explicit wholly synthetic cleanup only.
}
