# Experimental opt-in embedded Vim

## Application entry point

```sh
sp edit --editor=vim                 # today's private note
sp edit --editor=vim 2000-01-01       # one selected date
sp edit --editor=builtin 2000-01-01   # existing private editor
```

Privacy must already be enabled. Unlock uses the existing foreground-terminal
passphrase prompt; no passphrase flags, environment or redirected input. There
is no external-editor fallback. Date/editor/foreground-terminal/public-asset
admission precedes normal-store preparation. Installed program code comes only
from linked public artifacts, never CWD paths, Node or `$EDITOR`.

Normal `sp`, notebook/calendar editing, configuration and external plaintext
editing remain unchanged. This first integration is a standalone dated command,
not a notebook/calendar editor preference. The Vim path is explicit experimental
opt-in; it is NOT independent security/release approval or OS enforcement.

## Lifecycle and failure ownership

`:w`/Ctrl-S checkpoint encrypted content and continue the same history. `ZZ`,
`:wq`, `:x` or F2 finish; `ZQ`, `:q!` or F3 request cancellation, never loss consent.
Changed cancellation requires physical confirmation and durable encrypted
recovery. Canonical generation/CAS conflict checks, relock revocation and pinned
recovery sealing use the existing privateedit/scratchpad implementation. Shared
content is not handed to the guest or overwritten by editor saves.

Guest faults, rejected saves and ambiguous durability stay in the same terminal
recovery UI. If the terminal program itself exits with an owner/uncertain input,
the command retains both, keeps the manager unlocked and asks for Enter to
reopen recovery controls for the SAME owner—never a new loaded edit. Keys are
quarantined for recovery only. Neither manual R retry/resolve, a later input
receipt, nor a late frozen action may clear or replay them: the receipt may belong
to a subsequent recovery request rather than those events. K then a fresh physical
y allows possible-loss release after durable recovery/close; paste cannot authorize
it. With no retained input, explicit resume remains available. Failed recovery
keeps input and requires renewed consent. A continuation is manager/date/owner
bound, single-use and cannot be redirected to a native driver.

Catchable termination signals remain owned between terminal programs too; HUP
or TERM while awaiting recovery is not discard consent. If input is unavailable,
the process remains alive holding the draft; a physical
interrupt permits another recovery-input attempt. It never discovers a fallback
terminal or treats EOF as discard permission. Hard kill/crash/power loss can still
lose RAM. This unusual terminal-loss behavior needs manual acceptance, not a
claim of crash-proof persistence or finished native-platform assurance.

## Review scope

The focused integration review checks command admission, bundled assets, native
fail-closed separation, retained-owner/input recovery, and documentation/build
consistency. It is not an independent security audit or a full native-platform
review. A regression reproduced later receipts incorrectly settling older keys
after reentry; recovery-only input quarantine now prevents that. Shared resize/
confirmation screens no longer falsely label application notes as synthetic.
Already-cancelled commands stop before terminal/store admission. A separate
regression covers over-budget paste: proven pre-execution rejection no longer
traps a healthy editor. Actual VM/encrypted fixture tests check both recovery
and rejected-paste behavior without launching a terminal. Review verification
passed the 552-test JS suite, runtime/controller/application race regressions,
CGo-disabled tests, vet and module verification. A fresh v3 binary was built but
not launched; the user-reported smoke acceptance above applies to v2.

## Limits

The note limit is 1 MiB (larger notes reject without truncation/fallback). A
synchronously rejected oversized paste leaves a healthy editor usable with a
visible rejection notice. Only that operation's captured, known zero-acceptance
receipt qualifies; generic errors, partial/unknown input and later receipts do
not grant release. The
engine is capability-limited QuickJS/Wasm, not an OS-enforced native sandbox.
Paging, dumps, display capture, the trusted interpreter/Go host and aggregate
resource budgets remain limitations. Native `editorsandbox.Require` and
`Driver.OpenProtected` gates stay fail-closed. Compatibility differences, including
R Backspace not restoring overwritten text, remain in the
[compatibility evaluation](embedded-editor-evaluation.md).

## Manual validation — outside pi

**User-reported smoke acceptance:** the user ran integration v2 outside pi and
reported the entry point working, then confirmed all seemed to work after the
requested checkpoint/continue/finish/reopen and cancel/recovery checks. This is
manual user feedback, not independently observed fault handling, a full platform
matrix or security approval. Vim remains explicitly opt-in.

First use a disposable home/store, not your normal notes. Build a fresh versioned
binary (never overwrite a possibly running demo). From your own terminal at the
checkout, with the reviewed integration binary already built. Resolve the root
explicitly so these commands also work from the legacy `go-proof` directory:

```sh
sp_bin="$(git rev-parse --show-toplevel)/.privacy-adversarial/sp-vim-integration-v3"
fixture_home=$(mktemp -d)
HOME="$fixture_home" "$sp_bin" privacy enable
HOME="$fixture_home" "$sp_bin" edit --editor=vim 2000-01-01
```

Choose a throwaway passphrase at the terminal prompt; enter only synthetic text.
Try typing, visual edit, undo, `:w`, more editing, `:wq`, and reopening the same
command. Then try edited `:q!` and its physical confirmation, checking the reported
encrypted recovery with the existing privacy recovery commands under the SAME
fixture home. Do not delete the fixture until all runs are closed and any
recovery you care about is resolved. Do not mistake headless tests for this
manual terminal validation. No app/editor/terminal launch is performed by pi.

Headless application tests inject only terminal admission/transport at command
construction, using actual linked VM + prepared encrypted fixtures. They cover
checkpoint, cancel consent, relock, conflict, retention/reentry ordering and
canonical bytes. Shared controller tests exercise uncertain-input retention and
physical loss consent. Public assets, licenses and the pinned wazero dependency
are part of the normal Go build; Node remains only build/test tooling.
