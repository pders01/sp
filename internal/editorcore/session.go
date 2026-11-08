// Package editorcore defines editor-independent in-memory document, UI and
// ownership contracts. It has no editor executable, OS enforcement, store, key,
// editor discovery or normal CLI routing. An adapter is not a sandbox backend.
package editorcore

import (
	"context"
	"errors"
)

// Snapshots/retentions contain PRIVATE text. Never log/serialize them. Frozen
// means input-ordered handoff, NOT publication or filesystem-write prevention.
type Snapshot struct {
	SessionID  string
	DocumentID string
	Revision   uint64
	Text       string
	Frozen     bool
}

type Retention struct {
	Mirror        Snapshot
	Candidate     *Snapshot
	LatestUnknown bool
	Closed        bool
	Fault         error
	Version       string
}

type Action struct {
	Kind     string
	Snapshot Snapshot
}

type Observation struct {
	Observed              Snapshot
	Action                *Action
	Version               string
	LatestUnknown, Closed bool
	Fault                 error
}

// InputReceipt describes bytes queued, never processed/saved. Pending input
// remains owned until its exact transmission and response have settled.
type InputReceipt struct {
	RequestID                     uint64
	RequestedBytes, AcceptedBytes int
	Known                         bool
}

// Document is the storage-independent lifecycle used by the encrypted owner.
// Store calls remain parent-owned, never delegated to the editor or adapter.
type Document interface {
	Retained() Retention
	Set(context.Context, Snapshot, string) (Snapshot, error)
	Get(context.Context) (Snapshot, error)
	Resolve(context.Context) (Snapshot, error)
	Freeze(context.Context) (Snapshot, error)
	Close(context.Context, Snapshot) error
	Discard(context.Context, bool) (Retention, error)
}

// UI preserves input-ordering and retained-owner semantics across adapters.
// Resume revokes a frozen action before transmission, resolves the same request
// after timeout, and never replays old queued input. No close means saved.
type UI interface {
	Retained() Observation
	Frame() Frame
	Changed() <-chan struct{}
	Type(context.Context, string) (int, error)
	Key(context.Context, string) (int, error)
	InsertText(context.Context, string) (Snapshot, error)
	InputReceipt() InputReceipt
	Resize(context.Context, int, int) error
	WaitAction(context.Context) (Action, error)
	Resume(context.Context, Snapshot) (Snapshot, error)
	Resolve(context.Context) error
	Close(context.Context, Snapshot) error
	Discard(context.Context, bool) (Observation, error)
}

var (
	ErrUnsupported  = errors.New("editor: unsupported configuration or text")
	ErrProtocol     = errors.New("editor: invalid or unsupported protocol")
	ErrPending      = errors.New("editor: exact request still pending")
	ErrClosed       = errors.New("editor: session closed")
	ErrRevision     = errors.New("editor: snapshot ownership or revision mismatch")
	ErrFrozen       = errors.New("editor: input frozen")
	ErrNotFrozen    = errors.New("editor: exact frozen handoff required")
	ErrLossConsent  = errors.New("editor: explicit possible-loss consent required")
	ErrBackpressure = errors.New("editor: input not fully accepted")
	ErrRemote       = errors.New("editor: remote operation failed")
)

type adapterError struct {
	message string
	class   error
}

func (e *adapterError) Error() string { return e.message }
func (e *adapterError) Unwrap() error { return e.class }

// AdapterError preserves an existing fixed adapter diagnostic while allowing
// callers to classify it without importing that particular editor protocol.
// message MUST be a fixed application string, never child/user error text.
func AdapterError(message string, class error) error {
	return &adapterError{message: message, class: class}
}
