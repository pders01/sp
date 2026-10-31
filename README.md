# sp — Daily Scratchpad

CLI/TUI scratchpad for daily notes, todos, and quick thoughts. Built
with Go and the [charm.sh](https://charm.sh) toolchain.

> **Opt-in privacy (unreleased):** existing notes stay plaintext until you
> explicitly enable it. Privacy encrypts every private day (including today),
> while shared agent history deliberately remains plaintext. Read the
> [privacy guide, migration instructions, and security limits](docs/privacy.md)
> before enabling. See the [test results and remaining gaps](docs/privacy-validation.md):
> automated tests pass, but the live model-driven trial was provider-blocked.
> This is not an independent security audit. **Old SP versions are unsafe for
> privacy stores:** stop all instances and use only a privacy-aware build.

## Features

- Daily scratchpad: a fresh page per day, kept on disk forever
- Explicitly opt-in private-note encryption, with separately displayed shared
  contributions in the same daily file and a locked append CLI for tools
- Three view modes that drill into one another:
  - Year / month calendar with per-day previews and entry sparklines
  - Notebook viewer with glamour-rendered markdown, scrubbable across
    pages
  - External editor (`$EDITOR`) for writing in legacy plaintext mode;
    built-in in-memory private editing in privacy mode
- One semantic rule across the TUI: **Enter goes deeper**
- Legacy external editing: hand the editor to `tea.ExecProcess`, save on exit,
  resume the same view with a refreshed render — no shell hop between
  edits
- Light / dark palette swap driven by your terminal: `Ctrl+T` toggles
  in-session, `SIGUSR1` re-detects, macOS appearance changes are
  picked up automatically via an fsnotify watcher on the global
  preferences plist
- Glyph set choice: nerd-font icons or pure-ASCII fallback, set in
  `~/.sp/config.toml`
- Opt-in day templates: append one or more named Markdown sections from
  files or script output, with a built-in workday timeboxing helper

## Installation

### Homebrew

```sh
brew tap pders01/sp
brew install sp
```

### Build from source

Requires Go **1.25.13 or later**. The patched minimum is security-related: the
previous Go 1.24 baseline and older Markdown/text dependencies have reachable
advisories. See the [validation follow-up](docs/privacy-validation.md).

```sh
git clone https://github.com/pders01/sp.git
cd sp
make install   # → $GOPATH/bin/sp
```

Or manually:

```sh
go build -o sp ./cmd/sp
```

## Usage

```sh
sp          # open today's page ($EDITOR in legacy plaintext mode)
sp -n       # notebook viewer; Enter / e / i opens the editor
sp -c       # calendar; Enter drills into the notebook on that day
sp --version
```

Choose either `-n` or `-c`; enabling both is an error rather than silently
selecting a view. Explicit `--calendar=false`/`--notebook=false` values are allowed.

### Opt-in privacy usage (privacy-aware builds)

```sh
sp privacy enable   # interactive warnings, confirmation, passphrase twice; migrate
sp privacy status   # inspect migration/reversal state without unlocking
sp privacy disable  # confirm + existing passphrase; decrypt daily notes in place
sp privacy recover --list          # discover recovery paths without unlocking
sp privacy recover <recovery-file> # interactively review encrypted recovery; no auto-merge
sp append 2026-01-15 --section 'Agent notes' --source 'local-helper' -- '- Public task'
sp shared 2026-01-15 # shared-only read; no private unlock
```

In privacy mode, `u` opens a masked TUI unlock prompt; `Enter`/`e` edits privately
in memory (calendar `Enter` opens the notebook first). External editors are not
enabled for privacy editing in the initial version. Unlock lasts only in that
process. Never put a passphrase/private key in arguments, environment, or config.
Back up `privacy.json` with the notes: losing the passphrase or wrapped identity
means losing the private writing. Shared bullets and source labels are public,
self-reported data, not authenticated authorship. `sp privacy disable` restores
plaintext editing; interrupted reversal resumes with the same command. It retains
the wrapped key for encrypted backups/recovery files. Re-enabling uses the existing
passphrase/key and does not erase plaintext copies. Passphrase rotation and MCP
access are not implemented. See the [full guide](docs/privacy.md).

### Flow

```
sp -c   →   calendar   ──Enter──→   notebook   ──Enter/e/i──→   editor
                ▲ ▲                     │                          │
                │ └────── Esc ──────────┘                          │
                │                                                  │
                └─────── editor exits, view resumes ───────────────┘
            (Calendar 'e' is a power-user shortcut: skip the notebook
             and edit the picked day immediately.)
```

After the editor saves and exits, the active view repaints with the
updated content. Pop from notebook back to calendar with `Esc`/
`Backspace`; the calendar cursor follows wherever you were in the
notebook.

### Key reference

The tables below describe navigation; privacy-aware builds also provide `u` for
masked unlock. Private previews are not automatically decrypted across history.

Footers wrap essential actions to the terminal width. Press `?` or `F1` in a
viewer or template chooser for a scrollable key reference and full status details.
In the built-in editor or TUI unlock prompt, use **F1 only**: `?` stays literal.
Within help, `Esc`/`F1`/`q` closes only the reference; arrows, Page Up/Down, and
`g`/`G` scroll it. Unsupported screen sizes show a resize hint without discarding
notes, drafts, passphrases, or template selections.

#### Calendar

| Key                | Action                                |
| ------------------ | ------------------------------------- |
| `←/h` `→/l`        | day (month view) / month (year view)  |
| `↑/k` `↓/j`        | week (month view) / row (year view)   |
| `H` `L`            | jump month / year                     |
| `Enter`            | drill into the notebook on that day   |
| `e` `i`            | edit the day immediately (month view) |
| `a`                | choose template sections for the day  |
| `m` `y`            | switch to month / year view           |
| `t`                | reset cursor to today                 |
| `Ctrl+T`           | cycle theme: auto → light → dark      |
| `?` `F1`           | key reference and status details      |
| `q` `Ctrl+C` `Esc` | quit                                  |

Month/year jumps clamp to the target month's last valid day and remember the
chosen day: January 31 → February 28 → March 31 (February 29 in leap years).
Moving by a day/week, returning to today, or selecting a date resets that
preferred day. Year view has four columns; Up/Down moves one visual row.
Year timelines cover the complete month: narrow tiles group consecutive days,
with `■` meaning at least one entry in that range and `·` meaning none.

#### Notebook

| Key                  | Action                          |
| -------------------- | ------------------------------- |
| `←/h` `→/l`          | newer / older page              |
| `↑/k` `↓/j`          | scroll content                  |
| `Ctrl+u` `Ctrl+d`    | half-page up / down             |
| `b` `f` `pgup`/`pgdn`| full-page up / down             |
| `g` `G`              | jump to top / bottom            |
| `Enter` `e` `i`      | edit current page               |
| `a`                  | choose template sections         |
| `Esc` `Backspace`    | pop back to calendar (when -c)  |
| `Ctrl+T`             | cycle theme                     |
| `?` `F1`             | key reference and status details |
| `q` `Ctrl+C`         | quit                            |

## Configuration

Optional TOML at `~/.sp/config.toml`. Missing fields fall back to the
defaults shown in `config.example.toml`:

```toml
[ui]
icons = "unicode"   # or "nerd" if you have a Nerd Font installed
theme = "auto"      # "auto" | "light" | "dark"

[templates]
allow_commands = false

[[templates.items]]
name = "Meeting notes"
file = "~/.sp/templates/meeting.md"

# Requires allow_commands = true above.
[[templates.items]]
name = "Issue tracker"
command = ["/path/to/issue-template", "--markdown"]
```

`auto` resolves via `GLAMOUR_STYLE` → `COLORFGBG` → terminal
detection → macOS `AppleInterfaceStyle`. Send `SIGUSR1` (`pkill -USR1
sp`) to re-detect after a manual switch.

Press `a` on a day to open the multi-select template chooser. Markdown files
and command stdout become separate `##` sections; commands receive the selected
date in `SP_DATE`. Relative Markdown file paths resolve from the directory that
contains `config.toml`. Existing content is only appended to, never replaced.
Applied template IDs are stored in scratchpad JSON metadata to prevent duplicate
application without adding markers to the Markdown. Select an already-applied
template again to force a reapply—for example, after manually removing its
section. The built-in **Workday timebox** template remains opt-in like every
configured template.

> **Security:** Command templates are disabled unless
> `templates.allow_commands = true`. Enabling them runs explicitly configured
> programs with your user account's filesystem and network permissions. They
> receive a minimal environment and are limited to 10 seconds and 1 MiB of
> output, but they are not sandboxed and can still read or modify `~/.sp`. Only
> configure commands you fully trust.

## Editor support

**Legacy plaintext mode only:** privacy mode uses the built-in in-memory editor,
not an external editor. External editor plugins, swap files, and backups have no
privacy guarantee. See [private editing and recovery](docs/privacy.md#edit-failures-recovery-and-backups).

In legacy mode, `sp` resolves the editor via `$EDITOR`, then `$VISUAL`, then
platform-specific fallbacks (vim, nvim, nano, micro, emacs, code,
etc.).

```sh
export EDITOR=vim
export EDITOR="code --wait"
export EDITOR='nvim -u "/path with spaces/init.vim"'
```

SP parses quoted executable paths and arguments, then launches the argument
vector directly. POSIX builds support single/double quotes and backslash escapes;
Windows uses native Windows argument quoting. There is no implicit shell:
variables, `~`, globs, pipes, and command substitutions are not expanded. Malformed
POSIX quoting is rejected before launch rather than silently changing arguments.

**Caveat:** GUI editors that fork-and-detach (VS Code without
`--wait`, Sublime, Atom) will return immediately when invoked from
inside the TUI and look broken — `tea.ExecProcess` waits on the
launched process. For those, stick with bare `sp` (the synchronous
path keeps the original GUI polling loop).

## Data storage

Scratchpads live in `~/.sp/<YYYY-MM-DD>.json`. In legacy mode each file holds
plaintext Markdown, date, applied-template metadata, and creation / modified
timestamps. Privacy mode uses one version-2 file per day: an age-encrypted private
payload and plaintext `agent.content` with append provenance. Global
`~/.sp/privacy.json` holds the passphrase-wrapped identity and must be backed up
with the notes. Dates, file sizes, filesystem timestamps, and shared history
remain visible. Encryption does not prevent deletion, rollback, public-metadata
tampering, or unrestricted same-user malware. See the [storage and threat
model](docs/privacy.md#storage-and-cryptography).

Only regular files named with a valid `YYYY-MM-DD.json` date are listed as pages;
a loaded page's stored date must match its filename.

Saves write and sync a temporary file in the same directory before replacing the
note. Files request mode `0600` on Unix; Windows access depends on inherited ACLs,
not Go's POSIX-style mode bits. Encrypted private writes stage only ciphertext;
legacy writes still stage plaintext. On Unix, replacement is atomic and the
containing directory is synced afterward. If that last sync fails, the error explicitly reports that
the note **was replaced**, but its survival across a system crash could not be
confirmed; it does not imply that the old version was restored. Other platforms
use Go's `os.Rename` without a directory-sync or atomic-replacement guarantee.

SP refuses stale writes rather than silently overwriting another instance's
changes. Notes retain their loaded revision; saves check it under a short
per-day OS lock. The TUI also carries the editor's original text through the save
callback and reloads the current note before opening the editor. Lock files
(`.sp-YYYY-MM-DD.lock` and `.sp-privacy.lock`) remain in the storage directory; their locks are released
when the handle closes or the process exits. Do not delete them while SP runs.

In legacy mode, on an edit conflict or editor/save failure, the CLI, notebook,
and calendar keep the edited Markdown buffer and report its path. Copy or merge
that buffer into the current note before retrying; SP does not merge
automatically. Retained paths are printed after leaving the TUI and recorded as
path-only metadata for `sp privacy recover --list`; listed paths may be stale and
are never opened automatically. These buffers live in the OS temporary directory, so they
are not permanent backups. Privacy mode instead attempts encrypted recovery
using the public recipient; if recovery fails, keep the editor open to retain
the in-memory text and retry. See [recovery instructions](docs/privacy.md#edit-failures-recovery-and-backups).
Locks coordinate cooperating SP processes, not arbitrary programs that write
JSON files directly.

## Project layout

```
sp/
├── cmd/sp/                main.go            Cobra entry point
├── internal/
│   ├── config/            config.go          TOML loader
│   ├── editor/            editor.go          editor resolution + Prepare/Edit
│   ├── scratchpad/        scratchpad.go      JSON store, ListDates, Save/Load
│   └── tui/
│       ├── app.go         router model: calendar ↔ notebook ↔ editor
│       ├── calendar.go    full-screen month / year grid
│       ├── notebook.go    glamour viewer with inline edit
│       ├── branding.go    Palette struct + light/dark variants
│       ├── icons.go       IconSet (nerd / unicode)
│       ├── theme.go       glamour style resolution
│       ├── theme_watcher.go  shared SIGUSR1 + plist subscription
│       └── theme_watch_*.go  per-OS plist watchers
├── Makefile               build / test / lint / coverage / release
├── .golangci.yml          mirrors fwrd's lint config
├── .github/workflows/     ci.yml + release.yml
├── config.example.toml
└── .goreleaser.yml
```

## Development

```sh
make build       # ./sp
make test        # all packages
make test-race   # race detector
make privacy-terminal # POSIX PTY enable/edit/recover checks; synthetic stores only
make privacy-fuzz     # bounded parser/append fuzz campaigns
make vuln             # reachable dependency advisories
make coverage    # writes coverage.html
make lint        # golangci-lint (fall back to go vet)
make modernize   # gopls source.fixAll
make ci          # deps + lint + test + build
```

CI runs runtime tests and builds on Linux, macOS, and Windows, with synthetic
terminal checks on Unix, bounded fuzzing, and a reachable-vulnerability gate.
Lint is pinned to `golangci-lint v2.5.0` (v2 configuration for Go 1.25 support).
Workflow configuration is not evidence that a remote run has passed.
See `RELEASE.md` for the GoReleaser flow.

## License

MIT
