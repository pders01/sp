package tui

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSharedTerminalUnlockRejectsWithoutAuthenticatingPrefix(t *testing.T) {
	for _, raw := range []string{
		bracket("prefix\rsuffix") + "\r", bracket("prefix\nsuffix") + "\r",
		bracket("prefix\tsuffix") + "\r", bracket("prefix\x15suffix") + "\r",
		bracket("prefix\x03suffix") + "\r", bracket("prefix\xffsuffix") + "\r",
		bracket(strings.Repeat("x", 4097)) + "\r", "prefix\xff\r", "prefix\x00\r",
		"prefix\xc3\r", bracket(strings.Repeat("x", privateInputLimit+1)) + "\r",
	} {
		for _, standalone := range []bool{false, true} {
			calls := 0
			a := NewApp(NewCalendar(nil), NewNotebook(nil), ModeNotebook)
			a.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { calls++; return nil }})
			a.startUnlock()
			var target tea.Model = a
			var screen *unlockScreen
			if standalone {
				screen = &unlockScreen{app: a}
				target = screen
			}
			fragment := 1
			if len(raw) > 8192 {
				fragment = 256
			}
			runTerminalBytes(t, target, raw, fragment)
			// Even an additional physical submission must not authenticate a prefix.
			// After a rejected Enter has cleared everything, empty input remains invalid.
			runTerminalBytes(t, target, "\r", 256)
			if calls != 0 || (screen != nil && screen.accepted) {
				t.Fatalf("standalone=%v authenticated malformed/oversize prefix (input len %d)", standalone, len(raw))
			}
			a.Close()
		}
	}
}
func TestSharedTerminalUnlockLabelsRejectedPastes(t *testing.T) {
	for _, payload := range []string{"prefix\t\r\nsuffix", "prefix\xffsuffix"} {
		f := newUnlockFixture(t)
		runTerminalBytes(t, f.s, bracket(payload), 1)
		if !f.s.app.privacy.promptInvalid || !strings.Contains(f.s.app.privacy.promptErr, "Input rejected") {
			t.Fatal("rejected paste was not clearly labelled or did not poison submission")
		}
	}
}

func TestSharedTerminalUnlockPreservesUTF8(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		var got string
		a := NewApp(NewCalendar(nil), NewNotebook(nil), ModeNotebook)
		defer a.Close()
		a.SetPrivacy(PrivacyHooks{Unlock: func(secret []byte) error { got = string(secret); return errors.New("synthetic mismatch") }})
		a.startUnlock()
		var target tea.Model = a
		if standalone {
			target = &unlockScreen{app: a}
		}
		want := "before \ufffd 日 e\u0301 after"
		runTerminalBytes(t, target, "before \ufffd "+bracket("日 e\u0301 after"), 1)
		if a.privacy.prompt.Value() != want {
			t.Fatalf("standalone=%v secret changed: %q", standalone, a.privacy.prompt.Value())
		}
		// For standalone a generic non-age error is fatal, but the synthetic callback
		// must still receive precisely the complete secret, never a decoder prefix.
		runTerminalBytes(t, target, "\r", 1)
		if got != want {
			t.Fatalf("standalone=%v submitted %q", standalone, got)
		}
	}
}
func TestSharedTerminalMalformedUTF8AllSplits(t *testing.T) {
	for _, invalid := range []string{"\x80", "\xff", "\xc0\xaf", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xf0\x9f", "\xc3"} {
		raw := "prefix" + invalid + "\r\r\x07"
		for split := 1; split < len(raw); split++ {
			calls := 0
			a := NewApp(NewCalendar(nil), NewNotebook(nil), ModeNotebook)
			a.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { calls++; return nil }})
			a.startUnlock()
			runTerminalReader(t, a, io.MultiReader(strings.NewReader(raw[:split]), strings.NewReader(raw[split:])), false)
			if calls != 0 {
				t.Fatalf("malformed %x split %d authenticated a prefix", invalid, split)
			}
			a.Close()
		}
	}
}

func TestStandaloneUnlockSignalsCancelHelpAndBusyWork(t *testing.T) {
	f := newUnlockFixture(t)
	r := newTerminalInput(strings.NewReader(""))
	f.s.Update(tea.KeyMsg{Type: tea.KeyF1})
	f.s.Update(r.filter(f.s, tea.Quit()))
	if !f.s.done || f.s.accepted || !errors.Is(f.s.err, ErrUnlockCancelled) {
		t.Fatal("signal quit was reported as accepted or merely closed help")
	}
	f = newUnlockFixture(t)
	job := f.submit("synthetic")
	<-f.entered
	f.s.Update(r.filter(f.s, tea.Quit()))
	if f.s.done || !job.cancelRequested {
		t.Fatal("signal did not wait for owned crypto")
	}
	f.release <- nil
	<-job.done
	f.s.Update(<-job.result)
	if f.s.accepted || f.unlocked.Load() || !errors.Is(f.s.err, ErrUnlockCancelled) {
		t.Fatal("signal cancellation left an unlocked handoff")
	}
}
