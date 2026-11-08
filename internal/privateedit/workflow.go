package privateedit

import (
	"context"
	"errors"
	"sync"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/scratchpad"
)

var (
	ErrConfirmationRequired = errors.New("privateedit: explicit cancellation/loss consent required")
	ErrFinalized            = errors.New("privateedit: session already finalized")
	ErrRecoveryReceipt      = errors.New("privateedit: recovery returned no publication receipt")
	ErrNoKnownFailure       = errors.New("privateedit: resolve/freeze a healthy session instead")
	ErrIdentity             = errors.New("privateedit: snapshot does not belong to this session")
)

type Disposition string

const (
	Active         Disposition = "active"
	Finished       Disposition = "finished"
	Cancelled      Disposition = "cancelled"
	RecoveredKnown Disposition = "recovered-known"
)

// RecoveryArtifact reports a publication, not necessarily confirmed durability.
// Kind is frozen/mirror/candidate. Text and session identifiers are not exposed.
type RecoveryArtifact struct {
	Kind     string
	Revision uint64
	Path     string
	Durable  bool
}

// Result keeps publication, durability and cleanup separate. Published remains
// true even if later cancellation/cleanup fails; Disposition describes release,
// not reversal of a save. Published && !DurabilityUnconfirmed means saved.
type Result struct {
	Disposition           Disposition
	Published             bool
	DurabilityUnconfirmed bool
	Closed                bool
	Recoveries            []RecoveryArtifact
	MayHaveLostEdits      bool
}

// Retention contains PRIVATE text. Never format it into diagnostics or logs.
type Retention struct {
	Baseline string
	Editor   editorcore.Retention
	Result   Result
	OpenErr  error
}

type editor = editorcore.Document

type store interface {
	save(original, content string) error
	recover(content string) (string, error)
}

type sealed struct {
	snapshot editorcore.Snapshot
	artifact RecoveryArtifact
}

type Workflow struct {
	gate        chan struct{}
	mu          sync.Mutex
	editor      editor
	store       store
	date        string
	generation  string
	baseline    string
	frozen      *editorcore.Snapshot
	result      Result
	sealed      []sealed
	openErr     error
	terminalErr error
}

func newWorkflow(e editor, s store, baseline string) *Workflow {
	r := e.Retained()
	return &Workflow{gate: make(chan struct{}, 1), editor: e, store: s,
		date: r.Mirror.DocumentID, generation: r.Mirror.SessionID, baseline: baseline,
		result: Result{Disposition: Active, Closed: r.Closed}}
}

func (w *Workflow) enter(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case w.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-w.gate
			return nil, err
		}
		return func() { <-w.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Retained does not query the editor. Its snapshots remain available after
// failure/close; this does not promise that an unknown mirror is the latest text.
func (w *Workflow) Retained() Retention {
	w.mu.Lock()
	r := Retention{Baseline: w.baseline, Result: w.result, OpenErr: w.openErr}
	r.Result.Recoveries = append([]RecoveryArtifact(nil), w.result.Recoveries...)
	w.mu.Unlock()
	r.Editor = w.editor.Retained()
	r.Result.Closed = r.Editor.Closed
	return r
}

func (w *Workflow) outcome() Result {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.result
	r.Recoveries = append([]RecoveryArtifact(nil), r.Recoveries...)
	return r
}

func (w *Workflow) terminal(intent Disposition) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.result.Disposition == Active {
		return false, nil
	}
	if w.result.Disposition != intent {
		return true, ErrFinalized
	}
	return true, w.terminalErr
}

// Set is a parent-owned synthetic document operation, not UI input.
// Frozen sessions deliberately support retry/cancel, not implicit resume.
func (w *Workflow) Set(ctx context.Context, base editorcore.Snapshot, text string) (editorcore.Snapshot, error) {
	leave, err := w.enter(ctx)
	if err != nil {
		return editorcore.Snapshot{}, failure("edit", err)
	}
	defer leave()
	if done, _ := w.terminal(Active); done {
		return w.editor.Retained().Mirror, ErrFinalized
	}
	if w.openErr != nil {
		return w.editor.Retained().Mirror, w.openErr
	}
	snapshot, err := w.editor.Set(ctx, base, text)
	return snapshot, failure("edit", err)
}

func (w *Workflow) Get(ctx context.Context) (editorcore.Snapshot, error) {
	return w.query(ctx, false)
}

// Resolve settles the same timed-out editor request; it never republishes data.
func (w *Workflow) Resolve(ctx context.Context) (editorcore.Snapshot, error) {
	return w.query(ctx, true)
}

func (w *Workflow) query(ctx context.Context, resolve bool) (editorcore.Snapshot, error) {
	leave, err := w.enter(ctx)
	if err != nil {
		return editorcore.Snapshot{}, failure("edit", err)
	}
	defer leave()
	if done, _ := w.terminal(Active); done {
		return w.editor.Retained().Mirror, ErrFinalized
	}
	if w.openErr != nil {
		return w.editor.Retained().Mirror, w.openErr
	}
	var snapshot editorcore.Snapshot
	if resolve {
		snapshot, err = w.editor.Resolve(ctx)
	} else {
		snapshot, err = w.editor.Get(ctx)
	}
	return snapshot, failure("edit", err)
}

func (w *Workflow) own(snapshot editorcore.Snapshot) bool {
	return snapshot.DocumentID == w.date && snapshot.SessionID == w.generation
}

func (w *Workflow) freeze(ctx context.Context) (editorcore.Snapshot, error) {
	if w.openErr != nil {
		return editorcore.Snapshot{}, w.openErr
	}
	if w.frozen != nil {
		if *w.frozen != w.editor.Retained().Mirror {
			return editorcore.Snapshot{}, ErrIdentity
		}
		return *w.frozen, nil
	}
	snapshot, err := w.editor.Resolve(ctx)
	if err != nil {
		return snapshot, err
	}
	if !snapshot.Frozen {
		snapshot, err = w.editor.Freeze(ctx)
		if err != nil {
			return snapshot, err
		}
	}
	r := w.editor.Retained()
	if !w.own(snapshot) || !snapshot.Frozen || snapshot != r.Mirror || r.LatestUnknown || r.Closed {
		return editorcore.Snapshot{}, ErrIdentity
	}
	w.frozen = &snapshot
	return snapshot, nil
}

// Finish performs at most one synchronous policy/revision-checked publication
// attempt. An unconfirmed publication advances Baseline but does NOT close.
// Retry explicitly republishes against that advanced baseline; no blind retry.
func (w *Workflow) Finish(ctx context.Context) (Result, error) {
	leave, err := w.enter(ctx)
	if err != nil {
		return w.outcome(), failure("finish", err)
	}
	defer leave()
	if done, terminalErr := w.terminal(Finished); done {
		return w.outcome(), terminalErr
	}
	snapshot, err := w.freeze(ctx)
	if err != nil {
		return w.outcome(), failure("freeze", err)
	}
	if err := w.publish(ctx, snapshot); err != nil {
		return w.outcome(), err
	}
	return w.close(ctx, Finished, snapshot)
}

// checkpoint publishes only the exact already-frozen action authorized by the
// embedded owner. It neither releases the editor nor reauthenticates the store.
func (w *Workflow) checkpoint(ctx context.Context, snapshot editorcore.Snapshot) (Result, error) {
	leave, err := w.enter(ctx)
	if err != nil {
		return w.outcome(), failure("save", err)
	}
	defer leave()
	if done, _ := w.terminal(Active); done {
		return w.outcome(), ErrFinalized
	}
	if w.openErr != nil {
		return w.outcome(), w.openErr
	}
	r := w.editor.Retained()
	if !w.own(snapshot) || !snapshot.Frozen || snapshot != r.Mirror || r.LatestUnknown || r.Fault != nil || r.Closed ||
		(w.frozen != nil && *w.frozen != snapshot) {
		return w.outcome(), ErrIdentity
	}
	w.frozen = &snapshot
	if err := w.publish(ctx, snapshot); err != nil {
		return w.outcome(), err
	}
	return w.outcome(), nil
}

// publish is shared by finish and checkpoint under the workflow gate. Only the
// canonical store receipt advances baseline/publication; a later UI failure
// cannot roll those facts back. Unconfirmed publication requires explicit retry.
func (w *Workflow) publish(ctx context.Context, snapshot editorcore.Snapshot) error {
	w.mu.Lock()
	baseline, unconfirmed := w.baseline, w.result.DurabilityUnconfirmed
	w.mu.Unlock()
	if snapshot.Text != baseline || unconfirmed {
		if contextErr := ctx.Err(); contextErr != nil {
			return failure("save", contextErr)
		}
		// Store locks are not cancellable: own the call until it settles.
		saveErr := w.store.save(baseline, snapshot.Text)
		if saveErr == nil || errors.Is(saveErr, scratchpad.ErrDurabilityUnconfirmed) {
			w.mu.Lock()
			w.baseline = snapshot.Text
			w.result.Published = true
			w.result.DurabilityUnconfirmed = saveErr != nil
			w.mu.Unlock()
		}
		if saveErr != nil {
			return failure("save", saveErr)
		}
	}
	return nil
}

// Cancel seals changed or durability-unconfirmed text before releasing the
// exact frozen child. Consent is explicit; cancellation is never publication.
func (w *Workflow) Cancel(ctx context.Context, consent bool) (Result, error) {
	leave, err := w.enter(ctx)
	if err != nil {
		return w.outcome(), failure("cancel", err)
	}
	defer leave()
	if done, terminalErr := w.terminal(Cancelled); done {
		return w.outcome(), terminalErr
	}
	if !consent {
		r := w.editor.Retained()
		w.mu.Lock()
		needsConsent := r.LatestUnknown || r.Candidate != nil || r.Mirror.Text != w.baseline || w.result.DurabilityUnconfirmed
		w.mu.Unlock()
		if needsConsent {
			return w.outcome(), ErrConfirmationRequired
		}
	}
	snapshot, err := w.freeze(ctx)
	if err != nil {
		return w.outcome(), failure("freeze", err)
	}
	w.mu.Lock()
	needsRecovery := snapshot.Text != w.baseline || w.result.DurabilityUnconfirmed
	w.mu.Unlock()
	if needsRecovery {
		if !consent {
			return w.outcome(), ErrConfirmationRequired
		}
		if sealErr := w.seal(ctx, "frozen", snapshot); sealErr != nil {
			return w.outcome(), failure("recovery", sealErr)
		}
	}
	return w.close(ctx, Cancelled, snapshot)
}

func (w *Workflow) seal(ctx context.Context, kind string, snapshot editorcore.Snapshot) error {
	for _, s := range w.sealed {
		if s.snapshot == snapshot && s.artifact.Kind == kind && s.artifact.Durable {
			return nil
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := w.store.recover(snapshot.Text)
	if path != "" {
		artifact := RecoveryArtifact{Kind: kind, Revision: snapshot.Revision, Path: path, Durable: err == nil}
		w.sealed = append(w.sealed, sealed{snapshot: snapshot, artifact: artifact})
		w.mu.Lock()
		w.result.Recoveries = append(w.result.Recoveries, artifact)
		w.mu.Unlock()
	}
	if err != nil {
		return err
	}
	if path == "" {
		return ErrRecoveryReceipt
	}
	return nil
}

func (w *Workflow) close(ctx context.Context, intent Disposition, snapshot editorcore.Snapshot) (Result, error) {
	if err := ctx.Err(); err != nil {
		return w.outcome(), failure("close", err)
	}
	// A previous close may have timed out after a valid freeze. Settle that
	// same request first. Protocol/exit faults do not revoke the frozen handoff.
	if _, err := w.editor.Resolve(ctx); err != nil && !errors.Is(err, editorcore.ErrProtocol) && !errors.Is(err, editorcore.ErrClosed) {
		return w.outcome(), failure("close", err)
	}
	err := failure("close", w.editor.Close(ctx, snapshot))
	w.complete(intent, err)
	return w.outcome(), err
}

func (w *Workflow) complete(intent Disposition, err error) {
	closed := w.editor.Retained().Closed
	w.mu.Lock()
	defer w.mu.Unlock()
	w.result.Closed = closed
	if closed {
		w.result.Disposition = intent
		w.terminalErr = err
	}
}

// RecoverKnown is NOT normal cancellation. Only an already unknown/faulted
// session qualifies. Consent acknowledges potentially missing editor-only edits;
// both known copies are durably encrypted before explicit destructive discard.
func (w *Workflow) RecoverKnown(ctx context.Context, consent bool) (Result, error) {
	leave, err := w.enter(ctx)
	if err != nil {
		return w.outcome(), failure("recover-known", err)
	}
	defer leave()
	if done, terminalErr := w.terminal(RecoveredKnown); done {
		return w.outcome(), terminalErr
	}
	if !consent {
		return w.outcome(), ErrConfirmationRequired
	}
	r := w.editor.Retained()
	if !r.LatestUnknown && r.Fault == nil && w.openErr == nil {
		return w.outcome(), ErrNoKnownFailure
	}
	copies := []struct {
		kind string
		data editorcore.Snapshot
	}{{"mirror", r.Mirror}}
	if r.Candidate != nil {
		copies = append(copies, struct {
			kind string
			data editorcore.Snapshot
		}{"candidate", *r.Candidate})
	}
	for _, copy := range copies {
		if !w.own(copy.data) {
			return w.outcome(), ErrIdentity
		}
	}
	for _, copy := range copies {
		if sealErr := w.seal(ctx, copy.kind, copy.data); sealErr != nil {
			return w.outcome(), failure("recovery", sealErr)
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return w.outcome(), failure("discard", contextErr)
	}
	w.mu.Lock()
	w.result.MayHaveLostEdits = true
	w.mu.Unlock()
	_, discardErr := w.editor.Discard(ctx, true)
	err = failure("discard", discardErr)
	w.complete(RecoveredKnown, err)
	return w.outcome(), err
}

type operationError struct {
	stage string
	cause error
}

func (e *operationError) Error() string { return "privateedit: " + e.stage + " failed" }
func (e *operationError) Unwrap() error { return e.cause }

func failure(stage string, cause error) error {
	if cause == nil {
		return nil
	}
	switch stage {
	case "prepare", "open", "edit", "freeze", "finish", "save", "cancel", "recovery", "close", "discard", "recover-known":
	default:
		stage = "operation"
	}
	return &operationError{stage: stage, cause: cause}
}
