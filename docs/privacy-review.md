# Privacy implementation review — 2026-10-30

This is the initial review. The [validation follow-up](privacy-validation.md)
records later defects/fixes, the patched toolchain/dependencies, actual terminal
checks, bounded fuzz results, and the latest candidate identity.

**Status: automated checks passing; same-model source review completed; live
model-driven trial incomplete (provider-blocked). The full adversarial release
gate is NOT satisfied.** This is not an independent security/cryptographic audit
or evidence of protection from unrestricted same-user malware.

## Candidate and environment

- Repository base: `9b906c63279534754cce3caaa97d71918e20e9ae`, plus uncommitted
  privacy implementation changes. This report does not describe a released build.
- Go: `go1.24.10 darwin/arm64`. Encryption library: `filippo.io/age v1.2.1`.
  Filesystem type was not independently recorded.
- Final locally built candidate SHA-256:
  `4e2bd00292e70c065b8c9693e05bcfbb70f298b68f6dba2e9c1f2fc6bbb9d8c4`.
  Build: `go build -o .privacy-adversarial/sp-final ./cmd/sp`.
- Implementer and delegated reviewers: `gpt-6.1-sol`, provider `openai-codex`.
  Reviewers received fresh, scoped assignments, not the implementation conversation.
- Only disposable synthetic stores were used. The actual user's notes were not
  opened, migrated, or modified. Test artifacts are retained under the gitignored
  `.privacy-adversarial/` directory, not in the normal user store.

## Automated evidence

These commands exited successfully:

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
GOOS=windows GOARCH=amd64 go test -exec=true ./...
GOOS=linux GOARCH=amd64 go test -exec=true ./...
git diff --check
```

The Windows and Linux commands are **cross-compilation only**, not runtime testing
on those platforms.
The local race run does not establish correctness for every interleaving or OS.

Covered assertions include:

- Legacy opt-out behavior; explicit activation/confirmation; mismatched or
  cancelled passphrases leave privacy disabled; noninteractive enable/recover
  rejects input before initializing storage.
- Age encryption/decryption, wrong passphrases, authenticated date binding,
  private template metadata, encrypted recovery, and no plaintext private fields
  in encrypted persisted documents.
- Durable activation before resumable partial migration; migration verification;
  corrupt, missing, or changed key metadata; unknown formats/versions; no
  plaintext fallback on private saves.
- Locked shared reads/appends, ciphertext preservation, per-section exact
  deduplication, source records, concurrent writers, stale revisions, and
  post-publication durability errors.
- JSON case-alias rejection, invalid UTF-8, size/header/scrypt bounds, and
  preservation of a rejected document rather than lossy rewriting.
- Masked UI unlock, supported passphrase-length boundaries, lazy private reads,
  separation of private and shared projections, no private template output in
  calendar caches, and no SP-created plaintext editor file in private mode.
- Recovery recipient pinning; failed recovery retaining in-memory text;
  explicit recovery date/path validation and ciphertext authentication.

An executable smoke check used an explicit disposable `HOME`/`USERPROFILE` to
run status, append twice, and read shared content. A trusted comparison confirmed
that the private envelope was unchanged, the repeated bullet occurred once,
and only one new contribution was recorded. No unlock or private output was
needed. Mocked prompts and model-update tests are not a real interactive terminal
walkthrough.

## Completed same-model source review

A separate read-only reviewer inspected storage, parsing, migration, CLI, UI,
recovery, and documentation. Scope excluded personal files, credentials,
network access, and host attacks. Budget: 900 seconds; observed duration about
149 seconds with 51 tool calls. Source was changing during this review, rather
than pinned to the final binary. The reviewer executed no runtime attacks.

Findings and follow-up:

1. **JSON case aliases could bypass exact-name validation.** Go's struct decoder
   accepts case-insensitive keys; a later `Private: null` could shadow a canonical
   encrypted `private` field and cause a normal append to discard ciphertext.
   This is a preservation/integrity defect, not plaintext recovery. Canonical
   lowercase ASCII field names are now required, including rejection of Unicode
   case-fold aliases. Regression tests verify rejection without file rewriting.
2. **TUI passphrase truncation.** Its original input limit was smaller than the
   accepted storage limit. UI capacity and explicit byte-limit rejection now
   cover the supported range. Activation also rejects non-text/control-character
   passphrases that cannot round-trip through the UI.
3. **Recovery recipient substitution (defense in depth).** A metadata change
   during editing could redirect automatic recovery to another public recipient.
   The live manager now pins the original public recipient after unlock, including
   across `Lock`. Tests show replacement metadata cannot redirect that recovery.
   Fresh-process metadata authenticity and arbitrary same-user tampering remain
   outside the guarantee.

The findings were source-derived. Final regression tests exercise the fixes;
this report does not claim a completed pre-fix black-box reproduction or a
post-fix model-driven pass.

## Attempted live model-driven trial: incomplete

`scripts/privacy-fixture` generated a random 256-bit passphrase and random private
canary in process, migrated a synthetic historical note, verified decryption and
persisted-file scans, created shared content and encrypted recovery, then exited.
It printed neither the passphrase nor the canary. The fresh reviewer was given
only the canary's SHA-256 digest, fixture location, candidate executable, and
scope; initial black-box work was to precede any source-assisted review.

The candidate provided to this attempt preceded the fixes above; its SHA-256 was
`6d7c1ed037f891e38ebb8998a9cbb12429204a42f1bc7e745b99eca23e186390`.

The authorized scope allowed disposable fixture copies, bounded input tests,
and executable inspection; it prohibited real notes, outside credentials,
network access, process inspection, host modifications, and sandbox workarounds.
All SP invocations were required to select a synthetic home explicitly. Budget:
600 seconds; the worker ended after about 55 seconds and 5 tool calls with a
provider cybersecurity safety-filter error. **No completed attack report or
verified private-canary recovery was produced.** There was no retry or attempt
to evade the provider restriction.

This is neither an observed successful disclosure nor evidence that the candidate
resisted a completed attack. The worker also operated within the host sandbox;
it was not an unrestricted same-user OS-security experiment.

## Remaining validation and limits

Before describing this as adversarially validated, a permitted, completed fresh
model-driven exercise against the final candidate is still required. Also not
established here:

- Independent cryptographic/dependency audit or exhaustive parser/fuzz testing.
- Real terminal end-to-end enable/unlock/edit/recover automation.
- Linux/Windows runtime locking, power-loss behavior, or filesystem-specific
  durability beyond controlled injected errors and local tests.
- Protection against malicious modification of SP, future passphrase capture,
  debugger access, OS swap/core dumps, or terminal recording.
- Rollback prevention, authenticated source labels/public metadata, or erasure of
  old plaintext backups, snapshots, editor files, and filesystem remnants.
- Comprehensive backup-restoration drills or availability under hostile writers.

Use the [privacy guide](privacy.md) for the precise at-rest confidentiality promise
and [verification procedure](privacy-testing.md) for further scoped testing.
