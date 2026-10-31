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
	details := "No automatic merge. Saving a replacement still checks for concurrent changes.\n" +
		"CURRENT SAVED TEXT\n" + quoteLines(current) + "\nRETAINED DRAFT\n" + quoteLines(draft)
	viewer := newKeyHelp("Review draft · "+date, []helpEntry{
		{keys: "d", label: "select draft as replacement; next save replaces reviewed text", visible: true},
		{keys: "r", label: "select reload/edit saved text; preserve the retained draft", visible: true},
		{keys: "Enter", label: "confirm selected action; Esc closes without changes", visible: true},
	}, details, width, height)
	viewer.title = "Review draft · " + date
	return &draftReview{current: current, viewer: viewer, details: viewer.content}
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
			r.viewer.title = "Enter: use draft"
			if r.selected == 'r' {
				r.viewer.title = "Enter: reload saved"
			}
			return 0
		}
	}
	if key.Type == tea.KeyF2 || r.viewer.update(key) {
		return 'x'
	}
	return 0
}
