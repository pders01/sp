package tui

import (
	"bytes"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"filippo.io/age"
	tea "github.com/charmbracelet/bubbletea"
)

type unlockFixture struct {
	s        *unlockScreen
	entered  chan []byte
	release  chan error
	unlocked atomic.Bool
	locks    atomic.Int32
}

func newUnlockFixture(t *testing.T) *unlockFixture {
	t.Helper()
	t.Setenv("GLAMOUR_STYLE", "dark")
	f := &unlockFixture{entered: make(chan []byte, 4), release: make(chan error, 4)}
	f.s = newUnlockScreen(UnlockOptions{Theme: "dark", Title: "Unlock notes", IsUnlocked: f.unlocked.Load, Lock: func() { f.locks.Add(1); f.unlocked.Store(false) }, Unlock: func(secret []byte) error {
		f.entered <- secret
		err := <-f.release
		if err == nil {
			f.unlocked.Store(true)
		}
		return err
	}})
	f.s.Init()
	t.Cleanup(func() { f.release <- errors.New("synthetic cleanup"); f.s.app.Close() })
	return f
}

func (f *unlockFixture) submit(text string) *privateUnlockJob {
	f.s.Update(privacyKey(text))
	f.s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return f.s.app.privacy.job
}

func TestStandaloneUnlockRetriesMaskedWithoutRewritingAndHandsOffKey(t *testing.T) {
	f := newUnlockFixture(t)
	job := f.submit(" wrong synthetic secret ")
	borrowed := <-f.entered
	if string(borrowed) != " wrong synthetic secret " {
		t.Fatal("input rewritten")
	}
	if strings.Contains(f.s.View(), "wrong synthetic") {
		t.Fatal("secret rendered")
	}
	f.release <- &age.NoIdentityMatchError{}
	<-job.done
	f.s.Update(<-job.result)
	if f.s.done || f.s.app.privacy.prompt.Value() != "" || !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
		t.Fatal("retry ended, retained input, or failed to clear submitted bytes")
	}
	job = f.submit(" exact synthetic secret ")
	borrowed = <-f.entered
	if string(borrowed) != " exact synthetic secret " {
		t.Fatal("retry normalized spaces")
	}
	f.release <- nil
	<-job.done
	f.s.Update(<-job.result)
	if !f.s.accepted || !f.s.done || f.s.err != nil || !f.unlocked.Load() {
		t.Fatal("completed unlock was not handed to the caller")
	}
	f.s.app.Close()
	if !f.unlocked.Load() || f.locks.Load() != 0 {
		t.Fatal("successful handoff locked the calling action out")
	}
	f.unlocked.Store(false) // The calling operation owns final key release.
}

func TestStandaloneUnlockCancellationWaitsAndCannotUnlockLate(t *testing.T) {
	f := newUnlockFixture(t)
	job := f.submit("synthetic")
	<-f.entered
	f.s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if f.s.done || !strings.Contains(f.s.View(), "Cancelling unlock") {
		t.Fatal("busy cancellation didn't wait")
	}
	f.release <- nil
	<-job.done
	f.s.Update(<-job.result)
	if !errors.Is(f.s.err, ErrUnlockCancelled) || f.s.accepted || f.unlocked.Load() || f.locks.Load() == 0 {
		t.Fatal("cancelled work handed off an unlocked key")
	}
}

func TestStandaloneUnlockEmptyAndEscapeCancelWithoutWork(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}} {
		f := newUnlockFixture(t)
		f.s.Update(key)
		if !f.s.done || !errors.Is(f.s.err, ErrUnlockCancelled) || f.s.app.privacy.job != nil {
			t.Fatal("cancellation launched work")
		}
	}
}

func TestStandaloneUnlockRejectsInvalidInputAndPreservesSmallForm(t *testing.T) {
	f := newUnlockFixture(t)
	f.s.Update(privacyKey("bad\nsecret"))
	f.s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.s.done || f.s.app.privacy.job != nil || f.s.app.privacy.promptErr == "" {
		t.Fatal("invalid input cancelled or reached crypto")
	}
	f.s.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	f.s.Update(privacyKey("valid"))
	f.s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.s.app.privacy.job != nil || f.s.app.privacy.prompt.Value() != "valid" || !strings.Contains(f.s.View(), "Resize terminal") {
		t.Fatal("invisible field submitted or lost input")
	}
}

func TestStandaloneUnlockMetadataErrorDoesNotRetry(t *testing.T) {
	f := newUnlockFixture(t)
	job := f.submit("synthetic")
	<-f.entered
	cause := errors.New("synthetic metadata failure")
	f.release <- cause
	<-job.done
	f.s.Update(<-job.result)
	if !f.s.done || !errors.Is(f.s.err, cause) || f.s.accepted {
		t.Fatal("non-authentication failure was hidden or retried")
	}
	f.s.app.Close()
	if f.unlocked.Load() || f.locks.Load() == 0 {
		t.Fatal("failed session retained key")
	}
}

func TestStandaloneUnlockHelpOwnsKeysAndCloseOwnsUnstartedWork(t *testing.T) {
	f := newUnlockFixture(t)
	f.s.Update(tea.KeyMsg{Type: tea.KeyF1})
	f.s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.s.done || f.s.app.privacy.job != nil {
		t.Fatal("help forwarded empty submission")
	}
	f.s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	job := f.submit("synthetic")
	<-f.entered
	closed := make(chan struct{})
	go func() { f.s.app.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("close did not wait for owned work")
	default:
	}
	f.release <- nil
	<-closed
	f.s.Update(<-job.result)
	if f.unlocked.Load() || f.s.accepted {
		t.Fatal("late completion reopened the closed session")
	}
}

func TestStandaloneUnlockRequiresRealTerminalDescriptors(t *testing.T) {
	if err := UnlockNotes(UnlockOptions{}); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatal("implicit terminal fallback")
	}
}
