package privateedit

import (
	"context"
	"errors"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/scratchpad"
)

// Entirely in-memory lifecycle fixtures: no editor executable, manager, keys,
// filesystem publication, terminal or platform boundary is exercised here.
type checkpointUI struct {
	editorcore.UI
	r            editorcore.Observation
	resumes      int
	closes       int
	resumeErr    error
	revokeOnFail bool
	beforeResume func()
}

func (u *checkpointUI) Retained() editorcore.Observation {
	r := u.r
	if r.Action != nil {
		a := *r.Action
		r.Action = &a
	}
	return r
}
func (u *checkpointUI) Resolve(context.Context) error { return nil }
func (u *checkpointUI) Resume(ctx context.Context, exact editorcore.Snapshot) (editorcore.Snapshot, error) {
	u.resumes++
	if u.beforeResume != nil {
		u.beforeResume()
	}
	if err := ctx.Err(); err != nil {
		return u.r.Observed, err
	}
	if !currentEmbeddedFreeze(u.r) || exact != u.r.Action.Snapshot {
		return u.r.Observed, editorcore.ErrNotFrozen
	}
	if u.resumeErr != nil {
		if u.revokeOnFail {
			u.r.Action = nil
			u.r.LatestUnknown = true
		}
		return u.r.Observed, u.resumeErr
	}
	u.r.Action = nil
	u.r.Observed.Frozen = false
	return u.r.Observed, nil
}
func (u *checkpointUI) Close(_ context.Context, exact editorcore.Snapshot) error {
	u.closes++
	if !currentEmbeddedFreeze(u.r) || exact != u.r.Observed {
		return editorcore.ErrNotFrozen
	}
	u.r.Closed = true
	return nil
}
func (u *checkpointUI) edit(t *testing.T, text string) {
	t.Helper()
	if u.r.Observed.Frozen || u.r.Closed || u.r.LatestUnknown {
		t.Fatal("fixture attempted editing without a resumed healthy owner")
	}
	u.r.Observed.Text = text
	u.r.Observed.Revision++
}
func (u *checkpointUI) freeze(kind string) {
	u.r.Observed.Frozen = true
	u.r.Action = &editorcore.Action{Kind: kind, Snapshot: u.r.Observed}
}

type checkpointStore struct {
	content    string
	saves      [][2]string
	nextErr    error
	recoveries []string
	afterSave  func()
}

func (s *checkpointStore) save(original, content string) error {
	s.saves = append(s.saves, [2]string{original, content})
	if original != s.content {
		return scratchpad.ErrConflict
	}
	err := s.nextErr
	s.nextErr = nil
	if err == nil || errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
		s.content = content
	}
	if s.afterSave != nil {
		s.afterSave()
	}
	return err
}
func (s *checkpointStore) recover(content string) (string, error) {
	s.recoveries = append(s.recoveries, content)
	return "/synthetic/checkpoint-recovery.age", nil
}
func checkpointFixture() (*Embedded, *checkpointUI, *checkpointStore) {
	u := &checkpointUI{r: editorcore.Observation{Observed: editorcore.Snapshot{
		DocumentID: "2000-01-01", SessionID: "same-generation", Revision: 1, Text: "baseline",
	}}}
	s := &checkpointStore{content: "baseline"}
	return &Embedded{ui: u, workflow: newWorkflow(embeddedEditor{u}, s, s.content)}, u, s
}

func TestCheckpointRepeatedExactPublicationThenFinish(t *testing.T) {
	e, u, s := checkpointFixture()
	ctx := context.Background()
	baseline := "baseline"
	for _, text := range []string{privateCanary, privateCanary + "later\x00\n", privateCanary + "later\x00\n"} {
		u.edit(t, text)
		u.freeze("save")
		u.beforeResume = func() {
			if e.workflow.frozen != nil || e.Retained().Baseline != text || s.content != text {
				t.Fatal("resume preceded publication or retained old freeze authorization")
			}
		}
		before := len(s.saves)
		r, err := e.Checkpoint(ctx)
		if err != nil || !r.Published || r.DurabilityUnconfirmed || r.Closed || r.Disposition != Active || u.r.Observed.Frozen || u.r.Action != nil || u.closes != 0 {
			t.Fatal("checkpoint failed to publish and retain live ownership", err)
		}
		if text != baseline {
			if len(s.saves) != before+1 || s.saves[before] != [2]string{baseline, text} {
				t.Fatal("checkpoint used stale baseline or changed exact bytes")
			}
		} else if len(s.saves) != before {
			t.Fatal("unchanged checkpoint republished")
		}
		baseline = text
		if _, err := e.Checkpoint(ctx); !errors.Is(err, editorcore.ErrNotFrozen) {
			t.Fatal("old save action remained publication permission")
		}
	}
	u.edit(t, "final exact\r\n🙂\n\n")
	u.freeze("finish")
	r, err := e.Finish(ctx)
	if err != nil || !r.Published || !r.Closed || r.Disposition != Finished || u.resumes != 3 || u.closes != 1 || len(s.saves) != 3 || s.saves[2] != [2]string{baseline, u.r.Observed.Text} || u.r.Observed.SessionID != "same-generation" {
		t.Fatal("finish after checkpoints did not preserve lifecycle/baseline", err)
	}
}

func TestCheckpointUnchangedNoPublicationAndCancelRecovery(t *testing.T) {
	for _, changedAfter := range []bool{false, true} {
		e, u, s := checkpointFixture()
		u.freeze("save")
		r, err := e.Checkpoint(context.Background())
		if err != nil || r.Published || r.Closed || r.Disposition != Active || len(s.saves) != 0 || u.resumes != 1 {
			t.Fatal("unchanged checkpoint fabricated publication or closed", err)
		}
		if changedAfter {
			u.edit(t, privateCanary)
		}
		u.freeze("cancel")
		if changedAfter {
			if _, err := e.Cancel(context.Background(), false); !errors.Is(err, ErrConfirmationRequired) {
				t.Fatal("checkpoint bypassed draft recovery consent")
			}
		}
		r, err = e.Cancel(context.Background(), true)
		if err != nil || !r.Closed || r.Published || len(s.saves) != 0 || (changedAfter && (len(s.recoveries) != 1 || s.recoveries[0] != privateCanary)) || (!changedAfter && len(s.recoveries) != 0) {
			t.Fatal("cancel after checkpoint lost recovery semantics", err)
		}
	}
}

func TestCheckpointRejectedPublicationRemainsFrozenForExplicitRetry(t *testing.T) {
	for _, injected := range []error{scratchpad.ErrConflict, scratchpad.ErrLocked, scratchpad.ErrPrivacyPolicyChanged, editorcore.ErrRemote} {
		t.Run(injected.Error(), func(t *testing.T) {
			e, u, s := checkpointFixture()
			u.edit(t, privateCanary)
			u.freeze("save")
			s.nextErr = injected
			r, err := e.Checkpoint(context.Background())
			if !errors.Is(err, injected) || r.Published || r.Closed || r.Disposition != Active || u.resumes != 0 || u.closes != 0 || !currentEmbeddedFreeze(u.r) || e.Retained().Baseline != "baseline" || s.content != "baseline" {
				t.Fatal("failed checkpoint changed publication/owner facts")
			}
			if len(s.saves) != 1 {
				t.Fatal("checkpoint retried without consent")
			}
			r, err = e.Checkpoint(context.Background())
			if err != nil || !r.Published || r.Closed || u.resumes != 1 || len(s.saves) != 2 || s.saves[1] != [2]string{"baseline", privateCanary} {
				t.Fatal("explicit retry did not use retained baseline", err)
			}
		})
	}
}

func TestCheckpointLaterAuthFailureKeepsEarlierPublicationAndRecovery(t *testing.T) {
	for _, injected := range []error{scratchpad.ErrLocked, scratchpad.ErrPrivacyPolicyChanged} {
		e, u, s := checkpointFixture()
		u.edit(t, privateCanary)
		u.freeze("save")
		if _, err := e.Checkpoint(context.Background()); err != nil {
			t.Fatal(err)
		}
		later := privateCanary + "later unsaved edits"
		u.edit(t, later)
		u.freeze("save")
		s.nextErr = injected
		r, err := e.Checkpoint(context.Background())
		if !errors.Is(err, injected) || !r.Published || r.DurabilityUnconfirmed || r.Closed || e.Retained().Baseline != privateCanary || s.content != privateCanary || u.resumes != 1 || u.r.Observed.Text != later {
			t.Fatal("later auth failure rewound earlier publication or lost draft")
		}
		if _, err := e.Cancel(context.Background(), false); !errors.Is(err, ErrConfirmationRequired) {
			t.Fatal("earlier checkpoint bypassed later draft recovery consent")
		}
		r, err = e.Cancel(context.Background(), true)
		if err != nil || !r.Published || !r.Closed || r.Disposition != Cancelled || len(s.saves) != 2 || len(s.recoveries) != 1 || s.recoveries[0] != later || s.content != privateCanary {
			t.Fatal("later auth failure prevented explicit retained-owner recovery", err)
		}
	}
}

func TestCheckpointConflictResumeDoesNotResetCAS(t *testing.T) {
	e, u, s := checkpointFixture()
	u.edit(t, privateCanary)
	s.content = "competing publication"
	for i := 0; i < 2; i++ {
		u.freeze("save")
		r, err := e.Checkpoint(context.Background())
		if !errors.Is(err, scratchpad.ErrConflict) || r.Published || e.Retained().Baseline != "baseline" || s.content != "competing publication" {
			t.Fatal("conflicting checkpoint overwrote newer bytes or reset baseline")
		}
		if err := e.Resume(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	u.freeze("save")
	r, err := e.Cancel(context.Background(), true)
	if err != nil || !r.Closed || len(s.recoveries) != 1 || s.recoveries[0] != privateCanary || s.content != "competing publication" {
		t.Fatal("conflicting save could not be explicitly recovered", err)
	}
}

func TestCheckpointUnconfirmedPublicationRetriesAdvancedBaseline(t *testing.T) {
	e, u, s := checkpointFixture()
	u.edit(t, privateCanary)
	u.freeze("save")
	s.nextErr = scratchpad.ErrDurabilityUnconfirmed
	r, err := e.Checkpoint(context.Background())
	if !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) || !r.Published || !r.DurabilityUnconfirmed || r.Closed || r.Disposition != Active || u.resumes != 0 || !currentEmbeddedFreeze(u.r) || e.Retained().Baseline != privateCanary || s.content != privateCanary {
		t.Fatal("unconfirmed checkpoint hid publication or resumed")
	}
	r, err = e.Checkpoint(context.Background())
	if err != nil || !r.Published || r.DurabilityUnconfirmed || r.Closed || u.resumes != 1 || len(s.saves) != 2 || s.saves[1] != [2]string{privateCanary, privateCanary} {
		t.Fatal("durability retry skipped write or used stale CAS", err)
	}
}

func TestCheckpointFailedResumeRetainsPublicationTruthAndRevokesFreeze(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		e, u, s := checkpointFixture()
		u.edit(t, privateCanary)
		u.freeze("save")
		u.resumeErr, u.revokeOnFail = editorcore.ErrPending, revoke
		u.beforeResume = func() {
			if e.workflow.frozen != nil {
				t.Fatal("cached freeze not revoked before resume transmission")
			}
		}
		r, err := e.Checkpoint(context.Background())
		if !errors.Is(err, editorcore.ErrPending) || !r.Published || r.DurabilityUnconfirmed || r.Closed || r.Disposition != Active || e.Retained().Baseline != privateCanary || s.content != privateCanary || len(s.saves) != 1 || e.workflow.frozen != nil || u.closes != 0 {
			t.Fatal("resume failure rolled back durable publication or released owner")
		}
		if revoke {
			if _, err := e.Checkpoint(context.Background()); !errors.Is(err, editorcore.ErrNotFrozen) || len(s.saves) != 1 {
				t.Fatal("revoked/uncertain action authorized another save")
			}
		} else {
			u.resumeErr = nil
			if _, err := e.Checkpoint(context.Background()); err != nil || len(s.saves) != 1 || u.r.Observed.Frozen {
				t.Fatal("confirmed publication retry unnecessarily republished", err)
			}
		}
	}
}

func TestCheckpointAutoResumeRequiresSameExactActionAfterPublication(t *testing.T) {
	e, u, s := checkpointFixture()
	u.edit(t, privateCanary)
	u.freeze("save")
	// A faulty adapter changes its action while the store call is owned. The
	// publication remains true, but no different handoff can authorize resume.
	s.afterSave = func() { u.r.Action.Kind = "finish" }
	r, err := e.Checkpoint(context.Background())
	if !errors.Is(err, editorcore.ErrNotFrozen) || !r.Published || r.DurabilityUnconfirmed || r.Closed || e.Retained().Baseline != privateCanary || len(s.saves) != 1 || u.resumes != 0 || e.workflow.frozen != nil {
		t.Fatal("checkpoint resumed a different action or rolled publication back")
	}
}

func TestCheckpointCancellationAfterPublicationKeepsTruth(t *testing.T) {
	e, u, s := checkpointFixture()
	u.edit(t, privateCanary)
	u.freeze("save")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.afterSave = cancel
	r, err := e.Checkpoint(ctx)
	if !errors.Is(err, context.Canceled) || !r.Published || r.DurabilityUnconfirmed || r.Closed || e.Retained().Baseline != privateCanary || len(s.saves) != 1 || u.resumes != 0 {
		t.Fatal("cancelled auto-resume hid completed publication")
	}
}

func TestCheckpointRejectsWrongForeignAndUnhealthyAction(t *testing.T) {
	cases := map[string]func(*checkpointUI){
		"no action":          func(u *checkpointUI) { u.r.Action = nil },
		"finish":             func(u *checkpointUI) { u.r.Action.Kind = "finish" },
		"cancel":             func(u *checkpointUI) { u.r.Action.Kind = "cancel" },
		"unknown kind":       func(u *checkpointUI) { u.r.Action.Kind = "foreign" },
		"unfrozen":           func(u *checkpointUI) { u.r.Observed.Frozen = false; u.r.Action.Snapshot = u.r.Observed },
		"different revision": func(u *checkpointUI) { u.r.Action.Snapshot.Revision++ },
		"different text":     func(u *checkpointUI) { u.r.Action.Snapshot.Text = "other" },
		"foreign generation": func(u *checkpointUI) { u.r.Observed.SessionID = "foreign"; u.r.Action.Snapshot = u.r.Observed },
		"foreign document":   func(u *checkpointUI) { u.r.Observed.DocumentID = "1999-12-31"; u.r.Action.Snapshot = u.r.Observed },
		"unknown":            func(u *checkpointUI) { u.r.LatestUnknown = true },
		"fault":              func(u *checkpointUI) { u.r.Fault = editorcore.ErrRemote },
		"closed":             func(u *checkpointUI) { u.r.Closed = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e, u, s := checkpointFixture()
			u.edit(t, privateCanary)
			u.freeze("save")
			mutate(u)
			if _, err := e.Checkpoint(context.Background()); err == nil || len(s.saves) != 0 || u.resumes != 0 || u.closes != 0 || e.Retained().Result.Published || e.Retained().Baseline != "baseline" {
				t.Fatal("invalid action authorized publication/resume")
			}
		})
	}
}
