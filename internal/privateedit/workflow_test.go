package privateedit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/scratchpad"
)

const privateCanary = "SYNTHETIC-PRIVATE-WORKFLOW-CANARY\r\n\t🙂\n\n"

func snapshot(text string) editorcore.Snapshot {
	return editorcore.Snapshot{SessionID: "synthetic-generation", DocumentID: "2026-01-15", Revision: 1, Text: text}
}

type modelEditor struct {
	mu                                          sync.Mutex
	r                                           editorcore.Retention
	freezeErr, resolveErr, closeErr, discardErr error
	resolveTo                                   *editorcore.Snapshot
	reapOnCloseError                            bool
	freezes, closes, discards                   int
}

func (e *modelEditor) Retained() editorcore.Retention {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.r
	if r.Candidate != nil {
		candidate := *r.Candidate
		r.Candidate = &candidate
	}
	return r
}
func (e *modelEditor) Set(_ context.Context, base editorcore.Snapshot, text string) (editorcore.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if base != e.r.Mirror {
		return e.r.Mirror, editorcore.ErrRevision
	}
	if base.Frozen {
		return base, editorcore.ErrFrozen
	}
	e.r.Mirror.Text = text
	e.r.Mirror.Revision++
	return e.r.Mirror, nil
}
func (e *modelEditor) Get(context.Context) (editorcore.Snapshot, error) {
	return e.Retained().Mirror, nil
}
func (e *modelEditor) Resolve(context.Context) (editorcore.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resolveErr != nil {
		return e.r.Mirror, e.resolveErr
	}
	if e.resolveTo != nil {
		e.r.Mirror = *e.resolveTo
		e.r.LatestUnknown = false
		e.r.Candidate = nil
		e.resolveTo = nil
	}
	return e.r.Mirror, nil
}
func (e *modelEditor) Freeze(context.Context) (editorcore.Snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.freezes++
	if e.freezeErr != nil {
		e.r.LatestUnknown = true
		return e.r.Mirror, e.freezeErr
	}
	e.r.Mirror.Frozen = true
	return e.r.Mirror, nil
}
func (e *modelEditor) Close(_ context.Context, s editorcore.Snapshot) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closes++
	if !s.Frozen || s != e.r.Mirror {
		return editorcore.ErrNotFrozen
	}
	if e.closeErr == nil || e.reapOnCloseError {
		e.r.Closed = true
	}
	return e.closeErr
}
func (e *modelEditor) Discard(_ context.Context, consent bool) (editorcore.Retention, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !consent {
		return e.r, editorcore.ErrLossConsent
	}
	e.discards++
	if e.discardErr == nil {
		e.r.Closed = true
		e.r.LatestUnknown = true
	}
	return e.r, e.discardErr
}

type receipt struct {
	path string
	err  error
}

type modelStore struct {
	mu         sync.Mutex
	saves      [][2]string
	recoveries []string
	saveErrs   []error
	receipts   []receipt
	saveHook   func() error
}

func (s *modelStore) save(original, content string) error {
	s.mu.Lock()
	s.saves = append(s.saves, [2]string{original, content})
	index := len(s.saves) - 1
	hook := s.saveHook
	var err error
	if index < len(s.saveErrs) {
		err = s.saveErrs[index]
	}
	s.mu.Unlock()
	if hook != nil {
		return hook()
	}
	return err
}
func (s *modelStore) recover(content string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoveries = append(s.recoveries, content)
	index := len(s.recoveries) - 1
	if index < len(s.receipts) {
		return s.receipts[index].path, s.receipts[index].err
	}
	return fmt.Sprintf("/synthetic/recovery-%d.age", index), nil
}

func model(t *testing.T) (*Workflow, *modelEditor, *modelStore) {
	t.Helper()
	e := &modelEditor{r: editorcore.Retention{Mirror: snapshot("original\r\n")}}
	s := &modelStore{}
	w := newWorkflow(e, s, e.r.Mirror.Text)
	if _, err := w.Set(context.Background(), e.r.Mirror, privateCanary); err != nil {
		t.Fatal(err)
	}
	return w, e, s
}

func TestFinishPublishesExactFrozenBytesAndIsIdempotent(t *testing.T) {
	w, e, s := model(t)
	r, err := w.Finish(context.Background())
	if err != nil || !r.Published || r.DurabilityUnconfirmed || !r.Closed || r.Disposition != Finished {
		t.Fatal("finish failed to distinguish publication from cleanup", err)
	}
	if len(s.saves) != 1 || s.saves[0] != [2]string{"original\r\n", privateCanary} || w.Retained().Baseline != privateCanary {
		t.Fatal("finish normalized text or used the wrong baseline")
	}
	if len(s.recoveries) != 0 || e.freezes != 1 || e.closes != 1 {
		t.Fatal("finish performed unexpected recovery/child operations")
	}
	if _, err := w.Finish(context.Background()); err != nil || len(s.saves) != 1 || e.closes != 1 {
		t.Fatal("idempotent finish repeated publication/cleanup")
	}
	if _, err := w.Cancel(context.Background(), true); err != ErrFinalized {
		t.Fatal("cancel reinterpreted a completed finish")
	}
	if _, err := w.Set(context.Background(), w.Retained().Editor.Mirror, "later"); err != ErrFinalized {
		t.Fatal("finalized workflow accepted editing")
	}
}

func TestUnchangedFinishAndCancelDoNotWrite(t *testing.T) {
	for _, finish := range []bool{true, false} {
		e := &modelEditor{r: editorcore.Retention{Mirror: snapshot("unchanged")}}
		s := &modelStore{}
		w := newWorkflow(e, s, "unchanged")
		var result Result
		var err error
		if finish {
			result, err = w.Finish(context.Background())
		} else {
			result, err = w.Cancel(context.Background(), false)
		}
		if err != nil || result.Published || !result.Closed || len(s.saves) != 0 || len(s.recoveries) != 0 {
			t.Fatal("unchanged release wrote content or failed", err)
		}
	}
}

func TestPrepublicationFailuresRetainFrozenDraftAndBaseline(t *testing.T) {
	for _, injected := range []error{scratchpad.ErrConflict, scratchpad.ErrLocked, scratchpad.ErrPrivacyPolicyChanged, errors.New(privateCanary)} {
		w, e, s := model(t)
		s.saveErrs = []error{injected}
		r, err := w.Finish(context.Background())
		if !errors.Is(err, injected) || r.Published || r.Closed || e.closes != 0 || w.Retained().Baseline != "original\r\n" {
			t.Fatal("prepublication failure lost draft/baseline or closed editor")
		}
		if strings.Contains(err.Error(), privateCanary) || w.Retained().Editor.Mirror.Text != privateCanary {
			t.Fatal("failure leaked or lost private text")
		}
		if _, err := w.Cancel(context.Background(), true); err != nil || len(s.recoveries) != 1 || s.recoveries[0] != privateCanary || len(s.saves) != 1 {
			t.Fatal("failed save could not be encrypted/released without overwriting", err)
		}
	}
}

func TestUnconfirmedPublicationAdvancesBaselineAndRetainsChild(t *testing.T) {
	w, e, s := model(t)
	s.saveErrs = []error{scratchpad.ErrDurabilityUnconfirmed, nil}
	r, err := w.Finish(context.Background())
	if !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) || !r.Published || !r.DurabilityUnconfirmed || r.Closed || e.closes != 0 || w.Retained().Baseline != privateCanary {
		t.Fatal("unconfirmed publication was hidden, rolled back or released")
	}
	r, err = w.Finish(context.Background())
	if err != nil || !r.Published || r.DurabilityUnconfirmed || !r.Closed || s.saves[1] != [2]string{privateCanary, privateCanary} {
		t.Fatal("explicit durability retry used stale original or skipped publication", err)
	}
}

func TestUnconfirmedPublicationCancelStillRequiresRecovery(t *testing.T) {
	w, e, s := model(t)
	s.saveErrs = []error{scratchpad.ErrDurabilityUnconfirmed}
	_, _ = w.Finish(context.Background())
	if _, err := w.Cancel(context.Background(), false); err != ErrConfirmationRequired || e.closes != 0 {
		t.Fatal("unconfirmed save was treated as a clean cancellation")
	}
	r, err := w.Cancel(context.Background(), true)
	if err != nil || !r.Published || !r.DurabilityUnconfirmed || !r.Closed || len(r.Recoveries) != 1 || s.recoveries[0] != privateCanary {
		t.Fatal("unconfirmed save was released without recovery or lost publication fact", err)
	}
}

func TestCancelConsentDoesNotFreezeAndRecoveryFailuresKeepOwnership(t *testing.T) {
	w, e, s := model(t)
	if _, err := w.Cancel(context.Background(), false); err != ErrConfirmationRequired || e.freezes != 0 || e.r.Mirror.Frozen {
		t.Fatal("declining cancellation changed the editor")
	}
	s.receipts = []receipt{{"/synthetic/unconfirmed.age", scratchpad.ErrDurabilityUnconfirmed}, {"/synthetic/confirmed.age", nil}}
	r, err := w.Cancel(context.Background(), true)
	if !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) || r.Closed || e.closes != 0 || len(r.Recoveries) != 1 || r.Recoveries[0].Durable {
		t.Fatal("failed recovery discarded a child/path or claimed durability")
	}
	r.Recoveries[0].Path = "caller-mutated"
	r, err = w.Cancel(context.Background(), true)
	if err != nil || !r.Closed || len(r.Recoveries) != 2 || r.Recoveries[0].Path != "/synthetic/unconfirmed.age" || !r.Recoveries[1].Durable {
		t.Fatal("recovery retry lost original publication or aliased caller state", err)
	}
}

func TestMissingRecoveryReceiptFailsClosed(t *testing.T) {
	w, e, s := model(t)
	s.receipts = []receipt{{"", nil}}
	if r, err := w.Cancel(context.Background(), true); !errors.Is(err, ErrRecoveryReceipt) || r.Closed || e.closes != 0 {
		t.Fatal("empty recovery receipt permitted cleanup")
	}
}

func TestCloseFailureKeepsSavedFactAndDoesNotRepublish(t *testing.T) {
	for _, reaped := range []bool{false, true} {
		w, e, s := model(t)
		e.closeErr = editorcore.ErrProtocol
		e.reapOnCloseError = reaped
		r, err := w.Finish(context.Background())
		if !errors.Is(err, editorcore.ErrProtocol) || !r.Published || r.Closed != reaped || w.Retained().Baseline != privateCanary {
			t.Fatal("close failure changed publication outcome")
		}
		e.closeErr = nil
		r, err = w.Finish(context.Background())
		if len(s.saves) != 1 || !r.Published || !r.Closed {
			t.Fatal("cleanup retry republished or lost saved fact")
		}
		if reaped && !errors.Is(err, editorcore.ErrProtocol) {
			t.Fatal("idempotent result silently erased terminal cleanup error")
		}
		if !reaped && err != nil {
			t.Fatal("cleanup retry did not settle", err)
		}
	}
}

func TestFreezeTimeoutSettlesSameAcknowledgementBeforeSaving(t *testing.T) {
	w, e, s := model(t)
	e.freezeErr = context.DeadlineExceeded
	if _, err := w.Finish(context.Background()); !errors.Is(err, context.DeadlineExceeded) || len(s.saves) != 0 || e.closes != 0 {
		t.Fatal("unacknowledged freeze authorized publication/cleanup")
	}
	ack := e.r.Mirror
	ack.Frozen = true
	e.resolveTo = &ack
	e.freezeErr = nil
	if _, err := w.Finish(context.Background()); err != nil || e.freezes != 1 || len(s.saves) != 1 {
		t.Fatal("late freeze was replaced by another request or not settled", err)
	}
}

func TestCancellationCannotDetachInFlightPublication(t *testing.T) {
	w, e, s := model(t)
	entered, release := make(chan struct{}), make(chan struct{})
	s.saveHook = func() error { close(entered); <-release; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, err := w.Finish(ctx); done <- outcome{r, err} }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("cancel detached publication still owned by the backend")
	default:
	}
	wait, stop := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer stop()
	if r, err := w.Cancel(wait, true); !errors.Is(err, context.DeadlineExceeded) || r.Published || r.Closed {
		t.Fatal("competing cancellation bypassed the active operation")
	}
	close(release)
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || !got.result.Published || got.result.Closed || e.Retained().Closed {
			t.Fatal("settled publication was hidden by cancellation or closed implicitly")
		}
	case <-time.After(time.Second):
		t.Fatal("publication did not settle")
	}
	if r, err := w.Finish(context.Background()); err != nil || !r.Published || !r.Closed || len(s.saves) != 1 {
		t.Fatal("post-cancellation cleanup republished or failed", err)
	}
}

func TestRecoverKnownSealsBothCopiesAndRetainsPartialReceipts(t *testing.T) {
	w, e, s := model(t)
	candidate := e.r.Mirror
	candidate.Revision++
	candidate.Text += "candidate"
	e.r.Candidate = &candidate
	e.r.LatestUnknown = true
	s.receipts = []receipt{{"/synthetic/mirror.age", nil}, {"/synthetic/candidate-unconfirmed.age", scratchpad.ErrDurabilityUnconfirmed}, {"/synthetic/candidate.age", nil}}
	if _, err := w.RecoverKnown(context.Background(), false); err != ErrConfirmationRequired || len(s.recoveries) != 0 {
		t.Fatal("loss escape lacked explicit consent")
	}
	r, err := w.RecoverKnown(context.Background(), true)
	if !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) || r.Closed || e.discards != 0 || len(r.Recoveries) != 2 {
		t.Fatal("partial recovery lost an artifact or discarded the child")
	}
	r, err = w.RecoverKnown(context.Background(), true)
	if err != nil || !r.Closed || !r.MayHaveLostEdits || r.Disposition != RecoveredKnown || len(s.recoveries) != 3 || s.recoveries[0] != privateCanary || s.recoveries[2] != candidate.Text {
		t.Fatal("known-copy retry repeated the mirror, lost candidate or claimed latest", err)
	}
	retained := w.Retained()
	if retained.Editor.Mirror.Text != privateCanary || retained.Editor.Candidate.Text != candidate.Text || r.Published || len(s.saves) != 0 {
		t.Fatal("known-copy escape changed publication or retained bytes")
	}
}

func TestHealthyOrForeignSessionCannotUseKnownCopyEscape(t *testing.T) {
	w, e, s := model(t)
	if _, err := w.RecoverKnown(context.Background(), true); err != ErrNoKnownFailure {
		t.Fatal("healthy session bypassed freeze")
	}
	candidate := e.r.Mirror
	candidate.DocumentID = "2026-01-16"
	e.r.Candidate, e.r.LatestUnknown = &candidate, true
	if _, err := w.RecoverKnown(context.Background(), true); err != ErrIdentity || len(s.recoveries) != 0 || e.discards != 0 {
		t.Fatal("foreign candidate caused storage/cleanup effects")
	}
}

func TestErrorFormattingIsContentFree(t *testing.T) {
	cause := errors.New(privateCanary + "/synthetic/private-path")
	err := failure(privateCanary, cause)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), privateCanary) || strings.Contains(err.Error(), "/synthetic/") {
		t.Fatal("lifecycle error exposed untrusted cause/stage or lost unwrap semantics")
	}
}
