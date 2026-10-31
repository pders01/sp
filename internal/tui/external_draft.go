package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

func (n *Notebook) editLabel() string {
	if _, pending := n.drafts[n.GetCurrentPage()]; pending {
		return "review/retry"
	}
	return "edit"
}

func (c *Calendar) editLabel() string {
	if _, pending := c.drafts[c.CursorDate()]; pending {
		return "review/retry"
	}
	return "edit"
}

type externalDraftReview struct {
	*draftReview
	date, draft string
	unconfirmed bool
	readErr     error
}

func reviewExternalDraft(msg editDoneMsg, current string, width, height int) *externalDraftReview {
	draft, err := editor.ReadEdited(msg.path)
	r := newDraftReview(msg.date, current, draft, width, height)
	// An external editor saves on successful return, not on an SP Ctrl+S key.
	r.viewer.content = strings.Replace(r.viewer.content, "next save replaces reviewed text", "successful editor return attempts replacement of reviewed text", 1)
	r.details = r.viewer.content
	if err != nil {
		r.failure("Retained draft is unavailable; replacement is disabled: " + err.Error())
	}
	return &externalDraftReview{draftReview: r, date: msg.date, draft: draft, unconfirmed: msg.unconfirmed, readErr: err}
}

func prepareExternalDraft(ed *editor.Editor, date, baseline, initial string, unconfirmed bool, track func(scratchpad.RecoveryReference)) (tea.Cmd, error) {
	if ed == nil {
		return nil, errors.New("external editor is unavailable")
	}
	cmd, path, cleanup, err := ed.Prepare(initial)
	if err != nil {
		return nil, err
	}
	if track != nil {
		track(scratchpad.RecoveryReference{Date: date, Path: path})
	}
	return tea.ExecProcess(cmd, func(execErr error) tea.Msg {
		return editDoneMsg{date: date, original: baseline, path: path, cleanup: cleanup, err: execErr, unconfirmed: unconfirmed}
	}), nil
}

func rememberExternalFailure(pending map[string]editDoneMsg, msg editDoneMsg, result externalEditResult) {
	if result.published {
		msg.original, msg.unconfirmed = result.content, true
	}
	pending[msg.date] = msg
}
