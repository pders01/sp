package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// draftReview holds a read-only snapshot, separate from all calendar/shared
// caches. Controls in note text are quoted, never interpreted as terminal ANSI.
type draftReview struct {
	date     string
	current  string
	viewer   *keyHelp
	details  string
	selected byte
}

func newDraftReview(date, current, draft string, width, height int) *draftReview {
	quoteLines := func(text string) string {
		var out strings.Builder
		for i, line := range strings.Split(text, "\n") {
			fmt.Fprintf(&out, "%d | %s\n", i+1, strconv.Quote(line))
		}
		return out.String()
	}
	// This is a comparison document, not a generated key-reference screen.
	// Reuse only the viewer's themed shell and scrolling/input isolation API.
	// Quoting every original line preserves CR, tabs, trailing spaces and empty
	// lines while ensuring that note-originated ANSI is never interpreted.
	details := "Read-only comparison · no automatic merge.\n" +
		"d: use draft as replacement · r: reload/edit saved text; retain draft\n" +
		"Select an action, then press physical Enter. Nothing is saved here.\n" +
		"A further save checks for concurrent changes. F2/Esc: close.\n\n" +
		"Saved · current on disk\n" + quoteLines(current) + "\nDraft · retained writing\n" + quoteLines(draft)
	viewer := newKeyHelp("Review draft", nil, "", width, height)
	viewer.content = details
	r := &draftReview{date: date, current: current, viewer: viewer, details: details}
	r.updateTitle()
	viewer.resize(width, height)
	return r
}

func (r *draftReview) failure(message string) {
	r.viewer.content = r.details + "\nAction blocked: " + strconv.Quote(message)
	r.viewer.resize(r.viewer.width, r.viewer.height)
	r.viewer.viewport.GotoBottom()
}

// A letter selects; a separate physical Enter authorizes. Bubble Tea cannot
// distinguish a single pasted rune from a typed one, so letters alone must not
// reload or authorize replacement. Literal/batched Enter text is never a key.
func (r *draftReview) choice(key tea.KeyMsg) byte {
	if key.Type == tea.KeyEnter {
		return r.selected
	}
	if key.Type == tea.KeyRunes && !key.Alt && len(key.Runes) == 1 {
		if key.Runes[0] == 'r' || key.Runes[0] == 'd' {
			r.selected = byte(key.Runes[0])
			r.updateTitle()
			return 0
		}
	}
	if key.Type == tea.KeyF2 || r.viewer.update(key) {
		return 'x'
	}
	return 0
}

// Keep the selected action in fixed chrome, visible even at the end of a long
// comparison. Selection alone never authorizes a reload or replacement.
func (r *draftReview) updateTitle() {
	action := "choose d/r"
	switch r.selected {
	case 'd':
		action = "Enter: use draft"
	case 'r':
		action = "Enter: reload saved"
	}
	// Put the action first so narrow terminals trim context, not authorization.
	r.viewer.title = action + " · Review draft · " + r.date
}
