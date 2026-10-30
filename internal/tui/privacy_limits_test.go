package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPrivacyUnlockAcceptsStorageMaximumPassphrase(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	called := false
	app.SetPrivacy(PrivacyHooks{Unlock: func(passphrase []byte) error {
		called = true
		if len(passphrase) != 4096 {
			t.Fatalf("passphrase was truncated to %d bytes", len(passphrase))
		}
		return nil
	}})
	app.Update(privacyKey("u"))
	app.Update(privacyKey(strings.Repeat("a", 4096)))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !called {
		t.Fatal("maximum supported passphrase was not submitted")
	}
}

func TestPrivacyUnlockRejectsOversizedPassphraseInsteadOfTruncating(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 4097), strings.Repeat("é", 2049)} {
		app := newTestApp(ModeNotebook)
		app.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error {
			t.Fatal("oversized passphrase reached unlock")
			return nil
		}})
		app.Update(privacyKey("u"))
		app.Update(privacyKey(value))
		app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if app.privacy.prompt == nil || app.privacy.prompt.Value() != "" || !strings.Contains(app.privacy.promptErr, "4096 bytes") {
			t.Fatal("oversized input was not rejected explicitly and cleared")
		}
		app.Close()
	}
}
