# sp

A keyboard-first daily notebook for the terminal.

SP gives each day a page for notes, todos, and quick thoughts. Browse your history
in a calendar or notebook, write Markdown, and let tools append public updates
without unlocking your personal notes.

The focus is simple: make daily writing easy, keep history readable, and make
sharing deliberate.

## What it does

- **A page per day.** Start with today or browse saved dates in the calendar and
  notebook. Your personal note is the main view; shared updates stay separate.
- **Private when you choose.** Optional encryption protects personal notes at
  rest. Unlock and edit in memory, with encrypted recovery for changed drafts.
- **Familiar editing.** Use your external editor for plaintext notes, the built-in
  private writer, or the opt-in Vim-compatible editor.
- **Public updates from tools.** Read and append shared Markdown without unlocking
  private notes. Useful for task updates, session summaries, and daily digests.
- **Small conveniences.** Reusable templates, date navigation, light/dark themes,
  and optional Nerd Font icons.

## Installation

### Homebrew

```sh
brew tap pders01/sp
brew install sp
```

### From source

Requires Go 1.25.13 or later. Node is not needed to build or run SP.

```sh
git clone https://github.com/pders01/sp.git
cd sp
make install
```

Use the complete checkout: SP includes a local terminal-reader patch, so
version-suffixed `go install ...@version` is not supported.

The Vim editor and passphrase-change command are currently available in source
builds. See the [release notes](docs/release-notes.md) for version availability.

## Everyday use

```sh
sp          # edit today's note
sp -n       # browse the notebook
sp -c       # open the calendar
sp --help
```

In plaintext mode, SP opens your external editor. With privacy enabled, it uses
the built-in in-memory writer. Opening a viewer does not create or save a note.

### Getting around

The calendar and notebook share these shortcuts. Editing keys depend on the editor.

| Key | Action |
| --- | --- |
| `Enter` | Open the selected day or edit its note |
| `e` / `i` | Edit the selected day |
| `d` | Jump to a `YYYY-MM-DD` date |
| `c` | Read the day's shared updates |
| `a` | Append template sections |
| `u` | Unlock private notes |
| `Ctrl+L` | Relock private notes |
| `Ctrl+T` | Cycle auto, light, and dark themes |
| `F1` | Contextual help |
| `Esc` | Go back or close a dialog |
| `q` | Quit a viewer |

In the notebook, Left/Right or `h`/`l` browse saved dates; Up/Down or `k`/`j`
scroll. In the calendar, arrows or `h`/`j`/`k`/`l` move between dates, and `t`
returns to today. Shared updates have separate navigation and reading positions;
`Esc` or `c` returns to the personal note.

## Private notes

Notes are **plaintext by default**. Enable privacy explicitly:

```sh
sp privacy enable
sp privacy status
```

Privacy encrypts personal notes, including existing days. Shared updates remain
public. Stop other SP instances and old versions before enabling it, and follow
the [privacy guide](docs/privacy.md) for migration and backups.

Press `u` in a viewer to unlock for the current process. The built-in writer edits
in memory without passing private text to an external editor or creating a
plaintext editor file. Ctrl+S saves; F1 shows editing help. Changed drafts require
encrypted recovery before cancellation or relocking. If recovery fails, keep the
editor open and retry.

```sh
sp privacy recover --list
sp privacy recover <recovery-file>
sp privacy change-passphrase
sp privacy disable
```

Recovery opens a review rather than automatically merging text. Changing the
passphrase rewraps the same key; it does not rotate the key or revoke old
metadata/key copies. Disabling privacy creates plaintext notes and retains the
wrapped key for encrypted backups and recovery files.

**Keep `~/.sp/privacy.json` with your backups and keep the passphrase safe.**
Losing either can make encrypted notes unrecoverable. Never put passphrases or
private keys in command arguments, environment variables, or config. Encryption
does not hide dates, sizes, or shared text, and cannot protect an unlocked process
from malware running as your user.

## Editing options

### Plaintext notes

SP uses `$EDITOR`, then `$VISUAL`, then an available editor. GUI editors must
wait until editing is finished:

```sh
export EDITOR=vim
# or
export EDITOR="code --wait"
```

External editors are for plaintext notes only. Use the built-in writer or the
embedded Vim editor for encrypted notes.

### Vim-compatible private editing

```sh
sp edit --editor=vim
sp edit --editor=vim 2026-01-15
```

Privacy must already be enabled. The bundled editor runs in memory without Node
or an external Vim installation. It is selected per invocation or via
`editor.private = "vim"` in `~/.sp/config.toml`; an explicit `--editor` flag
always wins, and notebook/calendar in-viewer editing keeps the built-in writer.

| Command | Action |
| --- | --- |
| `:w` or Ctrl+S | Save an encrypted checkpoint and continue editing |
| `:wq`, `:x`, or F2 | Save and finish |
| `:q!` or F3 | Request cancellation and follow the recovery confirmation |

The note limit is 1 MiB. This is Vim compatibility, not a full Vim installation:
no vimrc, plugins, system clipboard, shell, or filesystem commands. It is not an
OS sandbox, and a crash or power loss can lose unsaved text. See the
[Vim guide](docs/embedded-vim-integration.md) for supported behavior and recovery.

## Shared updates and automation

```sh
sp append 2026-01-15 --section 'Agent notes' --source 'local-helper' -- '- Public task'
sp shared 2026-01-15
sp shared 2026-01-15 --json
```

Shared updates are deliberately plaintext and accessible without unlocking
private notes. Source labels are self-reported, not verified authorship. Never
append secrets or private note content. Use `--json` for structured read/append
results.

See [agent hooks and weekly summaries](docs/agent-hooks.md) for opt-in automation.
The experimental [shared-only MCP adapter](docs/shared-mcp-design.md) is a
separate source-built tool, not installed or configured by SP.

## Configuration and templates

Settings live in `~/.sp/config.toml`. See
[`config.example.toml`](config.example.toml) for all options.

```toml
[ui]
icons = "unicode"  # or "nerd"
theme = "auto"     # "auto", "light", or "dark"

[editor]
private = "builtin" # or "vim" for the experimental bundled Vim engine

[templates]
allow_commands = false

[[templates.items]]
name = "Meeting notes"
file = "~/.sp/templates/meeting.md"
```

Press `a` on a day to append selected templates as Markdown sections. Existing
content is preserved; selecting an already-applied template appends another copy.
Templates save before opening the editor, so cancelling the editor does not undo
the insertion.

Command templates require `allow_commands = true` and run with your account's
permissions. They are not sandboxed. Only enable commands you trust.

## Platforms and storage

Interactive views and private editing currently support macOS and Linux.
Windows supports shared CLI operations and plaintext external editing, but not
the interactive notebook, calendar, or masked prompts.

Notes live in `~/.sp/YYYY-MM-DD.json`, alongside configuration and privacy
metadata. Back up the entire directory. SP detects conflicting writes rather
than silently replacing another instance's changes. See
[editing failures and recovery](docs/privacy.md#edit-failures-recovery-and-backups)
for handling retained drafts.

## Development

```sh
make build
make test
make test-race
make lint
```

Node 22 and npm are needed only to work on the embedded Vim build/test harness;
see its [README](scripts/embedded-vim/README.md). Release tooling is documented
in [RELEASE.md](RELEASE.md).

## License

MIT. Bundled dependencies retain their respective licenses.
