# Passphrase changes and key rotation — proposal

**Design only; neither operation is implemented or approved for migration.**
Current `privacy enable` reuses a retained identity, including after disable. It
is not a passphrase-change or key-rotation command. Existing behavior and file
formats remain unchanged. See [privacy guarantees](privacy.md) and
[release evidence](release-preflight.md).

## Two different operations

| Operation | Changes | Does not do |
| --- | --- | --- |
| Change passphrase | Re-encrypt the same X25519 identity under a new passphrase | Rotate the data key, rewrite notes, revoke copied keys or old metadata |
| Rotate data key | Generate a new X25519 identity and re-encrypt selected current private documents | Erase old ciphertext, backups, passphrases, recovery keys or plaintext copies |

Shared agent content stays deliberately plaintext, with unchanged ownership and
self-reported provenance. Neither operation may load shared text into a private
editor, normalize Markdown, or introduce plaintext export/temp files.

An attacker holding old wrapped metadata can continue guessing its old passphrase.
If recovered, that same identity still decrypts notes after a passphrase-only
change. Rotating current notes does not revoke ciphertext/key copies already
held by somebody else. No UI may describe either operation as secure erasure or
unqualified revocation.

## Current constraints from source

- `internal/scratchpad/privacy.go` stores one recipient and wrapped identity in
  `privacy.json`. Version 1 means enabled; version 2 supports `decrypting` and
  `disabled`. Unsupported versions/states fail closed.
- Unlock authenticates the wrapped identity, checks the recipient, and compares
  metadata fingerprints before caching it. Later private access requires the
  same fingerprint. `Lock` drops references, not all copies of key material.
- Daily private envelopes use the `age-x25519` format. Private Markdown,
  timestamps and template metadata are inside their encrypted document; the
  shared area is separate (`internal/scratchpad/storage.go`).
- `SealRecovery` intentionally pins a public recovery recipient after unlock and
  retains it across Lock. It must not silently adopt a substituted metadata key.
  `ReadRecovery` currently decrypts with the single unlocked identity, validates
  the authenticated date and does not automatically import/merge the result.
- The existing publication helper distinguishes no publication from publication
  with durability unconfirmed. Every proposed transition must retain that
  distinction and the store-lock/revision-check contract.

## Proposed passphrase-change flow

Proposed command/UI names are illustrative, not available commands.

1. Require an explicit interactive action, no passphrases in argv/environment or
   ordinary agent APIs. Require other SP instances/editors to finish first.
   Show: **Changes the key's passphrase, not the key. Old copies remain usable.**
2. Reject incomplete enable/decrypt/rotation states and corrupt/missing metadata.
   Decide separately whether disabled-policy retained keys may be rewrapped;
   the conservative first implementation supports a verified enabled store only.
3. Ask for the current passphrase and authenticate it afresh, even if a viewing
   session is already unlocked. Ask for and confirm the new passphrase with the
   existing bounded, exact-byte masked input; cancellation must not alter files.
4. Perform expensive derivation outside the publication lock against a captured
   metadata fingerprint. Create the new wrapped identity in memory and verify
   it by decrypting it with the new passphrase and comparing its recipient.
   Keep existing KDF/work-factor caps; do not invent cryptography.
5. Acquire the store lock and re-read/compare the exact expected metadata and
   policy. A concurrent change is a conflict, not permission to overwrite it.
   Publish only the replacement metadata through the existing atomic/durable
   path. Preserve recipient and policy; daily/recovery files remain byte-exact.
6. Report **not changed**, **changed**, or **changed; durability unconfirmed**
   according to actual publication. Never restore old metadata automatically
   after a possibly published change. A retry must authenticate the metadata
   actually present; it must not guess which passphrase currently applies.
7. Drop this session's decrypted projections and require fresh unlock. Other
   managers' old metadata fingerprints must fail private access. The recovery
   recipient remains the same public key; existing encrypted recovery remains
   decryptable with the rewrapped identity. Do not claim all process memory was
   erased or all other processes were forcibly locked.

Do not silently create an extra old-passphrase metadata copy in the store. An
explicit backup plan must explain both recovery requirements and continued
access through previously retained wrapped metadata.

## Rotation requires a separate format/lifecycle design

Rotation cannot be implemented as a simple recipient replacement: it would
orphan existing daily ciphertext, retained recovery and old backups. Before any
runtime implementation, approve a versioned transition design with these gates:

- **Scope:** explicitly enumerate current private daily records. Shared-only days
  need no private key; do not manufacture private records. Do not silently scan
  arbitrary external backup/recovery paths or rewrite plaintext disabled stores.
- **Key retention:** decide how prior identities are retained under authenticated,
  versioned wrapped metadata and selected for recovery. The first iteration
  should retain old keys, not offer automatic deletion. External backups still
  require their matching metadata; never promise every backup was discovered.
- **Durable reachability:** durably publish sufficient wrapped old/new key state
  before any document uses the new recipient. Authenticate transition metadata,
  reject unknown/duplicate generations, and verify every new encrypted private
  document before publishing it. Resume must identify mixed generations without
  destructive trial rewrites or unbounded key search.
- **State machine:** define prepare, migrate, verify and finalize states, with
  idempotent resume after each publication boundary. Refuse incompatible old
  clients and normal private saves during migration; define whether public
  appends remain permitted under the store lock. No implicit policy downgrade.
- **Concurrent shared writes:** on every date publication, reread under lock and
  preserve the latest shared area exactly. A stale decrypted snapshot must never
  overwrite a concurrently appended contribution or its provenance.
- **Recovery generation fencing:** current `SealRecovery` may seal with its
  intentionally pinned old recipient without consulting new metadata. Stopping
  old sessions is therefore a real requirement, not something fingerprint checks
  alone enforce. New code needs an explicit generation/transition check while
  preserving anti-substitution pinning. Seal changed drafts before transition;
  do not silently redirect a live draft to a newly observed public key.
- **Cancellation:** before a transition is published, cancellation makes no
  change. Afterwards it leaves an explicit resumable transition; it must never
  advertise rollback or delete either generation's recovery material.
- **Completion:** verify all scoped current records and durability under the
  publication lock before marking complete. Report stage/count-only progress;
  no note text, secrets or private diagnostics in public/agent interfaces.

A concrete wire schema, lock ordering and compatibility analysis remain required.
This document deliberately does not select a keyring format or authorize changing
`privacy.json`, daily envelopes, recovery files or existing releases.

## Synthetic acceptance gates before shipping

- Exact-byte current/new secrets, retry/cancel/help/signal ownership, mismatch,
  input rejection and zero secret output. No real stores or user editor config.
- Wrong current secret, corrupt metadata, changed fingerprint while derivation
  runs, and unsupported policy/transition: no publication or plaintext fallback.
- Inject failures before replacement and after replacement/directory sync;
  verify truthful outcomes and safe retry after process restart.
- Passphrase-only change preserves every daily/recovery byte and recipient;
  old metadata backups remain demonstrably capable of decrypting old-key data.
- Old managers lose ordinary private access on metadata changes. Explicitly test
  stale pinned recovery behavior; do not call this universally revoked access.
- Rotation interrupted at every key-state/date/finalization boundary resumes
  without orphaning ciphertext or changing exact private bytes after decryption.
- Concurrent append/private-save/disable/enable/recovery and multiple processes:
  retain shared contributions, reject stale generations and avoid deadlocks.
- Recover both old/new generation drafts through explicit review, not automatic
  import. Backups with their matching metadata remain independently restorable.
- Native supported-platform locking/durability checks and independent
  cryptographic review remain separate gates; unit/race/fuzz success is not an
  independent audit or proof of crash safety on every filesystem.

## Approval required

Agree on passphrase-only scope first; enabled-only versus retained disabled keys;
backup warnings and UI names; rotation/recovery key retention; compatibility and
public-append behavior during a transition. Implement and review passphrase
rewrapping separately from rotation rather than bundling both into enable/disable.
