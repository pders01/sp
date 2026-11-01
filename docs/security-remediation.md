# Combined code/security review remediation — 2026-11-01

The review covered the entire application at `6ca8c14`, with a close pass over
its latest 30 commits (`b15d1b0..6ca8c14`). Eight confirmed findings led to the
changes below. The reviews used fresh scoped workers on the same provider/model,
not independent security auditors. All stores, notes, secrets and editor
configuration used for verification were synthetic.

The subsequent [release preflight](release-preflight.md) records configured v2
lint, CI race/coverage checks and packaging results beyond the historical
remediation candidate below. Platform and security-release gates remain separate.

## Findings and fixes

| Finding | Remediation |
| --- | --- |
| P1: standalone signal shutdown could return success and discard an unfinished draft | Graceful interruption seals recovery before leaving. Recovery failure retains the live writer; its signal subscription remains active for later attempts. Unexpected nil-error exits cannot claim completion. |
| P1: pasted controls could save or confirm conflict replacement; terminal decoding also lost valid bytes | A shared bounded raw-byte owner precedes Tea's decoder. Marked paste reaches typing contexts as one literal message; read-only/help/confirmation/template contexts cannot treat it as keys. CRLF, LF, tabs, valid U+FFFD and literal controls remain exact. Invalid/oversize paste is rejected atomically; invalid secret input cannot authenticate a prefix. |
| P2: HTML entities could recreate terminal controls after escaping | Dangerous entity references are projected before Markdown styling, with deferred ampersands to prevent nested/HTML-assembled references. A post-transform output gate allows only trusted renderer SGR. |
| P2: locked calendar annotations emitted raw controls | Annotation text is escaped before styling, with a composed-output boundary. Public shared data does not need an unlock to receive protection. |
| P2: enable retries skipped confirmation of previously unconfirmed durability | Existing metadata is synced before migration; final policy, current records and directory durability are verified under the store lock, including no-op retries. |
| P2: descendant-held command pipes defeated cancellation/timeouts | Output draining is bounded. Darwin/Linux retain an unreaped group leader while draining, permitting safe cancellation even after leader exit. Pipe-copy goroutines are joined before inspecting output. |
| P2: cancellation/retry test assumed a particular goroutine schedule | Explicit barriers exercise work-start-before-cancel and cancel-before-work-start; the fake applier waits for cancellation. |
| P3: first personal save on a shared-only date retained a zero creation timestamp | Creation time is initialized only at first private publication, including templates and published-but-unconfirmed saves; existing timestamps/read-only opens are unchanged. |

Follow-up review also closed repeated-signal recovery failure, identical-metadata
policy ABA, cancellation after group-leader exit, and case-normalized URL marker
collisions. Entity-marker alphabet exhaustion uses literal Markdown rather than
an ambiguous/expanding search. Integration checks fixed rapid Escape/physical
control ordering and a late theme-subscription shutdown race.

Stored Markdown and private/shared ownership are not normalized by these fixes.
No plaintext external-private-editor fallback, key rotation, secure-erasure
claim, or Neovim architecture change was introduced.

## Input compatibility and limits

Bubble Tea is now v1.3.10 for file-interface/raw-mode/suspend lifecycle support.
Its decoder is not trusted to preserve arbitrary secret/document bytes: the
application-owned reader retains the original messages and supplies only ordered
transport events to Tea. There is no input-pump goroutine or system clipboard
command. External editors receive real file stdin, avoiding an input-copy
routine that could consume resumed TUI input.

A lone Escape resolves after 75 ms. Known partial protocol prefixes remain
quarantined; late completion cannot release pasted controls as shortcuts.
Unsupported escape/Alt protocols are explicitly rejected rather than rewritten.
This is a terminal-protocol boundary, not authentication of an OS input source
or protection against a compromised terminal/same-user program.

**Windows interactive flows currently fail closed.** Tea's Windows console
reader can bypass the byte wrapper. Notebook/calendar, masked unlock and the
built-in writer therefore refuse that unsafe fallback. Shared append/read/status
and bare legacy external editing remain available. Restoring interactive Windows
support requires a safe console adapter and native verification; cross-builds
are not evidence that this work is complete. The enabled byte path was exercised
locally on macOS; Linux/BSD runtime behavior is not established by these results.

Interactive views/private writers also require actual terminal input and output.
Redirected secret input and captured private screens are rejected. Raw shared
CLI output remains deliberately raw.

Command templates remain trusted opt-in programs, not sandboxed. The default
10-second deadline has an additional 250 ms output-drain bound, with 1 MiB retained
per stream. Escaped groups/sessions are not contained. Other platforms use
immediate-child cancellation plus bounded pipe waiting, not a process-tree kill
guarantee. Graceful draft recovery cannot survive SIGKILL, process loss, or every
terminal/runtime/storage failure. Go memory erasure and physical power-loss
behavior are not guaranteed.

## Verification

Final application source through `31a5ad2`, with the added privacy PTY checks and
documentation in the working tree, produced tested candidate SHA256:

```text
c492e6171d5c84b980678e4aa3562fdfc077b83c2d1675feeaf5ea64a75f4936
```

The final `./sp` is rebuilt after commits; VCS build metadata can change its hash
without changing application behavior. This evidence does not identify a
published release.

Passed on local macOS arm64 / Go 1.25.13:

- Full uncached normal and race suites, vet, and whitespace checks.
- Repeated race checks: 100 cancellation/retry, Escape ordering and theme-close
  repetitions; focused process-group/pipe interleavings and signal failures.
- Actual reader tests across tiny chunks and every two-read split, invalid UTF-8,
  bounded/unterminated paste, ownership layers, caller-fd retention, and editor
  suspend/resume. No synthetic rune-only test is substituted for byte evidence.
- **14 privacy PTY checks**, including repeated SIGTERM with intentionally failed
  recovery, explicit save retry, exact Unicode/space secrets, literal multiline
  control paste, ciphertext preservation and encrypted recovery import.
- **9 integrated UX workflow groups** and **4 dark/light/wide/narrow design PTYs**.
- Four bounded storage fuzz campaigns (about 20 seconds each), 402 frame-bound
  exports under race, and six Python renderer tests. The full PNG atlas was not
  regenerated by this remediation.
- Windows/amd64 and Linux/amd64 cross-compilation only.
- govulncheck v1.1.4: **0 detected reachable vulnerable calls**; 10 imported-package
  and 38 required-module advisories still have no detected call paths. This is
  not a declaration that dependencies are vulnerability-free.

Matching final-candidate fixtures under ignored `.privacy-adversarial/`:

- `terminal-14ccf14cf23728c3`
- `ux-terminal-20b841e3cfe658b1`
- `design-terminal-2ab748badf860bb4`

No terminal transcripts were saved. The initial review/reproduction artifacts
remain local under `review-parent/`, `review-tui/`, `review-storage/`,
`review-build/`, `review-cli/` and `review-remediation/` in that ignored directory.
Earlier intermediate failures were investigated, not counted as final passes.

Installed golangci-lint v1 is incompatible with the project's v2 configuration;
vet is not a full configured-lint sign-off. No native Windows/Linux/GUI runtime,
physical power-loss experiment, remote CI/release publication, independent
cryptographic/dependency audit, or completed live model-driven trial is claimed.
The provider-blocked trial was not retried or worked around. Those release and
architecture gates remain separate from fixing these source findings.
