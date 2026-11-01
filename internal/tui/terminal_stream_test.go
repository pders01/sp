package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Observes real transport events; deliberately contains no application routing.
type terminalStreamModel struct {
	msgs         []tea.Msg
	escaped      chan struct{}
	stopOnReject bool
}

func (m *terminalStreamModel) Init() tea.Cmd { return nil }
func (m *terminalStreamModel) View() string  { return "" }
func (m *terminalStreamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.msgs = append(m.msgs, msg)
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.Type == tea.KeyEsc && m.escaped != nil {
			close(m.escaped)
			m.escaped = nil
		}
		if key.Type == tea.KeyCtrlG {
			return m, tea.Quit
		}
	}
	if _, ok := msg.(terminalRejectedMsg); ok && m.stopOnReject {
		return m, tea.Quit
	}
	return m, nil
}

func requireTerminalStreamPlatform(t *testing.T) {
	t.Helper()
	if err := terminalInputSupported(); err != nil {
		t.Skipf("interactive byte input explicitly fail-closed: %v", err)
	}
}

func TestTerminalStreamPlatformGuard(t *testing.T) {
	if runtime.GOOS != "windows" {
		return
	}
	if terminalInputSupported() == nil {
		t.Fatal("Windows must not bypass the byte adapter")
	}
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	m := &terminalStreamModel{}
	result, err := RunTerminalProgram(m, input)
	if err == nil || result != m || len(m.msgs) != 0 {
		t.Fatal("unsupported platform did not fail before starting Tea")
	}
}

func runTerminalPipe(t *testing.T, model tea.Model, write func(context.Context, *os.File)) tea.Model {
	t.Helper()
	requireTerminalStreamPlatform(t)
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); defer output.Close(); write(ctx, output) }()
	result, err := RunTerminalProgram(model, input, tea.WithContext(ctx), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	cancel()
	output.Close()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTerminalStreamSingleEscapeAndLateOwnership(t *testing.T) {
	for _, prefix := range []string{"\x1b", "\x1b[", "\x1b[2", "\x1b[200"} {
		t.Run(strings.ReplaceAll(prefix, "\x1b", "ESC"), func(t *testing.T) {
			escaped := make(chan struct{})
			m := &terminalStreamModel{escaped: escaped}
			payload := "literal\r\x13\x03\x00\x1b[A\ufffd日"
			result := runTerminalPipe(t, m, func(ctx context.Context, out *os.File) {
				io.WriteString(out, "before"+prefix)
				if prefix == "\x1b" {
					select {
					case <-escaped:
					case <-ctx.Done():
						return
					}
				} else {
					select {
					case <-time.After(2 * terminalEscapeDelay):
					case <-ctx.Done():
						return
					}
				}
				// A lone Escape resolves; a partial protocol prefix never does.
				// Finish the delayed marker without releasing its literal controls.
				for _, b := range []byte(pasteStart[len(prefix):] + payload + pasteEnd + "x\x07") {
					out.Write([]byte{b})
				}
			})
			if result != m {
				t.Fatal("runner returned transport wrapper")
			}
			escapes, pastes := 0, 0
			var text strings.Builder
			for _, msg := range m.msgs {
				switch event := msg.(type) {
				case tea.KeyMsg:
					switch event.Type {
					case tea.KeyEsc:
						escapes++
					case tea.KeyRunes:
						text.WriteString(string(event.Runes))
					case tea.KeyCtrlG:
					default:
						t.Fatalf("literal control escaped: %#v", event)
					}
				case privatePasteMsg:
					pastes++
					if string(event) != payload {
						t.Fatalf("paste changed: %q", event)
					}
				case terminalRejectedMsg:
					t.Fatalf("valid delayed paste rejected: %s", event.reason)
				case terminalEscapeStartMsg, terminalEscapeTimeoutMsg:
					t.Fatal("internal event leaked")
				}
			}
			wantEscapes := 0
			if prefix == "\x1b" {
				wantEscapes = 1
			}
			if escapes != wantEscapes || pastes != 1 || text.String() != "beforex" {
				t.Fatalf("escape=%d want=%d paste=%d text=%q", escapes, wantEscapes, pastes, text.String())
			}
		})
	}
}

func TestTerminalStreamResolvedEscapeNotRepeatedOnNextKey(t *testing.T) {
	escaped := make(chan struct{})
	m := &terminalStreamModel{escaped: escaped}
	runTerminalPipe(t, m, func(ctx context.Context, out *os.File) {
		io.WriteString(out, "\x1b")
		select {
		case <-escaped:
		case <-ctx.Done():
			return
		}
		io.WriteString(out, "x\r\x07")
	})
	var keys []tea.KeyType
	for _, msg := range m.msgs {
		if k, ok := msg.(tea.KeyMsg); ok {
			keys = append(keys, k.Type)
		}
	}
	if len(keys) != 4 || keys[0] != tea.KeyEsc || keys[1] != tea.KeyRunes || keys[2] != tea.KeyEnter || keys[3] != tea.KeyCtrlG {
		t.Fatalf("keys: %v", keys)
	}
}

func TestTerminalStreamEscapeThenPhysicalControlIsOrdered(t *testing.T) {
	m := &terminalStreamModel{}
	runTerminalPipe(t, m, func(_ context.Context, out *os.File) {
		io.WriteString(out, "\x1b\x0c\x07")
	})
	var keys []tea.KeyType
	for _, msg := range m.msgs {
		if key, ok := msg.(tea.KeyMsg); ok {
			keys = append(keys, key.Type)
		} else if _, rejected := msg.(terminalRejectedMsg); rejected {
			t.Fatal("physical control after Escape was discarded as Alt input")
		}
	}
	if len(keys) != 3 || keys[0] != tea.KeyEsc || keys[1] != tea.KeyCtrlL || keys[2] != tea.KeyCtrlG {
		t.Fatalf("physical keys reordered: %v", keys)
	}
}

func TestTerminalStreamEscapeClosesHelpBeforeImmediateRelock(t *testing.T) {
	const date = "2026-01-01"
	a := NewApp(NewCalendar([]string{date}), NewNotebook([]string{date}), ModeNotebook)
	defer a.Close()
	locked := false
	a.SetPrivacy(PrivacyHooks{
		IsUnlocked: func() bool { return !locked },
		Load: func(string) (PrivatePage, error) {
			return PrivatePage{Private: "synthetic", HasPrivate: true, Locked: locked}, nil
		},
		Lock:    func() { locked = true },
		Recover: func(_, _ string) (string, error) { return "synthetic.age", nil },
	})
	a.startPrivateEdit(date)
	a.privacy.editor.keyHelp = newKeyHelp("Write", nil, "", 80, 24)
	runTerminalPipe(t, a, func(_ context.Context, out *os.File) {
		io.WriteString(out, "\x1b\x0cq")
	})
	if !locked || a.privacy.editor != nil {
		t.Fatal("Escape followed immediately by Ctrl+L did not close help and relock")
	}
}

func TestTerminalStreamProductionEditorAndUnlock(t *testing.T) {
	var saved string
	e := newPrivateEditor("2026-01-01", "", func(_, _, text string) error { saved = text; return nil }, nil)
	payload := "a\r\n\t\ufffd日\x13"
	result := runTerminalPipe(t, e, func(_ context.Context, out *os.File) { io.WriteString(out, bracket(payload)+"\x13") })
	if result != e || !e.done || saved != payload {
		t.Fatalf("production editor: saved=%q done=%v", saved, e.done)
	}
	// Standalone unlock must cancel on ONE physical Escape, not a test hook.
	s := newUnlockScreen(UnlockOptions{IsUnlocked: func() bool { return false }, Unlock: func([]byte) error { t.Error("Escape authenticated"); return nil }, Lock: func() {}})
	defer s.app.Close()
	result = runTerminalPipe(t, s, func(_ context.Context, out *os.File) { io.WriteString(out, "\x1b") })
	if result != s || !s.done || s.accepted || !errors.Is(s.err, ErrUnlockCancelled) {
		t.Fatal("single Escape did not cancel production unlock")
	}
}

func TestTerminalStreamFilterUnwrapsAndComposesR1(t *testing.T) {
	requireTerminalStreamPlatform(t)
	e := newPrivateEditor("2026-01-01", "base", nil, nil)
	e.keyHelp = newKeyHelp("Synthetic", nil, "", 80, 24)
	r := newTerminalInput(strings.NewReader(""))
	wrapped := &terminalModel{model: e, input: r}
	if _, ok := r.filter(wrapped, tea.QuitMsg{}).(privateEditorInterruptedMsg); !ok {
		t.Fatal("R1 signal filter lost under wrapper")
	}
	// Execute the composed filter through the actual runner's event loop.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	r.source = input
	program := tea.NewProgram(wrapped, tea.WithContext(ctx), tea.WithInput(&terminalFileInput{terminalInput: r, file: input}), tea.WithFilter(r.filter), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	go program.Send(tea.QuitMsg{})
	_, err = program.Run()
	if err != nil || !e.done || !errors.Is(e.err, ErrPrivateEditorInterrupted) || e.keyHelp != nil {
		t.Fatalf("R1 shutdown lost: done=%v err=%v run=%v", e.done, e.err, err)
	}
}

func TestTerminalStreamQueueBoundedAndEOF(t *testing.T) {
	r := newTerminalInput(strings.NewReader(strings.Repeat("a", 10000)))
	defer r.clear()
	buf := make([]byte, 1<<20)
	count := 0
	for {
		n, err := r.Read(buf)
		if n > 256 || len(r.queue) > 256 {
			t.Fatal("unbounded event queue")
		}
		for range n {
			r.filter(nil, tea.KeyMsg{Type: tea.KeyCtrlAt})
			count++
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 10000 || len(r.queue) != 0 {
		t.Fatalf("lost bytes: %d", count)
	}
	for _, raw := range []string{"\xc3", "\xf0\x9f", pasteStart + "prefix", pasteStart + "prefix\x1b[201"} {
		m := &terminalStreamModel{stopOnReject: true}
		runTerminalPipe(t, m, func(_ context.Context, out *os.File) { io.WriteString(out, raw) })
		if len(m.msgs) == 0 {
			t.Fatal("EOF rejection missing")
		}
		rejected := 0
		for _, msg := range m.msgs {
			if _, ok := msg.(terminalRejectedMsg); ok {
				rejected++
			}
		}
		if rejected != 1 {
			t.Fatalf("EOF %q: rejected=%d", raw, rejected)
		}
	}
}

func TestTerminalStreamCancellationRetainsCallerFile(t *testing.T) {
	requireTerminalStreamPlatform(t)
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	ctx, cancel := context.WithCancel(context.Background())
	m := &terminalStreamModel{}
	if _, err := io.WriteString(output, pasteStart+"synthetic pending"); err != nil {
		t.Fatal(err)
	}
	// Cancel with an incomplete literal paste while the real fd-backed reader
	// is blocked, not after EOF. No pending payload may become physical keys.
	timer := time.AfterFunc(30*time.Millisecond, cancel)
	defer timer.Stop()
	_, err = RunTerminalProgram(m, input, tea.WithContext(ctx), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	if !errors.Is(err, tea.ErrProgramKilled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, msg := range m.msgs {
		switch msg.(type) {
		case tea.KeyMsg, privatePasteMsg:
			t.Fatal("incomplete paste escaped during cancellation")
		}
	}
	if err := input.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := input.Read(b[:]); err != nil || b[0] != 'x' {
		t.Fatalf("caller fd closed or lingering input consumer: %q %v", b, err)
	}
}

func TestTerminalExecProcessUsesRealFile(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	cmd := exec.Command("synthetic-never-executed")
	_ = TerminalExecProcess(cmd, input, nil)
	if cmd.Stdin != input {
		t.Fatal("child stdin is not the caller's actual file")
	}
}

type terminalExecDoneMsg struct{ err error }
type terminalExecModel struct {
	terminalStreamModel
	input   *os.File
	resumed chan struct{}
	err     error
}

func (m *terminalExecModel) Init() tea.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestTerminalExecSyntheticChild$")
	cmd.Env = append(os.Environ(), "SP_TERMINAL_SYNTHETIC_CHILD=1")
	return TerminalExecProcess(cmd, m.input, func(err error) tea.Msg { return terminalExecDoneMsg{err} })
}
func (m *terminalExecModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if done, ok := msg.(terminalExecDoneMsg); ok {
		m.err = done.err
		close(m.resumed)
		return m, nil
	}
	_, cmd := m.terminalStreamModel.Update(msg)
	return m, cmd
}
func TestTerminalExecSyntheticChild(t *testing.T) {
	if os.Getenv("SP_TERMINAL_SYNTHETIC_CHILD") == "1" {
		os.Exit(0)
	}
}
func TestTerminalStreamExecProcessResumesWithoutStdinCopy(t *testing.T) {
	requireTerminalStreamPlatform(t)
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := &terminalExecModel{input: input, resumed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer output.Close()
		select {
		case <-m.resumed:
			io.WriteString(output, "x\x07")
		case <-ctx.Done():
		}
	}()
	result, err := RunTerminalProgram(m, input, tea.WithContext(ctx), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	cancel()
	output.Close()
	<-done
	if err != nil || m.err != nil || result != m {
		t.Fatalf("ExecProcess resume: run=%v child=%v", err, m.err)
	}
	var keys []tea.KeyMsg
	for _, msg := range m.msgs {
		if key, ok := msg.(tea.KeyMsg); ok {
			keys = append(keys, key)
		}
	}
	if len(keys) != 2 || keys[0].Type != tea.KeyRunes || string(keys[0].Runes) != "x" || keys[1].Type != tea.KeyCtrlG {
		t.Fatalf("resumed reader lost or stole keys: %#v", keys)
	}
}

type terminalFinalReader struct{ data []byte }

func (r *terminalFinalReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, io.EOF
}
func TestTerminalStreamReadDataWithEOF(t *testing.T) {
	for _, raw := range []string{"abc", "abc\xc3", pasteStart + "suffix"} {
		r := newTerminalInput(&terminalFinalReader{data: []byte(raw)})
		buf := make([]byte, 256)
		n, err := r.Read(buf)
		if err != nil || n == 0 {
			t.Fatalf("discarded data returned with EOF: %q %d %v", raw, n, err)
		}
		rejects := 0
		for range n {
			if _, ok := r.filter(nil, tea.KeyMsg{Type: tea.KeyCtrlAt}).(terminalRejectedMsg); ok {
				rejects++
			}
		}
		want := 0
		if raw != "abc" {
			want = 1
		}
		if rejects != want {
			t.Fatalf("EOF %q rejects=%d", raw, rejects)
		}
		if n, err = r.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("EOF not deferred once: %d %v", n, err)
		}
		r.clear()
	}
}

func TestTerminalBatchedInvalidRunesDoNotAuthenticate(t *testing.T) {
	a := NewApp(NewCalendar(nil), NewNotebook(nil), ModeNotebook)
	defer a.Close()
	calls := 0
	a.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { calls++; return nil }})
	a.startUnlock()
	a.Update(privatePasteMsg("prefix"))
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x', -1}})
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if calls != 0 {
		t.Fatal("invalid decoded runes normalized into an authenticating secret")
	}
}

func TestTerminalBatchedRunesAreLiteralOwnership(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("d\r\x13\ufffd")},
		{Type: tea.KeyRunes, Runes: []rune("d"), Paste: true},
	} {
		e := newPrivateEditor("2026-01-01", "base", func(_, _, _ string) error { t.Error("batch saved"); return nil }, nil)
		e.conflicted = true
		e.review = newDraftReview(e.date, "saved", "draft", 80, 24)
		e.Update(key)
		if e.review == nil || !e.conflicted || e.input.Value() != "base" {
			t.Fatal("batch authorized conflict replacement")
		}
		e.review = nil
		e.input.SetValue("")
		e.Update(key)
		if e.input.Value() != string(key.Runes) {
			t.Fatalf("batch normalized: %q", e.input.Value())
		}
		a := NewApp(NewCalendar(nil), NewNotebook(nil), ModeNotebook)
		a.SetPrivacy(PrivacyHooks{Unlock: func([]byte) error { t.Error("batch authenticated"); return nil }})
		a.startUnlock()
		a.privacy.keyHelp = newKeyHelp("Synthetic", nil, "", 80, 24)
		a.Update(key)
		if a.privacy.keyHelp == nil || a.privacy.prompt.Value() != "" {
			t.Fatal("batch escaped help ownership")
		}
		a.Close()
	}
}
