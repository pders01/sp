package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/pders01/sp/internal/scratchpad"
)

func designColor(t *testing.T) {
	t.Helper()
	t.Setenv("GLAMOUR_STYLE", "dark")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestWritingAppearanceFollowsSessionAndStylesHelp(t *testing.T) {
	designColor(t)
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{IsUnlocked: func() bool { return true }, Load: func(string) (PrivatePage, error) {
		return PrivatePage{Private: "exact\tbytes\r\n", HasPrivate: true}, nil
	}})
	app.Update(privacyKey("e"))
	e := app.privacy.editor
	if e == nil || e.theme != app.nb.theme {
		t.Fatal("writer did not adopt shared appearance")
	}
	if !strings.Contains(e.View(), darkPalette().Header.Render("sp")) {
		t.Fatalf("writer has no SP accent: %q", e.View())
	}
	original := e.input.Value()
	e.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !strings.Contains(e.View(), darkPalette().Header.Render("sp")) {
		t.Fatal("writer help lost active palette")
	}
	app.cal.theme.SetPref(ThemePrefLight)
	app.syncAppearance(false)
	if !strings.Contains(e.View(), lightPalette().Header.Render("sp")) || e.input.Value() != original {
		t.Fatal("light appearance did not reach help, or altered draft")
	}
}

func TestWritingThemeShortcutPreservesBytesAndSessionPreference(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "dark")
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{IsUnlocked: func() bool { return true }, Load: func(string) (PrivatePage, error) {
		return PrivatePage{Private: "raw\tbytes\r\n", HasPrivate: true}, nil
	}})
	app.Update(privacyKey("e"))
	e := app.privacy.editor
	before := e.input.Value()
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if app.cal.theme.Pref() != app.nb.theme.Pref() || e.theme.Pref() != app.nb.theme.Pref() || e.input.Value() != before {
		t.Fatal("writer theme desynchronized preferences or changed bytes")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	pref := e.theme.Pref()
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if e.theme.Pref() != pref {
		t.Fatal("find forwarded a session shortcut")
	}
}

func TestWritingFailureShowsShortOutcomeAndRetainsFullDetails(t *testing.T) {
	cause := "synthetic detail " + strings.Repeat("cause ", 80)
	e := newPrivateEditor("2026-10-31", "saved", func(string, string, string) error { return errors.New(cause) }, func(string, string) (string, error) { return "", errors.New("synthetic recovery unavailable") })
	e.resize(80, 24)
	e.input.SetValue("DRAFT\tEXACT\r\n")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !strings.Contains(e.View(), "Recovery failed") || !strings.Contains(e.View(), "keep this editor open") {
		t.Fatalf("critical outcome hidden: %s", e.View())
	}
	e.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !strings.Contains(e.keyHelp.content, cause) || e.input.Value() != "DRAFT\tEXACT\r\n" {
		t.Fatal("full failure/draft lost during presentation")
	}
}

func TestWritingPublishedWarningNeverClaimsNotSaved(t *testing.T) {
	e := newPrivateEditor("2026-10-31", "saved", func(string, string, string) error { return scratchpad.ErrDurabilityUnconfirmed }, func(string, string) (string, error) { return "/synthetic/recovery.age", nil })
	e.resize(80, 24)
	e.input.SetValue("published")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	view := e.View()
	if strings.Contains(view, "Not saved") || !strings.Contains(view, "Saved, but durability") || !strings.Contains(view, "durability unconfirmed") {
		t.Fatalf("false publication outcome: %s", view)
	}
}

func TestWritingPublishedWarningAndFailedRecoveryStayExplicit(t *testing.T) {
	e := newPrivateEditor("2026-10-31", "saved", func(string, string, string) error { return scratchpad.ErrDurabilityUnconfirmed }, func(string, string) (string, error) { return "", errors.New("synthetic recovery failed") })
	e.resize(80, 24)
	e.input.SetValue("published")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	view := strings.Join(strings.Fields(e.View()), " ")
	for _, text := range []string{"Saved, but durability is unconfirmed", "recovery failed", "Keep this editor open"} {
		if !strings.Contains(view, text) {
			t.Fatalf("combined critical outcome hidden: %s", view)
		}
	}
	if strings.Contains(view, "Not saved") {
		t.Fatal("published data described as not saved")
	}
}

func TestWritingPolicyAndCancellationAreVisibleInCompactFrames(t *testing.T) {
	for _, policy := range []string{"Saving encrypted", "Saving as plaintext"} {
		e := recoveredPrivateEditor("2026-10-31", "saved", "draft", nil, func(string, string) (string, error) { return "/synthetic/recovery.age", nil }, nil, policy)
		e.resize(56, 16)
		if !strings.Contains(e.View(), policy) {
			t.Fatal("persistent recovery policy missing")
		}
		e.Update(tea.KeyMsg{Type: tea.KeyEsc})
		view := e.View()
		if !strings.Contains(view, "Esc again") || !strings.Contains(view, "encrypted recovery") {
			t.Fatalf("confirmation consequences clipped: %s", view)
		}
		for _, size := range layoutSizes {
			e.Update(size)
			assertFrameFits(t, e.View(), size)
		}
	}
}

func TestWritingNewDraftAfterUnconfirmedPublicationIsExplicitlyUnsaved(t *testing.T) {
	e := newPrivateEditor("2026-10-31", "saved", func(string, string, string) error { return scratchpad.ErrDurabilityUnconfirmed }, func(string, string) (string, error) { return "/synthetic/recovery.age", nil })
	e.resize(80, 24)
	e.input.SetValue("published")
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	e.Update(privacyKey(" newer"))
	view := strings.Join(strings.Fields(e.View()), " ")
	if !strings.Contains(view, "newer draft not saved") || !strings.Contains(view, "unsaved changes") || strings.Contains(view, "Saved, but durability") {
		t.Fatalf("new draft inherited prior saved outcome: %s", view)
	}
	e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(e.outcomeSummary(), "New changes not saved") {
		t.Fatal("cancellation hid newer unsaved changes")
	}
	e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if e.input.Value() != e.original || !strings.Contains(e.outcomeSummary(), "Saved, but durability") {
		t.Fatal("retry did not advance published baseline")
	}
}

func TestUnlockSmallFormDoesNotAcceptInvisibleSubmission(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	calls := 0
	app.SetPrivacy(PrivacyHooks{IsUnlocked: func() bool { return false }, Unlock: func([]byte) error { calls++; return nil }, Load: func(string) (PrivatePage, error) { return PrivatePage{Locked: true, HasPrivate: true}, nil }})
	app.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	app.Update(privacyKey("u"))
	app.Update(privacyKey("synthetic"))
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if calls != 0 || app.privacy.prompt.Value() != "synthetic" || !strings.Contains(app.View(), "Resize terminal") {
		t.Fatal("unsupported form submitted/cleared an invisible secret")
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if calls != 1 {
		t.Fatal("resized supported form could not submit")
	}
}

func TestUnlockFormUsesSharedShellWithoutExposingInput(t *testing.T) {
	designColor(t)
	app := newTestApp(ModeNotebook)
	defer app.Close()
	app.SetPrivacy(PrivacyHooks{IsUnlocked: func() bool { return false }, Load: func(string) (PrivatePage, error) { return PrivatePage{HasPrivate: true, Locked: true}, nil }})
	app.Update(privacyKey("u"))
	app.Update(privacyKey("synthetic-secret"))
	for _, size := range layoutSizes {
		app.Update(size)
		view := app.View()
		assertFrameFits(t, view, size)
		if strings.Contains(view, "synthetic-secret") {
			t.Fatal("form exposed secret")
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(app.View(), "Unlock notes") || !strings.Contains(app.View(), darkPalette().Header.Render("sp")) {
		t.Fatalf("unlock form lost common visual shell: %q", app.View())
	}
}
