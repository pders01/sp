package editorcore

import "context"

// Options contains public document-selection/UI metadata, not keys or stores.
// Executable/runtime configuration remains specific to each reviewed adapter.
type Options struct {
	DocumentID    string
	Width, Height int
}

// Driver is one editor variant. OpenProtected MUST obtain OS enforcement for
// the exact owned child BEFORE handing any text to it. The platform backend,
// not the adapter/child, establishes that enforcement. Adapters must return a
// retained UI even on partial startup failure; never abandon a live owner.
// No sandbox backend is currently approved; protected opens fail closed.
type Driver interface {
	ID() string
	OpenProtected(context.Context, Options, string) (UI, error)
}

// InMemoryFactory opens trusted capability-limited in-process code. It conveys
// NO OS enforcement and must never launch native editors or be a fallback from
// OpenProtected. Application opt-in selects the concrete bundled implementation.
type InMemoryFactory func(context.Context, Options, string) (UI, error)

// ExperimentFactory is deliberately separate from Driver. It is only for
// existing disposable synthetic evaluations; it conveys NO protection claim.
type ExperimentFactory func(context.Context, Options, string) (UI, error)
