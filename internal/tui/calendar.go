package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CalendarView selects which granularity the calendar renders.
type CalendarView int

const (
	ViewMonth CalendarView = iota
	ViewYear
)

// Calendar is a full-screen month/year calendar that drills down to a day.
type Calendar struct {
	icons    IconSet
	hasData  map[string]bool
	cursor   time.Time
	today    time.Time
	view     CalendarView
	selected string
	quitting bool
	width    int
	height   int
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
		icons:   DefaultIconSet(),
		hasData: hasData,
		cursor:  today,
		today:   today,
		view:    ViewMonth,
		width:   80,
		height:  24,
	}
}

// Init implements tea.Model.
func (c *Calendar) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (c *Calendar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		return c, nil
	case tea.KeyMsg:
		return c.handleKey(msg)
	}
	return c, nil
}

func (c *Calendar) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		c.quitting = true
		return c, tea.Quit
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
		c.selected = c.cursor.Format("2006-01-02")
		c.quitting = true
		return c, tea.Quit
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

	var headerText, body, helpText string
	switch c.view {
	case ViewYear:
		headerText = withIcon(c.icons.Calendar, fmt.Sprintf("Calendar · %d", c.cursor.Year()))
		body = c.renderYear()
		helpText = "←/h/→/l: month • ↑/k/↓/j: row • H/L: year • enter: open • m: month view • t: today • q: quit"
	default:
		headerText = withIcon(c.icons.Calendar, fmt.Sprintf("Calendar · %s", c.cursor.Format("2006-01")))
		body = c.renderMonth()
		helpText = "←/h/→/l: day • ↑/k/↓/j: week • H/L: month • enter: open • y: year view • t: today • q: quit"
	}

	header := HeaderStyle.Render(headerText)

	bodyHeight := c.height - 5 // header + 3 footer lines + 1 spacer
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	bodyBlock := lipgloss.NewStyle().
		Width(c.width).
		Height(bodyHeight).
		Align(lipgloss.Center, lipgloss.Center).
		Render(body)

	rule := SeparatorStyle.Render(strings.Repeat("─", max(c.width, 0)))
	focus := MutedStyle.Render(fmt.Sprintf("Focus: %s (%s)", c.cursor.Format("2006-01-02"), c.cursor.Format("Mon")))
	help := HelpStyle.Render(helpText)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		bodyBlock,
		focus,
		rule,
		help,
	)
}

// renderMonth renders a 7-column day grid for the cursor's month.
func (c *Calendar) renderMonth() string {
	year, month, _ := c.cursor.Date()
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.cursor.Location())

	// Monday-based weekday index (0=Mon ... 6=Sun)
	startOffset := (int(first.Weekday()) + 6) % 7
	gridStart := first.AddDate(0, 0, -startOffset)

	weekdays := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	headerCells := make([]string, len(weekdays))
	for i, w := range weekdays {
		headerCells[i] = MutedStyle.Width(4).Align(lipgloss.Center).Render(w)
	}
	rows := []string{lipgloss.JoinHorizontal(lipgloss.Top, headerCells...)}

	for week := 0; week < 6; week++ {
		cells := make([]string, 7)
		anyInMonth := false
		for d := 0; d < 7; d++ {
			day := gridStart.AddDate(0, 0, week*7+d)
			cells[d] = c.renderDayCell(day, month)
			if day.Month() == month {
				anyInMonth = true
			}
		}
		if !anyInMonth && week > 3 {
			break
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

func (c *Calendar) renderDayCell(day time.Time, month time.Month) string {
	label := fmt.Sprintf("%2d", day.Day())
	cell := lipgloss.NewStyle().Width(4).Align(lipgloss.Center)

	switch {
	case day.Equal(c.cursor):
		return cell.
			Foreground(lipgloss.Color("#1A1A2E")).
			Background(HighlightColor).
			Bold(true).
			Render(label)
	case day.Month() != month:
		return cell.Foreground(MutedColor).Faint(true).Render(label)
	case c.hasData[day.Format("2006-01-02")]:
		return cell.Foreground(AccentColor).Bold(true).Render(label)
	case day.Equal(c.today):
		return cell.Foreground(SecondaryColor).Underline(true).Render(label)
	default:
		return cell.Foreground(TextColor).Render(label)
	}
}

// renderYear renders 12 mini-months in a 3x4 grid.
func (c *Calendar) renderYear() string {
	year := c.cursor.Year()
	var rows []string
	for r := 0; r < 4; r++ {
		var tiles []string
		for col := 0; col < 3; col++ {
			m := time.Month(r*3 + col + 1)
			tiles = append(tiles, c.renderMonthTile(year, m))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, tiles...))
	}
	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

func (c *Calendar) renderMonthTile(year int, month time.Month) string {
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.cursor.Location())
	last := first.AddDate(0, 1, -1)

	count := 0
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if c.hasData[d.Format("2006-01-02")] {
			count++
		}
	}

	title := first.Format("Jan")
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(TextColor)
	if month == c.cursor.Month() {
		titleStyle = titleStyle.Foreground(HighlightColor)
	}
	titleLine := titleStyle.Render(title)

	var summary string
	if count > 0 {
		summary = lipgloss.NewStyle().Foreground(AccentColor).Render(fmt.Sprintf("%d entries", count))
	} else {
		summary = MutedStyle.Render("—")
	}

	border := lipgloss.NormalBorder()
	tile := lipgloss.NewStyle().
		Border(border).
		BorderForeground(MutedColor).
		Padding(0, 1).
		Width(14).
		Height(3).
		Align(lipgloss.Center, lipgloss.Center)

	if month == c.cursor.Month() {
		tile = tile.BorderForeground(HighlightColor)
	}

	return tile.Render(lipgloss.JoinVertical(lipgloss.Center, titleLine, summary))
}

// GetSelectedDate returns the selected date in YYYY-MM-DD form, or empty.
func (c *Calendar) GetSelectedDate() string { return c.selected }

// IsQuitting reports whether the calendar is exiting.
func (c *Calendar) IsQuitting() bool { return c.quitting }

// SetIcons overrides the icon set. Useful for tests or higher-level config.
func (c *Calendar) SetIcons(icons IconSet) { c.icons = icons }
