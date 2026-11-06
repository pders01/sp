# Private editor boundary: proposal for decision

**Status: discussion only; no integration selected or implemented.** The unresolved
local TODO asks for Neovim without silently introducing plaintext files. Better
built-in navigation/undo does not resolve that request: the user uses Neovim and
has rejected treating the reduced built-in editor as a workflow replacement.
This proposal does not change [the privacy contract](privacy.md).

## Two distinct promises

1. **Encrypted SP storage:** while privacy is active, private daily payloads and
   automatic private recovery are encrypted before publication. Shared writing
   remains plaintext. Migration remnants, old backups and intentional exports
   have the existing [limits](privacy.md#outside-the-boundary).
2. **No SP-created plaintext editor files:** SP does not put decrypted private
   drafts in temporary editor buffers, plaintext recovery, or persisted undo.
   This is narrower than “no plaintext ever reaches disk.” It does not cover OS
   paging, hibernation/core dumps, terminal recordings or hostile same-user code.

Promise 1 alone does **not** imply promise 2. Giving Neovim a plaintext temporary
file could retain encrypted daily storage while violating promise 2. A file on
a RAM-backed filesystem is still a plaintext editor file and may be paged or
captured; it is not a transparent way to preserve that promise.

Today [private CLI hooks](../cmd/sp/private_edit.go) select the
[in-memory writer](../internal/tui/privacy.go), not the
[legacy external editor](../internal/editor/editor.go). The latter explicitly
creates `sp-*.md`, waits for successful editor process exit, then reads the file;
it retains failed-save buffers. Reusing it for private notes would change the
boundary, not merely improve editor compatibility.

## Threat model and evidence limits

Keep the existing encrypted-at-rest boundary: protect private stored payloads
against someone possessing store files without the unlock secret, not an
unrestricted malicious process running as the user or a compromised OS. Private
text necessarily exists in memory and on the display during editing. Neovim
would be an additional plaintext-holding process and trusted executable; SP must
not give it the passphrase, identity or shared document. Template execution,
clipboard/export, plugins and network integrations remain separate exposure
paths, not encryption features.

An isolated launch means a dedicated child with an explicit minimal environment,
disposable state locations, no inherited user startup configuration/plugins,
no session restore and no attachment to an existing editor server. It does not
mean a security sandbox unless OS-enforced restrictions actually exist and are
verified. Disabling swap, undo, backup and history options is useful defense in
depth, **not proof of no disk writes**: editor/runtime code, libraries, plugins or
explicit commands can persist or transmit content. Whether the promise covers
SP's own writes only, or also all child-editor writes SP enables, needs approval.

Scans and syscall observation can find counterexamples within their scope; they
cannot prove that no transient copy ever existed or that arbitrary Neovim
versions/configurations are safe. No secure deletion, complete memory erasure,
independent security audit or cross-platform equivalence is claimed.

## Options, not a selection

| Option | Boundary and benefit | Costs / unresolved limits |
| --- | --- | --- |
| Current built-in writer | Existing no-SP-plaintext-editor-file path; encrypted recovery, lossless text and in-memory undo already exist. | Does not satisfy the requested Neovim workflow; retaining it is not user acceptance of mandatory built-in editing. OS/memory/display exclusions still apply. |
| Dedicated in-memory Neovim + private RPC | Proposed unnamed, non-file-backed buffer; SP sends/receives text through owned local pipes. No SP plaintext buffer file is required. Neovim editing without giving it SP keys. | New RPC/UI/lifecycle and lossless snapshot work; no normal user configuration by default. Editor-originated persistence needs separate restriction/validation. Normal plugin parity cannot be promised. |
| Explicit opt-in temporary-file workflow | Could use a familiar foreground editor/configuration while keeping final SP daily/recovery publication encrypted. | Deliberately abandons promise 2 for this session; copies, backups and crash remnants can survive cleanup. **Not authorized by this proposal**; no automatic fallback or implicit opt-in through `$EDITOR`. |

For an RPC prototype, use only parent-owned pipes, not a discoverable/reusable
RPC server or remote editor attachment. Do not place content in arguments,
environment, filenames, diagnostics or protocol trace logs. Allowlist protocol
operations; do not evaluate note text as commands. A minimal supplied integration
must map explicit SP save/cancel actions: a Neovim buffer write/exit is not by
itself proof of SP publication. Unsupported launch, protocol or platform support
must fail closed with the draft retained, never fall through to a file workflow.
Actual Neovim API/version/terminal-or-embedded-UI choices need a scoped prototype;
none have been validated here.

## Required session lifecycle

These are requirements for any future integration, not claims about a current
RPC implementation. Reuse current [policy-aware saving](../cmd/sp/recovery_policy.go),
[publication locks/revisions](../internal/scratchpad/scratchpad.go),
[policy checks](../internal/scratchpad/save_policy.go) and
[encrypted recovery](../internal/scratchpad/privacy.go), rather than a parallel
persistence path.

1. **Prepare:** select the approved mode before decrypting or creating editor
   resources. Unlock in SP, load only the selected date's private Markdown,
   capture its original text/save policy, and pin the recovery recipient. Send
   neither combined private/shared rendering nor private template metadata to
   the editor. Cancelled unlock/startup never publishes a note. Keep the initial
   text in SP until successful handoff; no editor resolution may trigger a file
   fallback.
2. **Edit:** bind one session generation to one date and child. Bound message/text
   sizes; reject malformed, oversized or unsupported text without truncation or
   normalization. The current writer preserves CRLF, tabs, Unicode, trailing
   lines and final-newline distinctions. Neovim line arrays alone are not a
   demonstrated lossless codec: validate end-of-line/empty-buffer behavior and
   retain the original bytes on rejection. Snapshot revisions must be monotonic;
   keep SP's last acknowledged draft in memory, not an on-disk RPC log.
3. **Save:** freeze edits and obtain an acknowledged exact snapshot/version;
   serialize saves, cancellation and recovery. Check the reviewed encrypted
   policy under the publication lock, including absent dates. Re-read current
   private content against the original baseline, then use revision-checked
   publication. Preserve current shared contributions and non-edited metadata;
   no long-lived note/store lock while the user edits. Only acknowledge “saved”
   after the matching snapshot's successful publication. If continued editing
   is supported, resume with a new baseline; later edits remain unsaved. Ignore
   late/duplicate replies from old generations.
4. **Conflicts:** never blindly retry against a stale baseline. Offer an explicit
   private comparison: cancel review, reload, or select draft replacement. Reload
   first seals the exact draft in encrypted recovery; failed recovery keeps it
   open. Replacement needs explicit confirmation and a later save, which checks
   for another change. Do not merge automatically, overwrite shared writing or
   change private/shared ownership. Current behavior is described in
   [edit failures and recovery](privacy.md#edit-failures-recovery-and-backups).
5. **Failure outcomes:** distinguish unchanged, saved, cancelled, not saved and
   published-but-durability-unconfirmed. Pre-publication failure keeps the old
   baseline. A directory-sync failure after replacement advances the baseline
   to the published snapshot but retains an unconfirmed/recovery requirement;
   it is not rollback. New edits after that snapshot are unsaved. A concurrent
   privacy disable must block the active encrypted save, not publish plaintext;
   reopening/reviewing policy is required, not automatic downgrade.
6. **Cancel / relock / graceful signal:** freeze and capture the latest draft;
   cancellation is not save. Changed or durability-unconfirmed text requires
   confirmation where interactive, then encrypted recovery before releasing it.
   Relock clears decrypted projections only after successful recovery. Failed
   recovery (including unconfirmed durability) leaves the draft and usable
   session available for retry; report any published recovery path plus error.
   A save already publishing must settle before cancellation is reported: no
   promise that cancellation reverses an in-flight publication. Wait for owned
   work, close IPC, reap the owned child, clear references and release keys only
   after outcomes are known. Repeated graceful signals must not bypass recovery.
7. **Crash / disconnect:** distinguish the last SP-acknowledged mirror from the
   latest editor text. Recover the mirror if possible, report its version and
   possible missing edits, and never claim it is the latest snapshot without an
   acknowledgement. Retain the child if still usable while recovery is retried.
   SIGKILL, child failure before synchronization, lost terminal or host failure
   cannot guarantee recovery. On restart require unlock and explicit review of
   encrypted recovery against the current note; retain the recovery artifact.
   No plaintext emergency export/fallback.

The asynchronous mirror protocol, freeze/ack handshake, maximum unsynchronized
edit window and acceptable crash-loss behavior need explicit design approval.
Rejecting late replies must not discard a still-reachable newer draft silently.

### Additional requirements if file-mode consent is later approved

Consent must name plaintext persistence, scope (per session versus remembered),
editor/configuration trust and cleanup limitations **before** any private bytes
are written. Do not silently reinterpret privacy enablement as that consent.
Use an exclusively created owner-only file inside a private disposable directory;
validate native ACLs where needed and fail closed if protections cannot be
established. Exclude note text from names. Require foreground completion, then
validate/read the completed buffer safely (including replacement/symlink races)
before ordinary policy/revision checks. A first modification is not completion.

Specify cancellation and failed-save cleanup separately from the legacy rule of
retaining plaintext buffers: seal encrypted recovery before deleting the primary
buffer, report cleanup failures/residual paths, and never call deletion secure
erasure. Editor-created backups and abrupt-exit leftovers cannot be assumed
removed. If encryption/recovery fails, preserve a usable session and report the
plaintext artifact rather than destroying the only draft. Whether any retained
plaintext recovery is acceptable requires separate consent; existing private
recovery guarantees remain unchanged until an approved contract is implemented.

## Synthetic validation plan (future implementation)

Use only synthetic canaries/secrets and disposable stores, startup configuration,
state/cache/temp directories. Never read real notes, HOME files, editor config or
transcripts. No providers/live models. No editor launch is part of this review.

- Extend [privacy test assertions](privacy-testing.md#deterministic-checks)
  and existing private input/conflict/shutdown tests. Run `go test ./...` and
  relevant race tests, then `make privacy-terminal` / `make ux-terminal` in an
  authorized disposable harness. Existing results are not RPC evidence.
- Unit-test the protocol with a fake child: startup cancellation, malformed and
  oversized frames, duplicate/out-of-order replies, mismatched sessions/dates,
  disconnect during snapshot/save/recovery, recovery failure, repeated signals,
  late unlock, child exit and parent cleanup. Inspect only synthetic output for
  secret leakage; do not retain private-content transcripts.
- Round-trip exact byte fixtures: empty text, lone/final/multiple newlines, CRLF,
  mixed line endings, tabs, combining characters, emoji, literal controls and
  long/near-limit notes. Unsupported data must fail explicitly and leave draft
  and stored bytes intact. Verify no stored-text normalization.
- Deterministically interleave shared append, private replacement/deletion,
  policy disable/re-enable and a second reviewed replacement. Inject failures
  before publication, after replacement/directory sync and during recovery.
  Assert shared preservation, conflict gating, baseline advancement, retained
  encrypted recovery, and no plaintext-policy downgrade, including new dates.
- Only after approval, test a specified Neovim build with synthetic minimal
  configuration. Check explicit save/cancel mappings and final shutdown, observe
  authorized filesystem/network activity including descendants, and scan only
  disposable artifacts for canaries. A deliberate synthetic startup/plugin
  writer should demonstrate that disabling a few options is insufficient.
  Specify the observation scope and any OS enforcement separately; stop on any
  sandbox denial without retry or workaround.
- If file mode is approved, separately test restrictive creation, substitution
  races, editor errors, cancellation, cleanup failures and crash remnants.
  Document expected plaintext exposure, not a false “no canary found” guarantee.
  Native ACL, terminal and durability validation are per OS/filesystem; Windows
  interactive support stays fail-closed pending a safe adapter and native tests.

## Decisions requiring user approval

- Which guarantee is required, including whether child-editor writes are in scope?
- Is isolated Neovim without the normal plugin/configuration environment useful,
  and which narrowly reviewed customizations or enforced restrictions are needed?
- Which interaction model (embedded UI or terminal child), explicit save/finish
  semantics, synchronization/loss bounds and failure UX are acceptable?
- Is any plaintext temporary-file workflow acceptable at all? If yes, approve its
  separate consent, residual-file/recovery policy and precise guarantee wording.
- Should the built-in writer remain available, and which mode should be default?
  No default or mandatory-editor choice is inferred here.

**Review evidence:** read-only review of the local unresolved TODO, linked source
and privacy documentation. Only this proposal was added; no application/test
changes, editor launch, configuration inspection or runtime tests were performed.
A prototype, independent review and native-platform validation remain outstanding.
