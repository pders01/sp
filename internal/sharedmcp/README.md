# Experimental shared-only stdio adapter

Separate executable: `cmd/sp-shared-mcp`. Not wired into `sp`, packaging, client
configuration, or automatic installation. No network transport or new dependency.
External-client/specification interoperability is **unverified**: this is a
bounded hand-written subset targeting exactly MCP `2025-06-18`, not a claim of
complete MCP compliance or release readiness.

Example invocation, **using a disposable synthetic store only**:

```sh
sp-shared-mcp --store=/absolute/existing/synthetic-store \
  --read-dates=2026-11-02,2026-11-08 --append-dates=2026-11-08
```

All three options must appear exactly once. Each date list is exact, canonical,
and at most 31 entries; `--read-dates=` or `--append-dates=` disables that operation.
At least one grant is required. Missing stores, symlink stores, relative paths,
unknown flags, repeated flags, invalid dates, and absent policy fail closed. There
is no HOME/config/environment lookup or implicit today/history authority.
Directory ancestors and same-user filesystem replacement remain trusted, as in
the proposal. Existing backend append policy checks may scan outer records outside
the grants; grants are API authority, not filesystem isolation.

## Implemented subset

One UTF-8 JSON-RPC object per newline-terminated frame. Frames, including newline,
are at most 64 KiB; a partial frame at EOF is never dispatched. Batches, duplicate
keys (including escaped aliases), unknown fields, case aliases, nulls, invalid
UTF-8/surrogates, excessive nesting, and unsafe/oversized IDs are rejected. IDs are
strings up to 128 UTF-8 bytes or safe-range integers. No notification bodies,
client names, rejected text, paths or backend diagnostics are logged/reflected.
Only valid request IDs and explicitly approved successful public inputs/content
are reflected. Errors use fixed messages.

Initialization requires `protocolVersion`, `capabilities: {}`, and `clientInfo`
with only `name` and `version` (each nonempty, at most 128 bytes). Unknown client
capabilities and optional protocol extensions, including `_meta`, are deliberately
unsupported. A successful `initialize` response must be followed by
`notifications/initialized` before tools. There is no version downgrade. Ping is
allowed before initialization. The static tool list includes both tools even
when disabled; `tools/list` accepts no cursor. Only the defined initialized and
cancellation notifications have effects; other notifications have no effects
and receive no reply. Unknown request methods receive fixed JSON-RPC errors.

Tool input/output contracts follow `docs/shared-mcp-design.md`: mandatory exact
dates; separate read/append grants; byte-bounded append validation before dispatch;
exact literal bullets and backend deduplication; no private fields; server-owned
`not_encrypted: true` and self-reported provenance. Success returns identical JSON
in `structuredContent` and one text block. Tool errors have one fixed JSON text
block and no success `structuredContent`. The read content cap is 64 KiB UTF-8;
append is at most 100 bullets and 32 KiB total decoded date/labels/bullets. A
conservative escaped dual-output append response is serialized before mutation.
Every complete outgoing frame, including newline, is checked against 1 MiB.

## Process ownership: review before shipping

**`sharedmcp.Main()` owns and exits the entire process; it never returns. Do not
embed this package in a persistent host.** There is intentionally no exported
returning `Serve` API. The private `serveProcess` function may return while a
backend worker is blocked; its only production caller immediately calls
`os.Exit`, without a logger drain, key operation, or other blocking cleanup.
Only the five approved backend operations are used: `ValidateSharedDate`,
`ValidateAppend`, `NewManagerAt`, `ReadShared`, and `AppendDetailed`.

- One storage worker, no queued tool calls. Additional calls receive `BUSY`.
  Input has an unbuffered handoff and at most one staged bounded frame; this is
  not a work queue. Ping/control traffic remains responsive while storage runs.
- The storage operation budget is fixed at 10 seconds. Existing locks/scans have
  **no context cancellation**. Expiry kills the whole process with nonzero exit;
  there is no timeout reply promising that storage stopped or nothing was written.
  An output write may use up to the additional one-second reply grace.
- Matching cancellation stops the session, rather than reopening it for new work.
  A dispatched append receives `APPEND_UNCONFIRMED`/`unknown` if delivery succeeds;
  a read is discarded with a generic error. Reply plus worker drain share a
  one-second grace, also capped by the remaining operation budget. If work cannot
  finish, the process exits nonzero. No subsequent call can overlap that worker.
- Before-dispatch rejection never starts storage. There is no cancellable queued
  call. Cancellation of an unknown/completed ID is inert, not a persistent
  reservation against future calls. A control message racing dispatch cannot be
  proof of no publication.
- EOF/signal stops acceptance and waits at most one second (or the remaining
  budget) for owned storage, then exits. EOF discards any active result, even when
  it completes in grace; do not half-close input expecting a guaranteed final
  reply. Signals and partial/oversized frames exit nonzero. A graceful idle EOF
  exits zero. EOF with a completed worker can also exit zero without acknowledging
  publication; only a delivered tool success is an acknowledgement.
- Broken/blocked stdout ends the process; each reply has a fixed one-second
  budget. A signal during a blocked reply also ends the process immediately.
  Startup diagnostics are fixed, under 256 bytes, on stderr with a bounded write.
- A disconnected/killed/unconfirmed append may already have published. Retry only
  identical approved date/section/bullets. A duplicate-only retry is not evidence
  of the earlier publication's crash durability. No rollback or exactly-once
  ledger is claimed. All backend errors after append dispatch are conservatively
  unknown, even when a particular failure probably preceded publication.

The OS is responsible for terminating goroutines and releasing handles/locks on
process exit. OS/filesystem hangs, forced termination/crash windows, and client
supervision remain limitations; Go cannot make the existing backend cancellable.
Do not distribute race-instrumented binaries as the adapter: the Go race runtime
has its own exit delay. Lifecycle test helpers disable that delay explicitly.

## Synthetic evidence and remaining gates

Executed on macOS/arm64 with cached Go 1.25.13, with module/toolchain downloads
disabled:

- `go test ./internal/sharedmcp ./cmd/sp-shared-mcp`
- `go test -race -count=1 ./internal/sharedmcp ./cmd/sp-shared-mcp`
- `go test -race -count=3 ./internal/sharedmcp ./cmd/sp-shared-mcp`
- `go vet ./internal/sharedmcp ./cmd/sp-shared-mcp`

Tests include a locally built executable against explicit disposable stores;
strict framing/JSON/IDs/schema/init checks; independent grants and indistinguishable
denials; decoded-byte/frame/read/output limits; exact replay/Unicode/literal data;
legacy, enabled/locked, disabled, decrypting, orphan/malformed metadata and records;
private canaries, protected payload/metadata/timestamps preserved; duplicate-only
no rewrite; invalid/case-aliased/symlink records; synthetic post-publication lost
confirmation; busy/ping/cancellation/EOF/signals; actual backend privacy/date locks
held by a different process; deadline process termination; no later worker write;
and executable broken/blocked stdout. The timeout helper uses shorter private
budgets; the actual executable pipe tests use its production reply grace. A
separate executable test holds the backend privacy lock through the full
production ten-second operation deadline and verifies process exit before unlock.
Tests do not read a real store, invoke a live client/provider, or unlock/decrypt
private notes. Fixtures deliberately contain invalid encrypted bodies behind
valid outer envelopes; shared operations still succeed without decryption.

Remaining release gates: parent review/approval of the exact process-exit
architecture; published-spec and external-client interoperability (including
unsupported optional client fields/capabilities); independently approved write
confirmation policy; other native platform and Windows lifecycle/ACL checks; syscall-level
pre/post-rename/fsync
crash injection and forced-kill/durability matrix; independently unlocked-fixture
comparison and broader cooperating-writer/private-save/migration/reversal races;
independent privacy/security audit. The adapter tests do not establish these.
No live-provider/model or previously blocked remote operations were attempted.
