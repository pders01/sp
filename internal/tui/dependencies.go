package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editor"
)

// EditorProvider resolves an editing dependency only after an explicit edit.
// A resolution error leaves the read-only viewer running; it is never a signal
// to fall back to an unwired external editor or discard a retained draft.
type EditorProvider func() (*editor.Editor, error)

func (n *Notebook) SetEditorProvider(resolve EditorProvider, save Saver, load func(string) (string, error)) {
	n.editor, n.editorProvider, n.save, n.loader = nil, resolve, save, load
}

func (c *Calendar) SetEditorProvider(resolve EditorProvider, save Saver, load func(string) (string, error)) {
	c.editor, c.editorProvider, c.save, c.loader = nil, resolve, save, load
}

func unavailableEdit(status func(string), err error) tea.Cmd {
	status(fmt.Sprintf("Editor unavailable: %v; browsing remains available. Correct EDITOR/VISUAL and retry.", err))
	return func() tea.Msg { return nil }
}

// SetTemplateResolver defers normalization/trust validation until application is
// requested. The resolver configures SetTemplates on success and may be retried
// after failure. It must not execute template commands while resolving.
func (a *App) SetTemplateResolver(resolve func() error) {
	a.templateResolver = resolve
	a.cal.templatesAvailable, a.nb.templatesAvailable = resolve != nil, resolve != nil
}

func (a *App) openTemplateChooser() bool {
	if a.templateResolver != nil {
		history := a.appliedTemplates
		if err := a.templateResolver(); err != nil {
			a.SetNotice(fmt.Sprintf("Templates unavailable: %v; browsing and ordinary editing remain available.", err))
			return true
		}
		a.templateResolver = nil
		for date, ids := range history {
			if a.appliedTemplates[date] == nil {
				a.appliedTemplates[date] = make(map[string]bool)
			}
			for id, applied := range ids {
				a.appliedTemplates[date][id] = applied
			}
		}
	}
	return a.startTemplateChooser()
}

func (a *App) SetConfigurationNotices(notices []string) {
	text := strings.Join(notices, "\n")
	a.cal.diagnostics, a.nb.diagnostics = text, text
	if text != "" {
		a.SetNotice(text)
	}
}

func diagnosticDetails(status, diagnostics string) string {
	if diagnostics == "" || strings.Contains(status, diagnostics) {
		return status
	}
	return status + "\nConfiguration warnings (still in effect):\n" + diagnostics
}

// SetNotice keeps configuration/action diagnostics available in both views and
// their expanded help instead of losing a warning behind the alternate screen.
func (a *App) SetNotice(message string) {
	a.cal.theme.SetStatus(message, 0)
	a.nb.theme.SetStatus(message, 0)
}
