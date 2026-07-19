package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/pders01/sp/internal/editor"
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
	minPreviewHeight = 10
)

// Calendar is a full-screen month/year calendar that drills down to a day.
type Calendar struct {
	icons      IconSet
	hasData    map[string]bool
	previews   map[string]string
	contents   map[string]string
	cursor     time.Time
	today      time.Time
	view       CalendarView
	selected   string
	directEdit bool
	quitting   bool
	width      int
	height     int
	theme      *themeWatcher
	editor     *editor.Editor
	save       Saver
	loader     func(date string) (string, error)
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
		icons:    DefaultIconSet(),
		hasData:  hasData,
		previews: make(map[string]string),
		contents: make(map[string]string),
		cursor:   today,
		today:    today,
		view:     ViewMonth,
		width:    80,
		height:   24,
		theme:    newThemeWatcher(ThemePrefAuto),
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
		c.width = msg.Width
		c.height = msg.Height
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
		return c.handleKey(msg)
	}
	return c, nil
}

func (c *Calendar) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	if msg.cleanup != nil {
		defer msg.cleanup()
	}
	if msg.err != nil {
		c.theme.SetStatus(fmt.Sprintf("editor: %v", msg.err), 2*time.Second)
		return c, c.theme.expireStatusCmd(2 * time.Second)
	}
	newContent, rerr := editor.ReadEdited(msg.path)
	if rerr != nil {
		c.theme.SetStatus(fmt.Sprintf("read: %v", rerr), 2*time.Second)
		return c, c.theme.expireStatusCmd(2 * time.Second)
	}
	if c.save != nil {
		if serr := c.save(msg.date, newContent); serr != nil {
			c.theme.SetStatus(fmt.Sprintf("save: %v", serr), 2*time.Second)
			return c, c.theme.expireStatusCmd(2 * time.Second)
		}
	}
	// Reflect the new entry in the calendar's data so cells repaint.
	delete(c.previews, msg.date)
	if newContent != "" {
		c.hasData[msg.date] = true
		c.contents[msg.date] = newContent
		if preview := extractPreview(newContent); preview != "" {
			c.previews[msg.date] = preview
		}
	} else {
		delete(c.contents, msg.date)
	}
	c.theme.SetStatus("Saved", 1500*time.Millisecond)
	return c, c.theme.expireStatusCmd(1500 * time.Millisecond)
}

func (c *Calendar) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		c.quitting = true
		return c, nil
	case "ctrl+t":
		c.theme.Cycle()
		return c, nil
	case "t", "T":
		c.cursor = c.today
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
		c.cursor = c.cursor.AddDate(0, 0, -1)
	case "right", "l":
		c.cursor = c.cursor.AddDate(0, 0, 1)
	case "up", "k":
		c.cursor = c.cursor.AddDate(0, 0, -7)
	case "down", "j":
		c.cursor = c.cursor.AddDate(0, 0, 7)
	case "H":
		c.cursor = c.cursor.AddDate(0, -1, 0)
	case "L":
		c.cursor = c.cursor.AddDate(0, 1, 0)
	}
	return c, nil
}

func (c *Calendar) handleYearKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		c.cursor = c.cursor.AddDate(0, -1, 0)
	case "right", "l":
		c.cursor = c.cursor.AddDate(0, 1, 0)
	case "up", "k":
		c.cursor = c.cursor.AddDate(0, -3, 0)
	case "down", "j":
		c.cursor = c.cursor.AddDate(0, 3, 0)
	case "H":
		c.cursor = c.cursor.AddDate(-1, 0, 0)
	case "L":
		c.cursor = c.cursor.AddDate(1, 0, 0)
	}
	return c, nil
}

// View renders the calendar full-screen.
func (c *Calendar) View() string {
	if c.quitting {
		return ""
	}

	bodyHeight := c.height - 4 // header + focus + rule + help
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var headerText, body, helpText string
	switch c.view {
	case ViewYear:
		headerText = withIcon(c.icons.Calendar, fmt.Sprintf("Calendar · %d", c.cursor.Year()))
		body = c.renderYear(c.width, bodyHeight)
		helpText = "←/h/→/l: month • ↑/k/↓/j: row • H/L: year • enter: open • m: month view • t: today • Ctrl+t: theme • q: quit"
	default:
		headerText = withIcon(c.icons.Calendar, fmt.Sprintf("Calendar · %s", c.cursor.Format("2006-01")))
		body = c.renderMonthLayout(c.width, bodyHeight)
		helpText = "←/h/→/l: day • ↑/k/↓/j: week • H/L: month • enter: open • e: edit • y: year view • t: today • Ctrl+t: theme • q: quit"
	}

	header := c.theme.Palette().Header.Render(headerText)
	if status := c.theme.StatusText(); status != "" {
		header = lipgloss.JoinHorizontal(
			lipgloss.Top,
			header,
			"   ",
			c.theme.Palette().MutedText.Render(status),
		)
	}
	bodyBlock := lipgloss.NewStyle().
		Width(c.width).
		Height(bodyHeight).
		Align(lipgloss.Center, lipgloss.Top).
		Render(body)

	rule := c.theme.Palette().Separator.Render(strings.Repeat("─", max(c.width, 0)))
	focus := c.theme.Palette().MutedText.Render(truncate(c.focusLine(), c.width))
	help := c.theme.Palette().Help.Render(truncate(helpText, c.width))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		bodyBlock,
		focus,
		rule,
		help,
	)
}

func (c *Calendar) focusLine() string {
	date := c.cursor.Format("2006-01-02")
	weekday := c.cursor.Format("Mon")
	if preview, ok := c.previews[date]; ok {
		return fmt.Sprintf("Focus: %s (%s) — %s", date, weekday, preview)
	}
	return fmt.Sprintf("Focus: %s (%s)", date, weekday)
}

// renderMonthLayout stacks the document preview below the calendar so the
// full terminal width remains available to both. The calendar gets its full
// useful height first; the preview appears only when the remaining space is
// large enough to show meaningful content.
func (c *Calendar) renderMonthLayout(width, height int) string {
	calendarH := weekHeaderRow + c.monthRows()*maxCellHeight
	previewH := height - calendarH - 1
	if previewH < minPreviewHeight {
		return c.renderMonth(width, height)
	}

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
	title := c.theme.Palette().MutedText.Bold(true).Render(
		fmt.Sprintf("%s · %s", date, c.cursor.Format("Monday")),
	)
	content := c.contents[date]
	if strings.TrimSpace(content) == "" {
		content = c.theme.Palette().MutedText.Render("No entry for this day.")
	} else {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(c.theme.Style()),
			glamour.WithWordWrap(max(width-4, 1)),
		)
		if err == nil {
			if rendered, renderErr := renderer.Render(content); renderErr == nil {
				content = strings.Trim(rendered, "\n")
			}
		}
	}

	lines := strings.Split(content, "\n")
	available := max(height-2, 0)
	if len(lines) > available {
		lines = lines[:available]
	}
	body := title
	if available > 0 {
		body += "\n\n" + strings.Join(lines, "\n")
	}
	return lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1).Render(body)
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
	if preview := c.previews[dateStr]; preview != "" {
		return c.theme.Palette().MutedText.Render(truncate(preview, w))
	}
	if c.hasData[dateStr] {
		return c.theme.Palette().MutedText.Render(truncate("• entry", w))
	}
	if day.Equal(c.today) {
		return c.theme.Palette().MutedText.Render(truncate("today", w))
	}
	return ""
}

// renderYear renders 12 month tiles in a 4x3 grid sized to fill the area.
func (c *Calendar) renderYear(width, height int) string {
	cols, gridRows := 4, 3
	tileW := width / cols
	if tileW < 14 {
		tileW = 14
	}
	tileH := height / gridRows
	if tileH < 5 {
		tileH = 5
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
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(p.Text)
	if cursorMonth {
		titleStyle = titleStyle.Foreground(p.Highlight)
	}
	titleLine := titleStyle.Render(titleText)

	var summary string
	switch {
	case count == 0:
		summary = p.MutedText.Render("no entries")
	case count == 1:
		summary = lipgloss.NewStyle().Foreground(p.Accent).Render("1 entry")
	default:
		summary = lipgloss.NewStyle().Foreground(p.Accent).Render(fmt.Sprintf("%d entries", count))
	}

	spark := c.monthSparkline(first, last, w-4)

	lines := []string{titleLine, summary}
	if spark != "" {
		lines = append(lines, spark)
	}
	content := lipgloss.JoinVertical(lipgloss.Center, lines...)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, content)
}

// monthSparkline returns one row of glyphs (one per day) with filled markers
// for days that have data, fitting within w columns.
func (c *Calendar) monthSparkline(first, last time.Time, w int) string {
	if w < 4 {
		return ""
	}
	var b strings.Builder
	written := 0
	for d := first; !d.After(last) && written < w; d = d.AddDate(0, 0, 1) {
		if c.hasData[d.Format("2006-01-02")] {
			b.WriteString("■")
		} else {
			b.WriteString("·")
		}
		written++
	}
	return c.theme.Palette().MutedText.Render(b.String())
}

// truncate cuts s to at most w display columns, appending an ellipsis
// when content was dropped. Operates on runes so multi-byte UTF-8 input
// (any non-ASCII preview line) is sliced safely. Treats every rune as
// one column; that is approximate for east-asian wides but correct for
// the latin previews we expect from scratchpad content.
func truncate(s string, w int) string {
	if w <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(runes[:w-1]) + "…"
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
	if c.editor == nil || c.save == nil || c.loader == nil {
		return nil
	}
	content, lerr := c.loader(date)
	if lerr != nil {
		c.theme.SetStatus(fmt.Sprintf("load: %v", lerr), 2*time.Second)
		return c.theme.expireStatusCmd(2 * time.Second)
	}
	cmd, path, cleanup, perr := c.editor.Prepare(content)
	if perr != nil {
		c.theme.SetStatus(fmt.Sprintf("editor prepare: %v", perr), 2*time.Second)
		return c.theme.expireStatusCmd(2 * time.Second)
	}
	return tea.ExecProcess(cmd, func(execErr error) tea.Msg {
		return editDoneMsg{date: date, path: path, cleanup: cleanup, err: execErr}
	})
}

// IsQuitting reports whether the calendar is exiting.
func (c *Calendar) IsQuitting() bool { return c.quitting }

// SetIcons overrides the icon set. Useful for tests or higher-level config.
func (c *Calendar) SetIcons(icons IconSet) { c.icons = icons }
