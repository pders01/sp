package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStandaloneUnlockBracketedPasteCannotSubmitOrEditControls(t *testing.T) {
	for _, control := range []tea.KeyType{tea.KeyEnter, tea.KeyCtrlJ, tea.KeyTab, tea.KeyCtrlU} {
		f := newUnlockFixture(t)
		filter := &unlockInputFilter{}
		filter.sequence([]byte("\x1b[200~"))
		for _, msg := range []tea.KeyMsg{privacyKey("prefix"), {Type: control}, privacyKey("suffix")} {
			if result := filter.filter(f.s, msg); result != nil {
				t.Fatal("paste forwarded a key before its boundary")
			}
		}
		f.s.Update(filter.sequence([]byte("\x1b[201~")))
		f.s.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if f.s.done || f.s.app.privacy.job != nil || !strings.Contains(f.s.app.privacy.promptErr, "invalid input cleared") {
			t.Fatal("pasted control normalized/submitted a partial secret")
		}
	}
}

func TestStandaloneUnlockPreservesLiteralReplacementRune(t *testing.T) {
	f := newUnlockFixture(t)
	filter := &unlockInputFilter{}
	f.s.Update(privacyKey("before "))
	var msg tea.Msg
	for _, b := range []byte("\ufffd") {
		msg = filter.unknownByte(b)
		if msg != nil {
			f.s.Update(msg)
		}
	}
	f.s.Update(privacyKey(" after"))
	if f.s.app.privacy.prompt.Value() != "before \ufffd after" {
		t.Fatal("valid replacement rune disappeared")
	}
	filter.sequence([]byte("\x1b[200~"))
	filter.filter(f.s, privacyKey(" paste "))
	for _, b := range []byte("\ufffd") {
		filter.unknownByte(b)
	}
	f.s.Update(filter.sequence([]byte("\x1b[201~")))
	if f.s.app.privacy.prompt.Value() != "before \ufffd after paste \ufffd" {
		t.Fatal("replacement rune was lost from paste")
	}
}

func TestStandaloneUnlockRejectsInvalidBytesAndOversizePaste(t *testing.T) {
	f := newUnlockFixture(t)
	filter := &unlockInputFilter{}
	f.s.Update(filter.unknownByte(0xff))
	f.s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if f.s.done || f.s.app.privacy.job != nil {
		t.Fatal("invalid byte became cancellation or authentication")
	}
	filter.sequence([]byte("\x1b[200~"))
	filter.appendPaste(bytes.Repeat([]byte{'x'}, 4097))
	f.s.Update(filter.sequence([]byte("\x1b[201~")))
	if !f.s.app.privacy.promptInvalid || len(filter.paste) != 0 || filter.pasting {
		t.Fatal("oversize paste was silently truncated or retained")
	}
	filter.unknownByte(0xef)
	filter.sequence([]byte("\x1b[200~"))
	filter.filter(f.s, privacyKey("valid"))
	if _, ok := filter.sequence([]byte("\x1b[201~")).(unlockRejectedInputMsg); !ok {
		t.Fatal("incomplete bytes disappeared at paste start")
	}
}

func TestStandaloneUnlockSignalsCancelHelpAndBusyWork(t *testing.T) {
	f := newUnlockFixture(t)
	filter := &unlockInputFilter{}
	f.s.Update(tea.KeyMsg{Type: tea.KeyF1})
	f.s.Update(filter.filter(f.s, tea.Quit()))
	if !f.s.done || f.s.accepted || !errors.Is(f.s.err, ErrUnlockCancelled) {
		t.Fatal("signal quit was reported as accepted or merely closed help")
	}
	f = newUnlockFixture(t)
	job := f.submit("synthetic")
	<-f.entered
	f.s.Update(filter.filter(f.s, tea.Quit()))
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
