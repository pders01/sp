package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNotebook(t *testing.T) {
	pages := []string{"2024-01-15", "2024-01-16", "2024-01-17"}
	notebook := NewNotebook(pages)

	assert.NotNil(t, notebook)
	assert.Equal(t, pages, notebook.pages)
	assert.Equal(t, 0, notebook.current)
	assert.Equal(t, 80, notebook.width)
	assert.Equal(t, 24, notebook.height)
	assert.False(t, notebook.quitting)
	assert.NotNil(t, notebook.contents)
}

func TestNotebook_Init(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})
	cmd := notebook.Init()
	assert.Nil(t, cmd)
}

func TestNotebook_Update_WindowSize(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15", "2024-01-16"})

	// Set initial content
	contents := map[string]string{
		"2024-01-15": "# Test Content",
		"2024-01-16": "# Another Test",
	}
	notebook.SetContents(contents)

	// Test window size update
	msg := tea.WindowSizeMsg{Width: 100, Height: 30}
	model, cmd := notebook.Update(msg)

	updatedNotebook, ok := model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 100, updatedNotebook.width)
	assert.Equal(t, 30, updatedNotebook.height)
	assert.Equal(t, 26, updatedNotebook.viewport.Height) // height - 4 (header + footer)
	assert.Equal(t, 100, updatedNotebook.viewport.Width)
	assert.Nil(t, cmd)
}

func TestNotebook_Update_Navigation(t *testing.T) {
	pages := []string{"2024-01-15", "2024-01-16", "2024-01-17"}
	notebook := NewNotebook(pages)
	notebook.width = 100
	notebook.height = 30

	// Test right navigation
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}}
	model, cmd := notebook.Update(msg)

	updatedNotebook, ok := model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 1, updatedNotebook.current)
	assert.Nil(t, cmd)

	// Test left navigation
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 0, updatedNotebook.current)
	assert.Nil(t, cmd)

	// Test arrow keys
	msg = tea.KeyMsg{Type: tea.KeyRight}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 1, updatedNotebook.current)
	assert.Nil(t, cmd)

	msg = tea.KeyMsg{Type: tea.KeyLeft}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 0, updatedNotebook.current)
	assert.Nil(t, cmd)
}

func TestNotebook_Update_NavigationBounds(t *testing.T) {
	pages := []string{"2024-01-15", "2024-01-16"}
	notebook := NewNotebook(pages)
	notebook.width = 100
	notebook.height = 30

	// Test navigation at boundaries
	// Try to go left when at first page
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	model, cmd := notebook.Update(msg)

	updatedNotebook, ok := model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 0, updatedNotebook.current) // Should stay at 0
	assert.Nil(t, cmd)

	// Go to last page
	msg = tea.KeyMsg{Type: tea.KeyRight}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 1, updatedNotebook.current)

	// Try to go right when at last page
	msg = tea.KeyMsg{Type: tea.KeyRight}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Equal(t, 1, updatedNotebook.current) // Should stay at 1
	assert.Nil(t, cmd)
}

func TestNotebook_Update_Scrolling(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})
	notebook.width = 100
	notebook.height = 30

	// Test up/down scrolling
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	model, cmd := notebook.Update(msg)

	updatedNotebook, ok := model.(*Notebook)
	require.True(t, ok)
	assert.Nil(t, cmd)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Nil(t, cmd)

	// Test page up/down
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Nil(t, cmd)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Nil(t, cmd)

	// Test goto top/bottom
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Nil(t, cmd)

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}
	model, cmd = updatedNotebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.Nil(t, cmd)
}

func TestNotebook_Update_Quit(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})

	// Test quit with 'q'
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	model, cmd := notebook.Update(msg)

	updatedNotebook, ok := model.(*Notebook)
	require.True(t, ok)
	assert.True(t, updatedNotebook.quitting)
	assert.NotNil(t, cmd) // Just check that a command is returned

	// Test quit with Ctrl+C
	notebook.quitting = false
	msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	model, cmd = notebook.Update(msg)

	updatedNotebook, ok = model.(*Notebook)
	require.True(t, ok)
	assert.True(t, updatedNotebook.quitting)
	assert.NotNil(t, cmd) // Just check that a command is returned
}

func TestNotebook_View_Empty(t *testing.T) {
	notebook := NewNotebook([]string{})
	view := notebook.View()

	assert.Contains(t, view, "No scratchpad pages found.")
}

func TestNotebook_View_Quitting(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})
	notebook.quitting = true
	view := notebook.View()

	assert.Equal(t, "", view)
}

func TestNotebook_View_WithContent(t *testing.T) {
	pages := []string{"2024-01-15", "2024-01-16"}
	notebook := NewNotebook(pages)
	notebook.width = 100
	notebook.height = 30

	contents := map[string]string{
		"2024-01-15": "# Test Content\n\nThis is a test.",
		"2024-01-16": "# Another Test\n\nMore content here.",
	}
	notebook.SetContents(contents)

	view := notebook.View()

	// Should contain header with current page (pages are sorted in reverse order)
	assert.Contains(t, view, "📖 Notebook - 2024-01-16")

	// Should contain navigation controls
	assert.Contains(t, view, "←/h: prev • →/l: next • ↑/k: up • ↓/j: down • q: quit")

	// Should contain page indicators
	assert.Contains(t, view, "2024-01-15")
	assert.Contains(t, view, "2024-01-16")
}

func TestNotebook_SetContents(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})
	notebook.width = 100
	notebook.height = 30

	contents := map[string]string{
		"2024-01-15": "# Test Content",
	}
	notebook.SetContents(contents)

	assert.Equal(t, contents, notebook.contents)
}

func TestNotebook_GetCurrentPage(t *testing.T) {
	pages := []string{"2024-01-15", "2024-01-16"}
	notebook := NewNotebook(pages)

	// Test first page (pages are sorted in reverse order, so 2024-01-16 is first)
	assert.Equal(t, "2024-01-16", notebook.GetCurrentPage())

	// Navigate to second page
	notebook.current = 1
	assert.Equal(t, "2024-01-15", notebook.GetCurrentPage())

	// Test empty pages
	emptyNotebook := NewNotebook([]string{})
	assert.Equal(t, "", emptyNotebook.GetCurrentPage())
}

func TestNotebook_IsQuitting(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})

	// Initially not quitting
	assert.False(t, notebook.IsQuitting())

	// Set quitting
	notebook.quitting = true
	assert.True(t, notebook.IsQuitting())
}

func TestNotebook_UpdateViewportContent(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})
	notebook.width = 100
	notebook.height = 30

	contents := map[string]string{
		"2024-01-15": "# Test Content\n\nThis is a test.",
	}
	notebook.SetContents(contents)

	// The viewport content should be updated with rendered markdown
	// We can't easily test the exact content due to glamour rendering,
	// but we can verify the method doesn't panic
	assert.NotPanics(t, func() {
		notebook.updateViewportContent()
	})
}

func TestNotebook_UpdateViewportContent_Empty(t *testing.T) {
	notebook := NewNotebook([]string{})
	notebook.width = 100
	notebook.height = 30

	// Should handle empty pages gracefully
	assert.NotPanics(t, func() {
		notebook.updateViewportContent()
	})
}

func TestNotebook_UpdateViewportContent_NoContent(t *testing.T) {
	notebook := NewNotebook([]string{"2024-01-15"})
	notebook.width = 100
	notebook.height = 30

	// Should handle missing content gracefully
	assert.NotPanics(t, func() {
		notebook.updateViewportContent()
	})
}

func TestNotebook_FooterScrolling(t *testing.T) {
	// Create many pages to test scrolling behavior
	pages := []string{
		"2024-01-10", "2024-01-11", "2024-01-12", "2024-01-13", "2024-01-14",
		"2024-01-15", "2024-01-16", "2024-01-17", "2024-01-18", "2024-01-19",
	}
	notebook := NewNotebook(pages)
	notebook.width = 100
	notebook.height = 30

	contents := make(map[string]string)
	for _, page := range pages {
		contents[page] = "# Content for " + page
	}
	notebook.SetContents(contents)

	// Test navigation through pages
	for i := 0; i < len(pages); i++ {
		notebook.current = i
		view := notebook.View()

		// Should always show current page
		assert.Contains(t, view, pages[i])

		// Should show navigation controls
		assert.Contains(t, view, "←/h: prev • →/l: next • ↑/k: up • ↓/j: down • q: quit")
	}
}
