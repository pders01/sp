package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
)

// These tests drive models only: returned commands (clipboard, terminal, blink,
// external processes) are never executed, except the synthetic template closure.
func TestPrivacyValidationLoadedMarkdownIsNotAnEdit(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"tabs", "\tindented\n\t\tcode\n"},
		{"CRLF", "first\r\nsecond\r\n"},
		{"over_99_lines", strings.Repeat("line\n", 120) + "last line"},
		{"long_single_line", strings.Repeat("x", 8192)},
		{"unicode_and_trailing_lines", "café 笔记\n\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []tea.KeyType{tea.KeyCtrlS, tea.KeyEsc} {
				t.Run(tea.KeyMsg{Type: action}.String(), func(t *testing.T) {
					writes, recoveries := 0, 0
					e := newPrivateEditor("2024-01-15", tc.content, func(_, _, _ string) error {
						writes++
						return nil
					}, func(_, _ string) (string, error) {
						recoveries++
						return "synthetic.age", nil
					})
					if got := e.input.Value(); got != tc.content {
						t.Errorf("opening changed Markdown: got %d bytes, want %d; equal=%v", len(got), len(tc.content), got == tc.content)
					}
					e.Update(tea.KeyMsg{Type: action})
					if !e.done || writes != 0 || recoveries != 0 {
						t.Errorf("untouched editor: done=%v, saves=%d, recoveries=%d; want immediate exit without writing", e.done, writes, recoveries)
					}
				})
			}
		})
	}
}

func TestPrivacyValidationMultilineInputIsNotTruncated(t *testing.T) {
	content := strings.Repeat("synthetic line\n", 130) + "END"
	var saved string
	e := newPrivateEditor("2024-01-15", "", func(_, original, value string) error {
		if original != "" {
			t.Errorf("baseline = %q, want empty", original)
		}
		saved = value
		return nil
	}, nil)
	e.Update(privacyKey(content)) // batched runes; no system clipboard access
	if e.input.Value() != content {
		t.Errorf("multiline input truncated: got %d lines, want %d", strings.Count(e.input.Value(), "\n")+1, strings.Count(content, "\n")+1)
	}
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if saved != content {
		t.Errorf("saved %d bytes, want all %d bytes", len(saved), len(content))
	}
}

func TestPrivacyValidationPromptMasksEditingKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []tea.KeyMsg
		want string
	}{
		{"backspace", []tea.KeyMsg{{Type: tea.KeyBackspace}}, "SYNTHETIC-secre"},
		{"delete", []tea.KeyMsg{{Type: tea.KeyHome}, {Type: tea.KeyDelete}}, "YNTHETIC-secret"},
		{"kill_before", []tea.KeyMsg{{Type: tea.KeyCtrlU}}, ""},
		{"kill_after", []tea.KeyMsg{{Type: tea.KeyHome}, {Type: tea.KeyCtrlK}}, ""},
		{"password_word_delete", []tea.KeyMsg{{Type: tea.KeyCtrlW}}, ""},
		{"insert_in_middle", []tea.KeyMsg{{Type: tea.KeyLeft}, privacyKey("X")}, "SYNTHETIC-secreXt"},
		{"scroll_long_input", []tea.KeyMsg{privacyKey(strings.Repeat("Z", 100)), {Type: tea.KeyHome}, {Type: tea.KeyEnd}}, "SYNTHETIC-secret" + strings.Repeat("Z", 100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			defer app.Close()
			var submitted string
			app.SetPrivacy(PrivacyHooks{Unlock: func(secret []byte) error {
				submitted = string(secret)
				return errors.New("synthetic failure")
			}})
			app.Update(privacyKey("u"))
			app.Update(privacyKey("SYNTHETIC-secret"))
			for _, key := range tc.keys {
				app.Update(key)
				view := app.View()
				for _, plaintext := range []string{"SYNTHETIC", "secret", "ZZZZ", "secreX"} {
					if strings.Contains(view, plaintext) {
						t.Fatalf("editing revealed input %q", plaintext)
					}
				}
			}
			if got := app.privacy.prompt.Value(); got != tc.want {
				t.Fatalf("edited value = %q, want %q", got, tc.want)
			}
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if submitted != tc.want || app.privacy.prompt == nil || app.privacy.prompt.Value() != "" {
				t.Fatal("submission/failed-attempt clearing did not preserve edited value")
			}
		})
	}
}

func TestPrivacyValidationBatchedTextIsNotAModalCommand(t *testing.T) {
	// Bubble Tea KeyRunes can carry multiple runes (paste/IME). A literal
	// spelling of a special key is text, not that physical special key.
	for _, text := range []string{"enter", "esc", "ctrl+c", "ctrl+s"} {
		t.Run("prompt/"+text, func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			defer app.Close()
			calls := 0
			app.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { calls++; return nil }})
			app.Update(privacyKey("u"))
			app.Update(privacyKey("prefix-"))
			app.Update(privacyKey(text))
			if calls != 0 || app.privacy.prompt == nil {
				t.Fatalf("literal %q submitted or cancelled the prompt", text)
			}
			if got := app.privacy.prompt.Value(); got != "prefix-"+text {
				t.Errorf("literal input = %q, want %q", got, "prefix-"+text)
			}
		})
		t.Run("editor/"+text, func(t *testing.T) {
			calls := 0
			e := newPrivateEditor("2024-01-15", "prefix-", func(_, _, _ string) error { calls++; return nil }, nil)
			e.Update(rawPhysical(tea.KeyCtrlEnd))
			e.Update(privacyKey(text))
			if calls != 0 || e.done || e.confirm || e.input.Value() != "prefix-"+text {
				t.Errorf("literal %q acted as a command: done=%v, confirm=%v, value=%q, saves=%d", text, e.done, e.confirm, e.input.Value(), calls)
			}
		})
	}
}

func TestPrivacyValidationInvalidPassphrasePasteIsNotRewritten(t *testing.T) {
	// Storage accepts single-line UTF-8 without controls. Reject invalid input
	// rather than silently authenticating a different, sanitized passphrase.
	for _, text := range []string{"alpha\nbeta", "alpha\r\nbeta", "alpha\tbeta", "alpha\x00beta"} {
		t.Run(fmt.Sprintf("%q", text), func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			defer app.Close()
			calls := 0
			app.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { calls++; return nil }})
			app.Update(privacyKey("u"))
			app.Update(privacyKey(text))
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if calls != 0 || app.privacy.prompt == nil {
				t.Fatal("invalid pasted passphrase was rewritten and submitted instead of rejected")
			}
		})
	}
}

func TestPrivacyValidationSaveRetryAfterPublication(t *testing.T) {
	visible, attempts, recoveries := "original", 0, 0
	e := newPrivateEditor("2024-01-15", visible, func(_, original, content string) error {
		attempts++
		if original != visible {
			return scratchpad.ErrConflict
		}
		visible = content
		if attempts == 1 {
			return fmt.Errorf("synthetic post-publication sync: %w", scratchpad.ErrDurabilityUnconfirmed)
		}
		return nil
	}, func(_, _ string) (string, error) {
		recoveries++
		return "synthetic.age", nil
	})
	e.input.SetValue("first edit")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.done || e.input.Value() != "first edit" || recoveries != 1 {
		t.Fatal("unconfirmed durability should retain text and warn, not silently exit")
	}
	// No intervening writer: the first save itself published this baseline.
	e.input.SetValue("second edit")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !e.done || visible != "second edit" || attempts != 2 {
		t.Fatalf("retry conflicts with own published save: done=%v, stored=%q, attempts=%d, status=%q", e.done, visible, attempts, e.status)
	}
}

func TestPrivacyValidationPublishedRecoveryReportsPath(t *testing.T) {
	const path = "synthetic/.sp-recovery-2024-01-15-example.age"
	for _, action := range []string{"save_failure", "cancel", "abnormal_shutdown"} {
		t.Run(action, func(t *testing.T) {
			e := newPrivateEditor("2024-01-15", "original", func(_, _, _ string) error {
				return errors.New("synthetic save failure")
			}, func(_, content string) (string, error) {
				if content != "changed" {
					t.Errorf("recovery = %q", content)
				}
				return path, fmt.Errorf("synthetic recovery sync: %w", scratchpad.ErrDurabilityUnconfirmed)
			})
			e.input.SetValue("changed")
			var diagnostic string
			switch action {
			case "save_failure":
				e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
				diagnostic = e.status
			case "cancel":
				e.Update(tea.KeyMsg{Type: tea.KeyEsc})
				e.Update(tea.KeyMsg{Type: tea.KeyEsc})
				diagnostic = e.status
			case "abnormal_shutdown":
				app := newTestApp(ModeNotebook)
				defer app.Close()
				app.SetPrivacy(PrivacyHooks{})
				app.privacy.editor = e
				err := app.RecoverPrivateEdit()
				if err == nil {
					t.Fatal("missing recovery durability warning")
				}
				diagnostic = err.Error()
			}
			if e.done || e.input.Value() != "changed" {
				t.Fatal("uncertain recovery durability discarded the running buffer")
			}
			if !strings.Contains(diagnostic, path) || !strings.Contains(diagnostic, scratchpad.ErrDurabilityUnconfirmed.Error()) {
				t.Errorf("published recovery path and durability warning must both be reported: %q", diagnostic)
			}
		})
	}
}

func TestPrivacyValidationPassphraseByteLimits(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		accepted   bool
	}{
		{"empty", "", false},
		{"maximum_ASCII", strings.Repeat("x", 4096), true},
		{"maximum_UTF8", strings.Repeat("é", 2048), true},
		{"oversized_ASCII", strings.Repeat("x", 10000), false},
		{"oversized_UTF8", strings.Repeat("é", 2049), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			defer app.Close()
			calls := 0
			app.SetPrivacy(PrivacyHooks{Unlock: func(secret []byte) error {
				calls++
				if string(secret) != tc.text {
					t.Error("passphrase was truncated or rewritten")
				}
				return nil
			}})
			app.Update(privacyKey("u"))
			app.Update(privacyKey(tc.text))
			if strings.Contains(app.View(), "xxxx") || strings.Contains(app.View(), "éééé") {
				t.Fatal("long input was not masked")
			}
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if tc.accepted {
				if calls != 1 || app.privacy.prompt != nil {
					t.Fatal("valid maximum-length input was not accepted")
				}
			} else if calls != 0 || app.privacy.prompt == nil || app.privacy.prompt.Value() != "" || !strings.Contains(app.privacy.promptErr, "4096 bytes") {
				t.Fatal("invalid-length input must be rejected explicitly and cleared")
			}
		})
	}
}

func TestPrivacyValidationFailedSaveKeepsBaselineForRetry(t *testing.T) {
	attempts, recoveries := 0, 0
	e := newPrivateEditor("2024-01-15", "original", func(_, before, content string) error {
		attempts++
		if before != "original" || content != "changed" {
			t.Errorf("failed pre-publication save changed retry baseline/content: %q/%q", before, content)
		}
		if attempts == 1 {
			return errors.New("synthetic pre-publication failure")
		}
		return nil
	}, func(_, content string) (string, error) {
		recoveries++
		if content != "changed" {
			t.Errorf("recovery content = %q", content)
		}
		return "", errors.New("synthetic recovery failure")
	})
	e.input.SetValue("changed")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.done || e.input.Value() != "changed" || recoveries != 1 {
		t.Fatal("failed save/recovery did not retain the editor")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !e.done || attempts != 2 || recoveries != 1 {
		t.Fatalf("retry after pre-publication failure: done=%v, attempts=%d, recoveries=%d", e.done, attempts, recoveries)
	}
}

func TestPrivacyValidationRecoveredTextUsesCurrentBaseline(t *testing.T) {
	const current, recovered = "current private", "recovered private"
	var before, after string
	// Same initialization as EditRecoveredPrivate, without starting a terminal.
	e := newPrivateEditor("2024-01-15", current, func(_, original, content string) error {
		before, after = original, content
		return nil
	}, nil)
	e.input.SetValue(recovered)
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !e.done || before != current || after != recovered {
		t.Fatalf("recovery save baseline/content = %q/%q, done=%v", before, after, e.done)
	}
}

func TestPrivacyValidationTemplateFailureAndLateResultIsolation(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	date := app.nb.GetCurrentPage()
	app.nb.SetContents(map[string]string{date: "shared notebook"})
	app.cal.SetContents(map[string]string{date: "shared calendar"})
	app.SetPrivacy(PrivacyHooks{
		IsUnlocked: func() bool { return true },
		Load:       func(string) (PrivatePage, error) { return PrivatePage{Private: "private baseline"}, nil },
	})
	app.SetTemplates([]DayTemplate{{ID: "synthetic", Name: "Synthetic"}}, nil,
		func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
			return TemplateApplyResult{Content: "private failed template", Applied: []string{"synthetic"}}, errors.New("synthetic failure")
		})
	app.Update(privacyKey("a"))
	app.Update(privacyKey(" "))
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("missing synthetic template command")
	}
	app.Update(cmd())
	if app.privacy.editor != nil || app.templateApplied(date, "synthetic") {
		t.Fatal("failed template reported successful application")
	}
	app.quitting = true
	app.Update(templateAppliedMsg{date: date, result: TemplateApplyResult{Content: "private late template", Applied: []string{"synthetic"}}})
	if app.privacy.editor != nil || app.nb.contents[date] != "shared notebook" || app.cal.contents[date] != "shared calendar" {
		t.Fatal("failed/late private template contaminated shared caches or reopened the editor")
	}
}

func TestPrivacyValidationSmallTerminalDoesNotLoseText(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 0, Height: 0}, {Width: 1, Height: 1}, {Width: 8, Height: 3}, {Width: 20, Height: 8}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			defer app.Close()
			app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) {
				return PrivatePage{Private: "private original", Shared: "shared"}, nil
			}})
			app.Update(size)
			_ = app.View()
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			app.privacy.editor.input.SetValue("private changed")
			app.Update(size)
			_ = app.View()
			if app.privacy.editor.input.Value() != "private changed" {
				t.Fatal("resize lost private text")
			}
		})
	}
}
