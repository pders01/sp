# Embedded Vim commit-scope inventory

Status: the user subsequently authorized preparation and execution of this
five-commit series. Portable/native tests have been separated; unrelated work
remains outside the selection. This document retains the original inventory and
explains the boundaries used. No application, native editor, terminal or PTY was
launched, no normal notes/configuration were inspected, and nothing is pushed.

The first four commits are `4ed0fdc` (storage), `6f24401` (shared lifecycle),
`0c46fbf` (packaged runtime) and `b6eac4e` (terminal). The final CLI/documentation
commit uses the subject listed below. Staged-tree exports, not the unrelated
working tree, are used for the per-commit verification gates.

## Findings and evidence

The editor is not a standalone three-file CLI change. Its required stack is:

```text
sp edit --editor=vim
  -> bundled embeddedvim + editorterminal
  -> editorcore + editorsandbox (native gates still fail closed)
  -> privateedit
  -> prepared scratchpad generation/CAS save + pinned encrypted recovery
  -> existing TUI terminal decoder, with literal-paste/EOF bridge
```

Original inventory before test splitting:

| Area | Files | Classification |
| --- | ---: | --- |
| `internal/editorcore/` | 8 | Shared contracts/frame validation |
| `internal/editorsandbox/` | 2 | Required fail-closed gate, not a backend |
| `internal/privateedit/` | 13 | Required lifecycle; mixed native test dependencies |
| `internal/editorterminal/` | 26 | Required controller; mixed native test dependencies |
| `internal/embeddedvim/` | 41 | Runtime, tests, public artifacts and licenses |
| Top-level files of `scripts/embedded-vim/` | 25 | Pinned JS source/build/compatibility harness |

The three canonical public program artifacts total 1,883,842 bytes. They and
`internal/embeddedvim/assets/licenses/` must be included, unlike ignored demo
executables. The root dependency delta is exactly `wazero v1.9.0` and its two
`go.sum` entries; no root toolchain change or fastschema Go-wrapper dependency.

A temporary **Go overlay**, not a Git index/worktree/staging operation, hid 60
untracked Go files belonging to the native prototypes, MCP and passphrase/secret
collection work, and substituted `HEAD:cmd/sp/privacy.go` to omit the unrelated
passphrase command registration. **The production `cmd/sp` CGo-free build passed.**
This establishes that those features are not production build prerequisites of
Vim. It does NOT prove a staged tree or every intermediate commit builds.

The same overlay's test dependency listing reports missing `internal/nvimbridge`
and `internal/nvimui`. These are real default-test compilation dependencies,
not production Vim dependencies. Skipping tests or unsetting native selectors
**does not remove their imports**. No suggested `go get` action is appropriate:
these are local parked packages, not missing external modules.

## Preparation performed before staging

Separate shared portable tests from native experiments. Do not solve the issue
by sweeping the Neovim prototypes into the Vim commits or dropping portable
coverage.

- `internal/privateedit/workflow_test.go`: use `editorcore` contracts/errors
  rather than `nvimbridge` compatibility aliases for the model editor.
- `internal/privateedit/manager_test.go`: retain `privateManager`, model store
  lifecycle and fixture helpers; split native config/startup branches and native
  test entry points from this mixed file. Port the relevant startup-rejection
  assertions to a generic factory; do not silently lose their coverage.
- `internal/privateedit/generation_test.go`: retain the model generation test;
  split the native entry point and its use of the mixed helper.
- Keep native-only `internal/privateedit/{nvim_compat_test.go,embedded_test.go,
  embedded_resume_test.go}` out of the Vim selection. The last file calls
  `embeddedFixture` despite not directly importing Neovim. Preserve these files
  in the working tree or a separately approved native-experiment change.
- `internal/editorterminal/{model_test.go,queue_test.go,failure_test.go,
  confirmation_test.go}`: replace native-package type/error aliases in portable
  fakes with the matching `editorcore` contracts/errors.
- Keep `internal/editorterminal/pty_fixture_test.go` out of this selection. It is
  an actual native/PTY fixture, not required by the bundled VM.
- `internal/editorcore/dependencies_test.go` scans the privateedit AND terminal
  source directories. Introduce it only once both exist (commit 4 below), or
  separately adjust the test in an explicitly reviewed preparation change.
- `internal/editorterminal/continuation_integration_test.go` imports the actual
  bundled VM, so the terminal test commit must follow the runtime commit.

This preparation is now implemented. Model aliases use `editorcore`; the shared
store/generation helpers accept a generic workflow factory. Native helpers and
entry points were preserved in the separate, unstaged `manager_native_test.go`,
alongside the other excluded native fixtures. Generic startup-rejection coverage
is retained. The staged terminal/private lifecycle dependency listing has no
`internal/nvim*` imports, and its headless/race tests pass.

## Required series

Tests travel with their implementation, not as a catch-all final test commit.
Each final selection needs a fresh compile/test gate; the order below accounts
for the current source, fixture and packaged-asset dependencies.

### 1. `feat(privacy): bind prepared edits to authenticated generations`

Include these six new files:

- `internal/scratchpad/private_edit.go`
- `internal/scratchpad/private_edit_test.go`
- `internal/scratchpad/private_edit_generation_test.go`
- `internal/scratchpad/recovery_pin.go`
- `internal/scratchpad/recovery_pin_test.go`
- `internal/scratchpad/save_content_policy_test.go`

Include the editor-foundation diffs in:

- `internal/scratchpad/privacy.go`: generation install/reset and public recovery
  sealer extraction, including `newAuthGeneration`.
- `internal/scratchpad/scratchpad.go`: generation field and reviewed-save checks.
- `internal/scratchpad/storage.go`: bind encoding to the selected generation/key.
- `internal/scratchpad/save_policy.go`: content-only canonical saver/CAS helper.
- `cmd/sp/recovery_policy.go`: use that canonical content saver for existing
  policy-aware saves instead of duplicating the loading/revision path.

These are prerequisites, not unrelated passphrase rewrapping. The generation is
an in-memory authentication nonce; this commit introduces no key/recovery format.
Gate: scratchpad prepared-edit/recovery/content-policy regressions and relevant
existing save/recovery-policy tests, with disposable fixtures only.

### 2. `feat(editor): add shared in-memory editor lifecycle contracts`

- `internal/editorcore/{driver.go,frame.go,session.go,session_test.go,
  frame_matches_test.go,frame_selection_test.go,frame_status_test.go}`.
- Both files in `internal/editorsandbox/`.
- Production files in `internal/privateedit/`:
  `doc.go`, `embedded.go`, `embedded_receipt.go`, `manager.go`, `workflow.go`.
- Portable `privateedit` checkpoint/driver/model/generation/store-lifecycle tests
  and their retained fixture helpers, after the preparation above.

Hold `editorcore/dependencies_test.go` until commit 4. Do not include native-only
compatibility/embedded fixture files just to make the current test imports work.
Gate: editorcore/sandbox/privateedit headless tests, with native selectors unset.

### 3. `feat(vim): package pinned capability-limited Wasm editor`

- All files under `internal/embeddedvim/`, including its tests, README, canonical
  `assets/{qjs.wasm,guest.js,manifest.json}` and all 15 `assets/licenses/*.txt`.
- All 25 top-level files under `scripts/embedded-vim/`: the two lock/package files,
  adapter/core instrumentation/entry/bundle/build sources, `.gitignore`, README,
  Node harness and all `*.test.js` compatibility suites.
- Only the pinned wazero additions in `go.mod` and `go.sum`.

Do NOT include `scripts/embedded-vim/node_modules/`, legacy `go-proof/assets/`
or the nested legacy demo module as prerequisites. Narrow `.gitattributes`
entries preserve generated/vendor whitespace and binary treatment without
rewriting hash-pinned public program/license bytes. Canonical Go tests check the
fixed top-level JS source hashes, so source/lock/generated program bytes belong
in this same commit. Ordinary Go build must continue to need no Node at runtime.
Gate: canonical Wasm tests, complete JS compatibility suite, module verification,
asset regeneration byte comparison, and CGo-free compilation.

### 4. `feat(editor): add retained-input encrypted terminal coordination`

- `internal/tui/terminal_events.go` and `terminal_events_test.go`.
- All currently modified EOF-observer hunks in `internal/tui/terminal_input.go`.
- Production files in `internal/editorterminal/`:
  `continuation.go`, `doc.go`, `model.go`, `terminal.go`, `view.go`, and all four
  `platform_*.go` implementation files (including unsupported-platform rejection).
- All portable terminal tests, after alias decoupling; exclude
  `pty_fixture_test.go`. Include the actual bundled-VM/encrypted continuation
  test and input rejection/quarantine regressions.
- The delayed `internal/editorcore/dependencies_test.go`.

This includes checkpoint/save controls, exact action authority, pending-input
ownership, reentry quarantine, physical loss consent, safe view/selection layers
and cleanup fixes. It must not introduce native launch/protection fallback.
Gate: shared controller/lifecycle race tests and actual bundled/encrypted fixtures,
without PTY/editor execution. This commit depends on 1–3.

### 5. `feat(cli): add explicit opt-in Vim editing for private notes`

- `cmd/sp/{embedded_edit.go,embedded_edit_test.go,embedded_signals_unix.go,
  embedded_signals_other.go}`.
- Only the `newEditCommand` registration hunk in `cmd/sp/main.go` and the edit
  help case in `cmd/sp/help_test.go`.
- `docs/embedded-vim-integration.md` and `docs/embedded-editor-evaluation.md`.
- This inventory document, if keeping the commit rationale is useful; otherwise
  leave it as a review artifact, not a runtime prerequisite.
- Selected README/Makefile/CI hunks below.

Gate: new command admission/help and bundled encrypted fixture tests; owned
retention before relock; headless shared race tests; fresh CGo-free builds.
Existing `sp`, calendar/notebook defaults and native gates remain unchanged.
Full `cmd/sp` tests can launch external-editor helpers: do not execute those
inside pi. Native terminal checks remain user-owned; prior user acceptance was
for v2, not silently reassigned to subsequent builds.

## Shared-file hunk selection

| File | Include | Leave for other work |
| --- | --- | --- |
| `README.md` | New “Experimental embedded Vim” section | Passphrase, MCP, native prototype history and their target listings |
| `Makefile` | `embedded-vim-assets` target and ONLY its `.PHONY` token | `build-mcp`, `key-terminal`, their `.PHONY` tokens |
| `.github/workflows/ci.yml` | `embedded-vim-compat` job only | Passphrase `key-terminal` step |
| `cmd/sp/main.go` | Edit-command registration | No default routing changes |
| `cmd/sp/help_test.go` | Edit help case | No new unrelated command assertions |
| `go.mod`, `go.sum` | Wazero v1.9.0 / two hashes only | No dependency/toolchain upgrades |
| `cmd/sp/privacy.go` | None | `change-passphrase` command registration |

Makefile `.PHONY` and adjacent target additions share diff hunks with unrelated
work. Whole-hunk/whole-file selection would accidentally include it. Selected
index blobs are built from HEAD plus only the editor additions; the fuller
working-tree files are preserved unchanged.

## Keep separate

- Passphrase rewrapping: `cmd/sp/passphrase*`, `cmd/sp/privacy.go` registration,
  `internal/scratchpad/passphrase*`, `internal/tui/secret*`, `scripts/key-terminal/`,
  `docs/key-lifecycle-design.md`, `docs/privacy.md` passphrase edits, and the
  corresponding README/Makefile/CI portions.
- Shared MCP: `cmd/sp-shared-mcp/`, `internal/sharedmcp/`,
  `docs/shared-mcp-design.md`, its README/Makefile portions.
- Parked native experiments: `internal/{nvimbridge,nvimui,nvimterminal}/`,
  `cmd/sp-nvim-demo/`, `scripts/nvim-terminal/`, native-only test fixtures above,
  `docs/{nvim-integration-plan,macos-editor-sandbox-feasibility,
  macos-editor-sandbox-dts-request}.md`, and native-prototype edits to
  `docs/private-editor-design.md` and README. The shared fail-closed
  `internal/editorsandbox` contract itself IS required and remains included.
- Rex attention bridge: `scripts/rex/` is not a runtime/build prerequisite.
- Ignored handoff/build/dependency files: `TODO.md`, `.privacy-adversarial/`,
  `scripts/embedded-vim/node_modules/`, `scripts/embedded-vim/go-proof/assets/`.
  Do not confuse these with canonical embedded program/attribution assets.

## Optional separate follow-up

`chore(vim): retain synthetic runtime preparation and demo tools` may include
`scripts/embedded-vim/go-proof/{.gitignore,go.mod,go.sum,README.md,cmd/}` after the
required series. Its demo imports the terminal controller and canonical runtime;
it is not needed by SP or the main JS suite. Keep all nested generated demo
executables/assets ignored. If deferred, update/hold final-tree documentation
links to these legacy tools rather than introducing dangling references.

## Verification record

The storage and shared-lifecycle commits were compiled/tested from their exact
index exports. The runtime export passed its Go race suite and all 552 JS tests;
regeneration with an ordinary copied pinned dependency layout is byte-identical.
A dependency symlink changes esbuild's public source-label paths, so it was not
used as reproducibility evidence. The terminal export passed shared/decoder race
tests and has no native test imports. Final CLI export validation passed command/
help/recovery-policy race tests (51.099s), shared controller/lifecycle races,
CGo-free runtime tests (37.188s), vet, module verification, 14 local documentation
targets and native/Linux/Windows compilation. No built executable was launched.
No source-tree-only success substitutes for these selected-tree gates. Legacy demo tools remain deferred and available only
in the working tree; active documentation links target the canonical runtime.
