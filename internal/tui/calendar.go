package tui

import (
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CalendarItem represents a date in the calendar
type CalendarItem struct {
	date    string
	hasData bool
	icons   IconSet
}

// FilterValue implements list.Item interface
func (i CalendarItem) FilterValue() string {
	return i.date
}

// Title returns the formatted title
func (i CalendarItem) Title() string {
	date, _ := time.Parse("2006-01-02", i.date)
	formatted := date.Format("Mon, Jan 2, 2006")

	glyph := i.icons.Article
	if !i.hasData {
		glyph = i.icons.Empty
	}
	return withIcon(glyph, formatted)
}

// Description returns additional info
func (i CalendarItem) Description() string {
	if i.hasData {
		return "Has content"
	}
	return "Empty"
}

// Calendar represents the calendar view
type Calendar struct {
	list     list.Model
	dates    []string
	selected string
	quitting bool
	width    int
	height   int
	icons    IconSet
}

// NewCalendar creates a new calendar instance
func NewCalendar(dates []string) *Calendar {
	icons := DefaultIconSet()

	// Sort dates in descending order (most recent first)
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	// Create list items
	var items []list.Item
	for _, date := range dates {
		items = append(items, CalendarItem{
			date:    date,
			hasData: true,
			icons:   icons,
		})
	}

	// Add today if not already present
	today := time.Now().Format("2006-01-02")
	todayExists := false
	for _, date := range dates {
		if date == today {
			todayExists = true
			break
		}
	}

	if !todayExists {
		items = append([]list.Item{CalendarItem{
			date:    today,
			hasData: false,
			icons:   icons,
		}}, items...)
	}

	// Start with a reasonable default size
	l := list.New(items, list.NewDefaultDelegate(), 40, 10)
	l.Title = withIcon(icons.Calendar, "Scratchpad Calendar")
	l.Styles.Title = TitleStyle
	l.SetShowHelp(true)

	return &Calendar{
		list:   l,
		dates:  dates,
		width:  40,
		height: 10,
		icons:  icons,
	}
}

// Init initializes the calendar
func (e *Calendar) Init() tea.Cmd {
	return nil
}

// Update handles calendar updates
func (e *Calendar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width = msg.Width
		e.height = msg.Height
		e.list.SetSize(msg.Width, msg.Height-5) // -5 for help/title
		return e, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			e.quitting = true
			return e, tea.Quit
		case "enter":
			if item, ok := e.list.SelectedItem().(CalendarItem); ok {
				e.selected = item.date
				return e, tea.Quit
			}
		}
	}

	var cmd tea.Cmd
	e.list, cmd = e.list.Update(msg)
	return e, cmd
}

// View renders the calendar
func (e *Calendar) View() string {
	if e.quitting {
		return ""
	}

	help := HelpStyle.MarginLeft(2).Render("enter: select • ctrl+c/esc: quit")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		e.list.View(),
		help,
	)
}

// GetSelectedDate returns the selected date
func (e *Calendar) GetSelectedDate() string {
	return e.selected
}

// SetIcons overrides the icon set on the calendar and its items. Useful for
// tests or callers that resolve icon mode from a higher-level config.
func (e *Calendar) SetIcons(icons IconSet) {
	e.icons = icons
	items := e.list.Items()
	for i, it := range items {
		ci, ok := it.(CalendarItem)
		if !ok {
			continue
		}
		ci.icons = icons
		items[i] = ci
	}
	e.list.SetItems(items)
	e.list.Title = withIcon(icons.Calendar, "Scratchpad Calendar")
}

// IsQuitting returns whether the calendar is quitting
func (e *Calendar) IsQuitting() bool {
	return e.quitting
}
