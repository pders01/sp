package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
)

// Both legacy views must project publication and retain failed drafts using
// the same lifecycle, including publication with unconfirmed durability.
func finishExternalDraft(msg editDoneMsg, save Saver, drafts map[string]editDoneMsg, theme *themeWatcher,
	changed, fallback func(string, string), recovery func(scratchpad.RecoveryReference), consumed func(string, string),
) tea.Cmd {
	result := completeExternalEdit(msg, save)
	if result.published {
		if changed != nil {
			changed(msg.date, result.content)
		} else {
			fallback(msg.date, result.content)
		}
	}
	if result.err != nil {
		rememberExternalFailure(drafts, msg, result)
		result.status += "; e: review/retry retained draft"
		if msg.path != "" && recovery != nil {
			recovery(scratchpad.RecoveryReference{Date: msg.date, Path: msg.path})
		}
		theme.SetStatus(result.status, 0)
		return nil
	}
	delete(drafts, msg.date)
	if consumed != nil {
		consumed(msg.date, msg.path)
	}
	theme.SetStatus(result.status, 1500*time.Millisecond)
	return theme.expireStatusCmd(1500 * time.Millisecond)
}
