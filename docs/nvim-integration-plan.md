# Bounded private-editor integration plan

**Native track archived:** the superseded Neovim adapters, demos and native-only
fixtures have been removed. The implemented opt-in path is the
[embedded Vim integration](embedded-vim-integration.md), not native sandbox
or Apple DTS work. This document retains the requirements for native protected
APIs; no runtime isolation claim satisfies or enables those gates automatically.

**Native-track decision:** the editor must be prevented by OS enforcement from
persisting private plaintext. A trusted-editor fallback is **not approved**.
Neovim is one adapter, not the owner of the security/storage architecture.
No real-note routing, default change, commit or security-completion claim follows
from this plan.

## Architecture and current implementation

| Layer | Responsibility | Current state |
| --- | --- | --- |
| `internal/editorcore` | Editor-independent document/snapshot/freeze, UI/frame, input receipts, driver and error contracts | Implemented; frame validation and fixed adapter diagnostics preserved |
| `internal/editorsandbox` | Required OS write/process/network/IPC/inherited-descriptor boundary, applied before private handoff | Immutable no-persistence requirement and fail-closed gate implemented; **no approved native backend** |
| `internal/privateedit` | Generation-bound encrypted save/recovery and retained-owner lifecycle | Production implementation is editor-independent; no Neovim imports |
| `internal/editorterminal` | Terminal/input/confirmation and operation ownership | Shared implementation; no Neovim dependency in production |
| Historical Neovim adapters and synthetic demo | Native RPC/Lua/grid experiments | Removed; no native editor is shipped |

The protected path fails before manager/private-data/driver/terminal access when
OS enforcement is missing. Protected Neovim open does not delegate to the old
unconfined experiment, even if platform availability later changes. An in-memory
second variant tests the shared encrypted lifecycle; it is a test double, not a
second shipping editor or an OS enforcement proof.

**This extraction does not implement write prevention.** All platforms currently
report enforcement unavailable. Policy booleans, child acknowledgements and
configuration isolation must never be accepted as native enforcement evidence.

**Backend feasibility result:** the reviewed supported macOS App Sandbox permits
container writes and program execution; the reviewed lower-level sandbox APIs
are deprecated. No supported mechanism meeting the complete policy was
established. The [feasibility stop record](macos-editor-sandbox-feasibility.md)
contains source evidence, adapter constraints, the proposed manual Apple DTS
question and a conditional synthetic proof matrix. No backend or exact target
build was approved; protected opening remains unavailable.

## 1. First native enforcement backend — only if separately resumed

Start with one specified platform/build, not a portability project. Historical
native evidence is macOS arm64 / Neovim 0.11.5; the user-reported 14-case terminal
pass did not include platform/version metadata.

- Establish a reusable platform backend controlling the exact owned process and
  inherited descriptors. Do not put OS policy logic in editor-specific Lua or
  assume a driver/child's self-reported success establishes enforcement.
- Before any draft handoff: deny filesystem persistence, new process/network/IPC
  routes that can bypass it, and unrelated inherited writable descriptors.
- No writable temporary-directory exception for editor text. Existing Neovim
  startup socket/log/runtime needs must be reconciled **without** opening that
  hole; startup/configuration guards are not a substitute for the OS policy.
- Investigate supported macOS mechanisms and deployment/deprecation constraints
  before selecting one. Do not claim that a particular mechanism is already
  viable or that Linux/Windows share its implementation.
- Native proof uses only disposable synthetic data, manually outside pi: ordinary
  writes, Lua/direct file I/O, shell/descendant and alternate channel escape paths,
  plus positive controls showing editing and parent encrypted publication work.
  Observe only owned fixtures; no real notes/configuration/credentials.
- No native launch/probe/retry or alternate launcher inside pi. Host sandbox
  denials remain final. Fail closed if the required mechanism is unavailable.

OS paging/core dumps, display capture and a compromised OS remain outside the
file-write guarantee. A backend must state its exact boundary and exclusions.

**Stop point:** report one natively evidenced backend, or a named feasibility
blocker. Do not enable `Current().Available`, substitute a weaker guarantee, or
expand UI work because enforcement is difficult. Bind the actual enforced child
to the adapter transport; do not merely flip a platform-availability boolean.

## 2. Neovim adapter on that enforced child

A future adapter would need to reconstruct and revalidate the removed Neovim
protocol/rendering experiment. Connect it to the backend-owned
process and permit private initialization only after the required boundary is
active. Preserve exact bytes, ordered freeze/resume, owned pending requests and
truthful publication/recovery/cleanup outcomes. No SP key enters the editor.
No user configuration/plugins or arbitrary `$EDITOR` discovery in this slice.

The existing 14-case terminal evidence and generic tests are reusable but do not
prove sandboxing. Repeat only acceptance affected by the changed launch boundary,
plus the security proof above; all native operations remain outside pi.

## 3. Small opt-in SP integration, only after 1 and 2

First slice: bare `sp`, today's encrypted private note, explicitly selected
adapter and trusted absolute executable. Flags do not exist yet. Built-in/default
editing and legacy plaintext behavior remain unchanged; reject unsupported
notebook/calendar/platform combinations without a fallback.

Fix CLI owner lifetime: `runScratchpad` currently defers `mgr.Lock()`. It must not
return, lock or discard the only usable owner/input after an unresolved failure.
Do not copy the demo's indefinite retention loop and call it finished recovery UX.
If that needs a larger architecture/recovery-format change, report the blocker.

Focused synthetic acceptance: routed unlock/edit/F2, F3 recovery/resume/relock,
conflict/failed-recovery ownership, exact private/shared preservation, and unchanged
no-opt-in behavior. Use injected manager construction, never normal store discovery.
Run race/vet/lint and compilation checks; do not restart a general hardening round.

## Not in this scope

Another real editor implementation; notebook/calendar integration; plugin parity;
highlight/selection polish; native paste parity; merge/reload UI; genuine job
suspend/resume; simultaneous native platform rollout; unrelated MCP/key/release work.
No automatic default or release enablement. Commits require separate authorization.

## Delegation

After the previous session's job-cap stop, three fresh-session scoped workers
completed backend feasibility, read-only Neovim binding and read-only security
review. No worker edited implementation files. The parent reviewed the findings
and freshly checked cited public sources; two read-only workers lacked web tools.
These same-provider reviews are not independent audits or native proof. Native
proof stays manual regardless of delegation.
