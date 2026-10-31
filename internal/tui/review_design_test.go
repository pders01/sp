package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReviewDesignComparisonAndExplicitAuthorization(t *testing.T) {
	for _, palette := range []Palette{darkPalette(), lightPalette()} {
		r := newDraftReview("2026-10-31", "saved text", "retained draft", 80, 24)
		view := ansi.Strip(r.viewer.view(palette))
		for _, want := range []string{"Review draft", "2026-10-31", "choose d/r", "Read-only comparison", "no automatic merge", "physical Enter", "Nothing is saved here", "further save", "Saved · current on disk", "Draft · retained writing", `1 | "saved text"`, `1 | "retained draft"`, "F2/Esc: close"} {
			if !strings.Contains(view, want) {
				t.Fatalf("comparison missing %q:\n%s", want, view)
			}
		}
		if strings.Contains(view, " · keys") || strings.Contains(view, "\nDetails\n") {
			t.Fatal("comparison is still presented as a generated key reference")
		}
		if r.choice(tea.KeyMsg{Type: tea.KeyEnter}) != 0 {
			t.Fatal("Enter authorized a default replacement")
		}
		for _, key := range []tea.KeyMsg{privacyKey("enter"), privacyKey("dr"), {Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true}} {
			if r.choice(key) != 0 || r.selected != 0 {
				t.Fatal("literal/batched/Alt text selected or authorized an action")
			}
		}
		if r.choice(privacyKey("d")) != 0 || r.selected != 'd' {
			t.Fatal("draft selection itself authorized replacement")
		}
		if !strings.Contains(ansi.Strip(r.viewer.view(palette)), "Enter: use draft") {
			t.Fatal("selected draft action is not visible")
		}
		if r.choice(tea.KeyMsg{Type: tea.KeyEnter}) != 'd' {
			t.Fatal("physical Enter did not authorize the selected draft action")
		}
		if r.choice(privacyKey("r")) != 0 || r.choice(tea.KeyMsg{Type: tea.KeyEnter}) != 'r' {
			t.Fatal("reload no longer requires separate selection and Enter")
		}
		for _, closeKey := range []tea.KeyType{tea.KeyF2, tea.KeyEsc} {
			if r.choice(tea.KeyMsg{Type: closeKey}) != 'x' {
				t.Fatal("F2/Esc did not close comparison without authorizing the selection")
			}
		}
	}
}

func TestReviewDesignQuotedBytesAndSelectedActionSurviveScrolling(t *testing.T) {
	current := "saved\x1b[31m\r\n\t  🙂  \n\x00\n"
	draft := strings.Repeat("synthetic draft line\n", 60) + "END_OF_DRAFT\r\n\t  \n"
	r := newDraftReview("2026-10-31", current, draft, 80, 24)
	if r.current != current || strings.Contains(r.viewer.content, "\x1b") || strings.Contains(r.viewer.content, "\x00") {
		t.Fatal("comparison changed the saved snapshot or interpreted note controls")
	}
	for _, original := range []string{current, draft} {
		for _, line := range strings.Split(original, "\n") {
			if !strings.Contains(r.viewer.content, strconv.Quote(line)) {
				t.Fatalf("comparison lost an exact quoted line: %q", line)
			}
		}
	}
	r.choice(privacyKey("d"))
	for _, size := range layoutSizes {
		r.viewer.resize(size.Width, size.Height)
		assertFrameFits(t, r.viewer.view(darkPalette()), size)
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 56, Height: 16}, {Width: 30, Height: 10}} {
		r.viewer.resize(size.Width, size.Height)
		r.viewer.viewport.GotoTop()
		r.choice(tea.KeyMsg{Type: tea.KeyPgDown})
		if r.viewer.viewport.YOffset == 0 {
			t.Fatal("small-terminal comparison cannot scroll")
		}
		r.choice(tea.KeyMsg{Type: tea.KeyEnd})
		view := ansi.Strip(r.viewer.view(lightPalette()))
		assertFrameFits(t, view, size)
		if !strings.Contains(view, "END_OF_DRAFT") {
			t.Fatal("scrolling cannot reach the retained draft's final lines")
		}
		if !strings.Contains(strings.Split(view, "\n")[0], "Enter: use draft") {
			t.Fatalf("selected action scrolled out of fixed chrome:\n%s", view)
		}
		if r.selected != 'd' || r.current != current {
			t.Fatal("scrolling changed the selected action or saved snapshot")
		}
	}
}

func TestReviewDesignFailureRemainsQuotedAndScrollable(t *testing.T) {
	r := newDraftReview("2026-10-31", "saved", "draft", 80, 24)
	r.choice(privacyKey("r"))
	message := strings.Repeat("synthetic recovery error ", 60) + "\x1b[31mEND_OF_ERROR"
	r.failure(message)
	if !strings.Contains(r.viewer.content, strconv.Quote(message)) || strings.Contains(r.viewer.content, "\x1b") {
		t.Fatal("failure diagnostic was lost or interpreted as terminal ANSI")
	}
	view := ansi.Strip(r.viewer.view(darkPalette()))
	for _, want := range []string{"Enter: reload saved", "END_OF_ERROR"} {
		if !strings.Contains(view, want) {
			t.Fatalf("blocked comparison missing %q:\n%s", want, view)
		}
	}
	if r.selected != 'r' || r.current != "saved" {
		t.Fatal("reporting a blocked action changed the snapshot or selection")
	}
}
