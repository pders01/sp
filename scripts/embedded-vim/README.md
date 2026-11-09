# Synthetic headless Vim-core proof

**Application integration:** Go runtime/tests now live in
[`internal/embeddedvim`](../../internal/embeddedvim/README.md). SP explicitly
opts in via `sp edit --editor=vim [YYYY-MM-DD]`; normal defaults and native gates
remain unchanged. Node here stays a synthetic build/test tool, never the runtime.
See [integration and manual testing](../../docs/embedded-vim-integration.md).
The Node harness below is synthetic build/compatibility tooling, not the
application entry point or a security approval. Canonical Go execution uses a
different pinned QuickJS-NG WASI artifact and the shared encrypted lifecycle.
Superseded local proof/demo tools were removed; canonical Go tests and linked
assets are the maintained execution path.

This Node harness has no normal-note routing, encrypted store or real terminal.
Node is only its trusted test/build host. The Vim core and editor model execute
inside **QuickJS compiled to WebAssembly**, not inside Node, `node:vm`, a browser
or a native editor process.

## Run

From the repository root, with Node 22 and npm:

```sh
npm --prefix scripts/embedded-vim ci --ignore-scripts --no-audit --no-fund
npm --prefix scripts/embedded-vim test
```

Use synthetic fixtures only. No secret/config/store arguments are supported.
Do not substitute real notes into tests or log session snapshots. Installation
scripts are disabled. `node_modules/` is ignored; the bundle is built in memory.
Direct dependencies are exact versions with transitive resolutions/integrities
recorded in `package-lock.json`; nothing has been staged or committed.

## What runs

- `@replit/codemirror-vim-core` **0.1.0**, MIT: existing command semantics.
- `@codemirror/state` **6.7.6** and `@codemirror/commands` **6.11.1**, MIT:
  existing document model and history. The adapter does not implement Vim or its
  undo stack. `@codemirror/streamparser` **6.0.0** supplies its token stream.
- `quickjs-emscripten` **0.32.0**, MIT: restricted guest runtime.
- `esbuild` **0.28.2**, MIT: trusted host-side bundling only.

`adapter.js` supplies the CM5-style range/event/bookmark/search-cursor adapter,
fixed geometry and tiny inert prompt nodes. It exposes no host functions. Literal
insertion is separate from key input. Search and Ex prompts are driven by typed
keys and actual upstream callbacks, not a direct-command test bypass.

`runtime.js` loads the guest with no module loader, host references, filesystem,
network, process, console or clipboard APIs. Guest-local `document` and timers
are data/callback shims, not browser objects or host scheduling. The trusted Node
loader reads installed source/Wasm files; that is not guest filesystem access.

## Deliberate source-pinned primitive instrumentation

Unmodified core 0.1.0 silently ignores some unsupported substitute flags, including
`n` and `I`, and can replace text unexpectedly. This was reproduced by failing
synthetic tests before adding the guard.

`core-policy.js` checks the installed core's SHA-256:

```text
a8c2887193e966a6d1c4acf01105a0b6e8ed11e8ff26dfe3bb8a898649efe650
```

An in-memory build insertion validates **upstream-parsed tokens** before document
or substitution-history mutation. It permits only `g/c/i` flags and rejects
unsupported flags/count suffixes with a fixed notice. It does not implement `n`
or `I`, rewrite Ex grammar, change async dispatcher metadata or modify installed
package files. Source/hash/anchor mismatch refuses the build. This experiment
therefore runs an **instrumented core**, not a claim of unchanged/full Vim parity.

A second unique-anchor hook redirects the core's one-element overwrite replay
records (`[text]`) to the adapter's grapheme-aware `overWriteSelection` primitive.
UTF-16 text length is not a source grapheme count. Counts, macro recording and
replay still belong to the reused engine; two-element insertion-offset records
retain their original path. Additional unique-anchor coordinate hooks delegate
character motion, content clipping, inclusive range edges and characterwise put
positions/caret to the same bounded `findPosH` primitive. They do not replace Vim
count parsing, operators, register handling or history. All anchors and the
original source hash are checked before building; installed files stay untouched.

## Everyday compatibility scope

Keep additions to common editing workflows and small existing-engine/CM6 glue.
No native replace restoration stack, broad regex port, virtual-column engine,
clipboard/plugins/windows or new app infrastructure is planned for compatibility.

The everyday pass corrects `P`'s single-line final caret and `A` before retained
CRLF. Enter/open-below reuse CM6 keep-indent (plain text, no syntax inference);
new separators are LF and untouched CRLF bytes remain intact. Blank CRLF lines
exclude their terminator from copied indentation. Insert Ctrl-W/U preserve the
original insert/change command and record existing `InsertModeKey` events, so
mixed typed corrections replay with dot/counts/macros rather than replacing the
last edit. These tiny engine hooks are source/hash/unique-anchor pinned too; no
new command parser, repeat engine, registers or history stack was added.

The closing common-workflow checklist also covers ASCII replace/case operators,
word/find/search motions, local marks, registers (including black-hole and yank
register), insert-register, visual replacements and basic Ex ranges. Small fixes
make `O` copy the current line indent, `J` respect existing whitespace/closing
parentheses and CRLF boundaries, visual-line `c` keep the remaining newline, and
whole-line put/change/final deletion retain complete CRLF separators. Exclusive
range endpoints preserve the core's EOF sentinel rather than clipping it early.
All use existing command/operator/register/history machinery; no general format,
virtual-column or language engine was introduced. This finite common-workflow
scope is the compatibility completion gate, not exhaustive native Vim parity.

## Evidence

**552 tests pass in the parent-run local Node 22.23.4 harness**, none skipped.
No other host/runtime platform was tested.

Tests cover typed `:` and `/`, search repetition/history recall, prompt closure, `%s`
with/without `g`, `c` confirmation including quit/Escape, capture replacement with
`nopcre`, alternate delimiters, named registers, macro replay, typed edit undo/
redo, literal paste, and exact round trips/edit-undo for CRLF/mixed/final endings,
Unicode and controls. Mixed-ending substitution is also checked.

Runtime negatives cover absence of ambient I/O globals, function-constructor
escape to host globals, interruption of an infinite JS loop, guest allocation
failure, fixed error diagnostics and retention of a copy of the last acknowledged
snapshot even when a fault follows an unacknowledged mutation. These are narrow fixture observations, not comprehensive escape proof.

## Limits and failures

- Default QuickJS heap accounting limit: **64 MiB**; stack: **512 KiB**.
- Separate bound Wasm linear memory: **128 MiB maximum**, initially 16 MiB.
  Runtime construction verifies the supplied memory is actually bound.
- Each guest call, including JSON serialization, has an interrupt-check budget
  and monotonic deadline. This is **cooperative interruption**, not a guaranteed
  hard wall-time bound for all regex/native-runtime internals.
- Prototype text limit: **1 MiB**, intentionally not SP's full 8 MiB domain.
- On guest failure, dispose it, retain only the last acknowledged host snapshot,
  reject further input and mark latest text unknown. Partial unacknowledged edits
  are not recovered or claimed rolled back.
- Host build time, strings, serialization copies, Node heap and multiple sessions
  are not collectively contained by guest limits. Output is checked after export;
  this is not a complete production memory-budget design.

## What this does not establish

- A production runtime dependency. The separate Go proof now verifies a
  Go-compatible host for the same JS bundle, not direct compatibility of this
  npm/Emscripten Wasm artifact with wazero.
- Real terminal rendering, cell/grapheme cursor correctness, resize or interactive
  usability. Guest geometry is synthetic; stored controls never reach a terminal.
- Full Vim, Vim regex, plugin, replace-mode, clipboard or filesystem compatibility.
  `R` now uses the upstream CM6 integration's overtype behavior, with whole-source
  grapheme/CRLF boundaries and corrected Unicode replay. Backspace moves left;
  it does NOT restore old characters like native Vim. Unsupported API calls are safely
  refused only before any mutation; partial changes still fault with retained ack.
  `:w` requests an encrypted live checkpoint; `:x/:exit/:wq/:q/:q!` request
  parent lifecycle actions. Normal-mode `ZZ` maps to `:wq`, `ZQ` to `:q!`
  through the existing engine; neither is a global host key interceptor. The guest
  cannot save, discard or authorize loss.
- Complete history grouping: basic non-adjacent substitution now has a
  single-undo regression test, but full Vim grouping parity is not established.
- Full insert-mode macro/dot-repeat, complex-selection/virtual-block or mapping
  timing parity. Basic dot/counts, named insert macros, text objects and block I/A
  have regression coverage; change-and-insert undo grouping is now fixed.
  Character/line/block selection metadata and Go cell highlighting now have
  bounded tests, including active caret, copy validation and safe resume.
  Guest timers drain deterministically rather than reproduce real elapsed time.
- Full search-cancellation semantics: first/later cancelled searches now restore
  the previous query through the upstream state API and have regression tests.
  Rejected Ex input can still update search/colon registers before policy
  validation; only document/substitute replacement-history mutation is guarded.
- General Unicode editing parity: whole-cluster insert deletion/motion and CRLF
  preservation are tested. Counted horizontal character motions/operators now
  step whole source graphemes; word, column and virtual-block compatibility is
  not thereby established. Separate combining/ZWJ continuations
  in replace mode refuse; whole-cluster literal input is supported. Earlier typed
  keys remain applied when a later continuation refuses. Unsupported boundaries reject rather
  than split them. Earlier edits in a multi-step command may require undo.
- Production lifecycle/interactive usability, real-note readiness, OS-enforced
  confinement, independent audit or a verified native platform support matrix.

The useful result is that the command engine can run headlessly in a
capability-restricted Wasm-hosted JS guest while retaining tested document bytes.
The **Go-host compatibility gate has now passed** in the separate proof. The
synthetic input/display and encrypted lifecycle adaptation are now implemented
in the separate Go experiment. Next is manual terminal acceptance outside pi,
not adding a Node runtime dependency or enabling existing native gates.
See [the evaluation record](../../docs/embedded-editor-evaluation.md).
