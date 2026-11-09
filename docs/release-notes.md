# SP v0.23.0-rc.1

Candidate for v0.23.0, adding explicitly opted-in private editing and same-key
passphrase changes while retaining existing defaults. This prerelease is not
stable-channel promotion, native platform sign-off or independent security
approval. See the preflight record for local versus hosted validation evidence.

## New since v0.22.0

- Explicitly opt in to an embedded Vim-compatible private editor with
  `sp edit --editor=vim [YYYY-MM-DD]`. Existing notebook/calendar and built-in
  defaults are unchanged. Privacy must already be enabled; the note limit is
  1 MiB. Pinned public Wasm/JS assets are linked into the executable: no runtime
  Node, external editor, user vimrc or filesystem-backed editing buffer.
- Vim-style movement, visual editing, search, undo/redo, counts, registers and
  repeat support common note editing. `:w`/Ctrl+S checkpoints encrypted content
  without losing editor history; `:wq`/F2 finishes. Cancel requests still require
  consent and durable encrypted recovery for changed drafts. Retained or
  uncertain input is never silently replayed or treated as discard consent.
- `sp privacy change-passphrase` explicitly rewraps the **same private key** using
  exact masked current/new/confirmation input. It requires a fully migrated,
  enabled store and rechecks policy/metadata at publication. Daily and recovery
  files are not rewritten. Stop other SP instances first and retain matching
  metadata with backups. Old metadata/key copies remain usable: this is not
  key rotation or revocation.
- Browse shared updates across saved dates without unlocking private text;
  reading positions and date navigation remain independent from private views.
- Source-only agent-hook examples and a bounded shared-only MCP stdio experiment
  use deliberate public shared data. The MCP executable is **not included in
  release archives**, installed or registered automatically. It requires an
  explicit existing store and separate exact read/append date grants. External
  client interoperability is unverified; the adapter is not release-ready.
- Offline asset checks support the embedded-engine license set. Release archives
  carry all 15 upstream notices, the application license and the existing
  cancelreader patch license. Superseded native Neovim/demo prototypes and
  repository-local notification tooling are removed; native research is archived.

See the [Vim integration guide](https://github.com/pders01/sp/blob/v0.23.0-rc.1/docs/embedded-vim-integration.md),
[privacy guide](https://github.com/pders01/sp/blob/v0.23.0-rc.1/docs/privacy.md), [key lifecycle record](https://github.com/pders01/sp/blob/v0.23.0-rc.1/docs/key-lifecycle-design.md) and
[preflight evidence](https://github.com/pders01/sp/blob/v0.23.0-rc.1/docs/release-preflight.md).

## Compatibility and privacy limits

- **Windows interactive views, masked prompts and private editing remain
  unavailable** pending a safe console adapter. Cross-compilation and unit CI
  do not establish Windows interactive support. Shared CLI operations and bare
  legacy external editing remain separate existing paths.
- Capability-limited in-memory Wasm execution is **not native OS enforcement**,
  an independent security audit or crash-proof persistence. The host/interpreter,
  memory, paging/dumps, display capture and unrestricted same-user code remain
  relevant risks. No native sandbox backend or plaintext private-editor fallback
  is enabled.
- This is bounded Vim compatibility, not full Vim/Neovim parity: no vimrc,
  plugins, clipboard, shell/filesystem commands, tabs/windows, language formatting
  or full regex/options/mapping parity. Replace-mode Backspace moves left; it
  does not restore overwritten text. Complex virtual-column/block semantics
  remain limited.
- Graceful ownership/recovery handling cannot guarantee the latest unacknowledged
  text after a guest fault, SIGKILL, host crash or power loss. Failed/uncertain
  recovery retains ownership; interruption is not rollback or discard consent.
- Privacy remains opt-in; shared text, provenance, dates and sizes are public.
  Stop old writers and back up the store before migration. Re-enable is not a
  passphrase change, data-key rotation, revocation or secure erasure. Command
  templates remain trusted user programs, not sandboxed integrations.
- User-reported Vim terminal smoke acceptance applies to the earlier integration
  v2 binary. Later builds have headless checks, not newly attributed manual or
  cross-platform acceptance. No fresh editor/PTY launch was performed inside pi.

## Candidate publication

The `v0.23.0-rc.1` tag marks a prerelease and skips the stable Homebrew formula
update. Publication requires explicit owner approval and hosted CI on the exact
candidate commit. Verify the actual five published platform archives and
checksums separately; local preflight snapshots are not release assets. Keep
existing tags unchanged.

Source builds still require `third_party/cancelreader`; version-suffixed
`go install ...@version` is unsupported while the local replacement is required.
