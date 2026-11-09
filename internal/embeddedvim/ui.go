package embeddedvim

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/pders01/sp/internal/editorcore"
)

var uiSequence atomic.Uint64

var (
	errUIStartup = editorcore.AdapterError("embeddedvim: startup failed", editorcore.ErrRemote)
	errUIFault   = editorcore.AdapterError("embeddedvim: guest failed; retained copy may be incomplete", editorcore.ErrRemote)
	errUIInput   = editorcore.AdapterError("embeddedvim: unsupported input or configuration", editorcore.ErrUnsupported)
)

// UI is an in-memory adapter, never an OS-protected native editor driver.
// All guest calls and ownership transitions complete under mu. There are no
// detached jobs, uncertain requests to replay, or frame-derived actions.
// Snapshots, frames and receipts are private data and must not be logged.
type UI struct {
	mu               sync.Mutex
	session          *Session
	observedSnapshot editorcore.Snapshot
	guestSnapshot    Snapshot
	action           *editorcore.Action
	receipt          editorcore.InputReceipt
	viewport         Viewport
	frame            editorcore.Frame
	width, height    int
	changed          chan struct{}
	fault            error
	latestUnknown    bool
	closed           bool
}

var _ editorcore.UI = (*UI)(nil)

// OpenUI deliberately does not implement Driver.OpenProtected. Use only an
// explicit in-memory factory. Even startup failure
// returns a logically owned copy of the original input until explicit discard.
func OpenUI(ctx context.Context, wasm, bundle []byte, options editorcore.Options, text string) (*UI, error) {
	width, height := options.Width, options.Height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	u := &UI{
		observedSnapshot: editorcore.Snapshot{
			SessionID:  "embedded-vim-" + strconv.FormatUint(uiSequence.Add(1), 10),
			DocumentID: options.DocumentID, Revision: 1, Text: text,
		},
		guestSnapshot: Snapshot{Text: text, Mode: "normal"},
		width:         width, height: height, changed: make(chan struct{}),
	}
	// Reserve document, status and prompt rows even before a prompt opens.
	if ctx == nil || !validText(text) || !editorcore.ValidGeometry(width, height) || height < 3 {
		u.fault = errUIInput
		return u, errUIInput
	}
	if err := u.renderLocked(); err != nil {
		u.fault = errUIStartup
		return u, errUIStartup
	}
	s, err := New(ctx, wasm, bundle, text, Options{})
	u.session = s // Never abandon a partially constructed owner.
	if err != nil {
		u.fault, u.latestUnknown = errUIStartup, true
		u.signalLocked()
		return u, errUIStartup
	}
	initial, err := s.ResizeEditor(ctx, width, height)
	if err != nil || initial.Text != text || initial.Action != nil {
		_ = u.failLocked()
		u.fault = errUIStartup
		return u, errUIStartup
	}
	if err := u.acceptLocked(initial, ""); err != nil {
		u.fault = errUIStartup
		return u, errUIStartup
	}
	return u, nil
}

// Changed is a broadcast generation, not a single-consumer notification queue.
func (u *UI) Changed() <-chan struct{} {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.changed
}
func (u *UI) signalLocked() {
	close(u.changed)
	u.changed = make(chan struct{})
}
func (u *UI) observationLocked() editorcore.Observation {
	r := editorcore.Observation{Observed: u.observedSnapshot, Version: "embedded-vim", LatestUnknown: u.latestUnknown, Closed: u.closed, Fault: u.fault}
	if u.action != nil {
		a := *u.action
		r.Action = &a
	}
	return r
}
func (u *UI) Retained() editorcore.Observation {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.observationLocked()
}
func (u *UI) Frame() editorcore.Frame {
	u.mu.Lock()
	defer u.mu.Unlock()
	f := u.frame
	f.Lines = append([]string(nil), f.Lines...)
	f.Selections = append([]editorcore.CellSpan(nil), f.Selections...)
	f.Matches = append([]editorcore.CellSpan(nil), f.Matches...)
	if f.StatusRow != nil {
		row := *f.StatusRow
		f.StatusRow = &row
	}
	return f
}
func (u *UI) InputReceipt() editorcore.InputReceipt {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.receipt
}
func (u *UI) renderLocked() error {
	f, err := u.viewport.Render(u.guestSnapshot, u.width, u.height)
	if err != nil || f.ANSI() == "" {
		return errUIFault
	}
	u.frame = f
	return nil
}
func (u *UI) failLocked() error {
	u.fault, u.latestUnknown, u.action = errUIFault, true, nil
	// Revocation makes every previously issued frozen snapshot unusable.
	u.observedSnapshot.Frozen = false
	u.observedSnapshot.Revision++
	u.signalLocked()
	return u.fault
}
func (u *UI) acceptLocked(s Snapshot, expectedAction string) error {
	if !validText(s.Text) || (s.Action != nil && (s.Action.Text != s.Text || !validActionKind(s.Action.Kind))) ||
		(expectedAction != "" && (s.Action == nil || s.Action.Kind != expectedAction)) {
		return u.failLocked()
	}
	u.guestSnapshot = clone(s)
	u.observedSnapshot.Text = s.Text
	u.observedSnapshot.Revision++
	u.observedSnapshot.Frozen = s.Action != nil
	u.action = nil
	if s.Action != nil {
		u.action = &editorcore.Action{Kind: s.Action.Kind, Snapshot: u.observedSnapshot}
	}
	if u.renderLocked() != nil {
		return u.failLocked()
	}
	u.signalLocked()
	return nil
}
func (u *UI) readyLocked(ctx context.Context, input bool) error {
	if ctx == nil {
		return errUIInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if u.closed {
		return editorcore.ErrClosed
	}
	if u.fault != nil {
		return u.fault
	}
	if u.latestUnknown || u.session == nil || u.session.Faulted() {
		return u.failLocked()
	}
	if input && u.observedSnapshot.Frozen {
		return editorcore.ErrFrozen
	}
	return nil
}
func (u *UI) beginInputLocked(size int) {
	// Once a guest fails, later rejected input must not erase the sole
	// receipt describing the uncertain transmission. It is never replayed.
	if u.fault != nil && !u.receipt.Known {
		return
	}
	u.receipt = editorcore.InputReceipt{RequestID: u.receipt.RequestID + 1, RequestedBytes: size, Known: true}
}
func (u *UI) completeInputLocked(s Snapshot, err error, expectedAction string) (int, error) {
	if err != nil {
		if errors.Is(err, ErrInput) && !u.session.Faulted() {
			return 0, errUIInput // Synchronous rejection before guest execution.
		}
		u.receipt.Known = false
		return 0, u.failLocked()
	}
	if err := u.acceptLocked(s, expectedAction); err != nil {
		u.receipt.Known = false
		return 0, err
	}
	// Includes intentionally dropped trailing keys after an input-ordered
	// guest action. They are settled, not pending input to replay on resume.
	u.receipt.AcceptedBytes = u.receipt.RequestedBytes
	return u.receipt.AcceptedBytes, nil
}

// Type is physical input, split into Unicode scalar keys, never Vim token
// syntax. Literal paste uses InsertText; controls must use named Key events.
func (u *UI) Type(ctx context.Context, text string) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.beginInputLocked(len(text))
	if !validText(text) || utf8.RuneCountInString(text) > 4096 {
		return 0, errUIInput
	}
	keys := make([]string, 0, utf8.RuneCountInString(text))
	for _, r := range text {
		if unicode.IsControl(r) {
			return 0, errUIInput
		}
		keys = append(keys, string(r))
	}
	if err := u.readyLocked(ctx, true); err != nil {
		return 0, err
	}
	s, err := u.session.Keys(ctx, keys...)
	return u.completeInputLocked(s, err, "")
}

func keyToken(name string) (token, action string) {
	switch name {
	case "Finish", "F2":
		return "", "finish"
	case "Save":
		return "", "save"
	case "Cancel", "F3":
		return "", "cancel"
	}
	keys := map[string]string{
		"Escape": "<Esc>", "Enter": "<CR>", "Backspace": "<BS>", "Delete": "<Del>", "Tab": "<Tab>",
		"Left": "<Left>", "Right": "<Right>", "Up": "<Up>", "Down": "<Down>",
		"Home": "<Home>", "End": "<End>", "PageUp": "<PageUp>", "PageDown": "<PageDown>", "Redo": "<C-r>", "Insert": "<Ins>",
	}
	if token = keys[name]; token != "" {
		return token, ""
	}
	if len(name) == 5 && strings.HasPrefix(name, "Ctrl") && name[4] >= 'A' && name[4] <= 'Z' {
		return "<C-" + strings.ToLower(name[4:]) + ">", ""
	}
	return "", ""
}
func (u *UI) Key(ctx context.Context, name string) (int, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.beginInputLocked(len(name))
	token, action := keyToken(name)
	if token == "" && action == "" {
		return 0, errUIInput
	}
	if err := u.readyLocked(ctx, true); err != nil {
		return 0, err
	}
	var s Snapshot
	var err error
	if action != "" {
		s, err = u.session.FreezeAction(ctx, action)
	} else {
		s, err = u.session.Keys(ctx, token)
	}
	return u.completeInputLocked(s, err, action)
}
func (u *UI) InsertText(ctx context.Context, text string) (editorcore.Snapshot, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.beginInputLocked(len(text))
	if !validText(text) {
		return u.observedSnapshot, errUIInput
	}
	if err := u.readyLocked(ctx, true); err != nil {
		return u.observedSnapshot, err
	}
	s, err := u.session.InsertLiteral(ctx, text)
	if err == nil && s.Action != nil {
		u.receipt.Known = false
		_ = u.failLocked()
		return u.observedSnapshot, u.fault
	}
	_, err = u.completeInputLocked(s, err, "")
	return u.observedSnapshot, err
}
func (u *UI) Resize(ctx context.Context, width, height int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !editorcore.ValidGeometry(width, height) || height < 3 {
		return errUIInput
	}
	if err := u.readyLocked(ctx, false); err != nil {
		return err
	}
	s, err := u.session.ResizeEditor(ctx, width, height)
	if err != nil {
		if errors.Is(err, ErrInput) && !u.session.Faulted() {
			return errUIInput
		}
		return u.failLocked()
	}
	// Display-only changes cannot revoke/reissue a frozen handoff. Verify
	// unchanged content/action and render without changing ownership revision.
	if s.Text != u.observedSnapshot.Text || (s.Action != nil) != (u.action != nil) ||
		(s.Action != nil && (s.Action.Kind != u.action.Kind || s.Action.Text != s.Text)) {
		return u.failLocked()
	}
	u.width, u.height, u.guestSnapshot = width, height, clone(s)
	if u.renderLocked() != nil {
		return u.failLocked()
	}
	u.signalLocked()
	return nil
}
func (u *UI) WaitAction(ctx context.Context) (editorcore.Action, error) {
	for {
		u.mu.Lock()
		if err := u.readyLocked(ctx, false); err != nil {
			u.mu.Unlock()
			return editorcore.Action{}, err
		}
		if u.action != nil {
			a := *u.action
			u.mu.Unlock()
			return a, nil
		}
		changed := u.changed
		u.mu.Unlock()
		select {
		case <-ctx.Done():
			return editorcore.Action{}, ctx.Err()
		case <-changed:
		}
	}
}
func (u *UI) exactFrozenLocked(s editorcore.Snapshot) bool {
	return s.Frozen && u.action != nil && s == u.observedSnapshot && s == u.action.Snapshot
}
func (u *UI) Resume(ctx context.Context, frozen editorcore.Snapshot) (editorcore.Snapshot, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.readyLocked(ctx, false); err != nil {
		return u.observedSnapshot, err
	}
	if !u.exactFrozenLocked(frozen) {
		return u.observedSnapshot, editorcore.ErrRevision
	}
	// Revoke BEFORE entering Wasm. A failed resume cannot resurrect the old
	// action or permit its frozen copy to authorize Close.
	u.action = nil
	u.observedSnapshot.Frozen = false
	u.observedSnapshot.Revision++
	u.signalLocked()
	s, err := u.session.ResumeAction(ctx)
	if err != nil || s.Action != nil || s.Text != frozen.Text {
		_ = u.failLocked()
		return u.observedSnapshot, u.fault
	}
	if err := u.acceptLocked(s, ""); err != nil {
		return u.observedSnapshot, err
	}
	return u.observedSnapshot, nil
}
func (u *UI) Resolve(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	// Calls are synchronous: there is no pending RPC to poll or replay. A
	// destroyed guest remains unknown until the owner explicitly discards.
	return u.readyLocked(ctx, false)
}
func (u *UI) Close(ctx context.Context, frozen editorcore.Snapshot) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		if u.exactFrozenLocked(frozen) {
			return nil
		}
		return editorcore.ErrClosed
	}
	if err := u.readyLocked(ctx, false); err != nil {
		return err
	}
	if !u.exactFrozenLocked(frozen) {
		return editorcore.ErrNotFrozen
	}
	if u.session.Close() != nil {
		return u.failLocked()
	}
	u.closed = true
	u.signalLocked()
	return nil
}
func (u *UI) Discard(ctx context.Context, possibleLossConsent bool) (editorcore.Observation, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !possibleLossConsent {
		return u.observationLocked(), editorcore.ErrLossConsent
	}
	if ctx == nil {
		return u.observationLocked(), errUIInput
	}
	if err := ctx.Err(); err != nil {
		return u.observationLocked(), err
	}
	if !u.closed {
		if u.session != nil && u.session.Close() != nil {
			_ = u.failLocked()
			return u.observationLocked(), u.fault
		}
		u.closed, u.action = true, nil
		u.signalLocked()
	}
	return u.observationLocked(), nil
}
