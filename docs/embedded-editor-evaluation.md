# Embedded Vim engine: bounded feasibility evaluation

## Current: explicit application integration

Canonical Go runtime/tests moved to `internal/embeddedvim`; public program and
license assets are linked with go:embed. Root depends on pinned wazero v1.9.0,
not fastschema/qjs.New or a Node runtime. `sp edit --editor=vim [YYYY-MM-DD]` is
an explicit experimental encrypted-private-note entry point. Existing bare-SP,
notebook/calendar defaults and native protected gates remain unchanged.

It uses the same prepared generation/CAS/checkpoint/recovery workflow via an
explicit in-memory factory (not an experiment/native fallback). Command tests
with actual bundled VM and disposable encrypted fixtures cover checkpoint/cancel/
relock/conflict; retained terminal/input reentry has physical-consent controls.
See [integration/manual acceptance](embedded-vim-integration.md). Parent headless
verification passes: canonical runtime/shared race (runtime 209.639s), command
entrypoint/actual encrypted fixture race, all 552 JS tests, CGo-disabled runtime
and command tests, vet and module verification. Guest regeneration is byte-identical.
CGo-free native/Linux/Windows binaries were built, never launched; this is NOT
native terminal/platform execution proof. Manual acceptance stays user-owned.
Earlier root-dependency/routing statements below describe historical milestones.

## Latest: common note-editing compatibility completion gate

User requested continuing autonomously to completion, still excluding rare or
extensive-app work. A finite 64-case closing checklist covers ordinary word/find/
search motions, marks, registers (named/black-hole/yank), insert register, replace/
case commands, visual operations and basic Ex ranges, with exact data, source
carets and undo/redo. Initial multi-edit undo oracles were corrected to check the
immediate prior edit rather than claiming two edits undo together.

Small fixes cover plain J spacing/complete CRLF joins, current-line indent for O,
visual-line change preserving a newline, and whole-line CRLF put/change/final
deletion. The last deletion exposed a previous coordinate-hook bug: zero-offset
exclusive ranges must retain the upstream EOF sentinel. Only actual inclusive
edges advance now. Core still owns parsing/counts/registers/operators/history.
New adapter helpers only insert an indented source line or rotate a complete
source separator. No application infrastructure or capabilities added.

Targeted actual Go/Wasm and encrypted CRLF linewise checkpoint/undo tests pass.
Final parent verification: **552/552 Node tests**, none skipped; Go race
205.744s, CGo-disabled tests 38.191s, vet/module verification/build and shared
lifecycle race tests pass. Ignored CGo-free v9 binary built, never launched.
This finite checklist closes the chosen compatibility scope, not full
Vim/regex/Unicode/option/native terminal parity.
No new manual terminal acceptance pause is needed for these headless-only changes.

## Earlier: bounded everyday line/insert compatibility

User chose further compatibility work, explicitly excluding rare behaviors that
need extensive app work. The pass corrects characterwise `P`'s final caret, `A`
before CRLF, and plain Enter/open-below indentation via CM6 keep-indent (no
language inference). Insert Ctrl-W/U no longer overwrite the original insert edit:
two pinned hooks preserve it and let the existing `InsertModeKey` recorder/replay
own the controls. Mixed corrections now repeat via dot/counts/named macros;
there is no custom repeat parser/history/restore model.

50 new common-workflow regressions cover data/caret/undo, controls, line changes,
basic joins, yank/put and LF/CRLF preservation. A mistaken 3J first-join caret oracle
was corrected from upstream/native source provenance (Vim uses the last join),
not by changing the engine. Two old standalone Ctrl-W/U redo expectations reflected
the replaced-last-edit bug; they now repeat the original A command correctly.
Go/Wasm and actual synthetic encrypted checkpoint/repeat/undo/redo also pass.
Final parent verification: **488/488 Node tests**, none skipped; Go race
184.095s, CGo-disabled tests 33.872s, vet/module verification/build and shared
lifecycle race tests pass. Versioned ignored v8 binary built, never launched.
No app UI/routing/root deps/gates change.
R restoration, full regex/options, virtual columns and other large parity projects
remain outside this course.

## Earlier: grapheme-count horizontal coordinate primitives

Counted `h`/`l` and horizontal operators now delegate to bounded line-local CM6
grapheme stepping. Pinned clipping/range-edge hooks prevent interior final-glyph
positions; two characterwise-put coordinate replacements fix insertion and final
caret after Unicode registers. Core parsing, counts, operator/put semantics,
registers and history remain reused. All instrumented anchors/hash must match;
this is not an unchanged-core or full Unicode Vim claim. Word/column/virtual-block
behavior remains separate. No UI, dependencies, protocol, normal routing or
capabilities changed. Parent Node suite: **438 passed**, no skipped tests.

Two initial Unicode put failures were fixed in coordinate glue, not test changes.
Go/Wasm checks cover coordinates/display cells, operator+put exact bytes and
counted Unicode deletion/undo through actual encrypted checkpoints. Actual native
terminal checks remain manual outside pi. R's upstream Backspace behavior remains.

## Earlier: bounded replace/overtype adapter

`R` now uses the existing core and the upstream CM6 integration's overwrite
primitive pattern (see [cm_adapter.ts](https://github.com/replit/codemirror-vim/blob/master/packages/codemirror-vim/src/cm_adapter.ts)
and [index.ts](https://github.com/replit/codemirror-vim/blob/master/packages/codemirror-vim/src/index.ts)).
Those moving public references informed behavior; they are not artifact pins.
The actual compiled core remains hash-pinned. A second unique-anchor replay
hook dispatches one-element overwrite records to a grapheme-safe primitive;
counts/macros/history remain engine-owned. No restoration stack was implemented.

Replace Backspace moves left, NOT native Vim's original-character restoration.
Whole-cluster input/replay and exact-byte undo are covered; unsafe separate
combining/ZWJ input and split-surrogate mapping tokens refuse. Counted source
motions still have upstream UTF-16 semantics. UI stays minimal; protected gates,
normal routing, keys/store formats and runtime capabilities remain unchanged.
Node suite now passes **258 tests**; parent Go/Wasm encrypted checkpoint/recovery
integration is separately exercised. Actual terminal checks stay manual.

## Earlier: bounded everyday-compatibility pass

Existing CM6 primitives now back bracket matching/text objects and indentation;
repeat/insert-macro/block-edit regressions pass, including corrected change/insert
undo grouping. Guest-authoritative vertical viewport and bounded search spans
provide scroll commands and search feedback without new UI chrome. `:w` and
Ctrl+S perform exact encrypted checkpoints with advanced CAS baseline and same
history/session; confirmed saves auto-resume, failures retain ownership, and
`:x`/`:exit` finish. Parent tests cover repeated saves, shared updates, auth/conflict,
durability ambiguity, stale authority and failed resume. No recovery/key format,
normal routing or protected gate changed. Public asset ABI is now v3.

`R` remains deliberately unsupported and refuses safely; indentation is plain
text, not a language formatter. Nonfatal unsupported handling applies only before
any document mutation; resource/partial failures retain the last acknowledgement.
No filesystem/clipboard/shell/plugin capabilities were added. Checkpoints discard
trailing queued keys/macro continuation at their ordered boundary. Actual terminal
checks remain manual outside pi; headless tests are not security approval.

## Earlier milestone: synthetic UI and encrypted lifecycle implemented

The earlier isolated Go experiment (now the
[canonical runtime](../internal/embeddedvim/README.md)) introduced a
reusable `editorcore.UI` adapter, control-safe Unicode/cell viewport, typed
lifecycle requests and a disposable encrypted synthetic demo. Node is build-time
only; Vim semantics remain in the reused guest. Normal SP routing, root dependency
files and all native protected gates remain unchanged. Shared terminal changes
are opt-in modal controls plus freeze-order/recovery fixes, not default routing.

Headless tests cover real Wasm input/frames/revisions, exact encrypted save with
shared updates, pinned consent-gated recovery after relock, resume revocation,
conflict retention and faulted known-copy recovery. Review identified prompt
paste destroying the guest and held input preventing recovery; both were fixed
and regression-tested. Known-copy recovery can discard pending keys ONLY after
fresh physical possible-loss consent and successful durable recovery/closure;
failed recovery retains ownership and requires renewed consent. No new recovery
or key format was added. The Node suite now passes **219 tests**, none skipped.

Visual display now transports bounded copied engine selection metadata and
projects it into validated cell spans for character/line/block modes. Fixed
parent-owned reverse/underline SGR handles selections and the active cursor;
gutters, EOF markers and chrome are never selected. An integrated resume test
found a hidden old CM range surviving the mutation barrier; resume now collapses
it after barrier release so the next insert cannot replace stale selected text.
Public asset manifest ABI v2 rejects incompatible pre-selection binaries before
store preparation; encrypted note/recovery formats and capabilities did not change.

Following explicit feedback against UI bloat, persistent header/help-footer and
fixture tutorial text were removed. Only the existing mode line retains a small
demo marker. A typed optional status-row slot lets necessary parent messages
reuse that line without overwriting a prompt. Two normal-mode engine mappings
connect `ZZ` to existing finish and `ZQ` to existing cancellation: changed-draft
recovery still requires consent. No new Ex/key parser, normal routing or key
capability was introduced. Headless tests include actual encrypted ZZ publication
with shared updates and ZQ consent/resume/pinned recovery without canonical save.

The earlier manual-only demo tools have since been removed; current instructions
are in the [integration guide](embedded-vim-integration.md). The user reported the
first manual synthetic run working and supplied a screenshot. That is initial
interaction evidence, not complete lifecycle/platform acceptance. UI polish
addresses the crowded/stale footer, adds line numbers/EOF markers and position
feedback, and removes per-keystroke busy flashing, using headless regression
checks. **Pi has not launched a terminal/PTY/editor/browser; no real notes or
independent security audit are involved.** Further visual checks remain manual
outside pi. Capability-runtime isolation is not OS enforcement or shipping approval.

## Earlier milestone: Go-host compatibility gate passed

The earlier isolated Go proof (now the
[canonical runtime](../internal/embeddedvim/README.md)) runs the SAME
JS guest bundle without Node/browser APIs at runtime, using pinned QuickJS-NG
WASI code and wazero through a small no-mount C-ABI host. It does not use the
upstream Go factory, which mounts CWD. Built-in module registration remains;
WASI host resources are withheld rather than claiming deletion unregisters them.

Parent local darwin/arm64 Go 1.25.13 race/CGo-disabled suites, vet and module
verification pass. Tests cover the commands/bytes plus heap/linear-memory limits,
caller deadline termination, partial-fault acknowledgement retention and an owned
parent filesystem/environment control. The Node suite still passes 36 tests.
Linux/Windows amd64 cross-compilation passes, NOT native runtime tests.

Root Go dependencies, SP editor/routing and protected native gates remain
unchanged. No interpreter port, real terminal, encrypted store or real note was
used. Source/lock hashes reject stale generated bundles; artifacts stay ignored.
The next bounded milestone is a **synthetic input/display adapter**, not another
runtime survey or real-note routing. See the Go proof README for exact provenance,
limits and verification, including the initial race-run compilation timeout.

## Earlier milestone: synthetic Node headless proof completed

After the user authorized trying the candidate, a separate proof was implemented
in [`scripts/embedded-vim`](../scripts/embedded-vim/README.md). **36 parent-run
tests pass**, none skipped, under local Node 22.23.4. The existing Vim core runs
inside QuickJS-in-Wasm with CodeMirror state/history, inert prompts, no guest
host-I/O bindings, guest heap/stack limits, a separately bound Wasm-memory ceiling
and cooperative execution interruption. Node is the trusted harness, not the
editor execution environment or a new SP production dependency.

Typed search/Ex, substitutions/confirmation, registers/macros, basic undo/redo,
exact fixture bytes, safe rejection of unsupported substitute flags and retained
acknowledgements after partial-operation failure are demonstrated. A source-hash-
pinned in-memory policy guard is used; this is not an unchanged/full Vim claim.
The README records known search-cancellation, grouping and runtime-budget limits.

No Go dependencies/routing/native gates or encrypted storage changed. This does
not establish Go-host compatibility, terminal usability, comprehensive containment
or a cross-platform security matrix. The subsequent Go-host gate above now
establishes the specific Go compatibility evidence; the npm Emscripten artifact
was not ported or assumed compatible with wazero.

The initial source-review record below is historical context; its unexecuted
status is superseded only by the specific proof evidence above.

## Decision and scope

The user changed course from native sandbox/DTS work to evaluating an existing
Vim-compatible engine embedded in a restricted JS/Wasm runtime. The objective is
to reuse search, Ex substitutions, registers and macros—not implement Vim in SP.
The current built-in editor, defaults, encryption and native protection gates
remain unchanged. No dependency, runtime or engine has been selected for shipping.

**Result:** a credible reusable command core exists, but no reviewed candidate is
a demonstrated drop-in fit for SP's Go terminal UI. Recommend **CodeMirror's Vim
core as the first bounded headless proof candidate**, not a broad port. A native
Neovim Wasm port is another lead, with unresolved runtime/licensing constraints.

The initial evaluation was public-source/repository review only; candidates
were not executed in that phase. Branch URLs below are moving source references,
not the proof's pinned dependency/source inventory. Package metadata alone is
not verified artifact or audit evidence.

## Candidate comparison

| Candidate | Existing behavior | Integration cost / disposition |
| --- | --- | --- |
| [`@replit/codemirror-vim-core`](https://github.com/replit/codemirror-vim/tree/master/packages/codemirror-vim-core) | Modal motions/operators, search, Ex substitutions, registers and macros; undo commands delegate to the host editor | **First proof candidate.** Explicit CM5-style adapter interface, MIT license. Not DOM-free or a complete editor model. |
| [`MuNeNICK/nvim-wasm`](https://github.com/MuNeNICK/nvim-wasm) | Actual Neovim compiled to wasm32-wasi; browser integration uses headless stdio MessagePack RPC | Potential reuse of SP's Neovim protocol/grid work, but not plain WASI: exception tags/custom imports, exact build identity and wrapper licenses need resolution. Not selected. |
| [`rhysd/vim.wasm`](https://github.com/rhysd/vim.wasm) | Actual Vim 8.2.0055, normal/small builds | Existing package is browser-only, experimental, worker/shared-memory/GUI based. Host integration includes IndexedDB, clipboard, fetch and JS evaluation. Not the small Go-terminal proof. |
| [`onivim/libvim`](https://github.com/onivim/libvim) | Vim-derived document/input/command API, no renderer | No maintained consumable standalone Wasm target established. Native linking is not confinement. Do not revive a port in this unit. |
| [Ace Vim](https://github.com/ajaxorg/ace/blob/master/src/keyboard/vim.js) | CodeMirror-derived Vim commands over Ace | Depends on Ace's browser editor and DOM. No advantage over the extracted core. |
| [`js-vim`](https://github.com/itsjoesullivan/js-vim) | Model-oriented search/registers/macros/undo and substitutions | Reviewed substitution implementation has material semantic gaps; old packaging. Not a mature compatibility target for this task. |

## CodeMirror core: what we can reuse

The [core README](https://github.com/replit/codemirror-vim/blob/master/packages/codemirror-vim-core/README.md)
explicitly offers `initVim(CodeMirror)` for custom editor adapters. The
[MIT-licensed package](https://github.com/replit/codemirror-vim/blob/master/packages/codemirror-vim-core/LICENSE)
is separately packaged; [registry metadata](https://registry.npmjs.org/@replit%2Fcodemirror-vim-core)
reports version 0.1.0, published July 28, 2026. Mature command-engine lineage does
not make this comparatively new standalone packaging a proven headless product.

The [shared tests](https://github.com/replit/codemirror-vim/blob/master/packages/codemirror-vim-core/test/vim_test.js)
include whole-file `%s`, ranges, capture replacements, alternate delimiters and
confirmation. The source-review worker found `g/c/i` flag handling and register/
macro implementations. **These tests were inspected, not executed.**

Important limits:

- Regex execution is JavaScript `RegExp`. The `pcre` option does not mean a PCRE
  implementation; Vim-style translation covers a subset, not full Vim regex.
- Full Vim substitution flags are not implemented. The worker found no `n` or
  `I` handling and a possible silent replacement for unsupported `n`. Unsupported
  input must not silently mutate a draft. This is a proof gate, not an excuse to
  recreate the Ex parser in SP.
- Undo/redo delegate to the host editor. The engine does not supply the whole
  text model/history implementation.
- The worker found DOM construction in search/Ex prompts before `openDialog`,
  notification DOM, timers and `navigator.clipboard` access for the `+` register.
  Merely providing an `openDialog` callback is insufficient.

The [adapter types](https://github.com/replit/codemirror-vim/blob/master/packages/codemirror-vim-core/types.ts)
require ranges, multiple selections, bookmarks, search cursors, change events,
operation grouping, history commands, geometry/scrolling and UI objects. This is
substantial editor plumbing, **not only forwarding keys**. Reuse existing model
primitives where possible; do not turn this into implementing another editor.

## Wasm Neovim: useful lead, not a rejected runtime assumption

The [build wrapper](https://github.com/MuNeNICK/nvim-wasm/blob/master/Makefile)
enables Wasm exception handling. The
[browser integration](https://github.com/MuNeNICK/monaco-neovim-wasm/blob/main/docs/architecture.md)
uses actual Neovim RPC and memory-backed WASI files, but includes custom
[`env` imports and exception tags](https://github.com/MuNeNICK/monaco-neovim-wasm/blob/main/packages/lib/src/nvimWorkerCommon.ts).
Do not reuse its frontend's text conversions as SP's byte codec or adopt arbitrary
host-command forwarding.

**Parent correction to the worker's initial runtime finding:** current wazero
source does expose
[`experimental.CoreFeaturesExceptionHandling`](https://github.com/tetratelabs/wazero/blob/main/experimental/features.go).
It is incorrect to reject the port simply because EH is absent from the default
CoreFeaturesV2 list. Exact instruction/tag/import compatibility remains untested;
an experimental feature constant alone does not prove it. Wrapper/shim licensing
and an exact Neovim/build pin were not established, so no artifact is selected.

## Restricted runtime and security boundary

A bare JS interpreter or Wasm runtime can withhold filesystem, networking,
processes, clipboard, environment and arbitrary host callbacks. Browser/Node
hosting with their ordinary APIs is not the proposed boundary. JS `eval` or an
in-memory virtual filesystem does not inherently grant host I/O; exposed host
capabilities determine the persistence risk. A virtual filesystem must remain
memory-only, without mounts, synchronization or logging of draft data.

Possible runtime leads, not selections:

- [Goja](https://github.com/dop251/goja) is pure Go and does not provide browser
  timers automatically. Current master requires Go 1.26, while SP declares
  1.25.13. A compatible pinned revision or an explicit toolchain decision is
  needed. Heap containment and interruption of expensive regex/host calls remain
  unverified; a document-size limit is not a JS-heap limit.
- [wazero](https://github.com/tetratelabs/wazero) is pure Go and supports memory
  limits/context termination, but is not a JavaScript engine. It cannot run the
  CodeMirror JS core directly. JS-in-Wasm would introduce another engine/build
  dependency; do not assume that is free integration.

This would be **capability-runtime isolation**, trusting the interpreter and
small host bridge—not OS enforcement against SP itself. Existing
`editorsandbox.Require`, `Driver.OpenProtected`, `privateedit.OpenDriver` and
`editorterminal.Run` must not be relabeled or enabled to claim this alternative.
Any later protected runtime API requires its own reviewed contract. No trusted
native fallback, real-note experiment routing or changed privacy promise is
approved by this evaluation. Existing paging/dump/display exclusions remain.

## SP integration facts

- `internal/tui/private_input.go` stores exact UTF-8 bytes, including controls,
  mixed endings and trailing-newline distinctions; positions are byte offsets.
- `private_undo.go` bounds delta history to 256 steps/16 MiB. Do not layer two
  inconsistent histories over engine edits or reset history on every snapshot.
- A JS adapter needs explicit UTF-16/UTF-8/grapheme/cell-coordinate conversion.
  Browser text models often normalize line separators; no-edit round trips alone
  do not establish correct edits around CRLF or supplementary Unicode.
- `editorcore.UI` already expresses input, literal insertion, safe frames, ordered
  freeze/actions and retained ownership. Reuse those concepts, not Vim commands
  implemented in the current `privateInput`.
- `editorterminal` consumes keys such as Ctrl+D/L/Z/C/Q and maps Ctrl+R to redo.
  This is not Vim-transparent; mode/prompt routing needs review before UI reuse.
- `:w/:q/:wq` must become typed parent lifecycle requests, not disk operations or
  proof of publication. `:q!` cannot bypass recovery/physical loss consent. Pasted
  commands and macros cannot authorize SP confirmation dialogs.

## Smallest proof and stop conditions

First resolve one exact core/runtime source pair, license attribution, runtime
syntax/regex support and CPU/heap containment. Then make **one synthetic headless
proof**, not application integration or a browser/terminal launch:

1. Drive real `:` and `/` prompt input, not just direct `handleEx` calls.
2. Exercise `%s/foo/bar/g`, confirmation, capture replacements, a register,
   macro record/replay and edit→undo→redo using the existing command engine.
3. Demonstrate unsupported flags fail without mutation. Document regex differences.
4. Round-trip and edit/undo exact text: empty, CRLF/mixed endings, no/final/multiple
   newlines, tabs, NUL/controls, U+FFFD, combining text and emoji. Reject unsupported
   input unchanged rather than normalize it.
5. Show a narrow capability bridge with no host I/O/store/key exposure, bounded
   prompt/output/timer queues and CPU/memory failures that retain acknowledged text.

Stop if this requires broad DOM emulation, a Vim-language implementation, an
unbounded editor-model rewrite or a runtime port. Do not progress to encrypted
save/recovery or general CLI/UI work before this proof is useful. Subsequent
integration must preserve ordered freeze, exact revisions, retained failures and
parent-owned encrypted publication/recovery. All existing host-denial/native
execution restrictions remain in force; no execution was performed in this review.

## Review provenance

Three scoped same-provider workers reviewed JS candidates, Wasm candidates and
local integration contracts. The parent reviewed their findings, freshly checked
core documentation/types/tests/license/metadata, Wasm wrapper/import source and
runtime documentation, and corrected the EH finding above. These are source
reviews, not independent audits. Search verification challenges and failed
metadata requests were not retried or used as evidence. No staging/commit,
configuration, dependency, application-routing or implementation changes occurred.
