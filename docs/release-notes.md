# SP v0.22.0

Promotes the application validated through v0.22.0-rc.3 to the stable release and
Homebrew channels. Earlier release-candidate tags remain unchanged. Stable is a
release-channel designation, not an independent security audit or a claim of
Windows interactive support.

## Release-candidate corrections

- Cancel and join active Unix terminal reads before closing internal polling/pipe
  descriptors during forced shutdown. Caller stdin remains open, and literal
  paste/privacy boundaries are unchanged. The dependency patch is locally
  attributed; CI retains race detection and adds 100 Linux cancellation repeats.
- Make command-start error assertions and quoted recovery-path checks portable
  across Windows and Unix, including drive, UNC and control-containing paths.
  Runtime command execution and recovery output are unchanged.
- Hosted CI and the rc.3 GoReleaser workflow passed, as reported by the release
  owner. See [release preflight](https://github.com/pders01/sp/blob/v0.22.0/docs/release-preflight.md) for the distinction between
  these results, historical local evidence, and remaining assurance limits.

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
  shared-first personal creation timestamps. See [remediation](https://github.com/pders01/sp/blob/v0.22.0/docs/security-remediation.md).

## Important compatibility and privacy limits

- **Windows interactive views, masked unlock and private writing currently fail
  closed** pending a safe console input adapter. Shared CLI operations and bare
  legacy external editing remain available. Passing Windows unit CI does not
  restore the unsupported interactive console path.
- Privacy is opt-in. Stop other SP instances and old writers, back up the store,
  and follow [the privacy guide](https://github.com/pders01/sp/blob/v0.22.0/docs/privacy.md) before migration. Retain the wrapped
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
  provider-blocked live exercise are not complete. Do not label this release
  independently audited or fully adversarially validated.

## Release status

A stable `v0.22.0` tag starts the Release workflow and enables the Homebrew formula
update; prereleases continue to skip that update. GoReleaser publishes these
notes with the release. Confirm the stable assets, checksums, version metadata
and tap update after publication; an earlier green RC workflow did not exercise
the stable tap upload or its token permissions.

The source checkout includes a local cancelreader module replacement. Keep
`third_party/cancelreader` when building from source; version-suffixed
`go install ...@version` remains unsupported while this patch is required.
