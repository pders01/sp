package tui

import (
	"fmt"
	"sort"
	"strings"
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
	theme    *themeWatcher
}

// NewNotebook creates a new notebook instance
func NewNotebook(pages []string) *Notebook {
	sort.Sort(sort.Reverse(sort.StringSlice(pages)))
	return &Notebook{
		pages:    pages,
		contents: make(map[string]string),
		current:  0,
		width:    80,
		height:   24,
		icons:    DefaultIconSet(),
		theme:    newThemeWatcher(ThemePrefAuto),
	}
}

// SetThemePref sets the initial theme preference. Call before Init so
// watchers and the first render pick up the value.
func (n *Notebook) SetThemePref(pref string) {
	n.theme.SetPref(pref)
}

// Init initializes the notebook
func (n *Notebook) Init() tea.Cmd {
	n.theme.start()
	return n.theme.wait()
}

// Close releases resources held by the notebook (theme watchers).
// Safe to call multiple times.
func (n *Notebook) Close() {
	n.theme.stop()
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
		if n.theme.applyResolved() {
			n.updateViewportContent()
			n.theme.SetStatus(MsgThemeApplied(n.theme.Pref(), n.theme.Style()), 2*time.Second)
			cmds = append(cmds, n.theme.expireStatusCmd(2*time.Second))
		}
		cmds = append(cmds, n.theme.wait())
		return n, tea.Batch(cmds...)
	case statusExpireMsg:
		n.theme.HandleStatusExpire()
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
		n.theme.Cycle()
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
	if status := n.theme.StatusText(); status != "" {
		header = lipgloss.JoinHorizontal(
			lipgloss.Top,
			header,
			"   ",
			MutedStyle.Render(status),
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
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(n.theme.Style()),
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
	n.viewport.SetContent(rendered)
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
