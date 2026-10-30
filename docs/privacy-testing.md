# Privacy verification procedure (synthetic data only)

This is a test procedure for [opt-in privacy](privacy.md), **not a passing
adversarial report**. Actual [results and gaps](privacy-review.md) are recorded
separately, including the provider-blocked live trial. The
[validation follow-up](privacy-validation.md) adds real PTY, bounded fuzz, and
dependency-scan evidence. No independent cryptographic audit is claimed.

## Authorization and isolation first

The operator must explicitly authorize the test, identify a disposable fixture
root, list allowed commands/tools and file access, and set a time budget. All
notes, identities, passphrases, recovery files, and backups must be synthetic.
**Never use real notes or the actual user store.** The runner must select an
isolated test home/store before any SP invocation, including status or help that
might initialize storage. If a safe fixture cannot be selected and verified,
stop; do not try the command against the normal home.

Use the repository's isolated manager/test harness, or a disposable OS user or
container with a verified disposable home. Do not assume a store override flag
exists. Keep fixture artifacts within the authorized root. Respect sandbox
boundaries: on a denial, stop that operation and ask the operator to perform it
manually outside the sandbox. Do not probe, retry, or find another route around
it. Do not enumerate processes. No network, other user files, credentials,
GitHub operations, persistence, real shell/profile modifications, or access to
production backups is authorized by this procedure.

Test direct tampering only on copies explicitly included in the authorized
fixture scope. Same-user malware and OS compromise are recognized limitations,
not permission to attack the host. Stronger experiments need a separately
approved disposable environment and a separately stated threat model.

## Trusted setup and secret separation

A trusted operator/harness, **not the attacker**, creates:

- A synthetic plaintext store containing past dates and today, private Markdown,
  private template IDs and timestamps, and no real personal information.
- Random, high-entropy private canaries whose exact contents are kept outside
  the attacker's accessible files and conversation. A SHA-256 digest may be
  provided for independent verification of a submitted candidate when the
  canary has sufficient random entropy; never do this with guessable real notes.
- A strong, unique test passphrase, kept out of attacker prompts, argv,
  environment, config, readable scripts, terminal transcripts, and logs.
- Intentionally public shared canaries, section names, and provenance labels.
- Copies for plaintext-baseline, migration-interruption, and tampering tests;
  plaintext baselines and oracle data must never be exposed to the attacker.

The trusted harness initializes and verifies the fixture through the manager API,
or an operator performs interactive enable/unlock, keeping secret input and
private screen output out of attacker-visible transcripts. API initialization
is not evidence that the real terminal interaction was tested. Fixtures must not
have shared caches, clipboard, logs, or a writable trusted launcher that would
accidentally hand over the secret. If evaluating an offline-file attacker, run
it against a store copy in a separate environment with no live unlocked SP
process or oracle access. Any reduced isolation must be documented.

### Repository fixture generator

```sh
mkdir -p .privacy-adversarial/home
go run ./scripts/privacy-fixture -dir .privacy-adversarial/home/.sp
go build -o .privacy-adversarial/sp ./cmd/sp
env HOME="$PWD/.privacy-adversarial/home" USERPROFILE="$PWD/.privacy-adversarial/home" \
  .privacy-adversarial/sp privacy status
```

The generator refuses an existing target directory. It generates the passphrase
and private canary in process, checks round-trip decryption and persisted files,
and prints only the location, date, and canary digest. It exits without saving
the passphrase or plaintext canary. There is intentionally no unlock oracle for
this locked fixture; create a separately controlled fixture for unlocked tests.
The fixture directory is gitignored. Never substitute your actual home/store.

## Repeatable local checks

With Go 1.25.13 or newer:

```sh
go test -race ./...
make privacy-terminal  # POSIX only, fresh synthetic homes; never your real store
make privacy-fuzz      # two workers, 20 seconds per target, 64 KiB inputs
make vuln              # scanner built with the selected project toolchain
```

These commands are functional/parser/dependency checks, not a replacement for
the blocked live model-driven trial. Do not retry or rephrase that trial to evade
a provider restriction. Terminal results are redacted and transcripts are not
saved. Preserve explicit distinctions between zero reachable advisories and
advisories elsewhere in imported/required dependencies.

## Deterministic checks

These are acceptance criteria, not claims of existing test coverage. Run against
the candidate revision, record commands, exit status, platform, and observations,
and mark unsupported injection points or UI automation as **not tested**.
Do not implement fault injection by attacking the host.

| Area | Check and expected result |
| --- | --- |
| Opt-in | Opening a legacy fixture without enable does not migrate or alter existing notes. Legacy editing remains plaintext. |
| Confirmation | Cancel before initialization; no active metadata or migrated notes. Exercise mismatched confirmation passphrases and rejected input without leaking input. |
| Key metadata | Successful interactive enable creates wrapped random X25519 identity metadata, not a plaintext private key or passphrase. Verify with a trusted parser; scans alone are insufficient. |
| All dates | Inspect each version-2 daily file, including today. The private payload decrypts to the expected private document only with the trusted identity; public record has no private Markdown/template leakage. |
| Offline guesses | Wrong passphrase fails to unlock. A separate deliberately weak synthetic passphrase fixture demonstrates the offline-guessing limitation; do not mistake one unsuccessful guess for strength evidence. |
| Interrupted migration | Stop/fail before metadata publication, after metadata but before first day, and between days. After publication policy remains active, status reports legacy dates/errors, new private saves encrypt, and enable resumes without data loss or plaintext downgrade. Use controlled harness hooks, not timing guesses where possible. |
| Invalid data | Exercise malformed/oversized JSON and base64, unsupported versions, date mismatches, missing/corrupt metadata, and damaged ciphertext. Private paths fail rather than showing a blank page and overwriting data. Status must not report a false clean bill of health. |
| Shared reads | Locked `sp shared <date>` returns shared content for both historical dates and today, never private content or an unlock prompt. Test missing and legacy dates without exposing private fallback text. |
| Shared appends | Locked append accepts the documented section/source/bullet syntax without private-key access. Verify exact per-section dedup, nonduplicate preservation, and per-operation self-reported provenance. |
| Payload preservation | Trusted harness compares protected private payload before/after shared append; it is unchanged. Private plaintext is not decrypted just to update shared content. |
| Concurrency | Concurrent cooperating appends preserve distinct bullets and dedup exact repeats. Test append versus private save and migration; full read-modify-write locking and revision checks prevent lost updates/stale overwrite, or surface a conflict. |
| Tampering | On disposable copies, corrupt ciphertext, edit shared/provenance/public metadata, delete notes, and replay valid older records. Corrupt encrypted payload must fail authentication; public edits and valid rollback are not claimed to be cryptographically prevented. Record actual validation errors. |
| TUI | Locked calendar/notebook does not preload private previews; `u` masks unlock; only current-page private reads decrypt lazily. Calendar Enter opens notebook; private edit uses the built-in editor, not `$EDITOR`. Verify displayed bindings against the candidate build. |
| Editor isolation | A harmless external-editor sentinel in the fixture must not be invoked for privacy edits. Check authorized fixture temp files/logs for private canaries, but report that a scan cannot prove no plaintext copies ever existed. |
| Recovery | Inject save conflict/failure, then recovery failure, through controlled test hooks. Successful private recovery is encrypted to the public recipient; failure keeps in-memory edits available for retry and does not report a nonexistent recovery file. Verify retained text through the trusted harness. |
| Durability | Exercise pre-publication and post-replacement sync errors. Distinguish failed save from replacement whose crash durability is unconfirmed. Do not promise rollback after publication. |
| Process scope | A second fresh SP process remains locked; no key/passphrase is persisted in config, args, or environment. Closing/reopening requires unlock. No claim that Go erased every old memory copy. |
| Backup | Restore daily files with matching global metadata and trusted passphrase in isolation. Demonstrate that removing the identity backup or withholding the passphrase prevents private recovery. Keep a usable baseline outside the attacker's scope. |

Compare expected private plaintext only inside the trusted harness. Include
empty days, Unicode, repeated sections/bullets, and multiple append sources.
Name the actual OS/filesystem for locking and durability checks; success on one
platform is not evidence for all platforms.

## Fresh same-model black-box adversarial exercise

1. Finish trusted setup, deterministic checks, and scope approval first. Start a
   **fresh attacker session using the same model/version as the implementation
   agent**, with no implementation conversation, secret passphrase, private
   canaries, identity, oracle, setup logs, or plaintext baseline. Record any
   difference in model/version rather than silently calling it same-model.
2. Provide only public user/security docs, the candidate executable, an encrypted
   synthetic fixture copy, authorized shared hooks, and the explicit permission
   scope. For this black-box exercise, withhold implementation source and internal
   unit tests; list whether executable inspection is allowed. A source review is
   a separate activity and must be labeled as such.
3. State the objective: recover exact synthetic private content from the permitted
   observations **without the passphrase**, or demonstrate a path that makes a
   documented no-unlock hook disclose private data. Permit bounded malformed-input
   and direct-file tests only within the disposable scope. Keep public shared
   history available: learning it is intended behavior, not a privacy break.
4. Let the attacker interact using only the enumerated tools and budget. The
   trusted harness, not the attacker, verifies submitted private-canary candidates.
   Do not give the attacker an unlimited oracle or reveal secrets in feedback.
5. Stop at the budget/scope boundary and preserve redacted observations. A denial
   is final, not an invitation to explore another interface. If the attempt
   reveals fixture setup secrets or obtains unintended access outside scope,
   stop and report contamination; do not count it as a clean SP result.
6. Have the trusted operator verify alleged findings independently on fresh
   fixture copies and classify them against the documented boundary. Add a
   separate report. Do not alter docs to claim success solely because the
   attacker timed out or asserted that encryption looked strong.

A replay, deletion, forged source label, public shared-history read, or inference
from dates/file sizes demonstrates a documented limit, not successful private
ciphertext decryption. Still report these observations, especially if any UI or
error message falsely suggests a stronger guarantee. An unexpected private
plaintext leak, private-key exposure, plaintext recovery file, or overwrite of
private data through a supported shared hook is a finding requiring investigation.

## Required report

Record at least:

- Candidate commit/revision, build identity, Go/age versions, OS/filesystem,
  fixture design and isolation method; identify uncommitted changes.
- Implementer and attacker model/provider/version, fresh-context status, available
  tools, actual tool permissions, black-box/source visibility, and any deviation
  from same-model setup.
- Explicit authorization scope, start/end time, wall-clock/tool-call/token budget
  and consumption where available.
- Deterministic tests actually executed with pass/fail/skip and command exit
  statuses; fault injections and observations; distinguish assertions from tests.
- Attacks actually tried, allowed artifacts observed, reproducible findings,
  trusted-oracle confirmation, failures, sandbox stops, and untested avenues.
- Secret/transcript handling, redactions, fixture cleanup or retention, and any
  setup contamination. Never publish the test passphrase or private canaries in
  a report that another attacker session will consume.
- Conclusion limited to the observations: for example, “No private-canary recovery
  observed within this budget and scope,” **not** “secure,” “audit passed,” or
  “same-user malicious agents cannot read notes.”

## Limits of this evidence

Deterministic tests exercise chosen paths, not every interleaving, failure mode,
side channel, parser input, dependency, or OS. A finite model-driven attack is
neither an independent cryptographic audit nor proof that offline guessing,
malicious same-user code, future unlock capture, swap/core dumps, terminal
recording, rollback, or metadata leakage is prevented. A strong synthetic
passphrase says nothing about a user's real passphrase. Treat any later audit
as separately scoped evidence, not a retroactive claim about this procedure.
