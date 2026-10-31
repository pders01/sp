package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// A physical space has a distinct key type; batched text does not model it.
func privateTypingKeys(text string) []tea.KeyMsg {
	var keys []tea.KeyMsg
	for _, r := range text {
		if r == ' ' {
			keys = append(keys, rawPhysical(tea.KeySpace))
		} else {
			keys = append(keys, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	return keys
}

func TestPrivateEditorPhysicalSpacesAreSavedVerbatim(t *testing.T) {
	const want = " This  is a test. 笔记 🙂 "
	var saved string
	e := newPrivateEditor("2026-10-31", "", func(_, original, content string) error {
		if original != "" {
			t.Fatal("space handling changed the save baseline")
		}
		saved = content
		return nil
	}, nil)
	for _, key := range privateTypingKeys(want) {
		e.Update(key)
	}
	if e.input.Value() != want {
		t.Fatalf("typed text = %q, want %q", e.input.Value(), want)
	}
	// Insertion and deletion in the middle must treat a space as one byte.
	e.Update(rawPhysical(tea.KeyHome))
	e.Update(rawPhysical(tea.KeyRight))
	e.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	e.Update(rawPhysical(tea.KeyBackspace))
	e.Update(rawPhysical(tea.KeyCtrlS))
	if !e.done || saved != want {
		t.Fatalf("saved text = %q, want %q", saved, want)
	}
}

func TestPrivateUnlockPreservesPhysicalSpacesWithoutEcho(t *testing.T) {
	const want = " alpha  beta "
	app := newTestApp(ModeNotebook)
	defer app.Close()
	var submitted string
	app.SetPrivacy(PrivacyHooks{Unlock: func(secret []byte) error {
		submitted = string(secret)
		return nil
	}})
	app.Update(privacyKey("u"))
	for _, key := range privateTypingKeys(want) {
		app.Update(key)
	}
	if app.privacy.prompt.Value() != want || app.privacy.prompt.EchoMode != textinput.EchoPassword {
		t.Fatal("space keys changed the passphrase or its masked mode")
	}
	if strings.Contains(app.View(), "alpha") || strings.Contains(app.View(), "beta") {
		t.Fatal("space handling exposed the passphrase")
	}
	app.Update(rawPhysical(tea.KeyEnter))
	if submitted != want {
		t.Fatal("physical spaces were lost from the submitted passphrase")
	}
}

func TestPrivateUnlockRejectsPhysicalSpaceBeyondByteLimit(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	calls := 0
	app.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { calls++; return nil }})
	app.Update(privacyKey("u"))
	app.Update(privacyKey(strings.Repeat("x", 4096)))
	app.Update(rawPhysical(tea.KeySpace))
	app.Update(rawPhysical(tea.KeyEnter))
	if calls != 0 || app.privacy.prompt == nil || app.privacy.prompt.Value() != "" {
		t.Fatal("an over-limit space was silently dropped to submit a different secret")
	}
}
