package privateedit

import (
	"context"
	"sync"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/editorsandbox"
	"github.com/pders01/sp/internal/scratchpad"
)

// Embedded joins owned UI/input to the existing encrypted lifecycle. No raw child close/discard handle is
// exposed: normal release must go through Finish/Cancel, and the fault escape
// encrypts known snapshots before explicit-loss discard.
type Embedded struct {
	// Keep action checks and their workflow call indivisible with Resume.
	gate     sync.Mutex
	workflow *Workflow
	ui       editorcore.UI
}

// OpenDriver requires the common OS boundary before preparing any private data.
// No platform backend is approved yet; it fails closed without calling a driver.
func OpenDriver(ctx context.Context, manager *scratchpad.Manager, options editorcore.Options, driver editorcore.Driver) (*Embedded, error) {
	if err := editorsandbox.Require(ctx, editorsandbox.NoPersistence()); err != nil {
		return nil, failure("isolate", err)
	}
	if driver == nil {
		return nil, failure("prepare", editorcore.ErrUnsupported)
	}
	return openWithFactory(ctx, manager, options, driver.OpenProtected)
}

// OpenExperimentWithDriver is only for disposable synthetic evaluations. Its
// separate factory type conveys NO OS enforcement and is never a fallback.
func OpenExperimentWithDriver(ctx context.Context, manager *scratchpad.Manager, options editorcore.Options, open editorcore.ExperimentFactory) (*Embedded, error) {
	return openWithFactory(ctx, manager, options, open)
}

// OpenInMemory prepares one authenticated edit for an explicitly selected
// capability-limited in-process UI. This is NOT OS enforcement or a native
// fallback. Native OpenDriver gates remain unchanged.
func OpenInMemory(ctx context.Context, manager *scratchpad.Manager, options editorcore.Options, open editorcore.InMemoryFactory) (*Embedded, error) {
	return openWithFactory(ctx, manager, options, editorcore.ExperimentFactory(open))
}

func openWithFactory(ctx context.Context, manager *scratchpad.Manager, options editorcore.Options, open editorcore.ExperimentFactory) (*Embedded, error) {
	if err := ctx.Err(); err != nil {
		return nil, failure("prepare", err)
	}
	if manager == nil || open == nil {
		return nil, failure("prepare", editorcore.ErrUnsupported)
	}
	prepared, err := manager.PreparePrivateEdit(options.DocumentID)
	if err != nil {
		return nil, failure("prepare", err)
	}
	ui, err := open(ctx, options, prepared.Content())
	if ui == nil {
		if err == nil {
			err = editorcore.ErrUnsupported
		}
		return nil, failure("open", err)
	}
	workflow := newWorkflow(embeddedEditor{ui}, managerStore{prepared: prepared}, prepared.Content())
	workflow.openErr = failure("open", err)
	return &Embedded{workflow: workflow, ui: ui}, workflow.openErr
}
func (e *Embedded) Retained() Retention     { return e.workflow.Retained() }
func (e *Embedded) Frame() editorcore.Frame { return e.ui.Frame() }
func (e *Embedded) Resize(ctx context.Context, width, height int) error {
	return e.ui.Resize(ctx, width, height)
}
func (e *Embedded) Changed() <-chan struct{}                           { return e.ui.Changed() }
func (e *Embedded) Type(ctx context.Context, text string) (int, error) { return e.ui.Type(ctx, text) }
func (e *Embedded) Key(ctx context.Context, key string) (int, error)   { return e.ui.Key(ctx, key) }
func (e *Embedded) InsertText(ctx context.Context, text string) (editorcore.Snapshot, error) {
	return e.ui.InsertText(ctx, text)
}
func (e *Embedded) WaitAction(ctx context.Context) (editorcore.Action, error) {
	return e.ui.WaitAction(ctx)
}
func (e *Embedded) Resolve(ctx context.Context) error {
	e.gate.Lock()
	defer e.gate.Unlock()
	_, err := e.workflow.Resolve(ctx)
	return err
}

// Resume is explicit cancellation-confirmation "No", not a new workflow.
// Publication, durability uncertainty and the stored baseline remain facts.
func (e *Embedded) Resume(ctx context.Context) error {
	e.gate.Lock()
	defer e.gate.Unlock()
	return e.resume(ctx, nil)
}

// resume runs with the embedded gate held, but acquires the workflow gate only
// after checkpoint publication has released it (neither gate is recursive).
func (e *Embedded) resume(ctx context.Context, expected *editorcore.Action) error {
	w := e.workflow
	leave, err := w.enter(ctx)
	if err != nil {
		return failure("edit", err)
	}
	defer leave()
	if done, _ := w.terminal(Active); done {
		return ErrFinalized
	}
	if w.openErr != nil {
		return w.openErr
	}
	// A timed-out transition may already have unfrozen the child. Never let
	// workflow.freeze's cached path authorize save/close with that old action.
	w.frozen = nil
	r := e.ui.Retained()
	if !currentEmbeddedFreeze(r) || (expected != nil && *r.Action != *expected) {
		return editorcore.ErrNotFrozen
	}
	_, err = e.ui.Resume(ctx, r.Action.Snapshot)
	return failure("edit", err)
}

func currentEmbeddedFreeze(r editorcore.Observation) bool {
	return r.Action != nil && r.Action.Snapshot.Frozen && r.Action.Snapshot == r.Observed &&
		!r.LatestUnknown && r.Fault == nil && !r.Closed
}

// Checkpoint accepts only a current, healthy, exact save action. Publication
// uses the existing prepared store/generation. Failed or uncertain saves remain
// frozen; only confirmed publication (or an unchanged no-op) permits auto-resume.
// If resume fails, the advanced baseline and publication facts remain retained.
func (e *Embedded) Checkpoint(ctx context.Context) (Result, error) {
	e.gate.Lock()
	defer e.gate.Unlock()
	if done, _ := e.workflow.terminal(Active); done {
		return e.workflow.outcome(), ErrFinalized
	}
	r := e.ui.Retained()
	if !currentEmbeddedFreeze(r) || r.Action.Kind != "save" {
		return e.workflow.outcome(), editorcore.ErrNotFrozen
	}
	action := *r.Action
	result, err := e.workflow.checkpoint(ctx, action.Snapshot)
	if err != nil {
		return result, err
	}
	err = e.resume(ctx, &action)
	return e.workflow.outcome(), err
}

// Finish only accepts the editor's input-ordered F2 action, never a queued-key
// reply, F3 cancellation, UI flush or process exit as publication permission.
func (e *Embedded) Finish(ctx context.Context) (Result, error) {
	e.gate.Lock()
	defer e.gate.Unlock()
	if done, err := e.workflow.terminal(Finished); done {
		return e.workflow.outcome(), err
	}
	r := e.ui.Retained()
	if !currentEmbeddedFreeze(r) || r.Action.Kind != "finish" {
		return e.workflow.outcome(), editorcore.ErrNotFrozen
	}
	return e.workflow.Finish(ctx)
}

// Cancel also permits an explicit SP-side cancellation after a failed F2 save;
// the already-frozen buffer cannot accept F3. Changed drafts still need consent.
func (e *Embedded) Cancel(ctx context.Context, consent bool) (Result, error) {
	e.gate.Lock()
	defer e.gate.Unlock()
	if done, err := e.workflow.terminal(Cancelled); done {
		return e.workflow.outcome(), err
	}
	if !currentEmbeddedFreeze(e.ui.Retained()) {
		return e.workflow.outcome(), editorcore.ErrNotFrozen
	}
	return e.workflow.Cancel(ctx, consent)
}
func (e *Embedded) RecoverKnown(ctx context.Context, consent bool) (Result, error) {
	e.gate.Lock()
	defer e.gate.Unlock()
	return e.workflow.RecoverKnown(ctx, consent)
}

type embeddedEditor struct{ ui editorcore.UI }

func (e embeddedEditor) Retained() editorcore.Retention {
	r := e.ui.Retained()
	return editorcore.Retention{Mirror: r.Observed, LatestUnknown: r.LatestUnknown, Closed: r.Closed, Fault: r.Fault, Version: r.Version}
}
func (e embeddedEditor) Set(context.Context, editorcore.Snapshot, string) (editorcore.Snapshot, error) {
	return e.ui.Retained().Observed, editorcore.ErrUnsupported
}
func (e embeddedEditor) Get(ctx context.Context) (editorcore.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return editorcore.Snapshot{}, err
	}
	r := e.ui.Retained()
	if r.Closed {
		return r.Observed, editorcore.ErrClosed
	}
	return r.Observed, r.Fault
}
func (e embeddedEditor) Resolve(ctx context.Context) (editorcore.Snapshot, error) {
	if err := e.ui.Resolve(ctx); err != nil {
		return e.ui.Retained().Observed, err
	}
	return e.Get(ctx)
}
func (e embeddedEditor) Freeze(context.Context) (editorcore.Snapshot, error) {
	r := e.ui.Retained()
	if r.Action == nil || !r.Action.Snapshot.Frozen || r.LatestUnknown || r.Fault != nil || r.Closed {
		return r.Observed, editorcore.ErrNotFrozen
	}
	return r.Action.Snapshot, nil
}
func (e embeddedEditor) Close(ctx context.Context, frozen editorcore.Snapshot) error {
	return e.ui.Close(ctx, frozen)
}
func (e embeddedEditor) Discard(ctx context.Context, consent bool) (editorcore.Retention, error) {
	_, err := e.ui.Discard(ctx, consent)
	return e.Retained(), err
}
