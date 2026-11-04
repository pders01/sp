# Privacy validation follow-up — 2026-10-30

**Historical report:** the candidate, hashes and local checks below describe the
2026-10-30 validation snapshot, not the v0.22.0 release artifacts. See the
[current release record](release-preflight.md) for later fixes, owner-reported
green native matrix CI and the stable tag. Earlier failed/pending CI statements
below are historical; broader platform assurance and the provider-blocked trial
remain separate from those subsequently reported CI passes.

**Local deterministic, terminal, race, lint, and bounded fuzz checks pass. The
live model-driven trial remains provider-blocked and has not been retried.**
This historical follow-up does not complete separate adversarial security
validation or constitute an independent cryptographic audit. See the [initial review](privacy-review.md) for
its scope, findings, and the blocked trial.

## Historical candidate and scope

- Validation used then-uncommitted source changes based on
  `9b906c63279534754cce3caaa97d71918e20e9ae`. Subsequent review commits package
  those changes; the recorded hash identifies the pre-commit validation build.
  VCS metadata can change executable hashes without changing source behavior.
- Final tested executable SHA-256:
  `4315041ccd7023f45946553febc587e057b6105f8ff55872abc190b372b7d1e4`.
  Built with `go build -o .privacy-adversarial/sp-validation ./cmd/sp`.
- Local platform: macOS, `go1.25.13 darwin/arm64`; filesystem type not independently
  recorded. Encryption format/library remains age, `filippo.io/age v1.2.1`.
- Implementer and fresh scoped validation workers: `openai-codex/gpt-6.1-sol`.
  Workers reviewed code and wrote ordinary deterministic regressions; they did
  not perform a replacement live model-driven trial.
- All note stores, passphrases, identities, and recovery files were synthetic.
  No actual user notes were read, migrated, installed over, or modified.

## Findings fixed

The follow-up found real correctness/data-loss defects even though the earlier
unit suite passed. None of these tests demonstrated recovery of encrypted private
content without the passphrase.

| Finding | Fix and evidence |
| --- | --- |
| Editor silently retained only 99 lines | Replaced the normalizing text-area component with a byte-preserving in-memory input. No line-count cap; 8 MiB input bound rejects explicitly rather than truncating. Regression tests and a real PTY check cover a 142-content-line note through its final line. |
| Tabs/CRLF changed merely by opening a note | Raw UTF-8 text is kept separately from its safe display projection. Untouched save/cancel does not rewrite data. Tests exercise edits, grapheme navigation/deletion, mixed line endings, tabs, and trailing lines. |
| Batched literal text could act as a special key | Modal commands now dispatch on physical key type. Text such as `enter`, `esc`, and `ctrl+s` is inserted literally. |
| Invalid passphrase paste could become a different sanitized secret | Raw single-line UTF-8 is checked before input sanitization; invalid/oversized input is rejected and cleared on submission. A separate masked projection never renders the actual secret. System clipboard commands are disabled; terminal bracketed paste is supported. |
| Retry after published-but-unconfirmed save used stale baseline | A publication durability warning advances the editor's baseline while preserving a retry/recovery requirement. Pre-publication failures retain the old baseline. |
| Published recovery path disappeared when its directory sync failed | Diagnostics retain both path and wrapped durability error; the running editor keeps the buffer when durability remains uncertain. |
| Recovery accepted missing/null private fields | Daily notes and recovery now share the same strict private-document validation. Invalid recovery never becomes a successful empty replacement. |
| Lone escaped UTF-16 surrogates were silently replaced during migration | Strict JSON rejects unpaired surrogate escapes before Go's decoder can replace them. Valid pairs and literal U+FFFD remain valid; rejected files are not rewritten. |
| A symlink or case-aliased date path could hide orphan ciphertext from policy checks | Privacy checks/migration/status reject date-named symlinks, special files, and noncanonical `.JSON` casing. Bounded data reads require regular files. Regressions use only synthetic in-store fixtures. This is not a claim of race-free protection against active filesystem replacement. |

## Real terminal integration

Executed after the fixes:

```sh
go build -o .privacy-adversarial/sp-validation ./cmd/sp
python3 scripts/privacy-terminal/main.py --binary .privacy-adversarial/sp-validation
```

The final run invoked these commands through `make privacy-terminal`.

The final POSIX pseudoterminal run passed **11 checks** in about 96 seconds:

1. Cancel activation without changing the legacy store.
2. Reject mismatched passphrase confirmation without creating key metadata.
3. Activate/migrate past, today, and future notes; hide passphrase input and
   restrict published note/key file permissions.
4. Start the notebook locked, reject a wrong passphrase, then unlock the selected
   private page through the masked prompt.
5. Open a long CRLF/tab document through its last line and confirm an untouched
   save leaves the ciphertext byte-for-byte unchanged.
6. Edit/save privately without invoking an external-editor sentinel or finding
   private markers in SP's persisted files or the fixture temporary directory.
7. Require a new unlock in a fresh process and read the saved edit.
8. Cancel changed text only after confirmation, creating encrypted recovery while
   retaining the current daily note.
9. Review/save recovery interactively; retain its artifact and verify restored text.
10. Preserve a shared append made while the private editor is open.
11. Restore matching wrapped identity and encrypted notes into another synthetic
    home, unlock, and verify the restored private text.

The harness creates a fresh home under `.privacy-adversarial/`, generates secrets
in process, and never accepts the real store as a target. It checks raw and
ANSI-stripped terminal output for passphrase echo. Raw transcripts and passphrases
are not saved; only a redacted result JSON is retained. Expected private text
exists in the trusted harness and unlocked test process, so this is **functional
integration testing**, not a blind confidentiality challenge. File scans cannot
prove that no transient plaintext copy ever existed.

## Bounded fuzz checks

Four campaigns passed with 64 KiB input bounds, two workers, 20 seconds each,
and no passphrase KDF inside a fuzz callback:

| Target | Executions observed |
| --- | ---: |
| `FuzzDecodeRecord` | 892,594 |
| `FuzzStrictJSONCanonicalRoundtrip` | 1,357,515 |
| `FuzzAppendBullets` | 1,275,005 |
| `FuzzValidateAgeEnvelope` | 970,665 |
| Total | 4,495,779 |

Reproduce the campaigns with `make privacy-fuzz`, which runs each named target
with `GOMAXPROCS=2`, `-run='^$'`, `-fuzztime=20s`, and `-parallel=2`.

Invariants cover accepted-record round trips, opaque private-payload preservation,
canonical JSON, forbidden-field mutations, byte-preserving insertion, repeat
append idempotency, header type/size bounds, and rejection without panics. These
short campaigns found no failures; they are not exhaustive fuzzing or a proof
that the parsers have no defects.

## Dependency and toolchain findings

The original Go 1.24.10 scan reported seven conservatively reachable advisories:
`GO-2026-6218`, `GO-2026-6088`, `GO-2026-5970`, `GO-2026-5320`,
`GO-2026-4602`, `GO-2026-4601`, and `GO-2025-3595`. Reachability is not proof of an
application-specific exploit, but these were treated as upgrade blockers.

Changes:

- Minimum Go raised from 1.24.4 to **1.25.13** for patched standard-library code.
- `goldmark` updated to v1.7.17, `x/net` to v0.38.0, and `x/text` to v0.39.0,
  with their selected transitive dependency updates.
- CI/release select the version from `go.mod`; lint uses a migrated v2 config and
  `golangci-lint v2.5.0` to support Go 1.25. Existing lint checks were not weakened.

The final scan exited successfully:

```sh
GOTOOLCHAIN=go1.25.13 go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
# or: make vuln
```

It reported **zero reachable vulnerabilities**, but also **10 advisories in
imported packages and 38 in required modules without detected call paths**.
Do not describe this as an entirely advisory-free dependency graph. Results are
limited to the current vulnerability database and static call-graph analysis;
future code paths or database updates can change them.

## Other final checks at this snapshot

Passed against the updated source:

```sh
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
GOTOOLCHAIN=go1.25.13 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 run --timeout=3m ./...
GOOS=windows GOARCH=amd64 go test -exec=true ./...
GOOS=linux GOARCH=amd64 go test -exec=true ./...
```

Lint reported zero issues. The Windows/Linux commands validate compilation only.
The Unix symlink regression is explicitly skipped on Windows because the test
cannot assume symlink-creation privileges.

At this snapshot, CI configuration included runtime race tests across
Linux/macOS/Windows, Unix PTY checks, bounded Linux fuzzing and a reachable-advisory
gate, but no successful remote run had yet been supplied. That was a historical
evidence gap, not the current status: later green CI reports are recorded in
[release preflight](release-preflight.md). Configuration alone remains distinct
from passing runtime evidence.

## Historical Windows CI follow-up

User-supplied logs from CI run `41132250043` showed two Windows failures:
PowerShell split an unquoted coverage argument and Go treated `.out` as a package;
the migration test also interpreted Windows `FileMode` bits as Unix permissions.
The workflow now quotes the entire coverage flag, and the POSIX permission-bit
assertion runs only on non-Windows hosts. Migration, ciphertext/non-disclosure,
regular-file, wrong-passphrase, and metadata-preservation assertions still run
on Windows. Go's `0666` mode does not validate or describe an NTFS DACL; see the
[platform permission limits](privacy.md#storage-and-cryptography).

That supplied run contained many passing Windows runtime tests but failed overall.
A successful rerun was required at the time. The owner later reported native
matrix CI green after further Unix-reader and Windows-assertion corrections;
see the current preflight. Cross-compilation and Unix runs were not substituted
for that owner-reported Windows CI result.

## Platform-validation boundary

A follow-up attempt to inspect local Docker configuration was denied by the
host sandbox. That operation was stopped without retrying or using another
access route. No container was launched and no Linux runtime result was obtained.
The later hosted results were supplied by the release owner and are recorded
separately in the current preflight; they do not turn this blocked local attempt
into container-execution evidence.

## Still open

- The provider-blocked live same-model trial, including the final candidate.
- Independent cryptographic/dependency review, longer fuzzing, and exhaustive
  lifecycle/error interleavings.
- Broader native GUI/accessibility, Windows interactive/ACL, and filesystem/
  power-loss validation beyond unit CI and local controlled error injection.
- A hostile same-user process, future-unlock tampering, OS compromise, memory/swap/
  core dumps, terminal recording, rollback, deletion, public metadata/provenance
  authenticity, and old plaintext backups remain outside the documented promise.

Validation performed no automatic activation, real-data migration, installation,
or release. Changes may subsequently be committed for review; that does not
complete the remaining security gates. Read the [privacy guide](privacy.md)
before opting in.
