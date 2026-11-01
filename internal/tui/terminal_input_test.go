package tui

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The harness only observes rejections and terminates synthetic streams. All
// routing belongs to the production target.Update, through Tea's real reader.
type terminalHarness struct {
	target       tea.Model
	rejected     int
	stopOnReject bool
}

func (m *terminalHarness) Init() tea.Cmd { return nil }
func (m *terminalHarness) View() string  { return "" }
func (m *terminalHarness) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyCtrlG {
		return m, tea.Quit
	}
	if _, ok := msg.(terminalRejectedMsg); ok {
		m.rejected++
		if m.stopOnReject {
			return m, tea.Quit
		}
	}
	_, cmd := m.target.Update(msg)
	return m, cmd
}

type fragmentedTerminalReader struct {
	data []byte
	size int
}

func (r *fragmentedTerminalReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), len(r.data), r.size)
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}
func runTerminalBytes(t *testing.T, target tea.Model, input string, fragment int) *terminalHarness {
	t.Helper()
	return runTerminalReader(t, target, &fragmentedTerminalReader{[]byte(input + "\x07"), fragment}, false)
}
func runTerminalReader(t *testing.T, target tea.Model, source io.Reader, stopOnReject bool) *terminalHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := newTerminalInput(source)
	defer r.clear()
	m := &terminalHarness{target: target, stopOnReject: stopOnReject}
	_, err := tea.NewProgram(&terminalModel{model: m, input: r}, tea.WithContext(ctx), tea.WithInput(r), tea.WithFilter(r.filter), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler()).Run()
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func bracket(text string) string { return pasteStart + text + pasteEnd }

func TestTerminalReaderPreservesPasteBytes(t *testing.T) {
	payload := "a\tb\r\nc \ufffd\nend\x1b[A\x13\x00\x1b[200~nested"
	for _, fragment := range []int{1, 2, 3, 5, 6, 7, 255, 256, 257, 4096} {
		e := newPrivateEditor("2026-01-01", "", nil, nil)
		m := runTerminalBytes(t, e, bracket(payload), fragment)
		if m.rejected != 0 || e.input.Value() != payload || e.done {
			t.Fatalf("fragment %d: got %q rejected=%d done=%v", fragment, e.input.Value(), m.rejected, e.done)
		}
	}
	// Every possible two-read split, including all marker and UTF-8 boundaries.
	raw := bracket(payload)
	for split := 1; split < len(raw); split++ {
		e := newPrivateEditor("2026-01-01", "", nil, nil)
		source := io.MultiReader(strings.NewReader(raw[:split]), strings.NewReader(raw[split:]+"\x07"))
		runTerminalReader(t, e, source, false)
		if e.input.Value() != payload {
			t.Fatalf("split %d: %q", split, e.input.Value())
		}
	}
}
func TestTerminalReaderPasteDoesNotAuthorizeWriter(t *testing.T) {
	for _, context := range []string{"writer", "conflict", "find", "help", "narrow"} {
		t.Run(context, func(t *testing.T) {
			saves := 0
			seals := 0
			e := newPrivateEditor("2026-01-01", "base", func(_, _, _ string) error { saves++; return nil }, func(_, _ string) (string, error) { seals++; return "synthetic.age", nil })
			e.input.SetValue("draft")
			switch context {
			case "conflict":
				e.conflicted = true
				e.review = newDraftReview(e.date, "new saved", "draft", 80, 24)
			case "find":
				e.input.findActive = true
			case "help":
				e.keyHelp = newKeyHelp("Writer", privateEditorBindings(false), "", 80, 24)
			case "narrow":
				e.resize(8, 3)
			}
			runTerminalBytes(t, e, bracket("d\r\x13suffix\x1b[A"), 1)
			if saves != 0 || seals != 0 || e.done {
				t.Fatalf("paste authorized action: saves=%d seals=%d done=%v", saves, seals, e.done)
			}
			if context == "conflict" && (e.review == nil || !e.conflicted || e.input.Value() != "draft") {
				t.Fatal("paste selected replacement")
			}
			if context == "find" && !e.input.Finding() {
				t.Fatal("paste escaped find ownership")
			}
			if context == "help" && (e.keyHelp == nil || e.input.Value() != "draft") {
				t.Fatal("paste escaped help ownership")
			}
		})
	}
}
func TestTerminalReaderPhysicalSaveRemainsOrdered(t *testing.T) {
	var saved string
	e := newPrivateEditor("2026-01-01", "", func(_, _, s string) error { saved = s; return nil }, nil)
	payload := "literal\r\n\ufffd\t\x13end"
	runTerminalBytes(t, e, bracket(payload)+"\x13", 1)
	if saved != payload || !e.done {
		t.Fatalf("physical save did not publish whole paste: %q", saved)
	}
}
func TestTerminalReaderRejectionsAreAtomic(t *testing.T) {
	for _, payload := range []string{"prefix\xffsuffix", "prefix\xc3suffix", strings.Repeat("x", privateInputLimit+1)} {
		e := newPrivateEditor("2026-01-01", "base", nil, nil)
		m := runTerminalBytes(t, e, bracket(payload), 256)
		if e.input.Value() != "base" || m.rejected != 1 || e.input.err == "" {
			t.Fatalf("rejection changed draft or failed: length=%d rejected=%d", len(e.input.Value()), m.rejected)
		}
	}
	for _, raw := range []string{pasteStart + "prefix", pasteStart + "prefix\x1b[201", "prefix\xc3"} {
		e := newPrivateEditor("2026-01-01", "", nil, nil)
		m := runTerminalReader(t, e, strings.NewReader(raw), true)
		if m.rejected != 1 {
			t.Fatal("incomplete input not rejected")
		}
		if strings.HasPrefix(raw, pasteStart) && e.input.Value() != "" {
			t.Fatal("incomplete paste inserted prefix")
		}
	}
}
func TestTerminalReaderUserNULCannotSpoofEvents(t *testing.T) {
	e := newPrivateEditor("2026-01-01", "", nil, nil)
	m := runTerminalBytes(t, e, "a\x00b"+bracket("c\x00d"), 1)
	if m.rejected != 1 || e.input.Value() != "abc\x00d" {
		t.Fatalf("NUL spoofed transport: %q rejected=%d", e.input.Value(), m.rejected)
	}
}
func TestTerminalReaderViewerContextsOwnPaste(t *testing.T) {
	for _, which := range []string{"template", "date-help", "viewer-help", "unlock-help"} {
		t.Run(which, func(t *testing.T) {
			a := NewApp(NewCalendar([]string{"2026-01-01"}), NewNotebook([]string{"2026-01-01"}), ModeNotebook)
			defer a.Close()
			switch which {
			case "template":
				a.SetTemplates([]DayTemplate{{ID: "synthetic", Name: "Synthetic"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
					t.Error("paste invoked template")
					return TemplateApplyResult{}, nil
				})
				if !a.openTemplateChooser() || a.templateChooser == nil {
					t.Fatal("synthetic template chooser did not open")
				}
			case "date-help":
				a.dateJump = newDateJumpPrompt("2026-01-01", 80, 24)
				a.dateJump.keyHelp = newKeyHelp("Jump", dateJumpBindings(), "", 80, 24)
			case "viewer-help":
				a.nb.keyHelp = newKeyHelp("View", nil, "", 80, 24)
			case "unlock-help":
				a.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { t.Error("paste authenticated"); return nil }})
				a.startUnlock()
				a.privacy.keyHelp = newKeyHelp("Unlock", unlockBindings(), "", 80, 24)
			}
			runTerminalBytes(t, a, bracket(" uade\r\x13"), 1)
			switch which {
			case "template":
				if a.templateChooser == nil || a.templateChooser.applying {
					t.Fatal("paste accepted template")
				}
			case "date-help":
				if a.dateJump == nil || a.dateJump.keyHelp == nil {
					t.Fatal("paste closed jump help")
				}
			case "viewer-help":
				if a.nb.keyHelp == nil {
					t.Fatal("paste closed viewer help")
				}
			case "unlock-help":
				if a.privacy.keyHelp == nil || a.privacy.prompt.Value() != "" {
					t.Fatal("paste escaped unlock help")
				}
			}
		})
	}
}
func TestTerminalReaderNormalUTF8AndKeys(t *testing.T) {
	e := newPrivateEditor("2026-01-01", "", nil, nil)
	runTerminalBytes(t, e, "a\ufffd日e\u0301\x1b[D\x7f", 1)
	if e.input.Value() != "a\ufffde\u0301" {
		t.Fatalf("normal UTF-8/navigation changed: %q", e.input.Value())
	}
	e = newPrivateEditor("2026-01-01", "", nil, nil)
	runTerminalBytes(t, e, "\x1bOP"+bracket("\r\x13"), 1)
	if e.keyHelp == nil || e.input.Value() != "" {
		t.Fatal("F1/help ownership failed")
	}
}
