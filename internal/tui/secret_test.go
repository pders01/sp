package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func secretFixture(t *testing.T) *secretScreen {
	t.Helper()
	s := newSecretScreen(SecretOptions{Theme: ThemePrefDark, Title: "New passphrase", Label: "New passphrase", Details: "Synthetic collection only."})
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 80})
	t.Cleanup(s.clear)
	return s
}

func secretKey(s *secretScreen, key tea.KeyType) { s.Update(tea.KeyMsg{Type: key}) }

func TestSecretExactMaskedHandoff(t *testing.T) {
	s := secretFixture(t)
	want := " ée\u0301�\u200d  " // Distinct composed/decomposed bytes, U+FFFD, format rune, spaces.
	secretKey(s, tea.KeySpace)
	s.Update(privatePasteMsg("ée\u0301�\u200d"))
	secretKey(s, tea.KeySpace)
	secretKey(s, tea.KeySpace)
	borrowed := s.prompt.secret
	if s.prompt.Value() != want || s.prompt.EchoMode != textinput.EchoPassword {
		t.Fatal("input changed or became unmasked")
	}
	view := s.View()
	if strings.Contains(view, "ée") || strings.Contains(view, "unlock: ") || !strings.Contains(view, "Enter: accept") || !strings.Contains(view, "New passphrase:") {
		t.Fatal("secret leaked or collection was mislabeled")
	}
	secretKey(s, tea.KeyEnter)
	if !s.done || s.prompt != nil || s.View() != "" || !bytes.Equal([]byte(string(borrowed)), make([]byte, len(borrowed))) {
		t.Fatal("acceptance retained an input reference")
	}
	got, err := s.takeResult(nil)
	if err != nil || string(got) != want || s.secret != nil {
		t.Fatal("accepted exact bytes were not transferred")
	}
	s.clear()
	if string(got) != want {
		t.Fatal("internal cleanup erased caller-owned result")
	}
	clear(got)
}

func TestSecretCancellationAndUnexpectedExitClearOwnedBuffers(t *testing.T) {
	for _, test := range []struct {
		name string
		msg  tea.Msg
	}{
		{"escape", tea.KeyMsg{Type: tea.KeyEsc}},
		{"control-c", tea.KeyMsg{Type: tea.KeyCtrlC}},
		{"graceful-signal", tea.QuitMsg{}},
		{"unexpected-exit", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := secretFixture(t)
			s.Update(privatePasteMsg("synthetic cancel candidate"))
			borrowed := s.prompt.secret
			if test.msg != nil {
				s.Update(test.msg)
			}
			got, err := s.takeResult(nil)
			if got != nil || !errors.Is(err, ErrUnlockCancelled) || s.prompt != nil || s.secret != nil || s.keyHelp != nil {
				t.Fatal("cancel/unexpected exit retained a secret or failed cancellation contract")
			}
			for _, r := range borrowed {
				if r != 0 {
					t.Fatal("owned rune buffer not cleared")
				}
			}
		})
	}
	t.Run("empty-enter", func(t *testing.T) {
		s := secretFixture(t)
		secretKey(s, tea.KeyEnter)
		got, err := s.takeResult(nil)
		if !s.done || got != nil || !errors.Is(err, ErrUnlockCancelled) {
			t.Fatal("empty physical Enter did not cancel")
		}
	})
}

func TestSecretProgramErrorClearsEvenAcceptedResult(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		s := secretFixture(t)
		s.Update(privatePasteMsg("synthetic program failure"))
		if accepted {
			secretKey(s, tea.KeyEnter)
		}
		borrowed := s.secret
		cause := errors.New("synthetic terminal failure")
		got, err := s.takeResult(cause)
		if got != nil || !errors.Is(err, cause) || s.prompt != nil || s.secret != nil {
			t.Fatal("program error handed off or retained a secret")
		}
		if !bytes.Equal(borrowed, make([]byte, len(borrowed))) {
			t.Fatal("accepted result not cleared on terminal failure")
		}
	}
}

func TestSecretRejectedInputCannotSubmitOrCancelAnotherSecret(t *testing.T) {
	invalid := []tea.Msg{
		privatePasteMsg("\xff"), privatePasteMsg("line\nfeed"), privatePasteMsg("\r"),
		privatePasteMsg("tab\t"), privatePasteMsg("\x00"), privatePasteMsg("\x03"),
		privatePasteMsg("\u2028"), privatePasteMsg("\u2029"), privatePasteMsg(strings.Repeat("é", 2049)),
		terminalRejectedMsg{reason: "synthetic sensitive transport reason"},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{-1}},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', -1}, Paste: true},
		tea.KeyMsg{Type: tea.KeyEnter, Paste: true},
		tea.KeyMsg{Type: tea.KeyEsc, Paste: true},
		tea.KeyMsg{Type: tea.KeyEnter, Alt: true},
	}
	for i, msg := range invalid {
		for _, prefix := range []string{"", "synthetic accepted prefix"} {
			s := secretFixture(t)
			s.Update(privatePasteMsg(prefix))
			s.Update(msg)
			if !s.invalid || s.done || s.prompt.Value() != prefix || strings.Contains(s.View(), "sensitive transport") {
				t.Fatalf("case %d: rejected payload altered input, acted as a key, or leaked", i)
			}
			// Neither a later valid insertion nor editing/navigation clears poison.
			s.Update(privatePasteMsg("x"))
			secretKey(s, tea.KeyLeft)
			secretKey(s, tea.KeyBackspace)
			secretKey(s, tea.KeyEnter)
			if s.done || s.secret != nil || s.prompt.Value() != "" || s.invalid || s.notice == "" {
				t.Fatalf("case %d: rejected candidate was accepted/cancelled rather than cleared", i)
			}
			s.Update(privatePasteMsg("synthetic retry"))
			secretKey(s, tea.KeyEnter)
			got, err := s.takeResult(nil)
			if err != nil || string(got) != "synthetic retry" {
				t.Fatalf("case %d: clean retry failed", i)
			}
			clear(got)
		}
	}
}

func TestSecretBoundAndFinalValidation(t *testing.T) {
	for _, want := range []string{strings.Repeat("é", 2048), strings.Repeat("x", 4096)} {
		s := secretFixture(t)
		s.Update(privatePasteMsg(want))
		secretKey(s, tea.KeyEnter)
		got, err := s.takeResult(nil)
		if err != nil || string(got) != want {
			t.Fatal("4096-byte boundary was changed or rejected")
		}
		clear(got)
	}
	s := secretFixture(t)
	s.Update(privatePasteMsg(strings.Repeat("é", 2048)))
	secretKey(s, tea.KeySpace)
	secretKey(s, tea.KeyEnter)
	if s.done || s.prompt.Value() != "" {
		t.Fatal("combined insertion byte limit bypassed")
	}
	// Even a synthetic bypass of insertion validation cannot bypass handoff.
	for _, invalid := range []string{"synthetic\ninvalid", strings.Repeat("x", 4097)} {
		s.prompt.SetValue(invalid)
		secretKey(s, tea.KeyEnter)
		if s.done || s.prompt.Value() != "" || s.secret != nil {
			t.Fatal("handoff did not validate the actual candidate")
		}
	}
}

func TestSecretHelpOwnsAllKeysAndPaste(t *testing.T) {
	s := secretFixture(t)
	s.Update(privatePasteMsg("synthetic hidden candidate"))
	secretKey(s, tea.KeyF1)
	for _, msg := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyCtrlU},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}, Paste: true},
		privatePasteMsg("q\n\x03"), terminalRejectedMsg{reason: "bad"},
	} {
		s.Update(msg)
		if s.keyHelp == nil || s.done || s.invalid || s.prompt.Value() != "synthetic hidden candidate" {
			t.Fatal("help forwarded an action or paste")
		}
	}
	if strings.Contains(s.View(), "synthetic hidden candidate") {
		t.Fatal("help revealed secret")
	}
	s.Update(tea.WindowSizeMsg{Width: 60, Height: 25})
	if s.keyHelp.width != 60 || s.keyHelp.height != 25 {
		t.Fatal("help did not resize")
	}
	secretKey(s, tea.KeyEsc)
	if s.keyHelp != nil || s.done || s.prompt.Value() != "synthetic hidden candidate" {
		t.Fatal("Escape did not close only help")
	}
	secretKey(s, tea.KeyF1)
	s.Update(tea.QuitMsg{})
	if !s.done || s.prompt != nil || s.keyHelp != nil {
		t.Fatal("graceful shutdown behind help did not cancel")
	}
}

func TestSecretInvisibleFormCannotSubmitOrEmptyCancel(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{}, {Width: 23, Height: 80}, {Width: 100, Height: 3}} {
		for _, candidate := range []string{"", "synthetic small candidate"} {
			s := secretFixture(t)
			s.Update(privatePasteMsg(candidate))
			s.Update(size)
			secretKey(s, tea.KeyEnter)
			if s.done || s.prompt.Value() != candidate {
				t.Fatal("invisible input submitted or cancelled")
			}
			secretKey(s, tea.KeyEsc)
			if !s.done || s.prompt != nil {
				t.Fatal("small form could not be explicitly cancelled")
			}
		}
	}
	s := secretFixture(t)
	s.Update(privatePasteMsg("synthetic geometry candidate"))
	_, _, minimum := s.layout()
	s.Update(tea.WindowSizeMsg{Width: 100, Height: minimum - 1})
	secretKey(s, tea.KeyEnter)
	if s.done || !strings.Contains(s.View(), "Resize terminal") {
		t.Fatal("partly clipped form was accepted")
	}
	s.Update(tea.WindowSizeMsg{Width: 100, Height: minimum})
	secretKey(s, tea.KeyEnter)
	if !s.done {
		t.Fatal("fully visible form was not accepted")
	}

	long := newSecretScreen(SecretOptions{Theme: ThemePrefDark, Label: strings.Repeat("Label", 20)})
	defer long.clear()
	long.Update(tea.WindowSizeMsg{Width: 80, Height: 80})
	long.Update(privatePasteMsg("synthetic long label"))
	secretKey(long, tea.KeyEnter)
	if long.done || !strings.Contains(long.View(), "Resize terminal") {
		t.Fatal("label-clipped field was accepted")
	}
}

func TestSecretEditingRetainsExactSpacesAndReplacementRune(t *testing.T) {
	s := secretFixture(t)
	s.Update(privatePasteMsg("a�z"))
	secretKey(s, tea.KeyHome)
	secretKey(s, tea.KeyRight)
	secretKey(s, tea.KeySpace)
	secretKey(s, tea.KeyEnd)
	secretKey(s, tea.KeyBackspace)
	if s.prompt.Value() != "a �" || s.prompt.EchoMode != textinput.EchoPassword {
		t.Fatal("masked editing changed exact bytes")
	}
	secretKey(s, tea.KeyEnter)
	got, err := s.takeResult(nil)
	if err != nil || string(got) != "a �" {
		t.Fatal("edited passphrase was rewritten")
	}
	clear(got)
}

// Exercise the real byte decoder/filter/model transport without a terminal,
// OS clipboard, running Tea program, signal handler, editor, or note store.
func feedSecretBytes(t *testing.T, s *secretScreen, text string) {
	t.Helper()
	r := newTerminalInput(strings.NewReader(text))
	defer r.clear()
	m := &terminalModel{model: s, input: r}
	buf := make([]byte, 256)
	for {
		n, err := r.Read(buf)
		for i := 0; i < n; i++ {
			m.Update(r.filter(s, tea.KeyMsg{Type: tea.KeyCtrlAt}))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("synthetic transport read failed")
		}
	}
}

func TestSecretRawTransportRejectsInvalidSuffixAndLiteralControls(t *testing.T) {
	for _, suffix := range []string{
		"\xff\r", "\xc3\r", pasteStart + "\xff\r" + pasteEnd + "\r",
		pasteStart + "\x1b\x03\r" + pasteEnd + "\r", pasteStart + "truncated",
	} {
		s := secretFixture(t)
		feedSecretBytes(t, s, "synthetic prefix"+suffix)
		if s.done || s.secret != nil {
			t.Fatal("malformed/pasted control input acted as a secret or cancellation")
		}
		if s.invalid {
			secretKey(s, tea.KeyEnter)
		}
		if s.done || s.prompt.Value() != "" || s.notice == "" {
			t.Fatal("transport rejection did not poison the whole candidate")
		}
	}
	s := secretFixture(t)
	feedSecretBytes(t, s, " "+pasteStart+"synthetic � e\u0301"+pasteEnd+" \r")
	got, err := s.takeResult(nil)
	if err != nil || string(got) != " synthetic � e\u0301 " {
		t.Fatal("raw physical/paste exact-byte handoff changed input")
	}
	clear(got)
}

func TestReadSecretRefusesMissingRedirectedAndClosedDescriptors(t *testing.T) {
	file, fileErr := os.CreateTemp(t.TempDir(), "synthetic-descriptor-")
	if fileErr != nil {
		t.Fatal(fileErr)
	}
	defer file.Close()
	for _, options := range []SecretOptions{
		{}, {Input: file}, {Output: file}, {Input: file, Output: file},
	} {
		got, err := ReadSecret(options)
		if got != nil || err == nil || !strings.Contains(err.Error(), "interactive terminal") {
			t.Fatal("missing or redirected descriptors used a fallback")
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSecret(SecretOptions{Input: file, Output: file})
	if got != nil || err == nil {
		t.Fatal("closed descriptor was accepted")
	}
}
