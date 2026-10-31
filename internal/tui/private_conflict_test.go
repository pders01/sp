package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
)

func chooseReview(e *privateEditor, action string) {
	e.Update(privacyKey(action))
	e.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

func TestPrivateConflictRequiresReviewAndStillChecksConcurrentChanges(t *testing.T) {
	current, saves, seals := "base", 0, 0
	e := newPrivateEditor("2026-01-01", current, func(_ string, original, draft string) error {
		saves++
		if original != current {
			return scratchpad.ErrConflict
		}
		current = draft
		return nil
	}, func(string, string) (string, error) { seals++; return "/synthetic/recovery.age", nil })
	e.load = func(string) (string, error) { return current, nil }
	e.input.SetValue("PRIVATE_DRAFT")
	current = "concurrent"
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if saves != 1 || seals != 1 || !e.conflicted || !strings.Contains(e.status, "F2") {
		t.Fatal("conflicted save retried an unusable baseline")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyF2})
	if e.review == nil || e.review.current != "concurrent" {
		t.Fatal("review did not load current text")
	}
	e.Update(privacyKey("d"))
	e.Update(privacyKey("enter"))
	e.Update(privatePasteMsg("\r"))
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.original != "base" || saves != 1 || e.review == nil {
		t.Fatal("review accepted literal text or hidden save")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.original != "concurrent" || e.input.Value() != "PRIVATE_DRAFT" || e.conflicted {
		t.Fatal("explicit replacement selection lost draft or baseline")
	}
	current = "newer concurrent"
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if current != "newer concurrent" || !e.conflicted {
		t.Fatal("review bypassed a subsequent conflict")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyF2})
	chooseReview(e, "d")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !e.done || !e.published || current != "PRIVATE_DRAFT" {
		t.Fatal("reviewed retry did not save")
	}
}

func TestPrivateConflictReloadRequiresSuccessfulDraftRecovery(t *testing.T) {
	const draft = "\tdraft\r\n笔记\n"
	fail := true
	sealed := ""
	e := newPrivateEditor("2026-01-01", "base", func(string, string, string) error { return scratchpad.ErrConflict }, func(_ string, text string) (string, error) {
		if fail {
			return "", errors.New("synthetic recovery failure")
		}
		sealed = text
		return "/synthetic/recovery.age", nil
	})
	e.load = func(string) (string, error) { return "current saved", nil }
	e.input.SetValue(draft)
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	e.Update(tea.KeyMsg{Type: tea.KeyF2})
	chooseReview(e, "r")
	if e.review == nil || e.input.Value() != draft || e.original != "base" {
		t.Fatal("failed recovery discarded draft")
	}
	fail = false
	e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.review != nil || e.input.Value() != "current saved" || sealed != draft || e.conflicted || !strings.Contains(e.status, "recovery.age") {
		t.Fatal("reload failed to preserve the exact draft")
	}
}

func TestPrivateConflictFailedLoadAndClosedReviewPreserveDraft(t *testing.T) {
	e := newPrivateEditor("2026-01-01", "base", nil, nil)
	e.conflicted = true
	e.input.SetValue("draft")
	e.load = func(string) (string, error) { return "", scratchpad.ErrLocked }
	e.Update(tea.KeyMsg{Type: tea.KeyF2})
	if e.review != nil || e.input.Value() != "draft" || !strings.Contains(e.status, "Cannot load") {
		t.Fatal("failed load changed the draft")
	}
	e.load = func(string) (string, error) { return "current", nil }
	e.Update(tea.KeyMsg{Type: tea.KeyF2})
	e.Update(privacyKey("r"))
	e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.review != nil || e.input.Value() != "draft" || e.original != "base" || !e.conflicted {
		t.Fatal("closing review changed state")
	}
}

func TestPrivateConflictComparisonStaysOutOfPublicCaches(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	current := "PRIVATE_BASE_CANARY"
	app.SetPrivacy(PrivacyHooks{
		Load:    func(string) (PrivatePage, error) { return PrivatePage{Private: current}, nil },
		Save:    func(string, string, string) error { return scratchpad.ErrConflict },
		Recover: func(string, string) (string, error) { return "/synthetic/recovery.age", nil },
	})
	app.Update(privacyKey("e"))
	app.privacy.editor.input.SetValue("PRIVATE_DRAFT_CANARY")
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	current = "PRIVATE_CURRENT_CANARY"
	app.Update(tea.KeyMsg{Type: tea.KeyF2})
	for _, cache := range []map[string]string{app.nb.contents, app.cal.contents, app.cal.previews} {
		for _, value := range cache {
			if strings.Contains(value, "PRIVATE_") {
				t.Fatal("comparison text entered a shared/calendar cache")
			}
		}
	}
}

func TestDraftReviewQuotesControlsAndFitsTerminal(t *testing.T) {
	r := newDraftReview("2026-01-01", "saved\x1b[31m", "draft\r\n\t🙂", 80, 24)
	if strings.Contains(r.viewer.content, "\x1b") || !strings.Contains(r.viewer.content, `\x1b`) {
		t.Fatal("comparison interpreted note controls")
	}
	for _, size := range layoutSizes {
		r.viewer.resize(size.Width, size.Height)
		assertFrameFits(t, r.viewer.view(Palette{}), size)
	}
}
