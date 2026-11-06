package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSharedUpdatesRouterCannotReachPrivateOrPublicationActions(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	loads, unlocks, saves, applies := 0, 0, 0, 0
	app.SetPrivacy(PrivacyHooks{Inspect: func(string) (PublicPage, error) { return PublicPage{Shared: "PUBLIC_UPDATE", HasPrivate: true}, nil }, IsUnlocked: func() bool { return false }, Unlock: func([]byte) error { unlocks++; return nil }, Load: func(string) (PrivatePage, error) {
		loads++
		return PrivatePage{Shared: "PUBLIC_UPDATE", Locked: true, HasPrivate: true}, nil
	}, Save: func(string, string, string) error { saves++; return nil }})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		applies++
		return TemplateApplyResult{}, nil
	})
	before := loads
	app.Update(privacyKey("c"))
	if !app.nb.sharedActive() || !strings.Contains(app.View(), "PUBLIC_UPDATE") || !strings.Contains(app.View(), "Not encrypted") {
		t.Fatal("read-only public context did not open")
	}
	for _, key := range []tea.KeyMsg{privacyKey("u"), privacyKey("e"), privacyKey("i"), privacyKey("a"), privacyKey("d"), {Type: tea.KeyEnter}, {Type: tea.KeyCtrlS}} {
		app.Update(key)
	}
	app.Update(privatePasteMsg("c"))
	if !app.nb.sharedActive() || loads != before || unlocks+saves+applies != 0 || app.privacy.editor != nil || app.privacy.prompt != nil || app.dateJump != nil || app.templateChooser != nil {
		t.Fatal("shared context leaked an action to the primary view")
	}
}

func TestSharedHelpOwnsSessionKeysAndBackLayer(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	locks := 0
	app.SetPrivacy(PrivacyHooks{Inspect: func(string) (PublicPage, error) { return PublicPage{Shared: "PUBLIC", HasPrivate: true}, nil }, IsUnlocked: func() bool { return true }, Lock: func() { locks++ }, Load: func(string) (PrivatePage, error) {
		return PrivatePage{Private: "PRIVATE", Shared: "PUBLIC", HasPrivate: true}, nil
	}})
	app.Update(privacyKey("c"))
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	pref := app.cal.theme.Pref()
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if locks != 0 || app.cal.theme.Pref() != pref {
		t.Fatal("shared help forwarded session actions")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !app.nb.sharedActive() || app.nb.shared.help != nil {
		t.Fatal("Escape did not close the help layer first")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if app.IsQuitting() || !app.nb.sharedActive() || app.nb.shared.help != nil {
		t.Fatal("modal help forwarded hard quit")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.nb.sharedActive() || app.IsQuitting() {
		t.Fatal("leaving updates did not return to note")
	}
}

func TestPlainNotebookDistinguishesSavedEmptyAbsentAndSharedOnly(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	date := app.nb.GetCurrentPage()
	app.nb.SetPageContent(date, "")
	if !strings.Contains(app.View(), "This note is empty") {
		t.Fatal("saved empty personal writing was described as absent")
	}
	app.SetSharedContents(map[string]string{date: "PUBLIC"})
	app.SetPersonalPresence(map[string]bool{date: false})
	if !strings.Contains(app.View(), "No personal note") || !strings.Contains(app.View(), "Shared updates available") {
		t.Fatal("shared-only state was described as a saved personal note")
	}
	app.nb.AddPage("2026-10-31")
	app.nb.SetCurrentDate("2026-10-31")
	if !strings.Contains(app.View(), "No personal note") || app.cal.HasData("2026-10-31") {
		t.Fatal("selecting an absent day manufactured a saved empty note")
	}
}

func TestRelockingSharedViewClearsHiddenPrivateRendering(t *testing.T) {
	app := newTestApp(ModeNotebook)
	defer app.Close()
	unlocked, loads := true, 0
	initialDate := app.nb.GetCurrentPage()
	app.nb.AddPage("2024-01-14")
	app.SetPrivacy(PrivacyHooks{Inspect: func(string) (PublicPage, error) { return PublicPage{Shared: "PUBLIC", HasPrivate: true}, nil }, IsUnlocked: func() bool { return unlocked }, Lock: func() { unlocked = false }, Load: func(string) (PrivatePage, error) {
		loads++
		p := PrivatePage{Shared: "PUBLIC", HasPrivate: true, Locked: !unlocked}
		if unlocked {
			p.Private = strings.Repeat("PRIVATE_CANARY\n\n", 100)
		}
		return p, nil
	}})
	app.nb.viewport.SetYOffset(19)
	app.Update(privacyKey("c"))
	app.Update(privacyKey("h"))
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if unlocked || strings.Contains(app.nb.viewport.View(), "PRIVATE_CANARY") || strings.Contains(app.View(), "PRIVATE_CANARY") || app.nb.privatePage.Private != "" {
		t.Fatal("relock left decrypted projections behind the shared pane")
	}
	if loads != 1 || !app.nb.sharedActive() || app.nb.shared.date != "2024-01-14" || app.nb.positions[initialDate] != 19 {
		t.Fatal("shared navigation/relock loaded a personal note or lost its offset")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if loads != 2 || app.nb.privateDate != "2024-01-14" || !app.nb.privatePage.Locked {
		t.Fatal("explicit return did not refresh the selected locked personal day")
	}
}
