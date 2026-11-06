# Shared-only MCP adapter proposal

**Status: design only; not implemented, enabled, or approved for release.**
MCP access remains deferred. This document does not establish a shipped security
boundary, install client settings/hooks, or change the CLI/storage contract.

## Existing boundary and intended scope

Sources inspected for this proposal:

- [CLI examples and current feature status](../README.md#agent-automation).
- [Shared operations, provenance, storage and durability](privacy.md#daily-operation).
- [Reviewed public hooks and weekly collection](agent-hooks.md).
- `cmd/sp/privacy.go`: opt-in `shared --json` and `append --json`.
- `internal/scratchpad/shared.go`: `ValidateSharedDate`, `ValidateAppend`,
  `ReadShared`, `AppendDetailed`, section parsing and exact deduplication.
- `internal/scratchpad/scratchpad.go`, `privacy.go`, `storage.go` and
  `lock_unix.go`: explicit store construction, policy checks, bounded records,
  publication and blocking locks. Existing outcome tests were read, not run.

Expose exactly two tools: `sp_shared_read` and `sp_shared_append`. A weekly
collector makes separate, explicitly authorized date reads; there is no archive
search, date discovery, bulk export, subscription, or automatic summarization.
All returned/appended text is **Not encrypted**. Shared material must already
be approved for public disclosure; this adapter is not a secrets detector.

Recommend a separate, opt-in Go entrypoint in the same module, using the existing
shared APIs directly, not shelling out to `sp`, parsing human CLI output, or
implementing a raw daily-JSON writer. Its manager is always locked and constructed
only with `NewManagerAt` for the configured store. This avoids the ordinary CLI's
implicit home-directory store. The proposed entrypoint and startup options do not
exist today. No runtime or dependency changes are part of this document.

## Transport and compatibility assumptions

- Initial transport: local **stdio only**, one operator-launched process per
  client connection, JSON-RPC 2.0, one UTF-8 JSON message per line. No TCP listener,
  HTTP/Streamable HTTP, SSE, sockets, remote service, or automatic daemon startup.
- Propose MCP protocol revision **`2025-06-18`** as the sole initial compatibility
  target, with initialization/initialized sequencing, tool input/output schemas,
  `structuredContent`, and `CallToolResult.isError`. This is a design assumption,
  not a claim of current-client or SDK compatibility. Verify the chosen published
  specification and SDK locally before implementation; none was fetched here.
- Identify the proposed server as `sp-shared`, with an adapter release version
  independent of the MCP protocol revision and storage versions. Require clients
  to accept the selected revision; no silent downgrade or legacy output mode.
  An unsupported revision must not result in an initialized tool session.
- Advertise only a static `tools` capability (`listChanged: false`). No resources,
  prompts, sampling/model calls, elicitation, roots, logging subscription,
  completion, or experimental extensions. Client roots cannot select a store.
  Accept only required lifecycle methods, ping, tool listing/calling and defined
  cancellation notifications; reject unknown request methods safely.
- Stdout is protocol-only; stderr contains bounded, content-free diagnostics.
  No banners, startup paths, stack traces, raw errors, or Markdown rendering.
- Tool annotations, if supported, mark read as read-only and append as mutating;
  both are closed-world. Do **not** advertise append as unconditionally idempotent:
  publication is deduplicating, but a retry's outcome arrays can differ. Annotations
  are hints, not authorization or a substitute for client write approval.

## Fixed authority and explicit date policy

Startup is operator-controlled, not tool-controlled. Require an existing absolute
store directory and explicit read-date and append-date allowlists. Do not default
to the user's store, create a missing store, inspect editor/client configuration,
read HOME configuration, import environment secrets, or discover transcripts.
`NewManagerAt` can create directories today, so the future entrypoint must reject
missing stores before construction. Trust the launcher, executable and directory
ancestors; refuse a symlink store or non-directory. Existing regular-file and
strict-record checks remain enabled. This is not race-free containment against
same-user filesystem replacement; it does not harden Windows ACLs.

No tool accepts a path, URI, working directory, environment, executable, command,
raw CLI flags, config override, or dynamic policy change. Do not invoke shells,
editors, templates, clipboard programs, hooks, network calls, or a model. The only
path component derived from a request is a validated canonical calendar date
inside the fixed store. Unknown fields are errors, not ignored extensions.

Possible policies, requiring an explicit operator choice:

| Policy | Benefit | Cost/risk |
| --- | --- | --- |
| Separate exact date allowlists | Reviewable, least-authority, deterministic across midnight | Restart to authorize another date |
| Fixed inclusive date interval | Convenient weekly collection | Grants every day in the interval, including future records |
| Today-only in a configured timezone | Convenient daily writes | Clock/timezone/midnight behavior can redirect retries |
| All dates in the store | Mirrors current shared CLI history access | Exposes complete shared history and permits historical writes if applied to append |

**Recommended fail-closed default:** absent policy grants nothing and startup
fails; no implicit today, all-history access, or read permission derived from
append permission. Use separate exact allowlists, each at most 31 dates; an
explicitly empty list disables that operation. Require at least one grant overall.
For a synthetic weekly workflow, approve reads for `2026-11-02` through
`2026-11-08` as seven explicit entries and append only for `2026-11-08`.
An append-only deployment may have no read grants.

Both tools require an explicit `date`; validate real calendar dates, not just the
schema pattern. Check authorization before opening/inspecting that day's record.
Do not disclose whether a denied date exists or has private/shared material.
Return the same denial for missing and populated denied dates. Append authorization
includes creation of a new shared-only record on that date; it does not authorize
editing/deleting existing bullets or any private data. Freeze policy for the
connection; changes require a reviewed restart. No automatic midnight rollover.

**Important limit requiring approval:** this is an API disclosure/write policy,
not a promise that the backend only reads allowlisted files. Existing append
calls `PrivacyEnabled`, which may scan other dates' public/outer records when
metadata is absent or privacy is disabled. Shared APIs also parse mixed records
and inspect wrapped-key metadata without unlocking; they do not use private Load
or decrypt. Preserve these safety checks. A requirement for filesystem-level
per-date isolation or zero wrapped-metadata reads would need a separately approved
storage/API design, not bypassing checks or weakening encryption guarantees.

## Tool contract (proposed v1)

All schemas below use JSON Schema 2020-12. Every object is closed with
`additionalProperties: false`, every listed field is required, and strings must
be valid UTF-8 with no unpaired surrogate escapes. Reject duplicate JSON keys,
case aliases, coercions, `null`, and unknown fields before storage. SDK decoding
must not silently repair invalid text. Schema character counts are supplemented
by the byte limits below; existing shared validation remains authoritative.

### `sp_shared_read`

Input schema:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["date"],
  "properties": {
    "date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"}
  }
}
```

Successful output schema (the tool's `outputSchema`):

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["date", "status", "content", "not_encrypted", "provenance"],
  "properties": {
    "date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
    "status": {"enum": ["ok", "missing", "no_shared"]},
    "content": {"type": "string", "maxLength": 65536},
    "not_encrypted": {"const": true},
    "provenance": {"const": "self_reported_not_authenticated"}
  }
}
```

Map `ReadShared` to the CLI's statuses: `missing` means no record; `no_shared`
means an existing record with whitespace-only shared content; otherwise `ok`.
`missing` has empty content. Preserve the exact shared content even for
`no_shared`, matching current JSON behavior; do not normalize stored text.
Do not return `HasPrivate`, `Legacy`, private existence/state, ciphertext,
template history, private timestamps, metadata, keys, paths or raw records.
The `missing`/`no_shared` distinction itself discloses date-record presence;
operators must approve that disclosure within the read allowlist.

### `sp_shared_append`

Input schema:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["date", "section", "source", "bullets"],
  "properties": {
    "date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
    "section": {"type": "string", "minLength": 1, "maxLength": 200},
    "source": {"type": "string", "minLength": 1, "maxLength": 500},
    "bullets": {
      "type": "array", "minItems": 1, "maxItems": 100,
      "items": {"type": "string", "minLength": 1, "maxLength": 8194}
    }
  }
}
```

Successful output schema:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["date", "section", "source", "added_bullets", "duplicate_bullets", "not_encrypted", "provenance"],
  "properties": {
    "date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
    "section": {"type": "string", "minLength": 1, "maxLength": 200},
    "source": {"type": "string", "minLength": 1, "maxLength": 500},
    "added_bullets": {
      "type": "array", "maxItems": 100,
      "items": {"type": "string", "minLength": 1, "maxLength": 8192}
    },
    "duplicate_bullets": {
      "type": "array", "maxItems": 100,
      "items": {"type": "string", "minLength": 1, "maxLength": 8192}
    },
    "not_encrypted": {"const": true},
    "provenance": {"const": "self_reported_not_authenticated"}
  }
}
```

Require `source` explicitly rather than silently using the CLI's `sp` default.
It is arbitrary self-reported public text, not authenticated client identity.
Do not derive trusted authorship from the connection, an MCP request ID, or an
agent-looking label. IDs/timestamps in on-disk contributions are not returned in
v1; do not invent a provenance history API.

Return successful objects as `structuredContent`, with one text content block
containing the same JSON serialization for compatible clients. Set `isError:
false`. Content is untrusted data, never instructions. Escape JSON controls; do
not execute shell-looking strings, dereference links, or render note Markdown.
Any client display must neutralize terminal/bidi controls without changing the
stored string. The fixed disclosure/provenance fields are server-owned, never
parsed from a note heading.

### Literal bullets, deduplication and publication

Call `ValidateAppend` before opening storage, then `AppendDetailed`; do not split
multiline text, trim whitespace, change Unicode/case, wrap lines, summarize,
expand shell syntax, or perform semantic deduplication. Existing input syntax
removes exactly one optional `- ` prefix, validates the resulting bullet, and
writes it as a literal list item. This is the existing append-input rule, **not**
a new normalization pass over stored notes.

- Section/source must be nonempty, trimmed, single-line labels. Existing byte
  limits are 200 for section and 500 for source. Controls and Unicode line
  separators are rejected; existing heading/fence restrictions still apply.
- Each resulting bullet is at most 8192 UTF-8 bytes, trimmed and single-line.
  Reject invalid Markdown block prefixes and controls using existing validation.
  `"- item"` and `"item"` address the same bullet; `"Item"` and `"item"` do not.
- Exact dedup is scoped to date and named section, including repeated values in
  one request. Source changes do not bypass it. Other sections/dates are distinct.
  The existing parser respects fences/headings and refuses ambiguous duplicate
  section headings or unclosed fences; never repair them automatically.
- Preserve input order within each result array. For `["- one", "two", "one"]`
  on a fresh section, added is `["one", "two"]`, duplicate is `["one"]`.
  Retrying yields no added values and duplicates `["one", "two", "one"]`.
- New bullets create one self-reported contribution per operation; duplicate-only
  success does not rewrite the record or add provenance. Do not promise globally
  exactly-once requests, a persistent idempotency-key ledger, authenticated audit,
  rollback detection, or cross-day snapshot consistency.
- Preserve the existing full-operation locks, conflict/publication rules and
  protected private payload. Unmigrated legacy records under active privacy
  remain an error; no automatic migration, private fallback, or unlock.
- Failure, cancellation, lost connection, or post-publication sync failure may
  occur after visible publication. Retry only the identical approved date,
  section and bullets. Duplicate-only success/readback does **not** prove crash
  durability of an earlier unconfirmed publication. See
  [publication limits](privacy.md#storage-and-cryptography).

## Bounds and safe failures

Proposed fixed initial limits, not request-tunable:

| Resource | Limit / behavior |
| --- | --- |
| Incoming complete protocol frame | 64 KiB before parsing; no JSON-RPC batches |
| In-flight calls / queued tool calls | One / zero; additional calls return `BUSY` |
| Append bullets | 1–100; at most 32 KiB combined decoded date/labels/bullets |
| Read content | 64 KiB UTF-8; reject the whole result if larger |
| Outgoing complete protocol frame | 1 MiB, including JSON escaping and duplicated text/structured content |
| Method/tool names and request IDs | Static method/tool names; string IDs at most 128 bytes or safe-range integer IDs |
| Diagnostics | At most 256 bytes per event; no request/response text |
| Storage | Existing 16 MiB document and 64 KiB metadata bounds remain; no bypass |

Reject oversize input atomically, without prefix publication. Preflight the
maximum append response size before mutation; an accepted append must not later
fail just because the adapter selected an undersized output buffer. The response
bound includes escaped content and both representations. Check serialized read
size before sending; no truncation, partial JSON, preview, pagination, or paging
cursor in v1. Separate per-date calls prevent an unbounded weekly result.

Malformed protocol/unknown methods use sanitized standard JSON-RPC errors.
Tool argument, authorization and execution failures use `isError: true` with a
single bounded JSON text block; error results omit success `structuredContent`
so they do not pretend to satisfy the success `outputSchema`. Proposed closed
error payload, with no other fields:

```json
{
  "code": "APPEND_UNCONFIRMED",
  "message": "Append outcome is unconfirmed; review before retrying identical input.",
  "write_outcome": "unknown"
}
```

`code` is one of `INVALID_INPUT`, `DATE_NOT_ALLOWED`, `BUSY`, `OUTPUT_TOO_LARGE`,
`STORE_UNAVAILABLE`, `APPEND_UNCONFIRMED`; `message` comes from a fixed table,
never `err.Error()`; `write_outcome` is `not_attempted`, `not_applicable`, or
`unknown`. Only errors known to precede mutation may say `not_attempted`.
All backend append errors after dispatch conservatively use
`APPEND_UNCONFIRMED`/`unknown`, even where a particular error probably preceded
publication. Read failures use generic `STORE_UNAVAILABLE`/`not_applicable`.
Never turn a storage/metadata/format failure into `missing`, `no_shared`, or empty
success. No paths, ciphertext, key metadata, private labels, OS error strings,
stack traces or rejected text are reflected. Denied dates are indistinguishable
regardless of store state. Operator troubleshooting happens outside MCP.

No API for private `Load`/`GetByDate`, unlock, passphrases, key retrieval/rotation,
privacy enable/disable/status, recovery discovery/export, editing, or plaintext
private-editor fallback. No credential/session-token persistence. Even an already
unlocked TUI is irrelevant: this process obtains no identity from it.

## Startup, lifecycle and implementation gates

1. Operator reviews grants, fixed store, executable and client write-confirmation
   policy. No settings are installed automatically. Validate startup arguments
   without opening notes; on missing/invalid policy, exit with a safe diagnostic.
2. Check the configured existing store and construct a locked manager. Do not
   enumerate notes, warm caches, preload history, or inspect private state at
   startup. Standard backend policy checks still run on authorized operations.
3. Finish protocol initialization before tool calls. Keep the tool list static;
   a disabled operation remains listed but always denied. Rate/size bounds apply
   to lifecycle traffic as well as tools. No transcript or request-body logs.
4. Validate/authorize a tool call fully, then dispatch at most one storage worker.
   Continue reading bounded control messages so ping/cancellation/shutdown are
   responsive; never build an unbounded queue or spawn a worker per request.
5. Recommend a 10-second active-operation budget and 1-second bounded reply/drain
   grace. Cancellation before dispatch prevents mutation. Once storage starts,
   cancellation is not rollback: discard a cancelled read; an append has an
   unknown outcome until completed. Never acknowledge cancellation as proof of
   no write, retry automatically, or let a cancelled worker overlap another call.
6. On EOF/signals, stop accepting calls and allow the active worker the bounded
   grace. If it cannot finish, terminate the process; do not keep a detached
   goroutine or background writer alive. A disconnected/terminated append is
   unconfirmed. Release manager state/locks on normal completion and exit.
   Client/operator supervision must handle a blocked stdout pipe or forced kill;
   an undelivered final error is not an acknowledgement.

**Current limitation:** shared methods do not accept contexts, Unix file locking
can block indefinitely, and policy validation may scan many dates. The proposed
worker deadline/process-exit strategy needs approval and crash-window tests; it
is not an existing bounded-latency guarantee. Do not bolt a timeout onto a
goroutine and then report no write or continue serving while it mutates. If
review requires graceful cancellable locks/scans instead of process termination,
that is a separate approved backend change. No new lock/storage semantics are
authorized by this design.

## Adversarial synthetic acceptance plan (not executed here)

Use only a disposable store, fake clock if relevant, synthetic public/private
canaries and a local protocol harness with pipes. No live MCP server/client,
provider, network, real notes, real secrets, HOME files or editor settings are
needed to review this design. Future implementation testing may launch only the
approved local candidate against that fixture.

- **Protocol:** supported/unsupported revision; calls before initialization;
  unknown methods/tools/capabilities; malformed UTF-8, lone surrogates, duplicate
  keys, case aliases, nulls, batches, overlong IDs/frames, partial line then EOF;
  protocol-only stdout, bounded safe diagnostics, invalid response rejection.
- **Authority:** absent policy fails startup without store access; read/write
  allowlists are independent; deny existing/missing out-of-policy dates identically
  with zero target access. Reject traversal, absolute paths, URIs, date suffixes,
  impossible/leap dates, unknown `path`/`command`/`root`/`private` fields, roots and
  dynamic-policy attempts. New shared-only dates require append grants.
- **Date stability:** fixed grants survive midnight/timezone/DST and replay of an
  identical append; no implicit today or archive enumeration. Test allowed reads
  for populated, missing, whitespace-only and legacy-private-only records.
- **Private isolation:** fixture spies forbid private Load/decrypt/unlock/recovery
  and template/editor/shell/network calls. Locked and independently unlocked
  fixtures return identical shared results. Check private canaries never appear
  in results/errors/logs; compare protected payload and private metadata before
  and after append. Corrupt ciphertext body without invalidating the outer record:
  shared operations must not decrypt merely to authenticate it.
- **Literal data:** shell-looking text (`$(...)`, backticks), links, forged system
  instructions, provenance headings and source impersonation remain inert public
  strings where existing syntax permits. Controls/newlines, block prefixes,
  invalid section headings, overlong UTF-8 and untrimmed labels reject atomically.
  Valid Unicode and U+FFFD remain exact; display escaping never rewrites storage.
- **Dedup/provenance:** repeated bullets within a call, exact replay, changed
  source, case/Unicode differences, distinct sections/dates, fences/subheadings,
  duplicate headings and unclosed fences; verify result ordering, contribution
  counts, no duplicate-only rewrite, no invented authenticated identity.
- **Bounds:** byte/character boundary cases; 100/101 bullets; 32 KiB append total;
  64 KiB frames/read content; worst-case JSON escaping and duplicated output;
  1 MiB response cap; oversized records/metadata; no partial output/publication.
- **Concurrency/durability:** cooperating appenders, private-save conflicts,
  migration/reversal races, locked stores and malformed metadata preserve existing
  refusal/locking rules. Fault-inject before and after rename/directory sync;
  no false success/no-write claims, rollback, weakening of encryption or private
  fallback. Repeat approved bullets after an unconfirmed result and preserve the
  original durability warning even if retry is duplicate-only.
- **Lifecycle:** cancellation before/after dispatch, slow lock/scans, repeated
  calls while busy, malformed control traffic, deadline, EOF, broken/blocked
  stdout, signals and forced termination. No queued growth, orphan worker,
  silent retry, content log, stale completion, or writes after process exit.
- **Filesystem/platform:** synthetic symlink/nonregular/case-aliased date paths,
  invalid/missing store and metadata, unknown formats and disabled/legacy modes;
  safe errors preserve bytes. Record platform durability and Windows ACL limits
  honestly; no decoder fallback or new isolation claim.

## Decisions requiring approval before implementation

1. **Disclosure/date policy:** separate explicit grants as default; presence
   disclosure, historical/future access, append-only operation, maximum 31 dates,
   restart-based changes, and backend policy scans outside granted dates. All-date
   or rolling-today modes are alternatives, not silently enabled conveniences.
2. **Transport/version:** stdio-only deployment and the exact protocol revision,
   initialization negotiation, structured error interoperability, supported local
   clients/platforms, and schema contract stability. Remote transport would need
   a separate authentication, authorization, origin/session and exposure review.
3. **Packaging/dependencies:** separate entrypoint versus an opt-in subcommand;
   exact maintained Go MCP SDK/version, license and transitive dependencies; or
   the review burden of a hand-written protocol. No SDK is chosen or added here.
   Confirm strict decoding, framing/queue bounds, cancellation and no hidden
   telemetry/network behavior before approval.
4. **Operational policy:** tool-write confirmation/user approval, exact limits,
   safe error taxonomy, deadline/termination versus cancellable backend work,
   supervision and per-platform durability testing. MCP grants are not an OS
   sandbox for unrestricted same-user agents; use host restrictions separately.
5. **Release gate:** approve and implement only after the above decisions; run
   synthetic contract/security/lifecycle tests and a separately approved client
   interoperability check. This document and existing CLI tests are not evidence
   that an adapter exists, passes these tests, or is safe to ship.

No private/shared ownership, stored-text normalization, encryption policy,
key/recovery interface or editor behavior changes are proposed. Existing hooks
remain opt-in, independent workflows; this adapter must not auto-register them.
