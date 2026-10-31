# Opt-in privacy (unreleased)

This document describes the implementation in this source tree, not a claim that
an installed release already supports it or that it has passed a security audit.
Check your build's command help before use. Read the
[latest validation results and remaining gaps](privacy-validation.md) alongside the
[verification procedure](privacy-testing.md). The live model-driven trial was
blocked by the provider; it is not a completed adversarial pass. No independent
cryptographic audit is claimed.

## Why two areas?

SP separates **private writing** from an explicitly **shared agent area** in
one daily file. A helper can append shared bullets without obtaining a private
key or unlocking private notes. This reduces accidental disclosure to cooperating
CLI tools; it is not an operating-system security boundary against programs
running unrestricted as your user.

Privacy is opt-in. Existing plaintext notes remain unchanged until you explicitly
enable it. In legacy mode, editing and storage remain plaintext. The enabled
policy encrypts **every private day, including today**, not just past days;
interrupted migration can still leave legacy plaintext until resumed. Shared
history stays plaintext and readable for **all dates**, not just today. Do not
put secrets in shared bullets, section names, or source labels.

## Enable and migrate

1. Stop **all** running SP instances and other note writers. Back up the existing
   store somewhere you control. That backup contains plaintext: protect it
   accordingly.
2. Use only a privacy-aware, supported SP build from this point onward. **Old SP
   versions are unsafe:** they may ignore new fields and overwrite ciphertext.
   Do not open this store with an older executable or mix old and new writers.
3. Run `sp privacy enable` interactively. Read the warnings, explicitly confirm,
   and enter a strong, unique passphrase twice at the hidden-input prompts. Do
   not put the passphrase or private key in command arguments, environment variables,
   config files, scripts, or a helper's context. SP accepts 1–4096 bytes of UTF-8
   single-line text without control characters; this limit is not a strength test.
4. Enable first writes the global key metadata to `~/.sp/privacy.json`, then
   migrates existing notes one day at a time. Keep the passphrase safe outside
   SP; there is no password reset or recovery service.
5. Run `sp privacy status` and check the listed remaining legacy plaintext dates.
   Do not assume migration completed merely because privacy is enabled. Status
   reports storage/migration state without unlocking; it is not a cryptographic
   integrity check of all encrypted contents or a guarantee of freshness.
6. Back up `privacy.json` **together with** the daily note files. The date files
   alone and the passphrase are not sufficient to replace a lost random identity.

### Interrupted or partial migration

**The privacy policy is active as soon as `privacy.json` has been written, even
if migration is incomplete.** New private saves must be encrypted; remaining
legacy files are still plaintext on disk. A cancelled command, I/O error,
conflict, or process interruption is not an automatic rollback to legacy mode.

Run `sp privacy status` to inspect the remaining legacy dates and any reported
errors. Resolve the reported error without deleting key metadata or hand-editing
ciphertext, then run `sp privacy enable` again and unlock to resume migration.
Already migrated days do not need to become plaintext again. Repeat status until
no legacy dates remain; a status error is not evidence that all notes are safe.
Keep other writers stopped during migration.

Rewriting live files does not securely erase the old plaintext. Backups,
snapshots, cloud history, filesystem remnants, earlier temporary files, and old
exports can retain it. Handle those separately according to your storage and
backup system; SP makes no secure-erasure claim.

## Reverse encryption (decrypt the store)

To return to the normal plaintext workflow:

1. Stop **all** SP instances and other note writers. Back up the daily files,
   `privacy.json`, and any encrypted recovery files together.
2. Run `sp privacy disable` in an interactive terminal. Read the plaintext
   exposure warning, type `DECRYPT ALL NOTES`, and enter the **existing**
   passphrase at the hidden-input prompt. Flags, environment variables, and
   redirected input cannot supply the passphrase or bypass confirmation.
3. SP authenticates and verifies every daily conversion before writing any
   plaintext. It then records a durable `decrypting` state, converts private
   notes one day at a time, and marks privacy disabled only after completion.
   Private Markdown, timestamps, template history, and shared contributions are
   preserved. Already plaintext and shared-only daily files are left unchanged.
4. Check `sp privacy status`. Completed reversal reports privacy disabled and
   zero encrypted dates. Normal reads/edits no longer require unlock, and the
   usual plaintext external-editor workflow is restored.

**This deliberately exposes all daily private writing to file readers, backup
and sync tools.** Plaintext staging files are used during conversion; a process
crash may leave one behind. There is no secure-erasure guarantee.

If interrupted, some days may already be plaintext. Status reports
`decrypting (incomplete)`; ordinary private access and all daily writes are
blocked, while shared-only reads remain available. Resolve the reported problem
and run `sp privacy disable` again with confirmation and the same passphrase.
Do not run enable, remove metadata, or hand-edit the state to finish a reversal.
A post-publication durability error is not a rollback: inspect status and retry.

`privacy.json` **is retained**, containing the passphrase-wrapped key and the
explicit disabled state. Existing encrypted backups and `.sp-recovery-*.age`
files are not decrypted or deleted. Keep that metadata and passphrase to read
those files; `sp privacy recover` can still review a recovery file after unlock,
with saves following the current plaintext policy. Re-enabling with
`sp privacy enable` requires confirmation and the existing passphrase and reuses
this key. It encrypts current daily notes again, but does not erase plaintext
copies created during reversal or revoke older backups/keys.

Reversal metadata uses a newer format. Older privacy-aware builds reject it;
very old builds may ignore unknown fields and remain unsafe. Continue using a
supported build even after disabling. Passphrase changes and key rotation are
not implemented; disabling and re-enabling is **not** either operation.

## Daily operation

```sh
sp privacy status
sp append 2026-01-15 --section 'Agent notes' --source 'local-helper' -- '- Follow up on the public task'
sp shared 2026-01-15
```

Dates use `YYYY-MM-DD`. The `--` separates CLI options from a bullet beginning
with `-`. `--source` is a self-reported label, not an authenticated identity.

- `sp shared <date>` reads only the shared area. It must not unlock, decrypt, or
  fall back to displaying private content.
- `sp append <date> --section <name> --source <source> -- '- bullet'` appends only
  to shared content, without a private unlock or access to the private key.
  Exact duplicate bullets are deduplicated within a section; this is not semantic
  similarity detection or cross-section deduplication. Append provenance is
  recorded for newly added bullets per operation, not proof that a trusted agent
  authored the text. A fully duplicate append is a no-op, not a new contribution.
  While privacy is active, append refuses an unmigrated legacy day: resume
  migration first. A new shared-only day needs no private payload or unlock.
- Private reads and edits require an interactive unlock in that process.
  Unlock is process-scoped, not a persistent login, exported secret, or background
  key service. Another process must unlock separately. Exit SP when finished;
  this is not a claim of guaranteed memory erasure.
- In the privacy TUI, `u` opens the masked unlock prompt. `Enter`/`e` edits the
  private page in the built-in in-memory editor; calendar `Enter` first opens
  that day's notebook. In the built-in editor, `Ctrl+S` saves and returns; `Esc`
  cancels, with confirmation and encrypted recovery for changed text. Follow your
  build's displayed key help if bindings differ.
- Privacy mode uses a lossless built-in in-memory editor, with horizontal
  scrolling rather than soft wrapping. Tabs, CRLF, Unicode, and trailing lines
  are retained; limits reject input explicitly instead of truncating it. Use
  arrows/Home/End, Ctrl+A/E for line start/end, Ctrl+K/U to delete after/before
  the cursor, Tab for a literal tab, and Ctrl+S to save. Terminal bracketed paste
  is supported; the editor/unlock prompt never invokes system clipboard commands.
  Press **F1** for the editor's scrollable key reference and full error/recovery
  status; `?` remains ordinary note text. F1 also opens the TUI's masked unlock
  help without exposing or submitting the passphrase. Closing help returns to the same draft
  or prompt; resizing does not discard either. Small unsupported dimensions show
  a resize hint rather than overflowing the terminal.
  The initial version does not enable external editing for private content.
  The plaintext legacy workflow still uses its normal external editor.
- Private previews are not automatically decrypted across the calendar/history.
  Private decryption is lazy for the current page after unlock, not a bulk
  background preload. Shared content and public metadata are not private previews.

Use the explicit reversal above rather than deleting `privacy.json` to disable
privacy. Passphrase-change/rotation and MCP access remain deferred; this design
does not claim an implemented MCP security boundary.

## Storage and cryptography

The privacy design uses a random **age X25519 identity**. Its public recipient
can encrypt without unlocking. The identity is stored only in passphrase-wrapped
form in the global `privacy.json`, using age's scrypt passphrase encryption. The
enabled metadata fields are `version` (1), `recipient` (public age recipient), and
`wrapped_identity` (base64-encoded passphrase-encrypted age bytes). Reversal uses
metadata `version` 2 with `state` set to `decrypting` or `disabled`, retaining the
same recipient and wrapped identity. Unknown versions/states fail closed, and a
disabled state with remaining daily ciphertext is rejected, not treated as
permission to write plaintext. Metadata presence alone does not mean encryption
is enabled after a completed reversal. The passphrase unlocks that identity; it
is not itself the daily note encryption key.
No unwrapped key or password belongs in SP's configuration, arguments, or
environment. This implementation uses `filippo.io/age` v1.2.1. The identity's
scrypt work factor is 18 (approximately 256 MiB of working memory); inputs asking
for a higher factor are rejected. Metadata, documents, decrypted payloads, and
age headers have size limits. These bounds reduce resource abuse; they do not
make arbitrary same-user code safe.

Each date remains a single `YYYY-MM-DD.json` file using the version-2 format:

- Public outer fields are `version` (2), `date`, `private`, and `agent`.
- `private.format` is `age-x25519`; `private.payload` is base64-encoded age
  ciphertext. The decrypted JSON fields are `version` (2), `date`, `content`
  (private Markdown), `created`, `modified`, and optional `applied_templates`.
  Private template history is not deliberately exported to the shared area.
- `agent.content` is shared plaintext Markdown. `agent.modified` and
  `agent.contributions` are public too; each contribution has `id`, `date`,
  `source`, `section`, `bullets`, and `created`. Contributions describe newly added
  bullets, not authenticated authorship. Shared history remains readable without
  unlock across historical dates.
- A shared-only date can omit `private`. Once private content is saved under the
  active policy, it uses the encrypted envelope, even for today or an empty page.

Privacy policy/migration/status checks reject date-named symlinks, directories,
special files, and noncanonical `.JSON` casing instead of treating them as a clean
migration. Data reads
require regular files; SP does not follow a symlink to migrate its target.
Strict JSON rejects malformed UTF-8, unpaired escaped UTF-16 surrogates, duplicate
or aliased field names, and unsupported fields rather than silently normalizing
or discarding them. This is not a race-free filesystem security boundary.

On Unix, SP requests restrictive file modes (`0600` for published files and
`0700` for newly created store directories). On Windows, Go's permission bits
do not describe NTFS access-control lists: files/directories inherit host ACLs,
and SP does not configure or validate an owner-only DACL. Restrict the store
using Windows permissions as appropriate. Encryption is not a replacement for
host access controls, and a `FileMode` such as `0666` does not establish that a
Windows file is readable by everyone.

These field names reflect the current implementation, not a stable direct-write
API. Do not build a direct JSON writer against this description; use the
supported CLI. Shared append locks the entire read-modify-write operation,
updates shared data, and preserves the protected
private payload unchanged. Revision checks and OS locks coordinate cooperating
SP writers. They do not bind arbitrary programs or prevent direct file edits.

Age authenticates its encrypted payload: corruption is not silently accepted as
valid decrypted private content. **That is not authentication of the public
record, shared text, provenance, or global metadata, nor a rollback-prevention
system.** A direct writer can delete, corrupt, replace, or restore old files.
Even a previously valid ciphertext can be replayed. Anyone with the public
recipient can also encrypt a replacement: successful decryption alone does not
prove who wrote it. Filename/private-date and format validation can reject some
substitutions; it cannot establish freshness.
There is no append-only, externally anchored audit log.

## What this protects, and what it does not

### Intended protection

With a strong passphrase and uncompromised key handling, someone who obtains only
the stored files cannot read encrypted private content without unlocking the
identity. This is protection against offline file reading, not against an
unrestricted malicious process on your logged-in account. Someone with the files
can make **offline passphrase guesses**; a weak passphrase defeats this design.
Use a long, unique, high-entropy passphrase, not merely a short password that
satisfies a minimum-length check.

Cooperating agent hooks need no private key and receive only the deliberately
shared text. Encrypting today as well as the past avoids a daily window in which
today's private writing is stored as plaintext.

### Visible information

Observers can still learn dates, existence and number of notes, ciphertext/file
sizes, filesystem timestamps, global public metadata, shared Markdown and its
history, and provenance labels/times. Private timestamps inside ciphertext do
not hide filesystem timestamps. Encryption does not provide traffic analysis
resistance or hide when or how much you write.

### Outside the boundary

- **Same-user compromise:** unrestricted malicious code may alter SP, shell
  startup, binaries, configuration, or future unlock input. It can tamper with
  files now and try to capture a later unlock. File permissions and encryption
  alone do not isolate you from that process. Source labels are forgeable.
- **Unlocked process and device:** private plaintext and key material exist in
  memory. Go does not guarantee erasure of all copies. Debuggers, OS swap,
  hibernation, core dumps, malware, and a compromised OS are outside this promise.
- **Display and copies:** rendering private text exposes it on screen. Terminal
  recording, scrollback, logs, screenshots, screen sharing, clipboard use, and
  intentional exports can expose plaintext. A masked passphrase prompt does not
  make a compromised terminal trustworthy.
- **External tooling:** an external editor may create swap, autosave, backup,
  history or plugin/network copies. It is not a privacy-safe substitute for the
  built-in path. Configured template commands run as your user and must be trusted;
  encryption does not sandbox them.
- **Availability/integrity of the store:** locks and atomic writes do not stop
  deletion, corruption, rollback, malicious public-recipient/metadata changes,
  or direct writers. Ciphertext authentication is not public metadata
  authentication or proof of the author's identity.

## Edit failures, recovery, and backups

If a private edit cannot be saved, the privacy editor attempts an **encrypted
recovery file** using the public recipient; encrypting recovery does not require
an unlocked private key. Do not expect a plaintext `.md` recovery buffer as in
legacy external-editor mode. Current recovery files are
`~/.sp/.sp-recovery-<date>-<random-id>.age`: raw age ciphertext of a private JSON
recovery document, not a daily version-2 envelope or a plaintext Markdown file.
Read the actual error and reported recovery path. SP prints retained paths again
after leaving the alternate-screen UI, even if another status message replaced
the original error. Discover candidates later without unlocking:

```sh
sp privacy recover --list
```

Listing reports filename-based encrypted candidates and path-only records of
legacy plaintext editor buffers. It does not authenticate/decrypt recovery,
open recorded external paths, or print note content. Records may be stale;
OS temporary buffers are not permanent backups. A failed index write is reported
with the retained path rather than implying discovery metadata was saved.
If recovery publication also fails, retain the running editor: it keeps the
in-memory text so you can retry rather than silently discarding it. Do not exit
or terminate the process assuming an on-disk recovery exists.

An encrypted recovery file is not a successful note save, automatic merge, or
permanent backup. Preserve it with the associated global metadata and resolve
the underlying conflict or storage failure. Recovery requires the matching
identity and passphrase. Use the interactive built-in recovery path:

```sh
sp privacy recover <reported-recovery-path>
```

A recovery basename is also accepted. The regular file must be inside the same
note store; its authenticated date must match the filename. After unlocking,
SP opens recovered text in memory against the current note's conflict baseline.
**Saving replaces current private Markdown; it does not merge it.** Review the
text before saving. Shared contributions and the recovery file are retained.
Do not delete the recovery file until you have verified the resulting note.

A live process pins its recovery public recipient after unlock, so later metadata
substitution cannot redirect that editor's automatic recovery to another key.
This is defense in depth, not authentication of metadata in a fresh process.
Any manual decryption or export deliberately creates plaintext and must happen
outside agent contexts with appropriate local handling.

Maintain tested backups of **both** `privacy.json` and the daily files, plus any
needed recovery files. Preserve older metadata versions if you preserve notes
that depend on them. Keep the passphrase separately safe. A lost passphrase or
lost wrapped identity makes private notes **unrecoverable**; shared text remains
readable. Do not publish metadata backups: they contain the wrapped identity and
permit offline guessing. Earlier plaintext backups remain sensitive forever
unless independently removed.

## Verification model and current evidence

[Privacy testing](privacy-testing.md) specifies deterministic checks and a
synthetic-only, black-box adversarial exercise. Required checks cover ciphertext
at rest for every private date, resumable migration, no-unlock shared hooks,
protected-payload preservation, locking/conflicts, failure recovery, explicit
resumable reversal/re-enable, and the UI unlock/editor path. Unit tests are necessary but cannot establish absence of
side channels, memory disclosure, cryptographic flaws, or same-user compromise.

The [latest validation report](privacy-validation.md) records the real terminal
walkthrough, bounded fuzz campaigns, regression fixes, patched Go minimum, and
reachable-advisory scan, including remaining dependency advisories without
detected call paths. The [initial review](privacy-review.md) records the completed
same-model source review and the separate provider-blocked live trial. Do not interpret
this document, use of age, or a failed attack attempt as an independent
cryptographic audit.
