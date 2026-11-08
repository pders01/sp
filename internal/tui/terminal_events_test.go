package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTerminalLiteralPaste(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.Msg
		text string
		ok   bool
	}{
		{"raw controls", privatePasteMsg("q\r\x03\x13\x00\x1b[A日"), "q\r\x03\x13\x00\x1b[A日", true},
		{"decoded paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}, Paste: true}, "q", true},
		{"decoded batch", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("quit")}, "quit", true},
		{"invalid decoded rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{-1}, Paste: true}, "\xff", true},
		{"replacement rune", privatePasteMsg("\ufffd"), "\ufffd", true},
		{"empty paste", privatePasteMsg(""), "", true},
		{"physical rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}, "", false},
		{"physical control", tea.KeyMsg{Type: tea.KeyCtrlC}, "", false},
		{"untyped text", "quit", "", false},
		{"rejection", terminalRejectedMsg{"fixed"}, "", false},
		{"nil", nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, ok := TerminalLiteralPaste(tc.msg)
			if text != tc.text || ok != tc.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", text, ok, tc.text, tc.ok)
			}
		})
	}
}

func TestTerminalRejectionBridge(t *testing.T) {
	const reason = "NUL input rejected; re-enter the complete input."
	r := newTerminalInput(strings.NewReader("\x00"))
	defer r.clear()
	out := make([]byte, 8)
	if n, err := r.Read(out); n != 1 || err != nil {
		t.Fatalf("Read = %d, %v", n, err)
	}
	msg := r.filter(&terminalStreamModel{}, tea.KeyMsg{Type: tea.KeyCtrlAt})
	if text, ok := TerminalRejection(msg); !ok || text != reason {
		t.Fatalf("rejection = (%q, %v)", text, ok)
	}
	for _, msg := range []tea.Msg{nil, reason, privatePasteMsg(reason), tea.KeyMsg{Type: tea.KeyCtrlC}} {
		if text, ok := TerminalRejection(msg); ok || text != "" {
			t.Fatalf("non-rejection exposed: (%q, %v)", text, ok)
		}
	}
}

// Returns data and an error together, unlike strings.Reader's separate EOF.
// The adapter must preserve those bytes and defer the error until after them.
type terminalEventFinalReader struct {
	data string
	err  error
}

func (r *terminalEventFinalReader) Read(out []byte) (int, error) {
	n := copy(out, r.data)
	r.data = r.data[n:]
	if r.data == "" {
		return n, r.err
	}
	return n, nil
}

func TestTerminalInputEndAfterFinalTransportEvents(t *testing.T) {
	const incomplete = "Incomplete terminal input rejected; re-enter the complete input."
	const malformed = "Malformed UTF-8 rejected; re-enter the complete input."
	const badPaste = "Paste rejected: input must be valid UTF-8 and at most 8 MiB. Re-enter the complete input."
	key := func(s string) tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	for _, tc := range []struct {
		name string
		data string
		want []tea.Msg
	}{
		{"empty", "", nil},
		{"keys", "ab", []tea.Msg{key("a"), key("b")}},
		{"malformed UTF8", "a\xff\x80b", []tea.Msg{key("a"), terminalRejectedMsg{malformed}, key("b")}},
		{"truncated UTF8", "a\xe2\x82", []tea.Msg{key("a"), terminalRejectedMsg{incomplete}}},
		{"truncated paste", "a" + pasteStart + "q\r\x03", []tea.Msg{key("a"), terminalRejectedMsg{incomplete}}},
		{"invalid paste UTF8", "a" + pasteStart + "\xff" + pasteEnd, []tea.Msg{key("a"), terminalRejectedMsg{badPaste}}},
		{"literal paste then key", pasteStart + "q\r\x03" + pasteEnd + "b", []tea.Msg{privatePasteMsg("q\r\x03"), key("b")}},
	} {
		for _, endErr := range []error{io.EOF, errors.New("synthetic read failure")} {
			t.Run(tc.name+"/"+endErr.Error(), func(t *testing.T) {
				r := newTerminalInput(&terminalEventFinalReader{data: tc.data, err: endErr})
				defer r.clear()
				calls := 0
				r.onEnd = func() {
					calls++
					if !r.mu.TryLock() {
						t.Error("callback invoked under input mutex")
						return
					}
					defer r.mu.Unlock()
					if !r.eof || len(r.queue) != 0 {
						t.Error("callback preceded end or final transport event")
					}
				}
				out := make([]byte, 256)
				n, err := r.Read(out)
				if n > 0 && err != nil || n == 0 && err != endErr {
					t.Fatalf("Read = %d, %v", n, err)
				}
				if n > 0 && calls != 0 {
					t.Fatal("notified before transport drained")
				}
				var got []tea.Msg
				for i := 0; i < n; i++ {
					if out[i] != 0 {
						t.Fatal("transport changed")
					}
					msg := r.filter(&terminalStreamModel{}, tea.KeyMsg{Type: tea.KeyCtrlAt})
					if _, internal := msg.(terminalEscapeStartMsg); !internal {
						got = append(got, msg)
					}
					if i < n-1 && calls != 0 {
						t.Fatal("notified before final pop")
					}
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("events = %#v, want %#v", got, tc.want)
				}
				if calls != 1 {
					t.Fatalf("notifications = %d", calls)
				}
				for i := 0; i < 2; i++ {
					if n, err := r.Read(out); n != 0 || err != io.EOF {
						t.Fatalf("subsequent Read = %d, %v", n, err)
					}
					r.filter(&terminalStreamModel{}, tea.KeyMsg{Type: tea.KeyCtrlAt})
				}
				r.clear()
				if calls != 1 {
					t.Fatal("duplicate end notification")
				}
			})
		}
	}
}

func TestTerminalInputEndAfterSeparateEOF(t *testing.T) {
	for _, drainFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "EOF before drain", true: "EOF after drain"}[drainFirst], func(t *testing.T) {
			r := newTerminalInput(strings.NewReader("ab"))
			defer r.clear()
			calls := 0
			r.onEnd = func() { calls++ }
			out := make([]byte, 8)
			if n, err := r.Read(out); n != 2 || err != nil {
				t.Fatalf("Read = %d, %v", n, err)
			}
			drain := func() {
				for i := 0; i < 2; i++ {
					r.filter(&terminalStreamModel{}, tea.KeyMsg{Type: tea.KeyCtrlAt})
					if i == 0 && calls != 0 {
						t.Fatal("notified before final queued event")
					}
				}
			}
			if drainFirst {
				drain()
			}
			if calls != 0 {
				t.Fatal("notified before EOF")
			}
			if n, err := r.Read(out); n != 0 || err != io.EOF {
				t.Fatalf("EOF Read = %d, %v", n, err)
			}
			if !drainFirst {
				if calls != 0 {
					t.Fatal("EOF notified with events still queued")
				}
				drain()
			}
			if calls != 1 {
				t.Fatalf("notifications = %d", calls)
			}
		})
	}
}

func TestTerminalInputEndCleanupSuppression(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked reader", true: "queued final event"}[queued], func(t *testing.T) {
			calls := 0
			var r *terminalInput
			var cancel func() error
			if queued {
				r = newTerminalInput(&terminalEventFinalReader{data: "x", err: io.EOF})
			} else {
				input, output := io.Pipe()
				defer input.Close()
				defer output.Close()
				r = newTerminalInput(input)
				// Clear before the blocked reader is released by cancellation.
				cancel = input.Close
			}
			r.onEnd = func() { calls++ }
			if queued {
				if n, err := r.Read(make([]byte, 8)); n != 1 || err != nil {
					t.Fatalf("Read = %d, %v", n, err)
				}
				r.clear()
				r.filter(&terminalStreamModel{}, tea.KeyMsg{Type: tea.KeyCtrlAt})
			} else {
				done := make(chan struct{})
				go func() {
					defer close(done)
					r.Read(make([]byte, 8))
				}()
				r.clear()
				cancel()
				<-done
			}
			r.clear()
			if calls != 0 {
				t.Fatalf("cleanup notified %d times", calls)
			}
		})
	}
}

type terminalEndedEvent struct{}

type terminalObserverModel struct {
	ended     chan struct{}
	calls     int
	events    []string
	shortcuts int
}

func (m *terminalObserverModel) TerminalInputEnded() { m.ended <- struct{}{} }
func (m *terminalObserverModel) Init() tea.Cmd {
	return func() tea.Msg { <-m.ended; return terminalEndedEvent{} }
}
func (m *terminalObserverModel) View() string { return "" }
func (m *terminalObserverModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if text, ok := TerminalLiteralPaste(msg); ok {
		m.events = append(m.events, "paste:"+text)
		return m, nil
	}
	if _, ok := TerminalRejection(msg); ok {
		m.events = append(m.events, "rejection")
		return m, nil
	}
	switch event := msg.(type) {
	case tea.KeyMsg:
		if event.Type == tea.KeyCtrlC || event.String() == "q" {
			m.shortcuts++
		}
		m.events = append(m.events, "key:"+event.String())
	case terminalEndedEvent:
		m.calls++
		m.events = append(m.events, "end")
		return m, tea.Quit
	}
	return m, nil
}

func TestRunTerminalProgramObserverOrderingAndLiteralPaste(t *testing.T) {
	m := &terminalObserverModel{ended: make(chan struct{}, 2)}
	payload := "q\r\x03\x13\x00\x1b[A日"
	result := runTerminalPipe(t, m, func(_ context.Context, out *os.File) {
		// Fragment both UTF-8 and paste framing without using any user terminal.
		for _, b := range []byte("a" + pasteStart + payload + pasteEnd + "b\xe2\x82") {
			out.Write([]byte{b})
		}
	})
	want := []string{"key:a", "paste:" + payload, "key:b", "rejection", "end"}
	if result != m || !reflect.DeepEqual(m.events, want) || m.calls != 1 || m.shortcuts != 0 || len(m.ended) != 0 {
		t.Fatalf("events=%q calls=%d shortcuts=%d", m.events, m.calls, m.shortcuts)
	}
}

type terminalCleanupObserverModel struct {
	terminalStreamModel
	init  tea.Cmd
	ended chan struct{}
}

func (m *terminalCleanupObserverModel) Init() tea.Cmd       { return m.init }
func (m *terminalCleanupObserverModel) TerminalInputEnded() { m.ended <- struct{}{} }

func TestRunTerminalProgramObserverCleanup(t *testing.T) {
	requireTerminalStreamPlatform(t)
	for _, cancelRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal quit", true: "cancel"}[cancelRun], func(t *testing.T) {
			input, output, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			defer output.Close() // Keep input open: cleanup itself must not notify.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			m := &terminalCleanupObserverModel{init: tea.Quit, ended: make(chan struct{}, 2)}
			if cancelRun {
				m.init = func() tea.Msg { cancel(); return nil }
			}
			_, err = RunTerminalProgram(m, input, tea.WithContext(ctx), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			if cancelRun && !errors.Is(err, tea.ErrProgramKilled) || !cancelRun && err != nil {
				t.Fatalf("Run = %v", err)
			}
			if len(m.ended) != 0 {
				t.Fatal("cleanup notified the observer")
			}
		})
	}
}

func TestTerminalInputWithoutObserverUnchanged(t *testing.T) {
	r := newTerminalInput(&terminalEventFinalReader{data: "x", err: io.EOF})
	defer r.clear()
	if n, err := r.Read(make([]byte, 8)); n != 1 || err != nil {
		t.Fatalf("Read = %d, %v", n, err)
	}
	msg := r.filter(&terminalStreamModel{}, tea.KeyMsg{Type: tea.KeyCtrlAt})
	if key, ok := msg.(tea.KeyMsg); !ok || key.String() != "x" || r.notified || r.onEnd != nil {
		t.Fatalf("unobserved input changed: %#v", msg)
	}
	m := &terminalStreamModel{}
	if result := runTerminalPipe(t, m, func(_ context.Context, out *os.File) { out.WriteString("x\x07") }); result != m {
		t.Fatal("unobserved runner changed final model")
	}
}
