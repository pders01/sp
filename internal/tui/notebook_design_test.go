package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNotebookBodyUsesOneTwoCellInset(t *testing.T) {
	for _, pref := range []string{ThemePrefDark, ThemePrefLight} {
		n := newNotebookAt([]string{"2026-10-31"}, "2026-10-31")
		t.Cleanup(n.Close)
		n.SetThemePref(pref)
		n.SetContents(map[string]string{"2026-10-31": "BODYCANARY"})
		found := false
		for _, line := range strings.Split(ansi.Strip(n.View()), "\n") {
			if idx := strings.Index(line, "BODYCANARY"); idx >= 0 {
				found = true
				if idx != 2 {
					t.Fatalf("%s: body starts at cell %d, want two-cell inset", pref, idx)
				}
			}
		}
		if !found {
			t.Fatal("note body missing")
		}
	}
}

func TestNotebookChronologyStripAndBounds(t *testing.T) {
	n := newNotebookAt([]string{"2026-10-31", "2026-10-27", "2026-11-02", "2026-10-29"}, "2026-10-31")
	t.Cleanup(n.Close)
	n.width = 100
	n.SetContents(map[string]string{"2026-10-31": "Today"})
	for _, key := range []tea.KeyMsg{privacyKey("h"), {Type: tea.KeyLeft}, {Type: tea.KeyLeft}} {
		n.Update(key)
	}
	if n.GetCurrentPage() != "2026-10-27" {
		t.Fatal("earlier navigation did not stop at the earliest saved date")
	}
	for _, key := range []tea.KeyMsg{privacyKey("l"), {Type: tea.KeyRight}, {Type: tea.KeyRight}, {Type: tea.KeyRight}} {
		n.Update(key)
	}
	if n.GetCurrentPage() != "2026-11-02" {
		t.Fatal("later navigation did not stop at the latest saved date")
	}
	strip := strings.Split(ansi.Strip(n.renderFooter()), "\n")[0]
	prev := -1
	for _, date := range []string{"2026-10-27", "2026-10-29", "2026-10-31", "2026-11-02"} {
		idx := strings.Index(strip, date)
		if idx <= prev {
			t.Fatalf("date strip is not chronological: %q", strip)
		}
		prev = idx
	}
	for _, width := range []int{24, 40, 56, 80, 100} {
		n.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		for _, date := range n.pages {
			n.SetCurrentDate(date)
			footer := ansi.Strip(n.renderFooter())
			if !strings.Contains(footer, date) {
				t.Fatalf("width %d: selected date absent from pager: %q", width, footer)
			}
			assertFrameFits(t, n.View(), tea.WindowSizeMsg{Width: width, Height: 24})
		}
	}
}

func TestNotebookDesignBoundedShellsAcrossThemesAndSizes(t *testing.T) {
	for _, theme := range []string{ThemePrefDark, ThemePrefLight} {
		t.Run(theme, func(t *testing.T) {
			n := sharedNotebook(t)
			n.SetThemePref(theme)
			for _, size := range []tea.WindowSizeMsg{
				{Width: 100, Height: 30}, {Width: 80, Height: 24}, {Width: 56, Height: 16},
				{Width: 48, Height: 12}, {Width: 24, Height: 8}, {Width: 8, Height: 3},
				{Width: 0, Height: 0},
			} {
				n.Update(size)
				assertFrameFits(t, n.View(), size)
				n.Update(privacyKey("c"))
				assertFrameFits(t, n.View(), size)
				n.Update(tea.KeyMsg{Type: tea.KeyF1})
				assertFrameFits(t, n.View(), size)
				n.Update(tea.KeyMsg{Type: tea.KeyEsc})
				if !n.sharedActive() || n.shared.help != nil {
					t.Fatal("Escape did not close shared help first")
				}
				n.Update(tea.KeyMsg{Type: tea.KeyEsc})
				if n.sharedActive() {
					t.Fatal("Escape from shared updates did not return to the note")
				}
			}
			empty := newNotebookAt(nil, "2026-10-31")
			t.Cleanup(empty.Close)
			empty.SetThemePref(theme)
			empty.privacyEnabled = true
			empty.privacyState = func() string { return "locked" }
			view := ansi.Strip(empty.View())
			for _, want := range []string{"Notebook", "Start today's note", "locked"} {
				if !strings.Contains(view, want) {
					t.Fatalf("empty shell missing %q: %s", want, view)
				}
			}
		})
	}
}

func TestNotebookSharedAppRelockDropsHiddenPrivateProjection(t *testing.T) {
	const date = "2026-10-31"
	app := workflowApp(t, ModeNotebook, map[string]string{date: "SHAREDCANARY"})
	unlocked, loads := true, 0
	app.SetPrivacy(PrivacyHooks{
		Inspect: func(string) (PublicPage, error) {
			return PublicPage{Shared: "SHAREDCANARY", HasPrivate: true}, nil
		},
		IsUnlocked: func() bool { return unlocked },
		Lock:       func() { unlocked = false },
		Load: func(string) (PrivatePage, error) {
			loads++
			page := PrivatePage{HasPrivate: true, Locked: !unlocked, Shared: "SHAREDCANARY"}
			if unlocked {
				page.Private = strings.Repeat("DECRYPTEDCANARY\n\n", 100)
			}
			return page, nil
		},
	})
	app.nb.viewport.SetYOffset(19)
	app.Update(privacyKey("c"))
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if unlocked || !app.nb.sharedActive() || app.nb.viewportReady || app.nb.positions[date] != 19 || loads != 1 {
		t.Fatal("locking shared pane changed private load/position contracts")
	}
	for _, view := range []string{app.nb.viewport.View(), app.nb.shared.viewport.View(), app.View()} {
		if strings.Contains(ansi.Strip(view), "DECRYPTEDCANARY") {
			t.Fatal("lock left a decrypted display projection")
		}
	}
	for _, cache := range []map[string]string{app.nb.contents, app.nb.sharedContents, app.cal.contents, app.cal.previews} {
		for _, text := range cache {
			if strings.Contains(text, "DECRYPTEDCANARY") {
				t.Fatal("private writing entered public/shared cache")
			}
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if loads != 1 || app.nb.positions[date] != 19 {
		t.Fatal("return from shared pane loaded private writing or lost its position")
	}
	unlocked = true // Synthetic session transition; no real secret or storage.
	app.refreshPrivatePage()
	if app.nb.viewport.YOffset != 19 {
		t.Fatal("unlock after shared-view relock lost private reading position")
	}
}

func TestNotebookSharedAppOwnsKeysWhileLockedWithoutPrivateLoad(t *testing.T) {
	const date = "2026-10-31"
	app := workflowApp(t, ModeNotebook, map[string]string{date: "SHARED_LOCKED"})
	loads, saves, unlocks, locks := 0, 0, 0, 0
	app.SetPrivacy(PrivacyHooks{
		Inspect:    func(string) (PublicPage, error) { return PublicPage{Shared: "SHARED_LOCKED", HasPrivate: true}, nil },
		IsUnlocked: func() bool { return false },
		Load: func(string) (PrivatePage, error) {
			loads++
			return PrivatePage{Locked: true, HasPrivate: true, Shared: "SHARED_LOCKED"}, nil
		},
		Unlock: func([]byte) error { unlocks++; return nil },
		Lock:   func() { locks++ },
		Save:   func(string, string, string) error { saves++; return nil },
	})
	initialLoads := loads
	app.Update(privacyKey("c"))
	if !app.nb.sharedActive() || loads != initialLoads {
		t.Fatal("opening shared updates invoked a private load")
	}
	for _, msg := range []tea.Msg{
		privacyKey("u"), privacyKey("e"), privacyKey("i"), privacyKey("a"), privacyKey("d"),
		tea.KeyMsg{Type: tea.KeyEnter}, tea.KeyMsg{Type: tea.KeyCtrlS},
		tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyRight},
		privatePasteMsg("e"), privatePasteMsg("c"), privatePasteMsg("q"),
	} {
		app.Update(msg)
		if !app.nb.sharedActive() || app.privacy.prompt != nil || app.privacy.editor != nil || app.templateChooser != nil || app.dateJump != nil || app.IsQuitting() {
			t.Fatalf("App routed shared action to another owner: %#v", msg)
		}
	}
	if loads != initialLoads || saves != 0 || unlocks != 0 || locks != 0 {
		t.Fatal("read-only shared keys touched private hooks")
	}
	app.Update(tea.WindowSizeMsg{Width: 56, Height: 16})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if !app.nb.sharedActive() || locks != 1 || loads != initialLoads {
		t.Fatal("session lock did not preserve shared viewing without a private load")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.nb.sharedActive() || app.IsQuitting() || loads != initialLoads {
		t.Fatal("return from locked shared view invoked private work or quit")
	}
}
