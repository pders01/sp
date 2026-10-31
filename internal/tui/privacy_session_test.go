package tui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type privacySessionFixture struct {
	app      *App
	entered  chan []byte
	release  chan error
	unlocked atomic.Bool
	locks    atomic.Int32
}

func newPrivacySessionFixture(t *testing.T) *privacySessionFixture {
	t.Helper()
	f := &privacySessionFixture{app: newTestApp(ModeNotebook), entered: make(chan []byte, 8), release: make(chan error, 8)}
	f.app.SetPrivacy(PrivacyHooks{
		AsyncUnlock: true,
		IsUnlocked:  f.unlocked.Load,
		Unlock: func(secret []byte) error {
			f.entered <- secret
			err := <-f.release
			f.unlocked.Store(err == nil)
			return err
		},
		Lock:    func() { f.unlocked.Store(false); f.locks.Add(1) },
		Recover: func(string, string) (string, error) { return "/synthetic/recovery.age", nil },
		Load: func(string) (PrivatePage, error) {
			page := PrivatePage{Locked: !f.unlocked.Load(), HasPrivate: true, Shared: "public"}
			if !page.Locked {
				page.Private, page.Applied = "PRIVATE CANARY", []string{"one"}
			}
			return page, nil
		},
	})
	f.app.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	t.Cleanup(func() {
		// Release a worker even when an assertion fails before its completion.
		f.release <- errors.New("test cleanup")
		if err := f.app.closePrivacy(); err != nil {
			t.Errorf("close privacy: %v", err)
		}
		f.app.Close()
	})
	return f
}

func privacySessionSubmit(t *testing.T, f *privacySessionFixture, secret string) (*privateUnlockJob, []byte, tea.Cmd) {
	t.Helper()
	f.app.Update(privacyKey(secret))
	_, cmd := f.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	job := f.app.privacy.job
	if job == nil || cmd == nil {
		t.Fatal("accepted unlock did not create an owned job and progress commands")
	}
	// No returned command has run: crypto must already have started.
	borrowed := <-f.entered
	if string(borrowed) != secret {
		t.Fatal("unlock changed literal secret bytes")
	}
	if f.app.privacy.prompt.Value() != "" {
		t.Fatal("prompt retained a submitted secret")
	}
	return job, borrowed, cmd
}

func privacySessionComplete(f *privacySessionFixture, job *privateUnlockJob, err error) privateUnlockedMsg {
	f.release <- err
	<-job.done
	msg := <-job.result
	f.app.Update(msg)
	return msg
}

func TestPrivacySessionAsyncRetryAndProgress(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.app.Update(privacyKey("u"))
	job, secret, cmd := privacySessionSubmit(t, f, " wrong secret ")
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatal("unlock must return both a completion waiter and spinner ticks")
	}
	if !strings.Contains(f.app.View(), "Unlocking") || strings.Contains(f.app.View(), "wrong secret") {
		t.Fatal("busy UI did not render safely")
	}
	f.app.Update(privateUnlockTickMsg{job: job})
	if job.frame != 1 {
		t.Fatal("progress tick was not routed")
	}
	first := privacySessionComplete(f, job, errors.New("incorrect passphrase"))
	if strings.Trim(string(secret), "\x00") != "" {
		t.Fatal("job did not clear its secret slice")
	}
	if f.app.privacy.prompt == nil || f.app.privacy.prompt.Value() != "" || f.app.privacy.prompt.EchoMode != textinput.EchoPassword {
		t.Fatal("failed unlock did not retain an empty masked retry prompt")
	}
	job, secret, _ = privacySessionSubmit(t, f, " exact correct secret ")
	f.app.Update(first)
	if f.app.privacy.job != job || f.app.privacy.prompt == nil {
		t.Fatal("stale completion mutated a newer unlock")
	}
	privacySessionComplete(f, job, nil)
	if !f.unlocked.Load() || f.app.privacy.prompt != nil || strings.Trim(string(secret), "\x00") != "" {
		t.Fatal("successful retry did not finish and clear its secret")
	}
}

func TestPrivacySessionCancelWaitsAndLocksSuccessfulKDF(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	job, _, _ := privacySessionSubmit(t, f, "synthetic secret")
	f.app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	f.app.Update(privacyKey("e"))
	f.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.app.privacy.job != job || !job.cancelRequested || f.app.privacy.editor != nil || f.app.privacy.editAfter != "" || f.locks.Load() != 0 {
		t.Fatal("cancel must retain ownership and block actions until KDF completion")
	}
	if !strings.Contains(f.app.View(), "waiting for key derivation") {
		t.Fatal("cancellation wait is not visible")
	}
	privacySessionComplete(f, job, nil)
	if f.unlocked.Load() || f.locks.Load() != 1 || f.app.privacy.prompt != nil || f.app.privacy.editor != nil {
		t.Fatal("cancelled successful KDF was not locked without resuming editing")
	}
}

func TestPrivacySessionCloseOwnsUnstartedCommand(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.app.Update(privacyKey("u"))
	job, secret, _ := privacySessionSubmit(t, f, "synthetic secret")
	closing, closed := make(chan struct{}), make(chan error, 1)
	go func() {
		close(closing)
		closed <- f.app.closePrivacy()
	}()
	<-closing
	select {
	case <-closed:
		t.Fatal("shutdown returned while KDF was still blocked")
	default:
	}
	f.release <- nil
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if f.unlocked.Load() || f.locks.Load() != 1 || strings.Trim(string(secret), "\x00") != "" {
		t.Fatal("shutdown did not wait, clear the secret, and lock")
	}
	// A buffered completion can arrive after Close; it cannot reopen anything.
	f.app.Update(<-job.result)
	f.app.Update(privacyKey("u"))
	if f.app.privacy.prompt != nil || f.app.privacy.editor != nil || !f.app.privacy.closed {
		t.Fatal("late completion or shortcut reopened a closed session")
	}
}

func TestPrivacySessionTemplateIntentResumesAndCancels(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume", true: "cancel"}[cancel], func(t *testing.T) {
			f := newPrivacySessionFixture(t)
			f.app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
				return TemplateApplyResult{}, nil
			})
			date := f.app.privateDate()
			f.app.Update(privacyKey("a"))
			if f.app.privacy.prompt == nil || f.app.privacy.templateAfter != date || f.app.templateChooser != nil {
				t.Fatal("locked template action did not prompt with retained intent")
			}
			if cancel {
				f.app.Update(tea.KeyMsg{Type: tea.KeyEsc})
				if f.app.privacy.templateAfter != "" {
					t.Fatal("cancel retained template intent")
				}
				f.app.Update(privacyKey("u"))
			}
			job, _, _ := privacySessionSubmit(t, f, "wrong synthetic secret")
			privacySessionComplete(f, job, errors.New("incorrect passphrase"))
			if !cancel && f.app.privacy.templateAfter != date {
				t.Fatal("failed authentication lost the pending template action")
			}
			job, _, _ = privacySessionSubmit(t, f, "synthetic secret")
			privacySessionComplete(f, job, nil)
			if cancel {
				if f.app.templateChooser != nil {
					t.Fatal("cancelled intent resumed on an unrelated unlock")
				}
			} else if f.app.templateChooser == nil || f.app.templateChooser.date != date || !f.app.templateApplied(date, "one") {
				t.Fatal("successful unlock did not resume the intended chooser with private metadata")
			}
		})
	}
}

func TestPrivacySessionRelockSealsDraftAndClearsProjections(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.unlocked.Store(true)
	f.app.refreshPrivatePage()
	f.app.Update(privacyKey("e"))
	e := f.app.privacy.editor
	e.input.SetValue("PRIVATE DRAFT")
	failed, sealed := true, false
	f.app.privacy.hooks.Recover = nil
	e.recover = func(_, content string) (string, error) {
		if content != "PRIVATE DRAFT" || !f.unlocked.Load() {
			t.Fatal("draft was not sealed before locking")
		}
		if failed {
			return "", errors.New("disk full")
		}
		sealed = true
		return "/synthetic/recovery.age", nil
	}
	f.app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if f.locks.Load() != 0 || f.app.privacy.editor != e || e.input.Value() != "PRIVATE DRAFT" || !strings.Contains(e.status, "Draft retained") {
		t.Fatal("failed sealing discarded the draft or locked its session")
	}
	failed = false
	f.app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if !sealed || f.unlocked.Load() || f.app.privacy.editor != nil || len(f.app.appliedTemplates) != 0 {
		t.Fatal("successful relock did not seal and discard private references")
	}
	if f.app.nb.privatePage == nil || !f.app.nb.privatePage.Locked || f.app.nb.privatePage.Private != "" || len(f.app.nb.privatePage.Applied) != 0 {
		t.Fatal("relock retained a decrypted page or metadata")
	}
	if strings.Contains(f.app.View(), "PRIVATE") || f.app.privacy.keyHelp != nil || f.app.templateChooser != nil {
		t.Fatal("relock left private modal/view projections")
	}
	if f.app.nb.privatePage.Shared != "public" {
		t.Fatal("relock discarded intentionally shared content")
	}
}

func TestPrivacySessionEditorModalKeysBeforeRelock(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.unlocked.Store(true)
	f.app.Update(privacyKey("e"))
	e := f.app.privacy.editor
	e.Update(tea.KeyMsg{Type: tea.KeyF1})
	f.app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if e.keyHelp == nil || f.locks.Load() != 0 {
		t.Fatal("relock stole a help key")
	}
	e.keyHelp = nil
	e.review = newDraftReview(e.date, "saved", "draft", 90, 25)
	f.app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if e.review == nil || f.locks.Load() != 0 {
		t.Fatal("relock stole a review key")
	}
	e.review = nil
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	f.app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	f.app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.input.Finding() || e.done || f.locks.Load() != 0 || f.app.privacy.editor != e {
		t.Fatal("find did not own relock/cancel keys")
	}
	// A batch spelling a key is literal Markdown, never a relock action.
	f.app.Update(privacyKey("ctrl+l"))
	if !strings.Contains(e.input.Value(), "ctrl+l") || f.locks.Load() != 0 {
		t.Fatal("literal editor input became a command")
	}
}

func TestPrivacySessionCloseFailureRetainsDraftButLocksKeys(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.unlocked.Store(true)
	f.app.Update(privacyKey("e"))
	e := f.app.privacy.editor
	e.input.SetValue("PRIVATE DRAFT")
	cause := errors.New("synthetic recovery failure")
	e.recover = func(string, string) (string, error) { return "", cause }
	err := f.app.closePrivacy()
	if !errors.Is(err, cause) || f.unlocked.Load() || f.app.privacy.editor != e || e.input.Value() != "PRIVATE DRAFT" {
		t.Fatal("shutdown lost the recovery error/draft or retained unlocked keys")
	}
	if f.app.nb.privatePage.Private != "" || len(f.app.appliedTemplates) != 0 || strings.Contains(f.app.View(), "PRIVATE DRAFT") {
		t.Fatal("shutdown retained/rendered decrypted reading projections")
	}
	// Restore synthetic encrypted recovery for fixture cleanup.
	e.recover = func(string, string) (string, error) { return "/synthetic/recovery.age", nil }
}

func TestPrivacySessionApplicationCloseWaitsAndLocks(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.app.Update(privacyKey("u"))
	job, _, _ := privacySessionSubmit(t, f, "synthetic secret")
	closed := make(chan struct{})
	go func() { f.app.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("application Close did not wait for unlock")
	default:
	}
	f.release <- nil
	<-closed
	if f.unlocked.Load() || f.locks.Load() != 1 {
		t.Fatal("application Close allowed late unlocked state")
	}
	f.app.Update(<-job.result)
	if f.app.privacy.prompt != nil || f.app.privacy.editor != nil {
		t.Fatal("completion reopened a closed application")
	}
}

func TestPrivacySessionFailedReconfigurationLeavesDraftUsable(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.unlocked.Store(true)
	f.app.Update(privacyKey("e"))
	e := f.app.privacy.editor
	e.input.SetValue("PRIVATE DRAFT")
	e.recover = func(string, string) (string, error) { return "", errors.New("synthetic disk failure") }
	f.app.SetPrivacy(PrivacyHooks{})
	if f.app.privacy.closed || f.app.privacy.editor != e || !f.unlocked.Load() {
		t.Fatal("failed reconfiguration trapped the only draft")
	}
	f.app.Update(privacyKey("X"))
	if !strings.Contains(e.input.Value(), "X") {
		t.Fatal("draft was not editable after failed reconfiguration")
	}
	e.recover = func(string, string) (string, error) { return "/synthetic/recovery.age", nil }
}

func TestPrivacySessionMetadataResumeKeepsDetailedError(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	unlocked := false
	app.SetPrivacy(PrivacyHooks{IsUnlocked: func() bool { return unlocked }, Unlock: func([]byte) error { unlocked = true; return nil }, Load: func(string) (PrivatePage, error) {
		if unlocked {
			return PrivatePage{}, errors.New("specific metadata read failure")
		}
		return PrivatePage{Locked: true}, nil
	}})
	app.Update(privacyKey("a"))
	app.Update(privacyKey("synthetic"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(app.nb.theme.StatusText(), "specific metadata read failure") {
		t.Fatal("resume hid actionable load error")
	}
}

func TestPrivacySessionAsyncRequiresLockHook(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.app.privacy.hooks.Lock = nil
	f.app.Update(privacyKey("u"))
	f.app.Update(privacyKey("synthetic secret"))
	f.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.app.privacy.job != nil || f.app.privacy.prompt.Value() != "" || !strings.Contains(f.app.privacy.promptErr, "lock hook") {
		t.Fatal("async crypto started without a way to lock after cancellation")
	}
}

func TestPrivacySessionValidationNeverLaunchesInvalidSecret(t *testing.T) {
	f := newPrivacySessionFixture(t)
	f.app.Update(privacyKey("u"))
	f.app.Update(privacyKey("bad\nsecret"))
	f.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.app.privacy.job != nil || f.app.privacy.prompt.Value() != "" || f.app.privacy.promptErr == "" {
		t.Fatal("invalid secret reached an unlock job or remained in the prompt")
	}
	select {
	case <-f.entered:
		t.Fatal("invalid secret reached crypto")
	default:
	}
}
