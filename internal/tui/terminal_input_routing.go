package tui

import tea "github.com/charmbracelet/bubbletea"

// routeTerminalInput must be the FIRST input routing in App.Update. Literal
// paste is owned by the visible text context, never a shortcut/modal key router.
// Read-only/help/confirmation/template contexts consume it without action.
func (a *App) routeTerminalInput(msg tea.Msg) (bool, tea.Cmd) {
	text, paste := privatePasteText(msg)
	rejected, bad := msg.(terminalRejectedMsg)
	if !paste && !bad {
		return false, nil
	}
	if a.templateChooser != nil {
		return true, nil
	}
	if a.dateJump != nil {
		if a.dateJump.keyHelp != nil {
			return true, nil
		}
		if bad {
			a.dateJump.err = rejected.reason
			return true, nil
		}
		_, cmd := a.updateDateJump(privatePasteMsg(text))
		return true, cmd
	}
	p := a.privacy
	if p == nil || p.closed {
		return true, nil
	}
	if p.editor != nil {
		e := p.editor
		if bad {
			e.rejectTerminalInput(msg)
			return true, nil
		}
		_, cmd := e.Update(privatePasteMsg(text))
		return true, cmd
	}
	if p.prompt != nil && p.job == nil && p.keyHelp == nil {
		if bad {
			p.promptInvalid = true
			p.promptErr = rejected.reason
		} else {
			p.insertPassphrase(text)
		}
	}
	return true, nil
}

// rejectTerminalInput must precede the writer's key dispatch. Like paste,
// rejections in a read-only help/review context do not mutate the hidden draft.
func (e *privateEditor) rejectTerminalInput(msg tea.Msg) bool {
	rejected, ok := msg.(terminalRejectedMsg)
	if !ok {
		return false
	}
	if e.keyHelp == nil && e.review == nil {
		e.input.err = rejected.reason
		e.status = rejected.reason
		e.confirm = false
	}
	return true
}
