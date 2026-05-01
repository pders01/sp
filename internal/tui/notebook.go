package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Notebook represents the notebook view
type Notebook struct {
	pages    []string
	contents map[string]string
	current  int
	viewport viewport.Model
	width    int
	height   int
	quitting bool
	icons    IconSet

	// Glamour rendering. themePref holds the user-facing preference
	// ("auto"/"light"/"dark"); glamourStyle is the resolved style passed
	// to the renderer. The renderer is cached and rebuilt on theme
	// changes or width-driven word-wrap shifts.
	themePref     string
	glamourStyle  string
	rendererStyle string

	// Theme change plumbing. themeEvents is signaled (without payload)
	// whenever an external source — SIGUSR1 or the macOS plist watcher —
	// asks the notebook to re-resolve. The reader-loop tea.Cmd installed
	// in Init waits on this channel and emits themeChangedMsg.
	themeEvents      chan struct{}
	themeWatchCancel context.CancelFunc
	themeWatchWG     sync.WaitGroup

	// Transient status banner shown briefly after a theme swap.
	status        string
	statusExpires time.Time
}

// themeChangedMsg is dispatched by waitThemeChange when an external
// signal source (SIGUSR1, macOS plist watcher) has fired.
type themeChangedMsg struct{}

// statusExpireMsg clears the transient status banner.
type statusExpireMsg struct{}

// NewNotebook creates a new notebook instance
func NewNotebook(pages []string) *Notebook {
	sort.Sort(sort.Reverse(sort.StringSlice(pages)))
	pref := ThemePrefAuto
	return &Notebook{
		pages:        pages,
		contents:     make(map[string]string),
		current:      0,
		width:        80,
		height:       24,
		icons:        DefaultIconSet(),
		themePref:    pref,
		glamourStyle: resolveGlamourStyle(pref),
		themeEvents:  make(chan struct{}, 1),
	}
}

// SetThemePref sets the initial theme preference and re-resolves the
// glamour style. Call before Init so watchers and the first render
// pick up the value.
func (n *Notebook) SetThemePref(pref string) {
	n.themePref = pref
	n.glamourStyle = resolveGlamourStyle(pref)
}

// Init initializes the notebook
func (n *Notebook) Init() tea.Cmd {
	n.startThemeWatchers()
	return n.waitThemeChange()
}

// Close releases resources held by the notebook (theme watchers).
// Safe to call multiple times.
func (n *Notebook) Close() {
	if n.themeWatchCancel != nil {
		n.themeWatchCancel()
		n.themeWatchCancel = nil
	}
	n.themeWatchWG.Wait()
}

// Update handles notebook updates
func (n *Notebook) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		n.width = msg.Width
		n.height = msg.Height
		vpHeight := n.height - 4 // 1 header + 3 footer lines (nav, rule, help)
		n.viewport.Width = n.width
		n.viewport.Height = vpHeight
		n.updateViewportContent()
		return n, nil
	case themeChangedMsg:
		var cmds []tea.Cmd
		if n.applyResolvedStyle() {
			n.updateViewportContent()
			n.setStatus(MsgThemeApplied(n.themePref, n.glamourStyle), 2*time.Second)
			cmds = append(cmds, n.expireStatusAfter(2*time.Second))
		}
		cmds = append(cmds, n.waitThemeChange())
		return n, tea.Batch(cmds...)
	case statusExpireMsg:
		if !n.statusExpires.IsZero() && time.Now().After(n.statusExpires) {
			n.status = ""
			n.statusExpires = time.Time{}
		}
		return n, nil
	case tea.KeyMsg:
		return n.handleKey(msg)
	}
	return n, nil
}

func (n *Notebook) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		n.quitting = true
		return n, tea.Quit
	case "ctrl+t":
		n.themePref = nextThemePref(n.themePref)
		n.signalThemeChange()
		return n, nil
	case "left", "h":
		if n.current > 0 {
			n.current--
			n.updateViewportContent()
			n.viewport.GotoTop()
		}
	case "right", "l":
		if n.current < len(n.pages)-1 {
			n.current++
			n.updateViewportContent()
			n.viewport.GotoTop()
		}
	case "up", "k":
		n.viewport.LineUp(1)
	case "down", "j":
		n.viewport.LineDown(1)
	case "pgup", "b":
		n.viewport.SetYOffset(n.viewport.YOffset - n.viewport.Height)
	case "pgdown", "f":
		n.viewport.SetYOffset(n.viewport.YOffset + n.viewport.Height)
	case "ctrl+u":
		n.viewport.SetYOffset(n.viewport.YOffset - n.viewport.Height/2)
	case "ctrl+d":
		n.viewport.SetYOffset(n.viewport.YOffset + n.viewport.Height/2)
	case "g":
		n.viewport.GotoTop()
	case "G":
		n.viewport.GotoBottom()
	}
	return n, nil
}

// View renders the notebook
func (n *Notebook) View() string {
	if n.quitting {
		return ""
	}
	if len(n.pages) == 0 {
		return MutedStyle.Render("No scratchpad pages found.")
	}

	header := HeaderStyle.Render(
		withIcon(n.icons.Notebook, fmt.Sprintf("Notebook · %s", n.pages[n.current])),
	)
	if n.status != "" {
		header = lipgloss.JoinHorizontal(
			lipgloss.Top,
			header,
			"   ",
			MutedStyle.Render(n.status),
		)
	}

	footer := n.renderFooter()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		n.viewport.View(),
		footer,
	)
}

// renderFooter renders the bottom navigation bar
func (n *Notebook) renderFooter() string {
	if len(n.pages) == 0 {
		return ""
	}

	// Calculate how many dates we can show in the available width
	// Reserve space for controls and separators
	controlsWidth := 60                           // Approximate width for controls
	separatorWidth := 3                           // " | " width
	availableWidth := n.width - controlsWidth - 4 // 4 for padding

	// Calculate width per date (approximate)
	avgDateWidth := 11 // "YYYY-MM-DD" width
	maxVisibleDates := availableWidth / (avgDateWidth + separatorWidth)

	// Ensure we show at least 3 dates (current + 2 neighbors) if possible
	if maxVisibleDates < 3 {
		maxVisibleDates = 3
	}

	// Calculate start index to show current date with neighbors
	// Try to center the current date
	startIdx := n.current - (maxVisibleDates / 2)
	if startIdx < 0 {
		startIdx = 0
	}

	// Ensure we don't go past the end
	if startIdx+maxVisibleDates > len(n.pages) {
		startIdx = len(n.pages) - maxVisibleDates
		if startIdx < 0 {
			startIdx = 0
		}
	}

	// Build visible page indicators
	sep := MutedStyle.Render(" " + n.icons.Sep + " ")
	var pageIndicators []string
	for i := startIdx; i < startIdx+maxVisibleDates && i < len(n.pages); i++ {
		page := n.pages[i]
		if i == n.current {
			pageIndicators = append(pageIndicators, SelectedDateStyle.Render(page))
		} else {
			pageIndicators = append(pageIndicators, MutedStyle.Render(page))
		}
	}

	// Add navigation indicators if there are more pages
	navLine := strings.Join(pageIndicators, sep)
	if startIdx > 0 && n.icons.Prev != "" {
		navLine = MutedStyle.Render(n.icons.Prev) + " " + navLine
	}
	if startIdx+maxVisibleDates < len(n.pages) && n.icons.Next != "" {
		navLine = navLine + " " + MutedStyle.Render(n.icons.Next)
	}

	// Horizontal rule separating dates from keybindings
	rule := SeparatorStyle.Render(strings.Repeat("─", max(n.width, 0)))

	// Controls on separate line
	help := HelpStyle.Render("←/h: prev • →/l: next • ↑/k: up • ↓/j: down • Ctrl+u/d: page up/down • Ctrl+t: theme • q: quit")

	// Center the navigation line
	navStyle := lipgloss.NewStyle().Width(n.width).Align(lipgloss.Center)
	navLine = navStyle.Render(navLine)

	// Join navigation, rule, and controls vertically
	return lipgloss.JoinVertical(
		lipgloss.Left,
		navLine,
		rule,
		help,
	)
}

// updateViewportContent renders the current page's markdown content into the viewport
func (n *Notebook) updateViewportContent() {
	if len(n.pages) == 0 {
		n.viewport.SetContent("")
		return
	}
	content := n.contents[n.pages[n.current]]
	style := n.glamourStyle
	if style == "" {
		style = resolveGlamourStyle(n.themePref)
		n.glamourStyle = style
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(n.width-4),
	)
	if err != nil {
		n.viewport.SetContent(content)
		return
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		n.viewport.SetContent(content)
		return
	}
	n.rendererStyle = style
	n.viewport.SetContent(rendered)
}

// applyResolvedStyle re-resolves the glamour style from the current
// preference and returns true when the style actually changed.
func (n *Notebook) applyResolvedStyle() bool {
	next := resolveGlamourStyle(n.themePref)
	if next == n.glamourStyle {
		return false
	}
	n.glamourStyle = next
	return true
}

// signalThemeChange wakes the watcher reader without blocking. The
// channel is buffered to one slot so coalesced bursts collapse into a
// single re-resolve.
func (n *Notebook) signalThemeChange() {
	if n.themeEvents == nil {
		return
	}
	select {
	case n.themeEvents <- struct{}{}:
	default:
	}
}

// waitThemeChange returns a tea.Cmd that blocks on the next theme
// event and emits themeChangedMsg. Update re-issues this command after
// each event so the watcher behaves like a long-lived subscription.
func (n *Notebook) waitThemeChange() tea.Cmd {
	return func() tea.Msg {
		_, ok := <-n.themeEvents
		if !ok {
			return nil
		}
		return themeChangedMsg{}
	}
}

// startThemeWatchers spawns SIGUSR1 and (on macOS) plist-based theme
// observers. Both write to n.themeEvents. Cancelling the context shuts
// them down via Close.
func (n *Notebook) startThemeWatchers() {
	ctx, cancel := context.WithCancel(context.Background())
	n.themeWatchCancel = cancel

	n.themeWatchWG.Add(1)
	go func() {
		defer n.themeWatchWG.Done()
		watchThemeSignal(ctx, n.signalThemeChange)
	}()

	if err := watchSystemTheme(ctx, &n.themeWatchWG, n.signalThemeChange); err != nil {
		// Watcher unavailable on this platform; SIGUSR1 + Ctrl+T still work.
		_ = err
	}
}

func (n *Notebook) setStatus(msg string, ttl time.Duration) {
	n.status = msg
	n.statusExpires = time.Now().Add(ttl)
}

func (n *Notebook) expireStatusAfter(ttl time.Duration) tea.Cmd {
	return tea.Tick(ttl, func(time.Time) tea.Msg { return statusExpireMsg{} })
}

// SetContents sets the contents for all pages
func (n *Notebook) SetContents(contents map[string]string) {
	n.contents = contents
	n.updateViewportContent()
}

// SetIcons overrides the icon set. Useful for tests or callers that resolve
// icon mode from a higher-level config instead of the SP_ICONS env var.
func (n *Notebook) SetIcons(icons IconSet) {
	n.icons = icons
}

// GetCurrentPage returns the current page date
func (n *Notebook) GetCurrentPage() string {
	if len(n.pages) == 0 {
		return ""
	}
	return n.pages[n.current]
}

// IsQuitting returns whether the notebook is quitting
func (n *Notebook) IsQuitting() bool {
	return n.quitting
}
