package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
)

// ReviewExternalRecovery requires an explicit caller-supplied path. Discovery
// records alone never cause SP to open an external file or launch an editor.
func (a *App) ReviewExternalRecovery(date, path string) error {
	if a.privacy != nil || a.nb.editor == nil || a.nb.loader == nil {
		return errors.New("plaintext recovery requires a plaintext store and an external editor")
	}
	a.nb.AddPage(date)
	a.nb.SetCurrentDate(date)
	a.mode, a.canPop, a.nb.canPop = ModeNotebook, false, false
	a.nb.drafts[date] = editDoneMsg{date: date, path: path}
	a.trackRecovery(scratchpad.RecoveryReference{Date: date, Path: path})
	a.nb.startEdit(date)
	if a.nb.review == nil {
		return errors.New("could not load current note for recovery review; retained buffer was not changed")
	}
	return nil
}

func prepareReviewedDraft(r *externalDraftReview, choice byte, ed *editor.Editor, track func(scratchpad.RecoveryReference)) (tea.Cmd, error) {
	initial := r.current
	if choice == 'd' {
		if r.readErr != nil {
			return nil, r.readErr
		}
		initial = r.draft
	}
	// Use a fresh editing buffer, leaving the failed one unchanged for recovery.
	return prepareExternalDraft(ed, r.date, r.current, initial, r.unconfirmed, track)
}

func (n *Notebook) updateDraftReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := n.review
	switch choice := r.choice(key); choice {
	case 'x':
		n.review = nil
	case 'r', 'd':
		cmd, err := prepareReviewedDraft(r, choice, n.editor, n.onRecovery)
		if err != nil {
			r.failure(err.Error())
			return n, nil
		}
		delete(n.drafts, r.date)
		n.review = nil
		return n, cmd
	}
	return n, nil
}

func (c *Calendar) updateDraftReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := c.review
	switch choice := r.choice(key); choice {
	case 'x':
		c.review = nil
	case 'r', 'd':
		cmd, err := prepareReviewedDraft(r, choice, c.editor, c.onRecovery)
		if err != nil {
			r.failure(err.Error())
			return c, nil
		}
		delete(c.drafts, r.date)
		c.review = nil
		return c, cmd
	}
	return c, nil
}
