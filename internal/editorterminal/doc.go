// Package editorterminal owns editor-independent terminal/input and encrypted
// session coordination. Run is protected admission: unavailable OS enforcement
// fails before terminal/manager/driver access. RunExperiment is explicitly
// synthetic and never a protected fallback. RunInMemory is an explicitly opted-in
// trusted in-process adapter, not OS protection or shipping approval. No native
// sandbox backend is yet approved; normal SP defaults remain unchanged.
//
// Run requires explicit terminal descriptors for the same supported character
// device, rejects Linux PTY masters, and requires its process group to own the
// controlling terminal's foreground at admission. It never grabs foreground or
// opens /dev/tty as a fallback. It captures/restores terminal state and uses
// SP's decoder, without plaintext/configuration/Windows-input fallbacks. Callers
// must keep the supplied descriptors stable. Admission is not ongoing exclusive
// terminal ownership: foreground-loss races, blocked output and job control
// remain unresolved. Prior native terminal acceptance is user-reported (14
// synthetic cases before this extraction), not a fresh independent rerun,
// platform expansion or audit.
//
// Ctrl+Z and catchable SIGTSTP/SIGHUP request encrypted cancellation/relock, NOT
// actual job suspension or later resume. An owned backend operation must settle
// first; failure retains the owner/key. SIGSTOP/SIGKILL and host failure cannot
// be made safe by signal handlers. No complete suspend/resume guarantee exists.
//
// Only input-ordered F2/F3 snapshots authorize normal lifecycle handoff. Queued
// byte receipts are not processed-input or save fences. Physical confirmation
// is separate from literal pasted text. One bounded overflow event stays at the
// FIFO tail; further intake is visibly rejected while paused. This is not full
// input, paste, highlight, selection or terminal-width parity.
//
// Scheduled input remains owned even if its Tea command never starts. Active
// backend calls settle synchronously; deadlines/signals cannot detach a save or
// promise rollback. Failed recovery parks for explicit retry, retaining its
// publication/durability facts and usable owner. Relock follows safe closure,
// never an unresolved input or recovery failure. Report retains PRIVATE editor
// and unsubmitted-input state; never format or log it. Known-copy recovery needs
// separate explicit possible-loss consent, renewed after a failed attempt;
// incidental input/resize cannot leave that confirmation. Recovery publication
// uncertainty is distinct from note-save uncertainty, not a note-save receipt.
// Known-copy discard is never automatic failure cleanup.
//
// ResumeInMemory retains the same manager/date/owner and unsettled input after
// a terminal program failure. Reentry never grants loss consent or reloads a
// second editor; only fresh physical confirmation can release pending input
// after durable recovery/close. Failed terminal admission returns the old owner.
//
// Editor-specific compatibility wiring belongs outside this package. Required
// OS child write/process/channel enforcement remains unimplemented. Interfaces,
// options, process groups and null logging are not protection receipts. No trusted-editor fallback is allowed.
package editorterminal
