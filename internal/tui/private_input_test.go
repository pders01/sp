package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/rivo/uniseg"
)

// All fixtures are synthetic. Commands returned by models are never executed.
func rawInputKeys(m privateInput, keys ...tea.KeyMsg) privateInput {
	for _, key := range keys {
		m, _ = m.Update(key)
	}
	return m
}
func rawPhysical(kind tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: kind} }

func TestPrivateInputRealEditingPreservesRawMarkdown(t *testing.T) {
	for _, tc := range []struct{ name, original, newline string }{
		{"tabs", "\talpha\tbetá\n\t\tuntouched\n\n", "\n"},
		{"CRLF", "first\r\n\tsecond\r\n\r\n", "\r\n"},
		{"mixed_endings", "first\r\nsecond\nthird\r\n", "\r\n"},
		{"over_130_lines", strings.Repeat("\tuntouched 笔记\r\n", 140) + "last\r\n\r\n", "\r\n"},
		{"Unicode_trailing_newlines", "café e\u0301 笔记 🙂\n\n\n", "\n"},
		{"long_line", strings.Repeat("x", 8192) + "\n", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, saved string
			e := newPrivateEditor("2024-01-15", tc.original, func(_, original, content string) error {
				before, saved = original, content
				return nil
			}, nil)
			// Navigate from the end, then actually edit the loaded buffer.
			for n := 0; n < strings.Count(tc.original, "\n"); n++ {
				e.Update(rawPhysical(tea.KeyUp))
			}
			e.Update(rawPhysical(tea.KeyHome))
			for _, key := range []tea.KeyMsg{
				privacyKey("新增🙂"), rawPhysical(tea.KeyBackspace),
				privacyKey("X"), rawPhysical(tea.KeyLeft), rawPhysical(tea.KeyDelete),
				rawPhysical(tea.KeyTab), rawPhysical(tea.KeyEnter),
			} {
				e.Update(key)
			}
			want := "新增\t" + tc.newline + tc.original
			if got := e.input.Value(); got != want {
				t.Fatalf("editing changed untouched bytes: got %q, want %q", got, want)
			}
			e.Update(rawPhysical(tea.KeyCtrlS))
			if !e.done || before != tc.original || saved != want {
				t.Fatal("saving failed to preserve baseline and exact edited Markdown")
			}
		})
	}
}

func TestPrivateInputCRLFAndUnicodeEditingUnits(t *testing.T) {
	for _, tc := range []struct {
		name, original, want string
		keys                 []tea.KeyType
	}{
		{"CRLF_backspace", "a\r\nb\r\n", "a\r\nb", []tea.KeyType{tea.KeyBackspace}},
		{"CRLF_delete", "a\r\nb\r\n", "ab\r\n", []tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyEnd, tea.KeyDelete}},
		{"CRLF_left_right", "a\r\nb\r\n", "a\r\nb\r\nX", []tea.KeyType{tea.KeyLeft, tea.KeyRight}},
		{"CRLF_line_kill", "a\r\nb", "a\r\n", []tea.KeyType{tea.KeyCtrlA, tea.KeyCtrlK}},
		{"CRLF_separator_kill", "a\r\nb", "ab", []tea.KeyType{tea.KeyHome, tea.KeyUp, tea.KeyEnd, tea.KeyCtrlK}},
		{"grapheme_backspace", "e\u0301🙂", "", []tea.KeyType{tea.KeyBackspace, tea.KeyBackspace}},
		{"grapheme_delete", "e\u0301🙂", "🙂", []tea.KeyType{tea.KeyHome, tea.KeyDelete}},
		{"physical_enter_CRLF", "a\r\nb", "a\r\nb\r\n", []tea.KeyType{tea.KeyEnter}},
		{"physical_enter_LF", "a\nb", "a\nb\n", []tea.KeyType{tea.KeyEnter}},
		{"literal_tab", "a", "a\t", []tea.KeyType{tea.KeyTab}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newPrivateInput()
			m.Focus()
			m.SetValue(tc.original)
			for _, kind := range tc.keys {
				m = rawInputKeys(m, rawPhysical(kind))
				if m.pos > 0 && m.pos < len(m.text) && m.text[m.pos-1:m.pos+1] == "\r\n" {
					t.Fatal("cursor entered a CRLF pair")
				}
			}
			if tc.name == "CRLF_left_right" {
				m = rawInputKeys(m, privacyKey("X"))
			}
			if m.Value() != tc.want {
				t.Fatalf("got %q, want %q", m.Value(), tc.want)
			}
		})
	}
}

func TestPrivateInputVerticalVisualColumns(t *testing.T) {
	m := newPrivateInput()
	m.Focus()
	m.SetValue("\t笔🙂z\r\n123456789\r\n")
	m = rawInputKeys(m, rawPhysical(tea.KeyUp), rawPhysical(tea.KeyHome), rawPhysical(tea.KeyRight), rawPhysical(tea.KeyRight), rawPhysical(tea.KeyRight), rawPhysical(tea.KeyRight))
	// Column four on the previous line is just after the untouched tab.
	m = rawInputKeys(m, rawPhysical(tea.KeyUp), privacyKey("X"))
	if m.Value() != "\tX笔🙂z\r\n123456789\r\n" {
		t.Fatalf("visual column navigation: %q", m.Value())
	}
}

func TestPrivateInputLiteralBatchesBypassAllKeyBindings(t *testing.T) {
	for _, text := range []string{"enter", "esc", "ctrl+s", "ctrl+c", "left", "home", "ctrl+a", "ctrl+k", "backspace", "ctrl+v", "tab", "\talpha\r\n" + strings.Repeat("笔🙂\n", 150)} {
		t.Run(fmt.Sprintf("%d/%s", len(text), strings.Split(text, "\n")[0]), func(t *testing.T) {
			e := newPrivateEditor("2024-01-15", "prefix-", nil, nil)
			e.Update(rawPhysical(tea.KeyCtrlEnd))
			e.Update(privacyKey(text))
			if e.done || e.confirm || e.input.Value() != "prefix-"+text {
				t.Fatal("batched text was normalized or interpreted as a binding")
			}
		})
	}
}

func TestPrivateInputMixedEditingCursorInvariants(t *testing.T) {
	m := newPrivateInput()
	m.Focus()
	m.SetValue("\talpha\r\n笔 e\u0301🙂\n\nlast\r\n")
	keys := []tea.KeyMsg{
		rawPhysical(tea.KeyLeft), rawPhysical(tea.KeyRight), rawPhysical(tea.KeyUp), rawPhysical(tea.KeyDown),
		rawPhysical(tea.KeyHome), rawPhysical(tea.KeyEnd), rawPhysical(tea.KeyCtrlA),
		rawPhysical(tea.KeyBackspace), rawPhysical(tea.KeyDelete), rawPhysical(tea.KeyCtrlK), rawPhysical(tea.KeyCtrlU),
		rawPhysical(tea.KeyEnter), rawPhysical(tea.KeyTab),
		privacyKey("\r"), privacyKey("\n"), privacyKey("\r\n"), privacyKey("\u0301"), privacyKey("笔🙂"),
	}
	state := uint32(42)
	for n := 0; n < 2000; n++ {
		state = state*1664525 + 1013904223
		key := keys[int(state)%len(keys)]
		before := m.Value()
		m = rawInputKeys(m, key)
		if m.pos < 0 || m.pos > len(m.text) || !utf8.ValidString(m.text[:m.pos]) || !utf8.ValidString(m.text[m.pos:]) {
			t.Fatalf("invalid UTF-8 cursor after step %d (%s)", n, key.String())
		}
		if m.pos > 0 && m.pos < len(m.text) && m.text[m.pos-1:m.pos+1] == "\r\n" {
			t.Fatalf("cursor split CRLF after step %d (%s)", n, key.String())
		}
		switch key.Type {
		case tea.KeyLeft, tea.KeyRight, tea.KeyUp, tea.KeyDown, tea.KeyHome, tea.KeyEnd, tea.KeyCtrlA:
			if m.Value() != before {
				t.Fatalf("navigation mutated raw text after step %d", n)
			}
		}
		m.SetWidth(n % 9)
		m.SetHeight(n % 5)
		_ = m.View()
	}
}

func TestPrivateInputExplicitByteLimitAndInvalidUTF8(t *testing.T) {
	m := newPrivateInput()
	m.Focus()
	maximum := strings.Repeat("é", privateInputLimit/2)
	m.SetValue(maximum)
	if m.Value() != maximum || m.err != "" {
		t.Fatal("storage maximum rejected or truncated")
	}
	m = rawInputKeys(m, privacyKey("x"))
	if m.Value() != maximum || !strings.Contains(m.err, "8 MiB") {
		t.Fatal("oversized insertion was not explicitly rejected")
	}
	m.SetValue(maximum + "x")
	if m.Value() != maximum || !strings.Contains(m.err, "8 MiB") {
		t.Fatal("oversized SetValue was not explicitly rejected")
	}
	m.SetValue("original\t\r\n")
	m.SetValue("invalid\xff")
	if m.Value() != "original\t\r\n" || !strings.Contains(m.err, "UTF-8") {
		t.Fatal("invalid UTF-8 was rewritten")
	}
	e := newPrivateEditor("2024-01-15", "original", func(_, _, _ string) error {
		t.Fatal("a rejected edit must not silently save the old buffer")
		return nil
	}, nil)
	e.Update(privacyKey(strings.Repeat("x", privateInputLimit)))
	e.Update(rawPhysical(tea.KeyCtrlS))
	if e.done || !strings.Contains(e.status, "8 MiB") {
		t.Fatal("rejected edit was not retained as an explicit warning")
	}
}

func TestPrivateInputSafeNarrowUnicodeRendering(t *testing.T) {
	content := "\t笔🙂e\u0301\x1b[31m\x00\x7f\u009b\u202e\u2028\r\n" + strings.Repeat("wide 笔🙂\n", 150) + "\t最后🙂"
	for _, width := range []int{0, 1, 2, 3, 8, 80} {
		for _, height := range []int{0, 1, 3, 18} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				m := newPrivateInput()
				m.Focus()
				m.SetValue(content)
				m.SetWidth(width)
				m.SetHeight(height)
				for _, key := range []tea.KeyMsg{rawPhysical(tea.KeyHome), rawPhysical(tea.KeyUp), rawPhysical(tea.KeyLeft), rawPhysical(tea.KeyRight)} {
					m = rawInputKeys(m, key)
					view := m.View()
					plain := strings.NewReplacer("\x1b[7m", "", "\x1b[0m", "").Replace(view)
					if strings.ContainsAny(plain, "\x1b\r\x00\x7f\u009b\u202e\u2028") {
						t.Fatalf("document control reached terminal: %q", plain)
					}
					rows := strings.Split(plain, "\n")
					if len(rows) > max(1, height) {
						t.Fatal("render exceeded height")
					}
					for _, row := range rows {
						if got := uniseg.StringWidth(row); got > max(1, width) {
							t.Fatalf("render width %d exceeds %d: %q", got, max(1, width), row)
						}
					}
				}
				if m.Value() != content {
					t.Fatal("render or navigation mutated raw text")
				}
			})
		}
	}
	// Actually render the controls too, not only the document tail.
	m := newPrivateInput()
	m.SetValue("\x1b[31m\x00\u009b\u202e\u2028\r")
	m.SetWidth(200)
	view := m.View()
	if strings.Contains(view, "\x1b[31m") || !strings.Contains(view, "\\u001b[31m\\u0000\\u009b\\u202e\\u2028\\u000d") {
		t.Fatalf("controls were not escaped: %q", view)
	}
}

func TestPrivatePassphraseTaintCannotBeEditedIntoAnotherSecret(t *testing.T) {
	for _, invalid := range []string{"a\nb", "a\r\nb", "a\tb", "a\x00b", "a\u0085b", "a\u2028b", "a\u2029b", strings.Repeat("é", 2049), "invalid\xff"} {
		t.Run(fmt.Sprintf("%q", invalid[:min(len(invalid), 20)]), func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			defer app.Close()
			calls := 0
			app.SetPrivacy(PrivacyHooks{Unlock: func(secret []byte) error {
				calls++
				if string(secret) != "valid second attempt" {
					t.Fatal("invalid input was sanitized into another secret")
				}
				return nil
			}})
			app.Update(privacyKey("u"))
			app.Update(privacyKey("valid prefix"))
			// Synthetic asynchronous paste event; no clipboard command is run.
			app.Update(privatePasteMsg(invalid))
			app.Update(rawPhysical(tea.KeyCtrlU))
			app.Update(privacyKey("valid second attempt"))
			if app.privacy.prompt.EchoMode != textinput.EchoPassword || strings.Contains(app.View(), "valid second attempt") {
				t.Fatal("password echo mode changed while editing")
			}
			app.Update(rawPhysical(tea.KeyEnter))
			if calls != 0 || app.privacy.prompt == nil || app.privacy.prompt.Value() != "" || app.privacy.promptInvalid {
				t.Fatal("tainted input submitted or was not cleared")
			}
			app.Update(privacyKey("valid second attempt"))
			app.Update(rawPhysical(tea.KeyEnter))
			if calls != 1 || app.privacy.prompt != nil {
				t.Fatal("fresh valid input after rejection failed")
			}
		})
	}
}

func TestPrivateInputAsyncPasteIsLiteralAndScoped(t *testing.T) {
	text := "ctrl+s\t\r\n" + strings.Repeat("笔记\r\n", 150)
	e := newPrivateEditor("2024-01-15", "prefix-", nil, nil)
	e.Update(rawPhysical(tea.KeyCtrlEnd))
	e.Update(privatePasteMsg(text))
	if e.input.Value() != "prefix-"+text || e.done || e.confirm {
		t.Fatal("asynchronous raw paste was normalized or interpreted as a command")
	}
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{})
	app.Update(privacyKey("u"))
	for _, text := range []string{"ctrl+c", "esc", "enter", "�"} {
		app.Update(privatePasteMsg(text))
	}
	if app.privacy.prompt == nil || app.privacy.prompt.Value() != "ctrl+cescenter�" {
		t.Fatal("asynchronous password paste was rewritten or treated as a command")
	}
	// Unrelated strings, including typed strings from other packages, must
	// not be treated as Bubbles clipboard messages by the adapter.
	type unrelatedPaste string
	for _, msg := range []tea.Msg{nil, "plain string", unrelatedPaste("alpha\nbeta"), errors.New("synthetic error"), rawPhysical(tea.KeyEnter)} {
		if _, ok := privatePasteText(msg); ok {
			t.Fatalf("unrelated event accepted as clipboard text: %T", msg)
		}
	}
}

func TestPrivatePassphraseValidReplacementRuneIsNotSanitized(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	var submitted string
	app.SetPrivacy(PrivacyHooks{Unlock: func(secret []byte) error {
		submitted = string(secret)
		return nil
	}})
	app.Update(privacyKey("u"))
	app.Update(privacyKey("a�笔🙂"))
	if app.privacy.prompt.Value() != "a�笔🙂" {
		t.Fatal("valid UTF-8 replacement rune was sanitized")
	}
	for _, key := range []tea.KeyMsg{rawPhysical(tea.KeyHome), rawPhysical(tea.KeyRight), rawPhysical(tea.KeyDelete), rawPhysical(tea.KeyEnd), privacyKey("�")} {
		app.Update(key)
	}
	if strings.ContainsAny(app.View(), "�笔🙂") {
		t.Fatal("Unicode secret reached the rendered prompt")
	}
	app.Update(rawPhysical(tea.KeyEnter))
	if submitted != "a笔🙂�" {
		t.Fatalf("submitted rewritten secret: %q", submitted)
	}
}

func TestPrivatePassphraseLiteralEditingKeyNames(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{})
	app.Update(privacyKey("u"))
	want := ""
	for _, text := range []string{"left", "right", "home", "end", "backspace", "ctrl+k", "ctrl+v", "tab"} {
		app.Update(privacyKey(text))
		want += text
		if app.privacy.prompt.Value() != want || app.privacy.prompt.EchoMode != textinput.EchoPassword {
			t.Fatalf("literal %q interpreted as an editing key", text)
		}
	}
	_, cmd := app.Update(rawPhysical(tea.KeyCtrlV))
	if cmd != nil {
		// Cursor commands are allowed, but a clipboard command must not be
		// generated. Verify the binding instead of executing any command.
		if app.privacy.prompt.KeyMap.Paste.Enabled() {
			t.Fatal("system clipboard binding enabled")
		}
	}
}

func TestPrivateEditorDurabilityRetryWithoutFurtherEdits(t *testing.T) {
	attempts, recoveries := 0, 0
	e := newPrivateEditor("2024-01-15", "old", func(_, before, after string) error {
		attempts++
		wantBefore := "old"
		if attempts > 1 {
			wantBefore = "changed"
		}
		if before != wantBefore || after != "changed" {
			t.Fatalf("save baseline/content %q/%q", before, after)
		}
		if attempts == 1 {
			return fmt.Errorf("synthetic sync: %w", scratchpad.ErrDurabilityUnconfirmed)
		}
		return nil
	}, func(_, _ string) (string, error) {
		recoveries++
		return "synthetic.age", nil
	})
	e.input.SetValue("changed")
	e.Update(rawPhysical(tea.KeyCtrlS))
	if e.done || !e.needsRecovery() || e.original != "changed" {
		t.Fatal("publication warning or baseline was lost")
	}
	e.Update(rawPhysical(tea.KeyCtrlS))
	if !e.done || attempts != 2 || recoveries != 1 {
		t.Fatal("unchanged published text was not explicitly retried")
	}
}

func TestPrivateEditorUnconfirmedSaveStillNeedsRecovery(t *testing.T) {
	for _, action := range []string{"cancel", "app_shutdown", "standalone_shutdown"} {
		t.Run(action, func(t *testing.T) {
			const path = "synthetic/recovery.age"
			e := newPrivateEditor("2024-01-15", "old", func(_, _, _ string) error {
				return scratchpad.ErrDurabilityUnconfirmed
			}, func(_, value string) (string, error) {
				if value != "changed" {
					t.Fatal("wrong recovery content")
				}
				return path, fmt.Errorf("synthetic recovery sync: %w", scratchpad.ErrDurabilityUnconfirmed)
			})
			e.input.SetValue("changed")
			e.Update(rawPhysical(tea.KeyCtrlS))
			var diagnostic string
			switch action {
			case "cancel":
				e.Update(rawPhysical(tea.KeyEsc))
				e.Update(rawPhysical(tea.KeyEsc))
				diagnostic = e.status
			case "app_shutdown":
				app := newTestApp(ModeNotebook)
				defer app.Close()
				app.SetPrivacy(PrivacyHooks{})
				app.privacy.editor = e
				err := app.RecoverPrivateEdit()
				if !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
					t.Fatalf("lost recovery error: %v", err)
				}
				diagnostic = err.Error()
			case "standalone_shutdown":
				stopped := errors.New("synthetic terminal stop")
				err := e.recoverStopped(stopped)
				if !errors.Is(err, stopped) || !errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
					t.Fatalf("lost terminal/recovery errors: %v", err)
				}
				diagnostic = err.Error()
			}
			if e.done || e.input.Value() != "changed" || !strings.Contains(diagnostic, path) || !strings.Contains(diagnostic, scratchpad.ErrDurabilityUnconfirmed.Error()) {
				t.Fatalf("uncertain recovery discarded text or diagnostics: %q", diagnostic)
			}
		})
	}
}
