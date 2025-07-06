package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Editor represents the text editor component
type Editor struct {
	textarea textarea.Model
	date     string
	content  string
	quitting bool
}

// NewEditor creates a new editor instance
func NewEditor(date, content string) *Editor {
	ta := textarea.New()
	ta.Placeholder = "Start typing your notes, todos, and thoughts..."
	ta.Focus()
	ta.SetValue(content)
	ta.CharLimit = 0
	ta.ShowLineNumbers = true
	ta.SetHeight(20)

	return &Editor{
		textarea: ta,
		date:     date,
		content:  content,
	}
}

// Init initializes the editor
func (e *Editor) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles editor updates
func (e *Editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			e.quitting = true
			return e, tea.Quit
		case "ctrl+s":
			e.content = e.textarea.Value()
			return e, tea.Quit
		}
	}

	var cmd tea.Cmd
	e.textarea, cmd = e.textarea.Update(msg)
	cmds = append(cmds, cmd)

	return e, tea.Batch(cmds...)
}

// View renders the editor
func (e *Editor) View() string {
	if e.quitting {
		return ""
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		MarginLeft(2).
		Render(fmt.Sprintf("📝 Scratchpad - %s", e.date))

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		MarginLeft(2).
		Render("ctrl+s: save • ctrl+c/esc: quit")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		e.textarea.View(),
		help,
	)
}

// GetContent returns the current content
func (e *Editor) GetContent() string {
	return e.textarea.Value()
}

// IsQuitting returns whether the editor is quitting
func (e *Editor) IsQuitting() bool {
	return e.quitting
}
