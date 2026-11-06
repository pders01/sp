package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

// Saver persists an edited scratchpad after checking the text originally handed
// to the editor. Returning an error keeps the buffer and does not exit the view.
type Saver func(date, original, content string) error

// editDoneMsg is dispatched after tea.ExecProcess has handed control
// back from the external editor.
type editDoneMsg struct {
	date        string
	original    string
	path        string
	cleanup     func()
	err         error
	unconfirmed bool // Retry must confirm a published replacement, even unchanged.
}

// Notebook represents the notebook view
type Notebook struct {
	pages              []string
	contents           map[string]string
	current            int
	today              string // Session-start date for startup and creation.
	diagnostics        string
	viewport           viewport.Model
	positions          map[string]int  // Session-only offsets, never private text.
	personalDates      map[string]bool // Public presence: saved-empty differs from absent.
	viewportDate       string          // The date actually rendered, not an index.
	viewportReady      bool            // False for transient private/error projections.
	width              int
	height             int
	quitting           bool
	popping            bool
	selected           string
	icons              IconSet
	theme              *themeWatcher
	editor             *editor.Editor
	editorProvider     EditorProvider
	save               Saver
	loader             func(date string) (string, error)
	templatesAvailable bool
	dateJumpAvailable  bool
	privacyEnabled     bool
	privacyState       func() string
	canRelock          bool
	privatePage        *PrivatePage
	privateDate        string
	sharedContents     map[string]string
	shared             *sharedUpdates
	sharedPositions    map[string]int  // Read-only shared offsets; never private text.
	sharedInspections  map[string]bool // Present: authoritative public result; false: unavailable.
	onChange           func(date, content string)
	onLoad             func(date, content string)
	onRecovery         func(scratchpad.RecoveryReference)
	onConsumed         func(date, path string)
	canPop             bool
	keyHelp            *keyHelp
	drafts             map[string]editDoneMsg
	review             *externalDraftReview
}

// NewNotebook creates a new notebook instance. Pages are copied and
// sorted descending internally so the caller's slice keeps its order.
func NewNotebook(pages []string) *Notebook {
	return newNotebookAt(pages, time.Now().Format(time.DateOnly))
}

func newNotebookAt(pages []string, today string) *Notebook {
	owned := append([]string(nil), pages...)
	sort.Sort(sort.Reverse(sort.StringSlice(owned)))
	n := &Notebook{
		pages:             owned,
		contents:          make(map[string]string),
		drafts:            make(map[string]editDoneMsg),
		positions:         make(map[string]int),
		personalDates:     make(map[string]bool),
		sharedPositions:   make(map[string]int),
		sharedInspections: make(map[string]bool),
		current:           notebookStartIndex(owned, today),
		today:             today,
		width:             80,
		height:            24,
		icons:             DefaultIconSet(),
		theme:             newThemeWatcher(ThemePrefAuto),
	}
	for _, date := range owned {
		n.personalDates[date] = true
	}
	return n
}

// SetThemePref sets the initial theme preference. Call before Init so
// watchers and the first render pick up the value.
func (n *Notebook) SetThemePref(pref string) {
	n.theme.SetPref(pref)
}

// SetEditor wires the external editor and a save callback. An optional loader
// refreshes the note before editing so a stale preview does not seed stale edits.
// Enter / e / i suspend the TUI via tea.ExecProcess and resume after saving.
// Without an editor or saver, those keys fall back to quit-with-selected.
func (n *Notebook) SetEditor(ed *editor.Editor, save Saver, load func(date string) (string, error)) {
	n.editor = ed
	n.editorProvider = nil
	n.save = save
	n.loader = load
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
		n.width, n.height = max(0, msg.Width), max(0, msg.Height)
		n.updateViewportContent()
		n.layoutViewport()
		if n.keyHelp != nil {
			n.keyHelp.resize(n.width, n.height)
		}
		if n.review != nil {
			n.review.viewer.resize(n.width, n.height)
		}
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
		if n.sharedActive() {
			return n.updateShared(msg)
		}
		if n.review != nil {
			return n.updateDraftReview(msg)
		}
		if n.keyHelp != nil {
			if n.keyHelp.update(msg) {
				n.keyHelp = nil
			}
			return n, nil
		}
		return n.handleKey(msg)
	}
	return n, nil
}

// startEdit prepares the editor command for the given date and asks
// bubbletea to suspend the TUI while it runs. Returns nil when no
// editor is wired or preparation fails (the caller falls back to
// quit-with-selected so the orchestrator can run the editor).
func (n *Notebook) startEdit(date string) tea.Cmd {
	if n.sharedActive() {
		return nil
	}
	if n.editor == nil && n.editorProvider != nil {
		ed, err := n.editorProvider()
		if err != nil {
			return unavailableEdit(func(text string) { n.theme.SetStatus(text, 0) }, err)
		}
		n.editor = ed
	}
	if n.editor == nil || n.save == nil {
		return nil
	}
	original := n.contents[date]
	if n.loader != nil {
		content, err := n.loader(date)
		if err != nil {
			n.flashError(fmt.Sprintf("load: %v", err))
			return n.theme.expireStatusCmd(2 * time.Second)
		}
		original = content
		if n.onLoad != nil {
			n.onLoad(date, content)
		} else {
			n.contents[date] = content
			n.updateViewportContent()
		}
	}
	if draft, ok := n.drafts[date]; ok {
		n.review = reviewExternalDraft(draft, original, n.width, n.height)
		return func() tea.Msg { return nil }
	}
	cmd, err := prepareExternalDraft(n.editor, date, original, original, false, n.onRecovery)
	if err != nil {
		n.flashError(fmt.Sprintf("editor prepare: %v", err))
		return n.theme.expireStatusCmd(2 * time.Second)
	}
	return cmd
}

// finishEdit reads the edited buffer, persists changes, and refreshes
// the rendered viewport. Failures retain the buffer and show its path
// without a timeout; cleanup happens only after success or an unchanged edit.
func (n *Notebook) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	cmd := finishExternalDraft(msg, n.save, n.drafts, n.theme, n.onChange, n.SetPageContent, n.onRecovery, n.onConsumed)
	return n, cmd
}

func (n *Notebook) flashError(text string) {
	n.theme.SetStatus(text, 2*time.Second)
}

func (n *Notebook) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Multi-rune text and modified rune keys never act as commands. Async
	// paste messages are ignored by Update rather than forwarded to an editor.
	if msg.Type == tea.KeyRunes && (msg.Alt || len(msg.Runes) != 1) {
		return n, nil
	}
	if helpRequested(msg, true) {
		n.keyHelp = newKeyHelp("Notebook", n.keyBindings(), diagnosticDetails(n.theme.StatusText(), n.diagnostics), n.width, n.height)
		return n, nil
	}
	n.layoutViewport()
	switch msg.String() {
	case "ctrl+c", "q":
		n.quitting = true
		return n, nil
	case "esc", "backspace":
		n.popping = true
		return n, nil
	case "enter", "e", "i":
		if msg.Type != tea.KeyEnter && (msg.Type != tea.KeyRunes || len(msg.Runes) != 1 || msg.Alt) {
			return n, nil // Literal key-name text is not an edit/create action.
		}
		date := n.editDate()
		if cmd := n.startEdit(date); cmd != nil {
			return n, cmd
		}
		// Fallback: caller wired no editor; signal the orchestrator.
		n.selected = date
		n.quitting = true
		return n, nil
	case "c":
		n.openShared()
		return n, nil
	case "ctrl+t":
		n.theme.Cycle()
		return n, nil
	case "left", "h":
		if n.current < len(n.pages)-1 {
			n.SetCurrentDate(n.pages[n.current+1])
		}
	case "right", "l":
		if n.current > 0 {
			n.SetCurrentDate(n.pages[n.current-1])
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
		n.viewport.SetYOffset(n.viewport.YOffset - max(1, n.viewport.Height/2))
	case "ctrl+d":
		n.viewport.SetYOffset(n.viewport.YOffset + max(1, n.viewport.Height/2))
	case "g":
		n.viewport.GotoTop()
	case "G":
		n.viewport.GotoBottom()
	}
	return n, nil
}

// View renders the notebook
func (n *Notebook) View() string {
	if n.review != nil {
		return n.review.viewer.view(n.theme.Palette())
	}
	if n.quitting {
		return ""
	}
	if n.sharedActive() {
		return n.sharedView()
	}
	if n.keyHelp != nil {
		return n.keyHelp.view(n.theme.Palette())
	}
	if len(n.pages) == 0 {
		return n.emptyView()
	}
	n.layoutViewport()
	header, footer := n.renderHeader(), n.renderFooter()
	minHeight := viewHeight(header) + viewHeight(footer) + 1
	if n.width < 24 || n.height < minHeight {
		return smallTerminal("Notebook · "+n.GetCurrentPage(), n.width, n.height, 24, minHeight, "F1: help · q: quit")
	}
	return screenView(n.headerTitle(), n.theme.StatusText(), n.viewport.View(), footer, n.theme.Palette(), n.width, n.height)
}

func (n *Notebook) headerTitle() string {
	return withIcon(n.icons.Notebook, "Notebook · "+n.GetCurrentPage()+privacyBadge(n.privacyEnabled, n.privacyState))
}

func (n *Notebook) renderHeader() string {
	return viewHeader(n.headerTitle(), n.theme.StatusText(), n.theme.Palette(), n.width)
}

func (n *Notebook) layoutViewport() {
	n.viewport.Width = max(1, n.width)
	n.viewport.Height = max(1, n.height-viewHeight(n.renderHeader())-viewHeight(n.renderFooter()))
	n.viewport.SetYOffset(n.viewport.YOffset)
}

// renderDateStrip is shared by both readers, without personal editing actions.
func (n *Notebook) renderDateStrip() string {
	if len(n.pages) == 0 {
		return ""
	}

	separator := " " + n.icons.Sep + " "
	reserved := 0
	for _, icon := range []string{n.icons.Prev, n.icons.Next} {
		if icon != "" {
			reserved += lipgloss.Width(icon) + 1
		}
	}
	innerWidth := max(1, n.width-2*viewGutter)
	maxVisibleDates := max(1, (innerWidth-reserved+lipgloss.Width(separator))/(10+lipgloss.Width(separator)))
	maxVisibleDates = min(maxVisibleDates, len(n.pages))

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
	sep := n.theme.Palette().MutedText.Render(separator)
	var pageIndicators []string
	// Internal pages descend; the horizontal strip reads earlier to later.
	for i := startIdx + maxVisibleDates - 1; i >= startIdx; i-- {
		page := n.pages[i]
		if i == n.current {
			pageIndicators = append(pageIndicators, n.theme.Palette().SelectedDate.Render(page))
		} else {
			pageIndicators = append(pageIndicators, n.theme.Palette().MutedText.Render(page))
		}
	}

	// Add navigation indicators if there are more pages
	navLine := strings.Join(pageIndicators, sep)
	if startIdx+maxVisibleDates < len(n.pages) && n.icons.Prev != "" {
		navLine = n.theme.Palette().MutedText.Render(n.icons.Prev) + " " + navLine
	}
	if startIdx > 0 && n.icons.Next != "" {
		navLine = navLine + " " + n.theme.Palette().MutedText.Render(n.icons.Next)
	}

	return n.centerNotebookLine(truncate(navLine, innerWidth))
}

// Center each wrapped line inside the same gutters as the writing and controls.
func (n *Notebook) centerNotebookLine(text string) string {
	width := max(1, n.width-2*viewGutter)
	return insetView(lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(ansi.Wrap(text, width, "")))
}

// renderFooter budgets dates independently of the controls below them.
func (n *Notebook) renderFooter() string {
	if len(n.pages) == 0 {
		return ""
	}
	back := "quit"
	if n.canPop {
		back = "back"
	}
	help := screenFooter([]helpEntry{
		{keys: "←/→", label: "earlier/later", visible: true},
		{keys: "enter/e", label: n.editLabel(), visible: true},
		{keys: "esc", label: back, visible: true},
		{keys: "?/F1", label: "help", visible: true},
	}, n.theme.Palette(), n.width)

	footer := lipgloss.JoinVertical(lipgloss.Left, n.renderDateStrip(), help)
	if n.hasShared() {
		footer = n.theme.Palette().MutedText.Render(n.centerNotebookLine("Shared updates available · c view")) + "\n" + footer
	}
	return footer
}

// updateViewportContent renders the current page's markdown content into the viewport
func (n *Notebook) updateViewportContent() {
	n.captureReadingPosition()
	// Always replace the private viewport, even while the shared pane is open:
	// relocking must drop its hidden decrypted display projection too.
	defer n.refreshShared()
	if len(n.pages) == 0 {
		n.setReadingContent("")
		return
	}
	content := n.contents[n.pages[n.current]]
	if n.privacyEnabled {
		page := PrivatePage{Shared: content, Locked: true, HasPrivate: true}
		if n.privatePage != nil && n.privateDate == n.pages[n.current] {
			page = *n.privatePage
		}
		n.setReadingContent(n.renderPrivateAreas(page))
		return
	}
	n.setReadingContent(n.renderPrivateAreas(PrivatePage{Private: content, HasPrivate: content != "" || n.personalDates[n.pages[n.current]]}))
}

// SetContents sets the contents for all pages
func (n *Notebook) SetContents(contents map[string]string) {
	n.contents = contents
	n.updateViewportContent()
}

// SetPageContent inserts or refreshes a persisted page without changing focus.
func (n *Notebook) SetPageContent(date, content string) {
	n.AddPage(date)
	n.personalDates[date] = true
	n.contents[date] = content
	if len(n.pages) > 0 && n.pages[n.current] == date {
		n.updateViewportContent()
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
	currentDate := n.GetCurrentPage()
	n.pages = append(n.pages, date)
	sort.Sort(sort.Reverse(sort.StringSlice(n.pages)))
	if currentDate != "" {
		for i, page := range n.pages {
			if page == currentDate {
				n.current = i
				break
			}
		}
	}
	if _, ok := n.contents[date]; !ok {
		n.contents[date] = ""
	}
}
