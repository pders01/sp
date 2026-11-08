// Package privateedit provides editor-independent encrypted lifecycles. The
// explicit OpenInMemory route supports a trusted capability-limited application
// adapter, not a native protection claim. OpenDriver requires OS admission
// before private preparation; with no approved backend it fails closed. The
// separate OpenDocumentExperiment/OpenExperimentWithDriver entry points are for
// disposable synthetic evaluation only, never a protected fallback. They require
// an already-unlocked privacy-enabled manager. No editor-specific imports or
// constructors are present in production; driver wiring belongs to adapters.
// Content and the public recovery sealer are prepared against one validated
// authenticated identity generation; concurrent relock/repinning (including ABA)
// fails closed. Only selected private Content reaches the child, never keys or
// shared/template metadata.
//
// Finish freezes an exact snapshot, uses the existing reviewed policy/revision
// saver bound to the opaque prepared authentication generation, and only then
// closes the child. Relock/re-unlock invalidates saving (even for the same key),
// but not encrypted recovery. Generation is rechecked with the selected encryption
// identity inside publication; it is not a new data key or persisted format.
// Cancellation of an in-flight store call cannot undo publication: calls run synchronously to settlement, never detached.
// A published-but-durability-unconfirmed result advances the baseline truthfully
// but retains the child and requires retry or encrypted recovery before release.
// Close failure does not undo a successful publication or durable recovery.
//
// Cancel requires explicit consent for changed/unconfirmed drafts and seals them
// before close. A public-only recovery sealer is pinned at preparation, unaffected by
// later manager relock or key replacement. Failed recovery retains the child,
// snapshots and any already-published recovery paths, including unconfirmed ones.
// RecoverKnown is a separate explicit-loss escape hatch for faulted/unknown
// sessions: it seals BOTH the mirror and any pending candidate before discard,
// never claiming those copies are necessarily the latest editor text.
//
// Retained contains plaintext. Do not log/serialize it. Errors format only fixed
// stages; errors.Is/As retain underlying causes, which callers must not log raw.
// The controller never unlocks/relocks the manager or silently overwrites a
// conflict. A frozen failure can be retried/cancelled or explicitly resumed with
// the adapter's exact acknowledged action. Conflict reload/merge remains absent.
// Embedded Finish requires the input-ordered finish snapshot; cancel never
// authorizes save. Live observations need not be latest, and queued typing is
// not a full-text candidate. Common interfaces/errors do not establish OS write
// prevention. No native OS backend is approved. Only explicit capability-limited
// in-process application selection is separate from those native gates.
// No plaintext editor/recovery fallback is introduced.
package privateedit
