# Private editor boundary: historical native research

**Archived, not an implementation plan.** The superseded native Neovim
prototypes, demos and native-only test fixtures were removed during wrap-up.
Source paths, commands and test selectors below describe that historical work;
they are not available entry points. Retained evidence is not native sandbox
approval. The current editor is the explicitly opted-in
[embedded Vim integration](embedded-vim-integration.md); common lifecycle code and
fail-closed native admission remain in place. No native backend is implemented.

For the next decision and limited implementation scope, use the
[bounded integration plan](nvim-integration-plan.md). This document retains the
technical evidence and broader backlog. The user has selected the stronger
OS-enforced write boundary and requested reusable platform/editor abstractions;
the previously proposed trusted-editor fallback is not approved.

**Current architecture:** `editorcore` defines common contracts;
`privateedit`/`editorterminal` own generic encrypted and terminal lifecycles;
`nvimui.Driver` is the Neovim variant. `editorsandbox` currently implements only
the required no-persistence policy and unavailable/fail-closed admission gate.
**No OS enforcement backend is implemented or approved yet.** Protected opens
cannot launch or hand off private data; explicit unconfined synthetic experiments
remain separate, never a fallback. A memory-only test variant exercises the
shared lifecycle without Neovim. Earlier native evidence predates this extraction.

**Status: headless, embedded UI/lifecycle and synthetic terminal prototypes exist; normal editing is unchanged.**
`internal/nvimbridge` contains owned-pipe framing, exact snapshots/revisions,
retention/freeze/close handling and a public Lua bootstrap. It has no CLI/TUI
routing, interactive UI, private-store/key/saver access or plaintext fallback.
The initial startup failure is resolved for the tested **Neovim 0.11.5 on macOS
arm64**, using only synthetic text and disposable state. Its hello now acknowledges
that version; exact-text/revision set/get, freeze, close and child reaping pass.
Fixtures include CRLF/mixed endings, trailing empty lines, Unicode, controls and
64 KiB text. Invalid UTF-8, NUL and oversize input are rejected without changing
the acknowledged draft. Five native negative cases reject additional/unexpected
startup listeners, an existing socket RPC channel, and listeners introduced before
init or a subsequent get. These bridge-only tests are not interactive editor,
live-application save/recovery routing, other-version/platform validation or a
security audit. Do not enable it for real private drafts.

Startup errors now carry fixed bootstrap phases, parent-side launch/hello/init
stage and OS exit status. Raw stderr, paths, session/document identifiers and
text are not formatted into them. The stderr parser accepts exact allowlisted
markers only, caps consumption at 4096 bytes and line length at 64 bytes, and
preserves the underlying error through `errors.Is`/`errors.As`. Phase markers are
untrusted diagnostic hints, not an isolation proof. Parent-requested shutdown is
recorded separately and must not be confused with the child's original failure.

The original diagnostic run identified `stage=hello`, `phase=guard-listener`,
`exit=1`, before any draft was sent. [Neovim's API documentation](https://neovim.io/doc/user/api/)
and [0.11.5 server initialization](https://raw.githubusercontent.com/neovim/neovim/v0.11.5/src/nvim/msgpack_rpc/server.c)
confirm the automatic listener; an empty `--listen` argument does not disable it.
The parent now chooses one fixed public startup socket inside the fresh child's
0700 directory using `--listen`. Paths over 103 bytes fail before launch, avoiding
Unix socket truncation/aliasing; there is no alternate path or bind-error fallback.
The bootstrap closes **only that exact child-owned endpoint**, then requires zero
listeners and zero RPC/socket channels before hello. It rechecks those guards on
every request and snapshot. Unexpected endpoints/channels fail closed. Existing
editor processes are never attached to or stopped. Neovim logging is explicitly
routed to the null device, not an inherited or persistent log path.

**A temporary RPC socket exists before bootstrap hardening.** No draft is sent
until after a valid hello from the hardened child. This is not a promise of zero
listeners from process entry, protection against malicious same-user code or an
OS-enforced sandbox. Disabling the listener before the private handoff does not
prove the absence of arbitrary child-originated writes or other channels.

The prototype requires an explicitly trusted executable and disposable state
parent, isolates startup configuration and sends drafts only over owned pipes.
Its literal LF-separated line representation treats CR as text and retains final
empty lines; NUL, malformed UTF-8 and oversize inputs fail explicitly without
normalization. Timeouts retain the acknowledged mirror and any pending Set
candidate; callers must resolve or explicitly acknowledge possible loss before
cleanup. Freeze/close is a byte handoff, never a save receipt. See the
historical prototype descriptions below for limits. Environment/options
are defense in depth, not proof against child-originated writes or OS paging.

The unresolved decision still asks for Neovim without silently introducing
plaintext files. Better built-in navigation/undo does not resolve that request:
the user rejected treating the reduced built-in editor as a workflow replacement.
The original proposal below does not change [the privacy contract](privacy.md);
editor selection, interactive/save/recovery integration and the precise file
guarantee remain approval gates.

## Headless lifecycle experiment

`internal/privateedit` now coordinates the bridge with SP's existing encrypted
publication/recovery implementation, without CLI/TUI registration or defaults.
Its [package contract](../internal/privateedit/doc.go) is deliberately narrower
than the full interactive requirements below:

- Preparation captures selected private content and an immutable public recovery
  sealer from one authenticated identity generation. Policy-changing writers are
  excluded during preparation; concurrent relock/repinning is rejected, including
  A-to-B-to-A identity changes. No key or shared/template metadata enters Neovim.
- Finish acknowledges an exact freeze, checks original bytes and reviewed enabled
  policy through the canonical saver, then closes. An opaque prepared-generation
  capability additionally checks authentication generation under the publication
  lock and against the exact identity selected for encryption. Relock/re-unlock
  invalidates saving, even for the same key; the original public recovery pin
  still works. The in-memory nonce is not a data-key/format change. Conflicts never
  overwrite, including an empty baseline on a missing date after key substitution.
  Publication with unconfirmed durability advances the baseline truthfully but
  retains the frozen child for an explicit retry or encrypted cancellation.
- Changed/unconfirmed cancellation needs consent and a durable encrypted recovery
  receipt before close. The session's public recipient/directory pin survives
  manager relock or later repinning. Already-published recovery paths with errors
  remain reported; a failed close never reverses a save or durable recovery.
- Explicit embedded Resume revokes the previous frozen action before RPC, drains
  typeahead while still frozen, and preserves in-memory undo. Only a verified
  acknowledgement enables another action generation. No-edit resume still needs
  a fresh action revision; timeouts retain the original request for Resolve.
  Publication, advanced baselines and durability uncertainty survive resume.
- Store calls remain synchronous/owned until settled: cancellation cannot detach
  an ongoing publication or promise rollback. IPC deadlines still require settling
  the original request. A distinct loss-consented fault escape seals both mirror
  and pending candidate before discard, explicitly reporting possible missing edits.
- Errors format fixed stages, not draft text or raw backend errors. Retained
  snapshots contain plaintext and must not be logged. Frozen failures support
  retry/cancel or explicit embedded resume, never implicit conflict overwrite/reload.

Synthetic real-Neovim 0.11.5/macOS arm64 tests cover encrypted finish preserving a
shared append after open, cancellation after manager lock, private conflict, and
policy disable while editing, and key substitution on a missing date. Unit/race
models cover publication/recovery/close
failures, unconfirmed durability, late freeze acknowledgement, partial known-copy
recovery and cancellation during an owned publication. Preparation interleaving
tests reject foreign-generation content even after returning to the original key;
save tests cover substitution after loader preflight, same-key reauthentication,
existing/missing dates and validation at encryption, not only a preflight check.
These tests use disposable stores and generated secrets; they are not interactive
UX, native hard-reap/boundary-length evidence, comprehensive write observation or
an independent audit. Interactive transport/input, resume/conflict review, real
application save/cancel/relock/signal routing and precise child-write guarantees
still need separate design/approval. The synthetic-only terminal experiment below
is not normal application routing. No plaintext editor/recovery fallback exists.

## Embedded UI/input experiment

`internal/nvimui` evaluates an embedded UI over the fresh child's parent-owned
stdio RPC channel. It does not attach to a user/editor server or change SP's
normal editor. The separate headless bridge's no-RPC guard remains unchanged.
The UI bootstrap closes only its exact owned startup socket before draft init,
then permits the stdio RPC channel only. It requires the 0.11 API; native evidence
is limited to **Neovim 0.11.5 on macOS arm64** with synthetic stores/text.

- API-fast `nvim_input` keeps operator/command-line input usable; a key receipt
  means bytes queued, not processed or saved. Literal typing escapes `<` notation;
  named keys are allowlisted. Literal insertion at the buffer cursor preserves
  CRLF/final-empty-line bytes without native paste normalization. It is not full
  clipboard/command-line paste parity. Undo is in-memory, not persistent undo.
- F2/F3 mappings freeze inside Neovim's input sequence and block trailing queued
  keys before publishing the exact frozen finish/cancel snapshot. A private
  action broadcast cannot be consumed by coalesced render events. A UI flush,
  input acknowledgement or process exit is never a save receipt.
- `privateedit.OpenExperimentWithDriver` joins that action to the prepared-generation
  saver and pinned encrypted recovery. It exposes no raw child close/discard
  handle. F3 cannot authorize saving; failed F2 publication remains owned and can
  be explicitly encrypted/cancelled by SP-side handling. Live observations are
  not necessarily latest, and queued keys are not a full-text Set candidate.
- MessagePack is bounded by 8 MiB, depth 24, 262144 nodes and 32768 items/container.
  Only tiny typed positive Neovim API handles are accepted as opaque extension
  metadata, not grid coordinates or request IDs. Raw remote errors are hidden.
  Both the request writer AND matching reply must settle before releasing pending
  ownership; early replies cannot accumulate blocked writers or permit retries.
- The single-grid model publishes only flushed defensive snapshots, validates
  bounds/control text and renders monochrome fixed-CUP/no-wrap ANSI. Geometry is
  bounded to 240×100. It handles the upstream 0.11 zero-repeat trailing-space
  clearing marker and ignores only allowlisted non-rendered metadata. Highlights,
  selection rendering and terminal/Unicode-width equivalence remain incomplete.

Four native synthetic UI scenarios cover rendering, typing/undo/redo, exact
literal insertion, command-line/operator cancellation, resizing and queued keys
following finish. Four joined encrypted-lifecycle scenarios cover finish with a
later shared append, cancellation after relock, private conflict and key
substitution on a missing date. These are library/grid tests, **not a real-TTY
walkthrough, terminal restoration/signal proof or an independent security audit**.
Tests are opt-in through `SP_NVIMUI_TEST_EXECUTABLE`, separate from the headless
`SP_NVIMBRIDGE_TEST_EXECUTABLE`. No actual user configuration or notes are used.

**OS-enforced child-write prevention is required and remains unimplemented.** Options and per-input/
snapshot guards do not prevent every deliberate Ex/Lua file write, shell action,
new listener, plugin loading or same-user attack. This is not a security sandbox
or permission to use real private notes. Complete input/backpressure/busy UX,
conflict/reload handling, approved command restrictions and final routing remain
separate work. No default or plaintext fallback is enabled by this candidate.

## Synthetic terminal experiment

`internal/editorterminal` joins the terminal decoder, generic editor UI and
prepared encrypted lifecycle. `internal/nvimterminal` is now only the unconfined
Neovim synthetic-demo compatibility wrapper; protected admission never uses it. The separate `cmd/sp-nvim-demo` requires
`--synthetic` and an explicitly trusted absolute `--nvim` executable. It creates
only a fresh disposable encrypted store, generated in-memory secret and isolated
configuration, using the constant date `2000-01-01`. It accepts no real store or
user configuration. Normal `sp` editing/routing remains unchanged.

**Historical native evidence (before shared-contract extraction):** the user reports all **14 synthetic PTY cases passed** in
an outside-pi run, alongside parent unit/race, vet/lint and cross-compilation
checks. This is user-supplied acceptance evidence, not an independent rerun or
audit. The JSON does not identify OS/build or executable version, so it does not
extend the verified platform/version matrix. Initial fixture failures were
ambiguous. Improved
content-free diagnostics in a subsequent user-run outside pi confirmed the
expected SP rejection (exit 2, fixed rejection message, no alternate screen),
followed by `termios` errno 25 (`ENOTTY`) during the post-exit mode assertion.
The fixture had already ended its controlling session, revoking its PTY.

The corrected fixture keeps a session controller alive after its exact Go child
exits, until terminal and artifact assertions finish. A bounded pipe protocol
reports the owned child PID/status; signals target that child, not its controller.
Only a release token after assertions ends the controller/session. Foreground
handoff occurs only inside this newly created fixture session; application
admission is unchanged. Controller/channel loss retains an unknown owner instead
of inferring child exit. Permission errors have no retry/alternate launch.
Seventeen mock-only tests pass, including restoration-before-controller-release,
child-status ownership and denial paths. After the individual background check
passed, the user's full run reported passes for all 14 cases, keys discarded,
no normal notebook updated and no transcript saved. The suite includes strict
terminal-state/alternate-screen assertions, synthetic artifact checks and both
background/detached no-store-creation rejection checks. Native terminal operations
were not retried inside pi. The earlier nine-case evidence remains historical
for the previous candidate.

- Input/output must name the same supported terminal character device; Linux
  masters are rejected rather than equated by their shared ptmx device number.
  Admission additionally requires the supplied terminal's foreground process
  group to match the caller; detached/background sessions are rejected before
  demo secret/store creation. The application never calls `tcsetpgrp` or grabs
  an existing terminal. Caller descriptors must remain stable. This is an initial
  ownership check, not ongoing exclusive control or a foreground-loss guarantee.
  Terminal state is captured before Tea and restored on return. There is no
  alternate terminal path or Windows fallback.
- Operations are serialized even across asynchronous faults. Scheduled input
  remains owned if its command never starts; an owned call may settle after the
  UI stops. Exact settled byte receipts distinguish zero, partial and full input
  acceptance without logging input. No receipt authorizes publication.
- A bounded FIFO retains one overflow event in order and pauses further intake.
  Literal paste cannot confirm save/recovery/relock. F3 asks physical consent for
  encrypted recovery; declining explicitly resumes without replaying old keys.
  Tiny screens hide the draft and pause ordinary confirmation; shutdown can still
  request an ordered cancel, disarming a queued finish rather than publishing it.
- Ctrl+L/C/Z and graceful SIGINT/SIGTERM/SIGTSTP/SIGHUP request encrypted
  cancellation and relock only after safe closure. Ctrl+Z/SIGTSTP **close after
  recovery instead of actually suspending/resuming**. Demo signal ownership
  begins before secret generation; cancelled setup cannot launch an editor.
  The embedded child requests its own process group, without a terminal/new
  session, so a parent-group stop does not prevent the freeze. Only the exact
  owned child is killed; this is not descendant containment or an OS sandbox.
  These latest signal/group paths still need native acceptance.
  An already-started store call must settle. Recovery
  failures park for explicit retry, not resize/paste/signal-triggered retries.
  Conflicts preserve newer stored bytes, earlier publication facts and the draft.
  Known-copy recovery separately warns that latest edits may be missing; a
  failed attempt needs renewed physical consent. Paste/resize cannot steal that
  confirmation. Dedicated confirmation screens show the complete loss warning
  and physical choices even at the minimum supported 40×10 size, without a draft
  preview. Recovery durability warnings are distinguished from note saves.
- Returned reports may retain PRIVATE snapshots and unsubmitted input. Unresolved
  owners are not destroyed or silently deleted by the demo; usable authentication
  is retained unless safe relock succeeded. Such failure handling is not a complete
  interactive recovery console and must not be mistaken for release readiness.

An earlier candidate passed nine native synthetic PTY cases on **macOS arm64 /
Neovim 0.11.5**, before the latest foreground/signal/process-group changes. Seven
exercised the separate demo: F2 save, F3 encrypted recovery, cancel/decline/resume/
edit/save, pasted authorization rejection, tiny resize/recovery, SIGTERM and
Ctrl+L relock. Two use a race-instrumented package-test binary and private factory
seam, with no fault flags or hooks exposed by the demo or normal CLI:

- Actual encrypted competing publication and shared append remain untouched
  through failed save, explicit retry, resume/edit, another conflict and durable
  encrypted cancellation. Recovery is decrypted only inside the owned fixture to
  verify exact frozen bytes and the unchanged newer private/shared content.
- A **scripted pre-recovery error**, not an observed filesystem failure, parks
  the live owner and unlocked key through paste, resize, SIGTERM and SIGINT.
  Only a physical retry performs real encrypted recovery, close and relock.
  Test-only progress counters fence fixture operations; they are not save receipts.

Checks cover leaving
alternate screen, restoring all terminal flags/speeds/control characters except
BSD's volatile `PENDIN` line-discipline state, and absence of fixture canaries in
the owned artifact roots. No transcript is saved. This is not comprehensive
filesystem write tracing, a human real-TTY walkthrough, Linux native validation,
or an independent audit. Linux/Windows compilation is not native runtime evidence.
Go regression models cover unstarted/late input ownership, async-fault busy
admission, overflow order, named-key receipts, tiny shutdown, explicit recovery
retry, renewed known-copy confirmation and truthful publication status. Uncertain
publication/recovery warning cases are model-tested, not native filesystem fault
observations. Native embedded tests additionally retain
an advanced uncertain baseline and a conflict across explicit/no-edit resume.

The updated harness defines 14 cases, adding foreground/background/detached
session admission, Ctrl+Z recovery, parent-group SIGTSTP, and injected SIGHUP
(not an actual PTY hangup). Its launcher claims only a fresh owned PTY once;
permission failure has no fallback. The corrected suite's complete 14-case pass
is user-reported; broader runtime/failure-path and child-write guarantees remain
unestablished.

Run these native checks **manually outside pi**, using a trusted executable;
do not retry or substitute another launcher inside pi:

```sh
python3 scripts/nvim-terminal/main.py --executable /absolute/trusted/nvim --case background-reject
python3 scripts/nvim-terminal/main.py --executable /absolute/trusted/nvim
# Interactive demonstration: synthetic state only, not a real note editor.
go run ./cmd/sp-nvim-demo --synthetic --nvim /absolute/trusted/nvim
```

**Before real-note integration, implement and validate the selected stronger
boundary:** editor-originated plaintext persistence must be prevented by an
OS-level child write/process/channel policy. The weaker trusted-editor option
was not approved. Command mappings, `nofile`, options and Lua guards cannot
establish this promise. Existing host sandbox boundaries must never be weakened
or worked around. No native backend is implemented or approved yet.
Native foreground/signal acceptance, ongoing ownership/job-control and actual
suspend/resume handling, output backpressure, complete paste/
selection/highlight UX, conflict reload/merge, additional native platforms/builds,
failure-path acceptance and final routing still gate a real editor.

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
The headless prototype validates only the owned-pipe snapshot path on the build
named above. Terminal-or-embedded-UI choices and additional Neovim builds remain
unvalidated; the prototype is not an interactive RPC/UI integration.

## Required session lifecycle

These are full frontend integration requirements, not a claim that the headless
lifecycle experiment implements an interactive workflow. Reuse current [policy-aware saving](../cmd/sp/recovery_policy.go),
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

## Synthetic validation plan (prototype evidence and remaining gates)

Use only synthetic canaries/secrets and disposable stores, startup configuration,
state/cache/temp directories. Never read real notes, HOME files, editor config or
transcripts. No providers/live models. The initial design review launched no
editor; subsequent authorized diagnostic and startup-fix tests are described
above. Native tests remain opt-in through `SP_NVIMBRIDGE_TEST_EXECUTABLE`, with
one child per scenario and no discovery, user configuration or fallback launch.

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

**Initial proposal evidence (before the prototype):** read-only review of the
local unresolved TODO, linked source and privacy documentation. At that point
only this proposal was added, with no application/test changes, editor launch,
configuration inspection or runtime tests. The later headless prototype is
separately described above and now has scoped native headless roundtrip evidence.
Interactive integration, independent review and additional native platforms/builds
remain outstanding.
