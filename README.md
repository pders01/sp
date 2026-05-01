# sp — Daily Scratchpad

CLI/TUI scratchpad for daily notes, todos, and quick thoughts. Built
with Go and the [charm.sh](https://charm.sh) toolchain.

## Features

- Daily scratchpad: a fresh page per day, kept on disk forever
- Three view modes that drill into one another:
  - Year / month calendar with per-day previews and entry sparklines
  - Notebook viewer with glamour-rendered markdown, scrubbable across
    pages
  - External editor (`$EDITOR`) for actually writing
- One semantic rule across the TUI: **Enter goes deeper**
- Inline editing: hand the editor to `tea.ExecProcess`, save on exit,
  resume the same view with a refreshed render — no shell hop between
  edits
- Light / dark palette swap driven by your terminal: `Ctrl+T` toggles
  in-session, `SIGUSR1` re-detects, macOS appearance changes are
  picked up automatically via an fsnotify watcher on the global
  preferences plist
- Glyph set choice: nerd-font icons or pure-ASCII fallback, set in
  `~/.sp/config.toml`

## Installation

### Homebrew

```sh
brew tap pders01/sp
brew install sp
```

### Build from source

Requires Go 1.24 or later.

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
sp          # open today's page in $EDITOR
sp -n       # notebook viewer; Enter / e / i opens the editor
sp -c       # calendar; Enter drills into the notebook on that day
sp --version
```

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

#### Calendar

| Key                | Action                                |
| ------------------ | ------------------------------------- |
| `←/h` `→/l`        | day (month view) / month (year view)  |
| `↑/k` `↓/j`        | week (month view) / row (year view)   |
| `H` `L`            | jump month / year                     |
| `Enter`            | drill into the notebook on that day   |
| `e` `i`            | edit the day immediately (month view) |
| `m` `y`            | switch to month / year view           |
| `t`                | reset cursor to today                 |
| `Ctrl+T`           | cycle theme: auto → light → dark      |
| `q` `Ctrl+C` `Esc` | quit                                  |

#### Notebook

| Key                  | Action                          |
| -------------------- | ------------------------------- |
| `←/h` `→/l`          | previous / next page            |
| `↑/k` `↓/j`          | scroll content                  |
| `Ctrl+u` `Ctrl+d`    | half-page up / down             |
| `b` `f` `pgup`/`pgdn`| full-page up / down             |
| `g` `G`              | jump to top / bottom            |
| `Enter` `e` `i`      | edit current page               |
| `Esc` `Backspace`    | pop back to calendar (when -c)  |
| `Ctrl+T`             | cycle theme                     |
| `q` `Ctrl+C`         | quit                            |

## Configuration

Optional TOML at `~/.sp/config.toml`. Missing fields fall back to the
defaults shown in `config.example.toml`:

```toml
[ui]
icons = "unicode"   # or "nerd" if you have a Nerd Font installed
theme = "auto"      # "auto" | "light" | "dark"
```

`auto` resolves via `GLAMOUR_STYLE` → `COLORFGBG` → terminal
detection → macOS `AppleInterfaceStyle`. Send `SIGUSR1` (`pkill -USR1
sp`) to re-detect after a manual switch.

## Editor support

`sp` resolves the editor via `$EDITOR`, then `$VISUAL`, then
platform-specific fallbacks (vim, nvim, nano, micro, emacs, code,
etc.).

```sh
export EDITOR=vim
export EDITOR="code --wait"
```

**Caveat:** GUI editors that fork-and-detach (VS Code without
`--wait`, Sublime, Atom) will return immediately when invoked from
inside the TUI and look broken — `tea.ExecProcess` waits on the
launched process. For those, stick with bare `sp` (the synchronous
path keeps the original GUI polling loop).

## Data storage

Scratchpads live in `~/.sp/<YYYY-MM-DD>.json`. Each file holds the
date, content (raw markdown), and creation / modified timestamps.

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
make coverage    # writes coverage.html
make lint        # golangci-lint (fall back to go vet)
make modernize   # gopls source.fixAll
make ci          # deps + lint + test + build
```

CI matrix runs on Linux, macOS, and Windows; lint pinned to
`golangci-lint v1.64.8`. See `RELEASE.md` for the GoReleaser flow.

## License

MIT
