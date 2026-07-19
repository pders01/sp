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
	"github.com/pders01/sp/internal/editor"
)

// Saver persists an edited scratchpad. Returning a non-nil error
// surfaces a transient banner inside the view but does not exit it.
type Saver func(date string, content string) error

// editDoneMsg is dispatched after tea.ExecProcess has handed control
// back from the external editor.
type editDoneMsg struct {
	date    string
	path    string
	cleanup func()
	err     error
}

// Notebook represents the notebook view
type Notebook struct {
	pages    []string
	contents map[string]string
	current  int
	viewport viewport.Model
	width    int
	height   int
	quitting bool
	popping  bool
	selected string
	icons    IconSet
	theme    *themeWatcher
	editor   *editor.Editor
	save     Saver
}

// NewNotebook creates a new notebook instance. Pages are copied and
// sorted descending internally so the caller's slice keeps its order.
func NewNotebook(pages []string) *Notebook {
	owned := append([]string(nil), pages...)
	sort.Sort(sort.Reverse(sort.StringSlice(owned)))
	return &Notebook{
		pages:    owned,
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

// SetEditor wires the external editor and a save callback. With both
// set, Enter / e / i suspend the TUI, run the editor via
// tea.ExecProcess, persist changes, and resume the notebook with the
// updated content. With either unset, those keys remain a quit-with-
// selected fallback so the caller can run the editor itself.
func (n *Notebook) SetEditor(ed *editor.Editor, save Saver) {
	n.editor = ed
	n.save = save
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
	case editDoneMsg:
		return n.finishEdit(msg)
	case tea.KeyMsg:
		return n.handleKey(msg)
	}
	return n, nil
}

// startEdit prepares the editor command for the given date and asks
// bubbletea to suspend the TUI while it runs. Returns nil when no
// editor is wired or preparation fails (the caller falls back to
// quit-with-selected so the orchestrator can run the editor).
func (n *Notebook) startEdit(date string) tea.Cmd {
	if n.editor == nil || n.save == nil {
		return nil
	}
	cmd, path, cleanup, err := n.editor.Prepare(n.contents[date])
	if err != nil {
		n.flashError(fmt.Sprintf("editor prepare: %v", err))
		return n.theme.expireStatusCmd(2 * time.Second)
	}
	return tea.ExecProcess(cmd, func(execErr error) tea.Msg {
		return editDoneMsg{date: date, path: path, cleanup: cleanup, err: execErr}
	})
}

// finishEdit reads the edited buffer, persists changes, and refreshes
// the rendered viewport. Errors surface as transient status banners.
func (n *Notebook) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	if msg.cleanup != nil {
		defer msg.cleanup()
	}
	if msg.err != nil {
		n.flashError(fmt.Sprintf("editor: %v", msg.err))
		return n, n.theme.expireStatusCmd(2 * time.Second)
	}
	newContent, rerr := editor.ReadEdited(msg.path)
	if rerr != nil {
		n.flashError(fmt.Sprintf("read: %v", rerr))
		return n, n.theme.expireStatusCmd(2 * time.Second)
	}
	if newContent == n.contents[msg.date] {
		return n, nil
	}
	n.contents[msg.date] = newContent
	if serr := n.save(msg.date, newContent); serr != nil {
		n.flashError(fmt.Sprintf("save: %v", serr))
		return n, n.theme.expireStatusCmd(2 * time.Second)
	}
	n.theme.SetStatus("Saved", 1500*time.Millisecond)
	n.updateViewportContent()
	return n, n.theme.expireStatusCmd(1500 * time.Millisecond)
}

func (n *Notebook) flashError(text string) {
	n.theme.SetStatus(text, 2*time.Second)
}

func (n *Notebook) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		n.quitting = true
		return n, nil
	case "esc", "backspace":
		n.popping = true
		return n, nil
	case "enter", "e", "i":
		if len(n.pages) == 0 {
			return n, nil
		}
		date := n.pages[n.current]
		if cmd := n.startEdit(date); cmd != nil {
			return n, cmd
		}
		// Fallback: caller wired no editor; signal the orchestrator.
		n.selected = date
		n.quitting = true
		return n, nil
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
		return n.theme.Palette().MutedText.Render("No scratchpad pages found.")
	}

	header := n.theme.Palette().Header.Render(
		withIcon(n.icons.Notebook, fmt.Sprintf("Notebook · %s", n.pages[n.current])),
	)
	if status := n.theme.StatusText(); status != "" {
		header = lipgloss.JoinHorizontal(
			lipgloss.Top,
			header,
			"   ",
			n.theme.Palette().MutedText.Render(status),
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
	sep := n.theme.Palette().MutedText.Render(" " + n.icons.Sep + " ")
	var pageIndicators []string
	for i := startIdx; i < startIdx+maxVisibleDates && i < len(n.pages); i++ {
		page := n.pages[i]
		if i == n.current {
			pageIndicators = append(pageIndicators, n.theme.Palette().SelectedDate.Render(page))
		} else {
			pageIndicators = append(pageIndicators, n.theme.Palette().MutedText.Render(page))
		}
	}

	// Add navigation indicators if there are more pages
	navLine := strings.Join(pageIndicators, sep)
	if startIdx > 0 && n.icons.Prev != "" {
		navLine = n.theme.Palette().MutedText.Render(n.icons.Prev) + " " + navLine
	}
	if startIdx+maxVisibleDates < len(n.pages) && n.icons.Next != "" {
		navLine = navLine + " " + n.theme.Palette().MutedText.Render(n.icons.Next)
	}

	// Horizontal rule separating dates from keybindings
	rule := n.theme.Palette().Separator.Render(strings.Repeat("─", max(n.width, 0)))

	// Controls on separate line
	help := n.theme.Palette().Help.Render("←/h: prev • →/l: next • ↑/k: up • ↓/j: down • Ctrl+u/d: page up/down • enter/e: edit • esc: back • Ctrl+t: theme • q: quit")

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

// SetPageContent refreshes one page after templates are applied.
func (n *Notebook) SetPageContent(date, content string) {
	n.contents[date] = content
	if len(n.pages) > 0 && n.pages[n.current] == date {
		n.updateViewportContent()
		n.viewport.GotoTop()
	}
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

// GetSelectedDate returns the date the user committed to (Enter/e/i),
// or empty when the user quit without selecting.
func (n *Notebook) GetSelectedDate() string {
	return n.selected
}

// SetCurrentDate positions the cursor on the given date if present in
// the page list. No-op when the date is unknown.
func (n *Notebook) SetCurrentDate(date string) {
	for i, p := range n.pages {
		if p == date {
			n.current = i
			n.updateViewportContent()
			n.viewport.GotoTop()
			return
		}
	}
}

// IsQuitting returns whether the notebook is quitting
func (n *Notebook) IsQuitting() bool {
	return n.quitting
}

// IsPopping reports whether the user requested back-navigation
// (Esc/Backspace). The router pops to the calendar in -c mode and
// quits the program in -n mode.
func (n *Notebook) IsPopping() bool { return n.popping }

// ClearState resets quit / pop / selected so the router can resume the
// notebook after pulling it back into focus.
func (n *Notebook) ClearState() {
	n.quitting = false
	n.popping = false
	n.selected = ""
}

// CurrentContent returns the rendered content of the active page.
// Used by the router so the calendar can update its hasData/previews
// after the notebook persists an inline edit.
func (n *Notebook) CurrentContent() (date, content string) {
	if len(n.pages) == 0 {
		return "", ""
	}
	d := n.pages[n.current]
	return d, n.contents[d]
}

// AddPage inserts the given date into the page list (sorted descending)
// when not already present and seeds an empty content entry. No-op when
// the date is already known.
func (n *Notebook) AddPage(date string) {
	for _, p := range n.pages {
		if p == date {
			return
		}
	}
	n.pages = append(n.pages, date)
	sort.Sort(sort.Reverse(sort.StringSlice(n.pages)))
	if _, ok := n.contents[date]; !ok {
		n.contents[date] = ""
	}
}
