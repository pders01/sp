package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

// CalendarView selects which granularity the calendar renders.
type CalendarView int

const (
	ViewMonth CalendarView = iota
	ViewYear
)

const (
	minCellWidth     = 6
	minCellHeight    = 2
	maxCellHeight    = 6
	weekHeaderRow    = 1
	minPreviewHeight = 4
	yearColumns      = 4
)

// Calendar is a full-screen month/year calendar that drills down to a day.
type Calendar struct {
	icons              IconSet
	diagnostics        string
	hasData            map[string]bool
	previews           map[string]string
	contents           map[string]string
	cursor             time.Time
	preferredDay       int // Retained across clamped period jumps; zero uses cursor day.
	today              time.Time
	view               CalendarView
	selected           string
	directEdit         bool
	quitting           bool
	width              int
	height             int
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
	privateDates       map[string]bool // Public presence only; never private previews.
	loadErrors         map[string]string
	onChange           func(date, content string)
	onLoad             func(date, content string)
	onRecovery         func(scratchpad.RecoveryReference)
	onConsumed         func(date, path string)
	keyHelp            *keyHelp
	drafts             map[string]editDoneMsg
	review             *externalDraftReview
}

// NewCalendar creates a calendar seeded with the given dates as "has data".
func NewCalendar(dates []string) *Calendar {
	hasData := make(map[string]bool, len(dates))
	for _, d := range dates {
		hasData[d] = true
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return &Calendar{
		icons:        DefaultIconSet(),
		hasData:      hasData,
		drafts:       make(map[string]editDoneMsg),
		previews:     make(map[string]string),
		contents:     make(map[string]string),
		privateDates: make(map[string]bool),
		loadErrors:   make(map[string]string),
		cursor:       today,
		today:        today,
		view:         ViewMonth,
		width:        80,
		height:       24,
		theme:        newThemeWatcher(ThemePrefAuto),
	}
}

// SetThemePref sets the initial theme preference. Call before Init so
// watchers pick up the value.
func (c *Calendar) SetThemePref(pref string) {
	c.theme.SetPref(pref)
}

// SetEditor wires the external editor and the save / load callbacks
// used by the e shortcut. With all three present, e suspends the TUI
// via tea.ExecProcess, persists changes, and resumes the calendar.
// With any unset, e falls back to quit-with-direct-edit so the caller
// can run the editor itself.
func (c *Calendar) SetEditor(ed *editor.Editor, save Saver, load func(date string) (string, error)) {
	c.editor = ed
	c.editorProvider = nil
	c.save = save
	c.loader = load
}

// SetContents stores documents and their short per-day annotations.
// Dates with non-empty content are also marked as "has data".
func (c *Calendar) SetContents(contents map[string]string) {
	c.contents = make(map[string]string, len(contents))
	c.previews = make(map[string]string, len(contents))
	for date, body := range contents {
		c.contents[date] = body
		preview := extractPreview(body)
		if preview != "" {
			c.previews[date] = preview
			c.hasData[date] = true
		}
	}
}

// extractPreview returns the first non-empty line of body, with leading
// markdown heading markers stripped.
func extractPreview(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return line
	}
	return ""
}

// Init implements tea.Model. Starts the theme watchers and arms the
// long-lived theme-event subscription.
func (c *Calendar) Init() tea.Cmd {
	c.theme.start()
	return c.theme.wait()
}

// Close releases theme watchers. Safe to call multiple times.
func (c *Calendar) Close() { c.theme.stop() }

// Update implements tea.Model.
func (c *Calendar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width, c.height = max(0, msg.Width), max(0, msg.Height)
		if c.keyHelp != nil {
			c.keyHelp.resize(c.width, c.height)
		}
		if c.review != nil {
			c.review.viewer.resize(c.width, c.height)
		}
		return c, nil
	case themeChangedMsg:
		var cmds []tea.Cmd
		if c.theme.applyResolved() {
			c.theme.SetStatus(MsgThemeApplied(c.theme.Pref(), c.theme.Style()), 2*time.Second)
			cmds = append(cmds, c.theme.expireStatusCmd(2*time.Second))
		}
		cmds = append(cmds, c.theme.wait())
		return c, tea.Batch(cmds...)
	case statusExpireMsg:
		c.theme.HandleStatusExpire()
		return c, nil
	case editDoneMsg:
		return c.finishEdit(msg)
	case tea.KeyMsg:
		if c.review != nil {
			return c.updateDraftReview(msg)
		}
		if c.keyHelp != nil {
			if c.keyHelp.update(msg) {
				c.keyHelp = nil
			}
			return c, nil
		}
		return c.handleKey(msg)
	}
	return c, nil
}

func (c *Calendar) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	result := completeExternalEdit(msg, c.save)
	if result.published {
		if c.onChange != nil {
			c.onChange(msg.date, result.content)
		} else {
			c.MarkDate(msg.date, result.content)
		}
	}
	if result.err != nil {
		rememberExternalFailure(c.drafts, msg, result)
		result.status += "; e: review/retry retained draft"
		if msg.path != "" && c.onRecovery != nil {
			c.onRecovery(scratchpad.RecoveryReference{Date: msg.date, Path: msg.path})
		}
		c.theme.SetStatus(result.status, 0)
		return c, nil
	}
	delete(c.drafts, msg.date)
	if c.onConsumed != nil {
		c.onConsumed(msg.date, msg.path)
	}
	c.theme.SetStatus(result.status, 1500*time.Millisecond)
	return c, c.theme.expireStatusCmd(1500 * time.Millisecond)
}

func (c *Calendar) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if helpRequested(msg, true) {
		context := "month"
		if c.view == ViewYear {
			context = "year"
		}
		details := c.focusLine()
		if c.privacyEnabled {
			details += "\nCalendar previews show shared text only; private notes stay hidden even while unlocked."
		}
		c.keyHelp = newKeyHelp("Calendar · "+context, c.keyBindings(), details+"\n"+diagnosticDetails(c.theme.StatusText(), c.diagnostics), c.width, c.height)
		return c, nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		c.quitting = true
		return c, nil
	case "esc":
		if c.view == ViewYear {
			c.view = ViewMonth
		} else {
			c.quitting = true
		}
		return c, nil
	case "ctrl+t":
		c.theme.Cycle()
		return c, nil
	case "t", "T":
		c.cursor = c.today
		c.preferredDay = 0
		return c, nil
	case "y", "Y":
		c.view = ViewYear
		return c, nil
	case "m", "M":
		c.view = ViewMonth
		return c, nil
	case "enter":
		if c.view == ViewYear {
			c.view = ViewMonth
			return c, nil
		}
		// Drill into the notebook positioned on the picked day; the
		// router pops the calendar and switches to the notebook.
		c.selected = c.cursor.Format("2006-01-02")
		return c, nil
	case "e", "i":
		if c.view == ViewYear {
			return c, nil
		}
		date := c.cursor.Format("2006-01-02")
		if cmd := c.startEdit(date); cmd != nil {
			return c, cmd
		}
		// Fallback: caller wired no editor; signal the orchestrator.
		c.selected = date
		c.directEdit = true
		c.quitting = true
		return c, nil
	}

	if c.view == ViewYear {
		return c.handleYearKey(msg)
	}
	return c.handleMonthKey(msg)
}

func (c *Calendar) handleMonthKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		c.shiftDays(-1)
	case "right", "l":
		c.shiftDays(1)
	case "up", "k":
		c.shiftDays(-7)
	case "down", "j":
		c.shiftDays(7)
	case "H":
		c.shiftPeriod(0, -1)
	case "L":
		c.shiftPeriod(0, 1)
	}
	return c, nil
}

func (c *Calendar) handleYearKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		c.shiftPeriod(0, -1)
	case "right", "l":
		c.shiftPeriod(0, 1)
	case "up", "k":
		c.shiftPeriod(0, -yearColumns)
	case "down", "j":
		c.shiftPeriod(0, yearColumns)
	case "H":
		c.shiftPeriod(-1, 0)
	case "L":
		c.shiftPeriod(1, 0)
	}
	return c, nil
}

func (c *Calendar) shiftDays(days int) {
	c.cursor = c.cursor.AddDate(0, 0, days)
	c.preferredDay = 0
}

// Anchor period arithmetic on day one so Go cannot normalize January 31 plus
// one month into March. Remember the chosen day through shorter months/years.
func (c *Calendar) shiftPeriod(years, months int) {
	if c.preferredDay == 0 {
		c.preferredDay = c.cursor.Day()
	}
	zone := c.cursor.Location()
	first := time.Date(c.cursor.Year()+years, c.cursor.Month()+time.Month(months), 1, 0, 0, 0, 0, zone)
	day := min(c.preferredDay, first.AddDate(0, 1, -1).Day())
	c.cursor = time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, zone)
}

// View renders the calendar full-screen.
func (c *Calendar) View() (view string) {
	// Defense in depth for the composed calendar, including help/review paths.
	// Project before the final fit so visible control notation is cell-bounded.
	defer func() { view = fitView(safeTerminalOutput(view), c.width, c.height) }()
	if c.review != nil {
		return c.review.viewer.view(c.theme.Palette())
	}
	if c.quitting {
		return ""
	}

	palette := c.theme.Palette()
	if c.keyHelp != nil {
		return c.keyHelp.view(palette)
	}
	year := c.view == ViewYear
	title := "Calendar · " + c.cursor.Format("2006-01")
	minWidth, minBodyHeight := minCellWidth*7, weekHeaderRow+c.monthRows()*minCellHeight
	if year {
		title = fmt.Sprintf("Calendar · %d", c.cursor.Year())
		minWidth, minBodyHeight = yearColumns*14, 12/yearColumns*4
	}
	title += privacyBadge(c.privacyEnabled, c.privacyState)
	header := viewHeader(withIcon(c.icons.Calendar, title), c.theme.StatusText(), palette, c.width)
	footer := screenFooter([]helpEntry{
		{keys: "enter", label: "open", visible: true},
		{keys: "e", label: c.editLabel(), visible: !year},
		{keys: "m/y", label: "view", visible: true},
		{keys: "t", label: "today", visible: !year},
		{keys: "q", label: "quit", visible: true},
		{keys: "?/F1", label: "help", visible: true},
	}, palette, c.width)
	legend := ""
	if year {
		legend = palette.MutedText.Render("  Timeline · full month: ■ active range · none")
	}
	minHeight := viewHeight(header) + minBodyHeight + viewHeight(legend) + viewHeight(footer)
	if c.width < minWidth || c.height < minHeight {
		return smallTerminal("Calendar · "+c.cursor.Format(time.DateOnly), c.width, c.height, minWidth, minHeight, "F1: help · q: quit")
	}
	bodyHeight := c.height - viewHeight(header) - viewHeight(legend) - viewHeight(footer)
	var body string
	if year {
		body = c.renderYear(c.width, bodyHeight)
	} else {
		body = c.renderMonthLayout(c.width, bodyHeight)
	}
	bodyBlock := lipgloss.NewStyle().Width(c.width).Height(bodyHeight).Align(lipgloss.Center, lipgloss.Top).Render(body)
	blocks := []string{header, bodyBlock}
	if legend != "" {
		blocks = append(blocks, legend)
	}
	blocks = append(blocks, footer)
	return fitView(lipgloss.JoinVertical(lipgloss.Left, blocks...), c.width, c.height)
}

func (c *Calendar) focusLine() string {
	if c.view == ViewYear {
		return fmt.Sprintf("Focus: %s · timeline: ■ active range, · none (full month)", c.cursor.Format("January 2006"))
	}
	date := c.cursor.Format("2006-01-02")
	weekday := c.cursor.Format("Mon")
	focus := fmt.Sprintf("Focus: %s (%s)", date, weekday)
	if c.loadErrors[date] != "" {
		return focus + " — entry unavailable: " + safeMarkdownDisplay(c.loadErrors[date])
	}
	if preview, ok := c.previews[date]; ok {
		return focus + " — " + safeMarkdownDisplay(preview)
	}
	if c.privacyEnabled && c.privateDates[date] {
		return focus + " — private note hidden"
	}
	return focus
}

// renderMonthLayout reserves a useful preview before growing day cells. Every
// week remains visible, even in a six-week month. At minimum frame heights the
// grid takes priority; taller frames grow whole cell rows, then the excerpt.
func (c *Calendar) renderMonthLayout(width, height int) string {
	rows := c.monthRows()
	calendarH := weekHeaderRow + rows*minCellHeight
	if height-calendarH-1 < minPreviewHeight {
		return c.renderMonth(width, height)
	}
	// Prefer enough room for prose before growing mostly empty day cells.
	// At narrow sizes retain the smaller excerpt rather than dropping it.
	previewBudget := min(minPreviewHeight+2, height-calendarH-1)
	cellH := clamp((height-weekHeaderRow-1-previewBudget)/rows, minCellHeight, maxCellHeight)
	calendarH = weekHeaderRow + rows*cellH
	previewH := height - calendarH - 1

	rule := c.theme.Palette().Separator.Render(strings.Repeat("─", max(width, 0)))
	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Width(width).Height(calendarH).Render(c.renderMonth(width, calendarH)),
		rule,
		c.renderDocumentPreview(width, previewH),
	)
}

// monthRows returns the number of calendar weeks needed by the cursor month.
func (c *Calendar) monthRows() int {
	year, month, _ := c.cursor.Date()
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.cursor.Location())
	startOffset := (int(first.Weekday()) + 6) % 7
	days := first.AddDate(0, 1, -1).Day()
	return (startOffset + days + 6) / 7
}

func (c *Calendar) renderDocumentPreview(width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}

	date := c.cursor.Format("2006-01-02")
	title := fmt.Sprintf("Preview · %s · %s", date, c.cursor.Format("Monday"))
	p := c.theme.Palette()
	contentWidth := max(width-4, 0)
	// Only the cached public projection belongs here. Rendering must not call a
	// loader or decrypt private notes, even when the notebook is unlocked.
	content := c.contents[date]
	switch {
	case c.loadErrors[date] != "":
		content = "  " + p.MutedText.Render(truncate("Entry unavailable: "+oneLine(safeMarkdownDisplay(c.loadErrors[date])), contentWidth))
	case strings.TrimSpace(content) == "":
		message := "No entry for this day."
		if c.privacyEnabled && c.privateDates[date] {
			message = "Private note hidden."
		} else if c.hasData[date] {
			message = "Entry is empty."
			if c.privacyEnabled {
				message = "Shared-only entry; no shared content."
			}
		}
		content = "  " + p.MutedText.Render(truncate(message, contentWidth))
	default:
		if c.privacyEnabled {
			title += " · shared"
			if ansi.StringWidth(title) > contentWidth {
				// The projection label matters more than an optional weekday.
				title = fmt.Sprintf("Preview · %s · shared", date)
			}
		}
		content = renderDocument(content, c.theme.Style(), width)
	}

	lines := strings.Split(content, "\n")
	// Glamour's document margins can be ANSI-styled spaces, not just newlines.
	// Do not spend the short excerpt on those otherwise invisible rows.
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	available := max(height-1, 0)
	clipped := len(lines) > available
	if clipped {
		// Only clipped excerpts need another open-note hint; ordinary previews
		// already have the footer action and should prioritize the writing.
		available = max(available-1, 0)
		lines = lines[:available]
	}
	body := "  " + p.Header.Render(truncate(title, contentWidth))
	if available > 0 {
		body += "\n" + fitView(strings.Join(lines, "\n"), width, available)
	}
	if clipped && height > 1 {
		body = lipgloss.NewStyle().Height(height - 1).Render(body)
		body += "\n  " + p.MutedText.Render(truncate("… · Enter: full note", contentWidth))
	}
	return fitView(body, width, height)
}

// renderMonth renders a 7-column day grid sized to fill the given area.
func (c *Calendar) renderMonth(width, height int) string {
	cellW := width / 7
	if cellW < minCellWidth {
		cellW = minCellWidth
	}

	year, month, _ := c.cursor.Date()
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.cursor.Location())
	rowsNeeded := c.monthRows()
	cellH := (height - weekHeaderRow) / rowsNeeded
	cellH = clamp(cellH, minCellHeight, maxCellHeight)

	// Monday-based weekday index (0=Mon ... 6=Sun)
	startOffset := (int(first.Weekday()) + 6) % 7
	gridStart := first.AddDate(0, 0, -startOffset)

	weekdays := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	headerCells := make([]string, len(weekdays))
	headerStyle := c.theme.Palette().MutedText.Width(cellW).Align(lipgloss.Center).Bold(true)
	for i, w := range weekdays {
		headerCells[i] = headerStyle.Render(w)
	}
	rows := []string{lipgloss.JoinHorizontal(lipgloss.Top, headerCells...)}

	for week := range rowsNeeded {
		cells := make([]string, 7)
		for d := range 7 {
			day := gridStart.AddDate(0, 0, week*7+d)
			cells[d] = c.renderDayCell(day, month, cellW, cellH)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

func (c *Calendar) renderDayCell(day time.Time, month time.Month, w, h int) string {
	dateStr := day.Format("2006-01-02")
	dayLabel := fmt.Sprintf("%2d", day.Day())

	innerW := w - 2 // left/right padding
	if innerW < 2 {
		innerW = 2
	}

	cursorMatch := day.Equal(c.cursor)
	outOfMonth := day.Month() != month
	hasData := c.hasData[dateStr]
	isToday := day.Equal(c.today)

	p := c.theme.Palette()
	dayStyle := lipgloss.NewStyle()
	switch {
	case cursorMatch:
		dayStyle = dayStyle.Foreground(p.Highlight).Bold(true).Underline(true)
	case outOfMonth:
		dayStyle = dayStyle.Foreground(p.Muted).Faint(true)
	case hasData:
		dayStyle = dayStyle.Foreground(p.Accent).Bold(true)
	case isToday:
		dayStyle = dayStyle.Foreground(p.Secondary).Underline(true)
	default:
		dayStyle = dayStyle.Foreground(p.Text)
	}

	var rendered string
	if cursorMatch {
		// Bracket marker stays un-underlined so the highlight reads as
		// "this is the day" without dragging the line across the glyph.
		bracket := lipgloss.NewStyle().Foreground(p.Highlight).Bold(true).Render("▌")
		number := dayStyle.Render(strings.TrimSpace(dayLabel))
		rendered = bracket + number
	} else {
		rendered = dayStyle.Render(dayLabel)
	}

	// Compact cells retain their blank separator row, but presence must not
	// depend on color alone when there is no room for an annotation.
	if h == minCellHeight && !outOfMonth && (hasData || c.privacyEnabled && c.privateDates[dateStr]) {
		rendered += p.MutedText.Render("•")
	}
	lines := []string{rendered}
	if h > minCellHeight && !outOfMonth {
		if ann := c.cellAnnotation(day, dateStr, innerW); ann != "" {
			lines = append(lines, ann)
		}
	}

	cell := lipgloss.NewStyle().Width(w).Height(h).Padding(0, 1)
	return cell.Render(strings.Join(lines, "\n"))
}

func (c *Calendar) cellAnnotation(day time.Time, dateStr string, w int) string {
	if c.loadErrors[dateStr] != "" {
		return c.theme.Palette().MutedText.Render(truncate("unavailable", w))
	}
	if preview := c.previews[dateStr]; preview != "" {
		// Preview text is raw shared/note content, not trusted ANSI. Escape it
		// before ANSI-aware truncation and application-owned styling.
		return c.theme.Palette().MutedText.Render(truncate(oneLine(safeMarkdownDisplay(preview)), w))
	}
	if c.hasData[dateStr] {
		label := "• entry"
		if c.privacyEnabled {
			label = "• shared"
			if c.privateDates[dateStr] {
				label = "• private"
			}
		}
		return c.theme.Palette().MutedText.Render(truncate(label, w))
	}
	if day.Equal(c.today) {
		return c.theme.Palette().MutedText.Render(truncate("today", w))
	}
	return ""
}

// renderYear renders 12 month tiles in a 4x3 grid sized to fill the area.
func (c *Calendar) renderYear(width, height int) string {
	cols, gridRows := yearColumns, 12/yearColumns
	tileW := width / cols
	if tileW < 14 {
		tileW = 14
	}
	tileH := height / gridRows
	if tileH < 4 {
		tileH = 4
	}

	year := c.cursor.Year()
	var rendered []string
	for r := range gridRows {
		var tiles []string
		for col := range cols {
			m := time.Month(r*cols + col + 1)
			tiles = append(tiles, c.renderMonthTile(year, m, tileW, tileH))
		}
		rendered = append(rendered, lipgloss.JoinHorizontal(lipgloss.Top, tiles...))
	}
	return lipgloss.JoinVertical(lipgloss.Center, rendered...)
}

func (c *Calendar) renderMonthTile(year int, month time.Month, w, h int) string {
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.cursor.Location())
	last := first.AddDate(0, 1, -1)

	count := 0
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if c.hasData[d.Format("2006-01-02")] {
			count++
		}
	}

	cursorMonth := month == c.cursor.Month()
	p := c.theme.Palette()

	titleText := first.Format("January")
	if cursorMonth {
		titleText = "▌ " + titleText + " ▐"
	}
	titleStyle := p.MutedText
	if count > 0 {
		titleStyle = p.Header
	}
	if cursorMonth {
		titleStyle = p.SelectedDate
	}
	titleLine := titleStyle.Render(titleText)

	var summary string
	switch count {
	case 0:
		// Empty months retain their position, not repeated diagnostic text.
	case 1:
		summary = lipgloss.NewStyle().Foreground(p.Accent).Render("1 entry")
	default:
		summary = lipgloss.NewStyle().Foreground(p.Accent).Render(fmt.Sprintf("%d entries", count))
	}

	spark := ""
	if count > 0 {
		spark = c.monthSparkline(first, last, w-4)
	}

	// Keep month headings aligned even when an empty tile has no summary.
	lines := []string{titleLine, summary, spark}
	content := lipgloss.JoinVertical(lipgloss.Center, lines...)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

// monthSparkline covers the complete month, grouping consecutive days when the
// width is smaller than the day count. A filled cell means any day in its range
// has an entry; full-width timelines retain one cell per day.
func (c *Calendar) monthSparkline(first, last time.Time, w int) string {
	if w < 4 || last.Before(first) || first.Year() != last.Year() || first.Month() != last.Month() {
		return ""
	}
	days := last.Day() - first.Day() + 1
	cells := min(w, days)
	var b strings.Builder
	for cell := range cells {
		active := false
		for offset := cell * days / cells; offset < (cell+1)*days/cells; offset++ {
			date := first.AddDate(0, 0, offset).Format(time.DateOnly)
			if c.hasData[date] {
				active = true
				break
			}
		}
		if active {
			b.WriteString("■")
		} else {
			b.WriteString("·")
		}
	}
	return c.theme.Palette().MutedText.Render(b.String())
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// CursorDate returns the focused date in month view. Year view returns empty
// because templates apply to a specific day.
func (c *Calendar) CursorDate() string {
	if c.view != ViewMonth {
		return ""
	}
	return c.cursor.Format("2006-01-02")
}

// GetSelectedDate returns the selected date in YYYY-MM-DD form, or empty.
func (c *Calendar) GetSelectedDate() string { return c.selected }

// IsDirectEdit reports whether the user pressed e/i to skip the
// notebook drill and jump straight to the editor.
func (c *Calendar) IsDirectEdit() bool { return c.directEdit }

// ClearSelection wipes drill / direct-edit / quit state so the router
// can resume the calendar after a notebook pop.
func (c *Calendar) ClearSelection() {
	c.selected = ""
	c.directEdit = false
	c.quitting = false
}

// HasData reports whether the calendar already tracks the given date.
// Used by the router so it doesn't re-add a known day to the notebook.
func (c *Calendar) HasData(date string) bool { return c.hasData[date] }

// SetCursor moves the cursor to the given YYYY-MM-DD date. Bad input
// is ignored. Used by the router to sync the calendar to wherever the
// user ended up in the notebook before popping back.
func (c *Calendar) SetCursor(date string) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return
	}
	c.cursor = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, c.cursor.Location())
	c.preferredDay = 0
	c.view = ViewMonth
}

// MarkDate ensures the calendar shows the given day as having data and
// stores its document, so both the cell and preview repaint after an edit.
func (c *Calendar) MarkDate(date, content string) {
	c.hasData[date] = true
	c.contents[date] = content
	delete(c.previews, date)
	if preview := extractPreview(content); preview != "" {
		c.previews[date] = preview
	}
}

// startEdit suspends the TUI to run the editor on the picked day. Returns
// nil when no editor is wired or preparation fails (caller falls back to
// quit-with-direct-edit so the orchestrator can run the editor).
func (c *Calendar) startEdit(date string) tea.Cmd {
	if c.editor == nil && c.editorProvider != nil {
		ed, err := c.editorProvider()
		if err != nil {
			return unavailableEdit(func(text string) { c.theme.SetStatus(text, 0) }, err)
		}
		c.editor = ed
	}
	if c.editor == nil || c.save == nil || c.loader == nil {
		return nil
	}
	content, lerr := c.loader(date)
	if lerr != nil {
		c.theme.SetStatus(fmt.Sprintf("load: %v", lerr), 2*time.Second)
		return c.theme.expireStatusCmd(2 * time.Second)
	}
	if c.onLoad != nil {
		c.onLoad(date, content)
	} else if c.HasData(date) || content != "" {
		c.MarkDate(date, content)
	}
	if draft, ok := c.drafts[date]; ok {
		c.review = reviewExternalDraft(draft, content, c.width, c.height)
		return func() tea.Msg { return nil }
	}
	cmd, perr := prepareExternalDraft(c.editor, date, content, content, false, c.onRecovery)
	if perr != nil {
		c.theme.SetStatus(fmt.Sprintf("editor prepare: %v", perr), 2*time.Second)
		return c.theme.expireStatusCmd(2 * time.Second)
	}
	return cmd
}

// IsQuitting reports whether the calendar is exiting.
func (c *Calendar) IsQuitting() bool { return c.quitting }

// SetIcons overrides the icon set. Useful for tests or higher-level config.
func (c *Calendar) SetIcons(icons IconSet) { c.icons = icons }
