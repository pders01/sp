package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// terminalRejectedMsg invalidates the entire current secret, not just the bad
// suffix. A later physical Enter must never authenticate an accepted prefix.
type terminalRejectedMsg struct{ reason string }

// The start travels through the same ordered transport as keys. Its timer is
// scheduled by the model, never by a reader goroutine. Resolution leaves the
// parser's prefix quarantined: even a very late paste marker still owns all of
// its contents. A resolved Escape cannot subsequently become a second key.
const terminalEscapeDelay = 75 * time.Millisecond

type terminalEscape struct{ active, resolved bool }
type terminalEscapeStartMsg struct{ escape *terminalEscape }
type terminalEscapeTimeoutMsg struct{ escape *terminalEscape }
type terminalEscapeControlMsg struct{ key tea.KeyMsg }

// terminalInput owns bytes BEFORE Tea's lossy UTF-8/key/paste decoder. Tea sees
// only one NUL per queued event, including user-supplied NUL (a rejection).
// Read never buffers transport output: each call emits no more bytes than it
// consumed. Thus cancelreader can wait on the ORIGINAL fd without bypassing or
// stranding adapter-buffered output. There is no input pump goroutine.
type terminalInput struct {
	source             io.Reader
	mu                 sync.Mutex
	queue              []tea.Msg
	pending            []byte
	paste              []byte
	pasting, oversized bool
	end                []byte
	eof                bool
	escape             *terminalEscape
	onEnd              func()
	notified           bool
}

func newTerminalInput(source io.Reader) *terminalInput { return &terminalInput{source: source} }

func (r *terminalInput) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEnd = nil
	r.notified = true
	clear(r.queue)
	r.queue = nil
	clear(r.pending)
	clear(r.paste)
	r.pending, r.paste, r.end = nil, nil, nil
	if r.escape != nil {
		r.escape.active = false
	}
}

func terminalUnderlying(model tea.Model) tea.Model {
	if wrapped, ok := model.(*terminalModel); ok {
		return wrapped.model
	}
	return model
}

func (r *terminalInput) filter(model tea.Model, msg tea.Msg) tea.Msg {
	model = terminalUnderlying(model)
	if _, quit := msg.(tea.QuitMsg); quit {
		if screen, ok := model.(*unlockScreen); ok && !screen.done {
			return unlockInterruptedMsg{}
		}
	}
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyCtrlAt && !k.Alt {
		r.mu.Lock()
		if len(r.queue) == 0 {
			msg = terminalRejectedMsg{"Unexpected input transport event; input rejected."}
		} else {
			msg = r.queue[0]
			r.queue[0] = nil
			r.queue = r.queue[1:]
		}
		notify := r.endNotificationLocked()
		r.mu.Unlock()
		if notify != nil {
			notify()
		}
	}
	// Compose R1's recovery/cancellation filter using the actual editor, not
	// the transport wrapper. Callers need not (and must not) replace this filter.
	return privateEditorSignalFilter(model, msg)
}

// terminalModel owns only transport events; application Update receives the
// original physical/literal messages and Run returns its actual final model.
type terminalModel struct {
	model tea.Model
	input *terminalInput
}

func (m *terminalModel) Init() tea.Cmd { return m.model.Init() }
func (m *terminalModel) View() string  { return m.model.View() }
func (m *terminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch event := msg.(type) {
	case terminalEscapeControlMsg:
		// A C0 key cannot continue CSI/SS3 or a paste marker. Preserve both
		// physical keys in order, even when they arrive before Escape's timer.
		next, first := m.model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m.model = next
		finished := false
		switch current := next.(type) {
		case *privateEditor:
			finished = current.done
		case *unlockScreen:
			finished = current.done
		case *App:
			finished = current.IsQuitting()
		}
		if finished {
			return m, first
		}
		next, second := m.model.Update(event.key)
		m.model = next
		return m, tea.Batch(first, second)
	case terminalEscapeStartMsg:
		return m, tea.Tick(terminalEscapeDelay, func(time.Time) tea.Msg {
			return terminalEscapeTimeoutMsg(event)
		})
	case terminalEscapeTimeoutMsg:
		m.input.mu.Lock()
		// Only a lone Escape is ambiguous. A known partial CSI/SS3 or
		// paste prefix must never act as a key merely because it is slow.
		resolve := event.escape.active && !event.escape.resolved && len(m.input.pending) == 1
		if resolve {
			event.escape.resolved = true
		}
		m.input.mu.Unlock()
		if !resolve {
			return m, nil
		}
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	}
	next, cmd := m.model.Update(msg)
	m.model = next
	return m, cmd
}

func (r *terminalInput) Read(out []byte) (int, error) {
	if len(out) == 0 {
		return 0, nil
	}
	if r.eof {
		return 0, io.EOF
	}
	// Bound queued events even if the caller supplies an unusually large buffer.
	buf := make([]byte, min(len(out), 256))
	n, err := r.source.Read(buf)
	r.mu.Lock()
	before := len(r.queue)
	for offset := 0; offset < n; {
		if r.pasting && len(r.end) == 0 {
			// Most paste bytes cannot start the closing marker. Retain/drop
			// them in bounded chunks instead of per-byte state-machine work;
			// this also keeps race/coverage instrumentation proportional to
			// reads rather than millions of ordinary pasted characters.
			plain := bytes.IndexByte(buf[offset:n], 0x1b)
			if plain < 0 {
				plain = n - offset
			}
			if plain > 0 {
				r.appendPaste(buf[offset : offset+plain])
				offset += plain
				continue
			}
		}
		r.byte(buf[offset])
		offset++
	}
	clear(buf)
	// Tea discards bytes returned WITH an error. Defer EOF until after our
	// final rejection event; truncated paste/UTF-8 must poison a secret too.
	if err != nil {
		r.eof = true
		// A lone physical Escape remains eligible for bounded resolution at
		// EOF. Partial markers and UTF-8, unlike Escape, are invalid input.
		if r.pasting || (len(r.pending) != 0 && string(r.pending) != "\x1b") {
			r.reject("Incomplete terminal input rejected; re-enter the complete input.")
			clear(r.pending)
			clear(r.paste)
			r.pending, r.paste, r.end = nil, nil, nil
			r.pasting = false
			if r.escape != nil {
				r.escape.active = false
			}
		}
	}
	count := len(r.queue) - before
	notify := r.endNotificationLocked()
	r.mu.Unlock()
	if notify != nil {
		notify()
	}
	if count > len(out) {
		panic("terminal input expanded transport")
	}
	clear(out[:count])
	if count > 0 {
		return count, nil
	}
	return 0, err
}

// endNotificationLocked claims the notification only once, after every queued
// transport event has passed through the filter. Invoke the result unlocked.
func (r *terminalInput) endNotificationLocked() func() {
	if !r.eof || len(r.queue) != 0 || r.notified || r.onEnd == nil {
		return nil
	}
	r.notified = true
	return r.onEnd
}

func (r *terminalInput) emit(msg tea.Msg)     { r.queue = append(r.queue, msg) }
func (r *terminalInput) reject(reason string) { r.emit(terminalRejectedMsg{reason}) }

const pasteStart = "\x1b[200~"
const pasteEnd = "\x1b[201~"

func (r *terminalInput) appendPaste(data []byte) {
	remaining := privateInputLimit - len(r.paste)
	if len(data) > remaining {
		r.oversized = true
		data = data[:remaining]
	}
	r.paste = append(r.paste, data...)
}

func (r *terminalInput) pasteByte(b byte) {
	r.end = append(r.end, b)
	for len(r.end) > 0 && !bytes.HasPrefix([]byte(pasteEnd), r.end) {
		r.appendPaste(r.end[:1])
		copy(r.end, r.end[1:])
		r.end = r.end[:len(r.end)-1]
	}
	if string(r.end) != pasteEnd {
		return
	}
	if r.oversized || !utf8.Valid(r.paste) {
		r.reject("Paste rejected: input must be valid UTF-8 and at most 8 MiB. Re-enter the complete input.")
	} else {
		r.emit(privatePasteMsg(string(r.paste)))
	}
	clear(r.paste)
	r.paste, r.end = nil, nil
	r.pasting, r.oversized = false, false
}

func (r *terminalInput) byte(b byte) {
	if r.pasting {
		r.pasteByte(b)
		return
	}
	if len(r.pending) == 0 {
		switch {
		case b == 0:
			r.reject("NUL input rejected; re-enter the complete input.")
		case b == 0x1b:
			r.pending = append(r.pending, b)
			r.escape = &terminalEscape{active: true}
			r.emit(terminalEscapeStartMsg{r.escape})
		case b >= utf8.RuneSelf:
			r.pending = append(r.pending, b)
		case b == ' ':
			r.emit(tea.KeyMsg{Type: tea.KeySpace})
		case b < 32 || b == 127:
			r.emit(tea.KeyMsg{Type: tea.KeyType(b)})
		default:
			r.emit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(b)}})
		}
		return
	}
	if r.pending[0] != 0x1b {
		// Do not consume a following physical Enter as part of malformed UTF-8.
		if b < 0x80 || b >= 0xc0 {
			r.reject("Malformed UTF-8 rejected; re-enter the complete input.")
			clear(r.pending)
			r.pending = nil
			// Reject the boundary byte too. In particular an Enter after an
			// incomplete rune is not a secret submission.
			return
		}
		r.pending = append(r.pending, b)
		if !utf8.FullRune(r.pending) {
			return
		}
		if !utf8.Valid(r.pending) {
			r.reject("Malformed UTF-8 rejected; re-enter the complete input.")
		} else {
			r.emit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(r.pending))})
		}
		clear(r.pending)
		r.pending = nil
		return
	}
	// After timed resolution, an ordinary following key is not Alt input
	// and must not re-emit Escape. '[' and 'O' stay quarantined regardless
	// of delay, so a late/fragmented marker cannot leak literal controls.
	if len(r.pending) == 1 && r.escape.resolved && b != '[' && b != 'O' {
		r.escape.active = false
		clear(r.pending)
		r.pending = nil
		r.byte(b)
		return
	}
	if len(r.pending) == 1 && b > 0 && (b < 32 || b == 127) && b != 0x1b {
		r.escape.active = false
		r.emit(terminalEscapeControlMsg{tea.KeyMsg{Type: tea.KeyType(b)}})
		clear(r.pending)
		r.pending = nil
		return
	}
	r.pending = append(r.pending, b)
	if string(r.pending) == pasteStart {
		r.escape.active = false
		clear(r.pending)
		r.pending = nil
		r.pasting = true
		return
	}
	if bytes.HasPrefix([]byte(pasteStart), r.pending) {
		return
	}
	// Unresolved Alt keys and unknown protocols are rejected, not normalized.
	if string(r.pending) == "\x1b\x1b" {
		r.escape.active = false
		r.emit(tea.KeyMsg{Type: tea.KeyEsc})
		clear(r.pending)
		r.pending = nil
		return
	}
	if len(r.pending) == 2 && (b == '[' || b == 'O') {
		return
	}
	if len(r.pending) >= 3 && (r.pending[1] == '[' || r.pending[1] == 'O') && b >= 0x20 && b <= 0x3f && len(r.pending) < 32 {
		return
	}
	r.escape.active = false
	if key, ok := terminalKeys[string(r.pending)]; ok {
		if !r.escape.resolved {
			r.emit(tea.KeyMsg{Type: key})
		}
	} else {
		r.reject("Unsupported terminal escape/Alt input rejected. Use Escape or Ctrl+C to cancel.")
	}
	clear(r.pending)
	r.pending = nil
}

// Only the application's navigation/edit/help keys are accepted. Unknown
// escape protocols are not allowed to turn into an authorization key.
var terminalKeys = map[string]tea.KeyType{
	"\x1b[A": tea.KeyUp, "\x1b[B": tea.KeyDown, "\x1b[C": tea.KeyRight, "\x1b[D": tea.KeyLeft,
	"\x1bOA": tea.KeyUp, "\x1bOB": tea.KeyDown, "\x1bOC": tea.KeyRight, "\x1bOD": tea.KeyLeft,
	"\x1b[H": tea.KeyHome, "\x1b[F": tea.KeyEnd, "\x1bOH": tea.KeyHome, "\x1bOF": tea.KeyEnd,
	"\x1b[1~": tea.KeyHome, "\x1b[4~": tea.KeyEnd, "\x1b[7~": tea.KeyHome, "\x1b[8~": tea.KeyEnd,
	"\x1b[2~": tea.KeyInsert, "\x1b[3~": tea.KeyDelete, "\x1b[5~": tea.KeyPgUp, "\x1b[6~": tea.KeyPgDown,
	"\x1b[Z": tea.KeyShiftTab,
	"\x1bOP": tea.KeyF1, "\x1bOQ": tea.KeyF2, "\x1bOR": tea.KeyF3, "\x1bOS": tea.KeyF4,
	"\x1b[11~": tea.KeyF1, "\x1b[12~": tea.KeyF2, "\x1b[13~": tea.KeyF3, "\x1b[14~": tea.KeyF4,
	"\x1b[15~": tea.KeyF5, "\x1b[17~": tea.KeyF6, "\x1b[18~": tea.KeyF7, "\x1b[19~": tea.KeyF8,
	"\x1b[20~": tea.KeyF9, "\x1b[21~": tea.KeyF10, "\x1b[23~": tea.KeyF11, "\x1b[24~": tea.KeyF12,
	"\x1b[1;5A": tea.KeyCtrlUp, "\x1b[1;5B": tea.KeyCtrlDown, "\x1b[1;5C": tea.KeyCtrlRight, "\x1b[1;5D": tea.KeyCtrlLeft,
	"\x1b[1;5H": tea.KeyCtrlHome, "\x1b[1;5F": tea.KeyCtrlEnd,
	"\x1b[5;5~": tea.KeyCtrlPgUp, "\x1b[6;5~": tea.KeyCtrlPgDown,
}

func terminalInputSupported() error {
	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "openbsd", "netbsd", "dragonfly":
		return nil
	default:
		return errors.New("safe terminal byte input is unsupported on this platform (including Windows); refusing an unsafe decoder fallback")
	}
}

// RunTerminalProgram installs the shared byte owner, the R1 editor signal
// filter, and Tea's bracketed-paste lifecycle. Options must not set a filter or
// replace its input. It never opens a
// fallback tty. Tea owns raw-mode restoration and cancelreader handles, also
// across ReleaseTerminal/RestoreTerminal. Caller retains ownership of input.
func RunTerminalProgram(model tea.Model, input *os.File, options ...tea.ProgramOption) (tea.Model, error) {
	if input == nil {
		return model, errors.New("terminal input descriptor is required")
	}
	if err := terminalInputSupported(); err != nil {
		return model, err
	}
	r := newTerminalInput(input)
	if observer, ok := terminalUnderlying(model).(TerminalInputObserver); ok {
		r.onEnd = observer.TerminalInputEnded
	}
	defer r.clear()
	options = append(options, tea.WithInput(&terminalFileInput{terminalInput: r, file: input}), tea.WithFilter(r.filter))
	wrapped := &terminalModel{model: model, input: r}
	result, err := tea.NewProgram(wrapped, options...).Run()
	return terminalUnderlying(result), err
}

// TerminalExecProcess keeps child stdin an actual file, not the Tea adapter.
// Otherwise os/exec creates a copy goroutine which can steal resumed TUI input
// or wait forever after the child exits. Use the same descriptor as Run above.
func TerminalExecProcess(cmd *exec.Cmd, input *os.File, callback tea.ExecCallback) tea.Cmd {
	if cmd.Stdin == nil {
		if input == nil {
			return func() tea.Msg {
				if callback != nil {
					return callback(errors.New("child terminal input descriptor is required"))
				}
				return nil
			}
		}
		cmd.Stdin = input
	}
	return tea.ExecProcess(cmd, callback)
}

// terminalFileInput intentionally satisfies cancelreader.File. On supported
// Unix platforms cancelreader polls this fd then calls our Read (not file.Read).
// The adapter never retains transport bytes requiring another readiness edge.
type terminalFileInput struct {
	*terminalInput
	file *os.File
}

func (r *terminalFileInput) Fd() uintptr                 { return r.file.Fd() }
func (r *terminalFileInput) Name() string                { return r.file.Name() }
func (r *terminalFileInput) Write(p []byte) (int, error) { return r.file.Write(p) }

// Tea/cancelreader must not close the caller-owned terminal.
func (r *terminalFileInput) Close() error { return nil }
