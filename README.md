# sp - Daily Scratchpad

A CLI/TUI-based scratchpad application for quickly storing notes, todos, and thoughts. Built with Go and charm.sh tools.

## Features

- **Daily Scratchpad**: Automatically creates a new scratchpad for each day
- **TUI Interface**: Beautiful terminal user interface using charm.sh/bubbletea
- **Markdown Support**: Write notes in markdown format with rich rendering
- **Calendar View**: Browse and select historical scratchpads with TUI
- **Notebook View**: Browse all scratchpads with markdown rendering and navigation
- **External Editor Integration**: Use your preferred editor (vim, VSCode, etc.)
- **Auto-save**: Content is automatically saved when you exit the editor
- **Clean Daily Reset**: Each day starts with a fresh scratchpad
- **Vim-style Navigation**: Familiar key bindings throughout the interface

## Installation

### Prerequisites

- Go 1.24.4 or later

### Build from Source

```bash
git clone https://github.com/pders01/sp.git
cd sp
go build -o sp ./cmd/sp
```

### Install Locally

```bash
go install ./cmd/sp
```

## Usage

### Basic Usage

```bash
# Open today's scratchpad
sp

# Open calendar view to select a date
sp --calendar

# Open notebook view to browse all scratchpads
sp --notebook
```

### Editor Support

The application uses your preferred editor for editing scratchpads. It will:

1. **Check `$EDITOR` environment variable** first
2. **Check `$VISUAL` environment variable** as fallback
3. **Use platform-specific defaults** (vim, nano, etc.)

**Supported editors include:**

- **Terminal editors**: vim, nvim, nano, micro, emacs
- **GUI editors**: VSCode, Sublime Text, Atom, gedit, kate
- **Platform editors**: Notepad (Windows), TextEdit (macOS)

**To set your preferred editor:**

```bash
export EDITOR=vim
# or
export EDITOR="code --wait"
```

### Calendar Controls

- **Arrow Keys**: Navigate through dates
- **Enter**: Select a date and open its scratchpad
- **Ctrl+C** or **Esc**: Exit calendar without selection

### Notebook Controls

The notebook view provides a full-screen interface for browsing all scratchpads with rich markdown rendering:

- **Page Navigation**: `←/h` (previous), `→/l` (next)
- **Content Scrolling**: `↑/k` (up), `↓/j` (down)
- **Page Scrolling**: `Ctrl+u` (page up), `Ctrl+d` (page down), `b/f` (full page)
- **Jump to**: `g` (top), `G` (bottom)
- **Quit**: `q` or `Ctrl+c`

The interface automatically shows neighboring dates in the footer and provides smooth navigation between pages.

## Data Storage

Scratchpads are stored as JSON files in `~/.sp/` directory:

```plain
~/.sp/
├── 2024-01-15.json
├── 2024-01-16.json
└── 2024-01-17.json
```

Each file contains:

- Date
- Content (markdown text)
- Creation timestamp
- Last modified timestamp

## Project Structure

```plain
sp/
├── cmd/sp/
│   └── main.go          # CLI entry point with Cobra integration
├── internal/
│   ├── editor/
│   │   └── editor.go     # External editor integration
│   ├── scratchpad/
│   │   └── scratchpad.go # Core scratchpad logic
│   └── tui/
│       ├── calendar.go   # Calendar view component
│       └── notebook.go   # Notebook view component
├── go.mod
└── README.md
```

## Development

### Dependencies

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles` - TUI components (viewport, textarea)
- `github.com/charmbracelet/lipgloss` - Styling
- `github.com/charmbracelet/glamour` - Markdown rendering
- `github.com/spf13/cobra` - CLI framework
- `github.com/stretchr/testify` - Testing framework

### Building

```bash
go build -o sp ./cmd/sp
```

### Running Tests

```bash
go test ./...
```

## Roadmap

- [x] ~~TUI calendar view for browsing historical entries~~
- [x] ~~External editor integration~~
- [x] ~~Notebook view with markdown rendering~~
- [x] ~~Vim-style navigation controls~~
- [x] ~~Comprehensive test coverage~~
- [ ] Search functionality across all scratchpads
- [ ] Export scratchpads to different formats
- [ ] Tags and categories
- [ ] Backup and sync functionality
- [ ] Custom themes and styling
- [ ] Keyboard shortcuts customization

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

MIT
