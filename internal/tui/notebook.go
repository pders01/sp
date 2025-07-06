package tui

import (
	"fmt"
	"sort"
	"strings"

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
	}
}

// Init initializes the notebook
func (n *Notebook) Init() tea.Cmd {
	return nil
}

// Update handles notebook updates
func (n *Notebook) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		n.width = msg.Width
		n.height = msg.Height
		vpHeight := n.height - 4 // header + footer (now 2 lines)
		n.viewport.Width = n.width
		n.viewport.Height = vpHeight
		n.updateViewportContent()
		return n, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			n.quitting = true
			return n, tea.Quit
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
		case "g":
			n.viewport.GotoTop()
		case "G":
			n.viewport.GotoBottom()
		}
	}
	return n, nil
}

// View renders the notebook
func (n *Notebook) View() string {
	if n.quitting {
		return ""
	}
	if len(n.pages) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			Render("No scratchpad pages found.")
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Render(fmt.Sprintf("📖 Notebook - %s", n.pages[n.current]))

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
	var pageIndicators []string
	for i := startIdx; i < startIdx+maxVisibleDates && i < len(n.pages); i++ {
		page := n.pages[i]
		if i == n.current {
			pageIndicators = append(pageIndicators, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#00FF00")).
				Render(page))
		} else {
			pageIndicators = append(pageIndicators, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#626262")).
				Render(page))
		}
	}

	// Add navigation indicators if there are more pages
	navLine := strings.Join(pageIndicators, " | ")
	if startIdx > 0 {
		navLine = "◀ " + navLine
	}
	if startIdx+maxVisibleDates < len(n.pages) {
		navLine = navLine + " ▶"
	}

	// Controls on separate line
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		Render("←/h: prev • →/l: next • ↑/k: up • ↓/j: down • q: quit")

	// Center the navigation line
	navStyle := lipgloss.NewStyle().Width(n.width).Align(lipgloss.Center)
	navLine = navStyle.Render(navLine)

	// Join navigation and controls vertically
	return lipgloss.JoinVertical(
		lipgloss.Left,
		navLine,
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
		glamour.WithAutoStyle(),
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
