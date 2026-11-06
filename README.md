# sp — Daily Scratchpad

CLI/TUI scratchpad for daily notes, todos, and quick thoughts. Built
with Go and the [charm.sh](https://charm.sh) toolchain.

> **Opt-in privacy (v0.22.0):** existing notes stay plaintext until you
> explicitly enable it. Privacy encrypts every private day (including today),
> while shared agent history deliberately remains plaintext. Read the
> [privacy guide, migration instructions, and security limits](docs/privacy.md)
> before enabling. See the [test results and remaining gaps](docs/privacy-validation.md):
> automated tests pass, but the live model-driven trial was provider-blocked.
> This is not an independent security audit. **Old SP versions are unsafe for
> privacy stores:** stop all instances and use only a privacy-aware build.

## Features

- Daily scratchpad: a fresh page per day, kept on disk forever
- Explicitly opt-in private-note encryption, with a personal-note-first notebook,
  separate read-only shared updates in the same daily file, and a locked append
  CLI for tools
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

Keep `third_party/cancelreader` with the checkout: it carries an attributed
terminal-reader teardown fix via a local Go module replacement. Version-suffixed
`go install ...@version` is not supported while this replacement is required;
use a release binary or build from the complete checkout. See
[patch details](third_party/cancelreader/PATCHES.md).

## Usage

```sh
sp          # open today's page ($EDITOR in legacy plaintext mode)
sp -n       # notebook viewer; Enter / e / i opens the editor
sp -c       # calendar; Enter drills into the notebook on that day
sp --version
```

Choose either `-n` or `-c`; enabling both is an error rather than silently
selecting a view. Explicit `--calendar=false`/`--notebook=false` values are allowed.
Notebook startup prefers today, then the newest earlier saved date, then the
nearest future date; fallback choices are labelled. An empty notebook stays open:
Enter/e/i starts today's note (unlocking first in privacy mode). Opening or
quitting the empty viewer does not save a note. Reading positions are remembered
per date for the current session, including calendar round-trips, and clamped
when content or layout changes. The position cache holds offsets, not note text,
and is not persisted. Dates run left to right in ascending order: Left/h opens
an earlier saved date, Right/l a later one.

The primary notebook shows only your personal note. Press `c` in an ordinary
calendar or notebook viewer to open separate, read-only **Shared updates** for
that date—even while locked, without unlocking or loading the private note.
Shared updates are labelled **Not encrypted**; provenance is self-reported, not
verified authorship. Left/h and Right/l browse earlier/later saved dates while
staying in shared updates, without loading private notes—even when unlocked.
Days without updates or with unavailable shared data remain explicitly labelled
in this view. `Esc` or `c` returns to the selected day's personal note; personal
and shared reading offsets are remembered independently. If help is open, close
that layer first; Escape closes help only.

### Opt-in privacy usage (privacy-aware builds)

```sh
sp privacy enable   # confirm new passphrase twice; retained keys authenticate once
sp privacy status   # bounded policy/key summary without unlocking
sp privacy status --dates # explicitly list every date by storage state
sp privacy disable  # confirm + existing passphrase; decrypt daily notes in place
sp privacy recover --list          # discover recovery paths without unlocking
sp privacy recover <recovery-file> # interactively review encrypted recovery; no auto-merge
sp append 2026-01-15 --section 'Agent notes' --source 'local-helper' -- '- Public task'
sp shared 2026-01-15 # shared-only read; no private unlock
sp shared 2026-01-15 --json # explicit missing/no-shared/content outcome
sp append 2026-01-15 --json --section 'Agent notes' -- '- Public task' # added/duplicate arrays
```

In privacy mode, `u` opens a masked TUI unlock prompt; `Enter`/`e` edits privately
in memory (calendar `Enter` opens the notebook first). External editors are not
enabled for privacy editing in the initial version. Unlock lasts only in that
process; Ctrl+L explicitly relocks and seals changed drafts first. If recovery
fails, the draft stays usable instead of being discarded. Locked template actions
prompt and resume, and incorrect passphrases remain masked for retry. Unlocking
shows busy progress; cancellation waits for owned work and locks before returning.
Never put a passphrase/private key in arguments, environment, or config.
Back up `privacy.json` with the notes: losing the passphrase or wrapped identity
means losing the private writing. Shared bullets and source labels are public,
self-reported data, not authenticated authorship. `sp privacy disable` restores
plaintext editing; interrupted reversal resumes with the same command. It retains
the wrapped key for encrypted backups/recovery files. Re-enabling uses the existing
passphrase/key (one authentication entry, not creation/confirmation) and does not
erase plaintext copies. Status labels plaintext as normal after disabling and
states the retained-key backup obligation. The writer/recovery view keeps a
**Saving encrypted** or **Saving as plaintext** caption visible; a concurrent
store-policy change blocks recovery saving until recovery is reopened. Concise
outcomes distinguish saved, unchanged, cancelled, not saved, and unconfirmed
durability; F1 retains full error/recovery details. Newer edits after an
unconfirmed publication are explicitly labelled unsaved. Interactive
migration/reversal reports stage/count progress, not secrets. Raw shared reads and quiet append remain the
default; `--json` is opt-in and contains shared data only. Passphrase rotation and MCP
access are not implemented. See the [full guide](docs/privacy.md).

### Agent automation

See [agent session-end notes and weekly summaries](docs/agent-hooks.md) for an
opt-in hook using reviewed public manifests and a collector that reads only
`sp shared --json`. It does not install hooks, read transcripts, unlock private
notes or provide a secrets detector. All summaries remain deliberate public
shared content.

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

Shared `sp`/location chrome and branded Glamour headings unify the screens.
Compact footers show fewer default actions; press `?` or `F1` in a viewer or
template chooser for the full grouped, scrollable key reference and status details.
Bare `sp` and encrypted recovery use the same themed, masked unlock screen as
the viewers—not a separate inline password prompt. Enter unlocks; Escape or an
empty submission cancels these standalone screens. F1 shows contextual help.

In the built-in editor or TUI unlock prompt, use **F1 only**: `?` stays literal.
The in-memory private writer is titled **Write / date**, with themed help and
conflict review. It opens at the start and provides Ctrl+Z/Y
undo/redo (256 edits/16 MiB of history), Page Up/Down, Ctrl+Left/Right word movement,
Ctrl+Home/End document movement, and Ctrl+F/F3 find within the current draft only.
Find owns Enter/Escape and cannot save the note. Line/column and off-screen column
indicators orient the literal horizontal viewport. These features do not write
undo/search files, normalize Markdown, or settle the pending Neovim integration
choice.
Within help, `Esc`/`F1`/`q` closes only the reference; arrows, Page Up/Down, and
`g`/`G` scroll it. Unsupported screen sizes show a resize hint without discarding
notes, drafts, passphrases, or template selections. The unlock prompt updates
truthfully on resize and cannot submit a hidden form at unsupported sizes.
Physical Ctrl+T cycles appearance in the private writer too, except while help
or find owns the keys; literal/pasted text and lossless input remain unchanged.

Press `d` in an ordinary viewer to jump to an exact `YYYY-MM-DD` date. Calendar
jumps focus that day in month view; notebook jumps open the selected day and
restore its session reading position. Missing dates can be selected without
saving a note. Enter confirms a valid date; Escape/Ctrl+C cancels, Ctrl+U clears,
and F1 opens help. Invalid dates/pastes are rejected, not trimmed or clamped.
The shortcut does not apply while editing, unlocking, or using other modals.
Private date navigation loads only the selected notebook page, never a history
search or archive-wide decryption; calendar jumps do not decrypt private notes.

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
| `d`                | jump to an exact date in month view   |
| `c`                | read shared updates for the day       |
| `Ctrl+T`           | cycle theme: auto → light → dark      |
| `?` `F1`           | key reference and status details      |
| `Esc`              | year → month; quit from month view    |
| `q` `Ctrl+C`       | quit directly from either view        |

Month/year jumps clamp to the target month's last valid day and remember the
chosen day: January 31 → February 28 → March 31 (February 29 in leap years).
Moving by a day/week, returning to today, or selecting a date resets that
preferred day. Year view has four columns; Up/Down moves one visual row.
Year timelines cover the complete month: narrow tiles group consecutive days,
with `■` meaning at least one entry in that range and `·` meaning none.
Month layout uses compact cells when needed to show a labelled excerpt at ordinary
terminal heights. Enter opens the full note; calendar previews remain public-only
in privacy mode and never trigger private decryption.

#### Notebook

| Key                  | Action                          |
| -------------------- | ------------------------------- |
| `←/h` `→/l`          | earlier / later saved date       |
| `↑/k` `↓/j`          | scroll content                  |
| `Ctrl+u` `Ctrl+d`    | half-page up / down             |
| `b` `f` `pgup`/`pgdn`| full-page up / down             |
| `g` `G`              | jump to top / bottom            |
| `d`                  | jump to an exact date           |
| `Enter` `e` `i`      | edit current page               |
| `a`                  | choose template sections         |
| `c`                  | read shared updates (Esc/c returns) |
| `Esc` `Backspace`    | pop back to calendar (when -c)  |
| `Ctrl+T`             | cycle theme                     |
| `?` `F1`             | key reference and status details |
| `q` `Ctrl+C`         | quit                            |

## Configuration

Optional TOML at `~/.sp/config.toml`. Missing fields fall back to the
defaults shown in `config.example.toml`. Unknown keys and invalid UI values produce
specific warnings while preserving valid settings; syntax errors use safe defaults.
Warnings remain in viewer F1 details after later status messages. Read-only
browsing does not resolve an editor or validate template actions until requested:
a broken EDITOR or untrusted/invalid template definition does not hide your notes.

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
sp`) to re-detect after a manual switch. Calendar/notebook and private writer
share the application appearance preference; standalone private editing/recovery
also honors the configured theme. Ctrl+T acknowledges preference changes even
if the resolved colors stay the same; changes are session-only and do not rewrite
`config.toml`.

Press `a` on a day to open the multi-select template chooser. Markdown files
and command stdout become separate `##` sections; commands receive the selected
date in `SP_DATE`. Relative Markdown file paths resolve from the directory that
contains `config.toml`. Existing content is only appended to, never replaced.
Applied template IDs are stored in scratchpad JSON metadata to prevent duplicate
application without adding markers to the Markdown. Selection checkboxes are
separate from `[applied]` history. Selecting an applied section means **append
again**: another copy is added, not refreshed or replaced.

Enter is **SAVE NOW, then edit**. Cancelling the subsequent editor does not undo
template insertion. Escape while applying requests cancellation and waits for
the actual outcome; an insertion already published is not rolled back. Errors
retain selections and full details for correction/retry. Closing the application
also cancels and waits for owned template work. The built-in **Workday timebox**
template remains opt-in like every configured template.

> **Security:** Command templates are disabled unless
> `templates.allow_commands = true`. Enabling them runs explicitly configured
> programs with your user account's filesystem and network permissions. They
> receive a minimal environment, a 10-second deadline, and 1 MiB per output
> stream. Pipe draining has an additional 250 ms bound. Darwin/Linux cancel the
> owned process group; escaped descendants are not contained, and other platforms
> cancel only the immediate child. Commands are not sandboxed and can still read
> or modify `~/.sp`. Only configure commands you fully trust.

## Editor support

**Interactive input compatibility:** notebook/calendar, masked unlock and the
built-in writer now use a shared byte-preserving input boundary. They require
terminal input/output, not redirected secret input or captured private screens.
Windows interactive flows currently **fail closed** because the console reader
can bypass that boundary. Shared append/read/status and bare legacy external
editing remain available; Windows interactive support needs a safe console
adapter and native validation. See the [security remediation report](docs/security-remediation.md).

**Legacy plaintext mode only:** privacy mode uses the built-in in-memory editor,
not an external editor. The notebook's personal note and separate read-only
shared updates are independently rendered under application-owned labels, so
note Markdown cannot consume the shared provenance boundary. The custom lossless
input is unchanged by the redesign. Neovim/external-private-editor integration
remains unresolved; no plaintext fallback is introduced. External editor plugins,
swap files, and backups have no privacy guarantee. See [private editing and recovery](docs/privacy.md#edit-failures-recovery-and-backups).

In legacy mode, an explicit edit resolves the editor via `$EDITOR`, then `$VISUAL`, then
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

Both bare `sp` and TUI editing wait for the prepared editor process to finish,
not for the first file modification. Recognized GUI launchers receive their
supported wait/foreground option (`code --wait`, `kate --block`, `gvim --nofork`,
etc.); supplied options are retained. Unsupported detached launchers such as
`open`, TextEdit, modern Notepad, and direct Electron GUI `.exe` binaries are
rejected with configuration advice; a GUI binary is not its blocking CLI wrapper.
Custom wrappers must remain in the foreground until editing is finished.
Closing unchanged returns normally; intermediate saves do not finish the edit.

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
and calendar keep the edited Markdown buffer and report its path. Reopening the
edit offers a scrollable comparison with the current saved text. Select `d` for
the retained draft or `r` for saved text, then confirm with a physical Enter.
A fresh editor buffer is opened; the failed one stays intact. Choosing the draft
explicitly authorizes a replacement attempt when the editor returns successfully.
Any further concurrent change still causes a conflict; SP never auto-merges.

Retained paths print after leaving the TUI and are recorded as path-only metadata
for `sp privacy recover --list`; listed paths may be stale and are never opened
automatically. To explicitly review a plaintext buffer from an earlier process:

```sh
sp privacy recover --plaintext-date YYYY-MM-DD <buffer-file>
```

This interactive path refuses privacy-enabled stores. Temporary editor buffers
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
make test-race   # race detector + coverage (matches the CI test command)
make privacy-terminal # POSIX PTY enable/edit/recover checks; synthetic stores only
make ux-terminal      # integrated reader/editor/template/privacy UX; synthetic stores only
make design-terminal  # 4 synthetic PTY cases: dark/light × 100x30/56x16
make design-capture   # 418 actual frames / 55 scenes + historical concepts; needs ImageMagick
make privacy-fuzz     # five bounded record/private/append fuzz campaigns
make test-release     # stub-Git release checks + offline synthetic asset checks
make vuln             # reachable dependency advisories
make coverage    # writes coverage.html
make lint        # CI-pinned golangci-lint v2.5.0; no vet-only fallback
make modernize   # gopls source.fixAll
make ci          # deps + pinned lint + race/coverage + release checks + vuln + build
```

CI runs runtime tests and builds on Linux, macOS, and Windows, with synthetic
terminal checks on Unix, bounded fuzzing, and a reachable-vulnerability gate.
Lint is pinned to `golangci-lint v2.5.0` (v2 configuration for Go 1.25 support).
Workflow configuration is not evidence that a remote run has passed.
See `RELEASE.md` for the GoReleaser flow.

The cohesive TUI redesign is implemented: personal-note-first reading, secondary
shared updates, ascending date navigation, and shared reader/writer chrome.
`make design-capture` captures 418 actual application frames across 55 synthetic
scenes alongside labelled historical concepts from the
[visual design study](docs/design/2026-10-31/README.md); concept images are not
runtime evidence.

Local CI-equivalent checks and matching synthetic PTYs pass; see the
[release preflight record](docs/release-preflight.md) for exact tool versions,
candidate hashes, packaging evidence and remaining manual/hosted checks.
The release owner reports hosted CI green through the final preparation commit,
plus the rc.3 release workflow green. The annotated local `v0.22.0` tag identifies
`b8f4eb3`. User-supplied GitHub metadata confirms the stable release was
[published November 3, 2026](https://github.com/pders01/sp/releases/tag/v0.22.0),
with all five platform archives and `checksums.txt` uploaded. Downloaded checksum
verification and the Homebrew update remain unconfirmed here.
Passing Windows unit CI is not Windows interactive, native GUI/accessibility,
or independent audit sign-off. Later documentation updates on `main` do not
change the tagged release or its assets.

## License

MIT
