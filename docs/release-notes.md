# Draft next release — proposed v0.22.0-rc.2

The user pushed v0.22.0-rc.1; its Ubuntu CI reported a terminal-cancellation data
race. This proposed correction leaves that tag unchanged. No rc.2 tag has been
created. Require green hosted CI on the correction before tagging; rc.1's
Release/publication outcome is not established here.

## Correction since rc.1

- Cancel and join active Unix terminal reads before closing internal polling/pipe
  descriptors during forced shutdown. Caller stdin remains open, and literal
  paste/privacy boundaries are unchanged. The dependency patch is locally
  attributed; CI retains race detection and adds 100 Linux cancellation repeats.
- Native Linux verification of the correction is still pending. Local macOS
  race/coverage, repeated regressions and all 14 privacy PTYs pass. See
  [release preflight](release-preflight.md).

## Highlights

- Opt-in encrypted personal notes for every date, including today, with resumable
  activation and explicit confirmed decryption/re-enable. Shared updates remain
  deliberately public and available without unlocking.
- A quieter notebook, separate read-only shared updates, earlier-left/later-right
  navigation, today-first startup, remembered reading positions and direct dates.
- A memory-only private writer with bounded undo/redo, navigation and literal find.
  Shared raw-byte handling preserves marked paste without interpreting it as save,
  template or conflict-confirmation keys.
- Consistent masked unlock, encrypted recovery discovery/review, conflict handling,
  truthful publication/durability outcomes and explicit relock.
- Process-finished external-editor completion in legacy plaintext mode; template
  application owns cancellation and reports whether insertion was published.
- Security/correctness fixes for signal recovery, display controls/entities,
  migration durability and policy races, template descendant-pipe waits and
  shared-first personal creation timestamps. See [remediation](security-remediation.md).

## Important compatibility and privacy limits

- **Windows interactive views, masked unlock and private writing currently fail
  closed** pending a safe console input adapter. Shared CLI operations and bare
  legacy external editing remain available. Windows cross-compilation is not
  native interactive validation.
- Privacy is opt-in. Stop other SP instances and old writers, back up the store,
  and follow [the privacy guide](privacy.md) before migration. Retain the wrapped
  identity metadata with encrypted notes/recovery files and keep the passphrase
  safe. Old writers can damage the newer storage format.
- Decryption deliberately creates plaintext. Re-enable is not key rotation,
  passphrase replacement, revocation or secure erasure. Shared updates, metadata,
  dates and file sizes remain visible.
- No plaintext external-private-editor fallback was added. Neovim integration
  remains a separate design decision. Plaintext and secrets exist in an unlocked
  process; encryption does not protect against unrestricted same-user malware.
- Command templates are trusted user programs, not sandboxed. Escaped descendants
  are not universally contained. Graceful recovery does not cover SIGKILL or every
  terminal, storage or runtime failure.
- Independent cryptographic review, native platform sign-off, and the separate
  provider-blocked live exercise are not complete. Do not label this candidate
  independently audited or fully adversarially validated.

## Release status

Local CI-equivalent checks and packaging evidence are tracked in
[release preflight](release-preflight.md). Hosted CI must pass on the exact
approved commit before tagging. Nothing in this document authorizes a push,
publication, replacement of existing tags, or an update to the stable Homebrew
formula from a prerelease.
