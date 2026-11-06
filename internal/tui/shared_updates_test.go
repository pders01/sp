package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/editor"
)

func sharedNotebook(t *testing.T) *Notebook {
	t.Helper()
	const date = "2026-10-31"
	n := newNotebookAt([]string{date, "2026-10-30"}, date)
	t.Cleanup(n.Close)
	n.SetThemePref(ThemePrefDark)
	n.sharedContents = map[string]string{date: strings.Repeat("SHARED_ONLY\n\n", 100), "2026-10-30": "OTHER_SHARED"}
	n.SetContents(map[string]string{date: strings.Repeat("PRIVATE_ONLY\n\n", 100), "2026-10-30": "OTHER_PRIVATE"})
	return n
}

func TestSharedUpdatesExplicitSecondaryAndIndependentPositions(t *testing.T) {
	n := sharedNotebook(t)
	n.viewport.SetYOffset(17)
	primary := ansi.Strip(n.View())
	if !strings.Contains(primary, "Shared updates available · c view") || strings.Contains(primary, "SHARED_ONLY") || strings.Contains(primary, "Private notes") || strings.Contains(primary, "Agent contributions") {
		t.Fatal("primary view mixed documents or competing headings")
	}
	n.Update(privacyKey("c"))
	if !n.sharedActive() || n.shared.viewport.YOffset != 0 || n.viewport.YOffset != 17 {
		t.Fatal("opening shared view disturbed the private reading position")
	}
	shared := ansi.Strip(n.View())
	for _, want := range []string{"Shared updates", "Read-only", "Not encrypted", "self-reported", "SHARED_ONLY"} {
		if !strings.Contains(shared, want) {
			t.Fatalf("shared view missing %q", want)
		}
	}
	if strings.Contains(shared, "PRIVATE_ONLY") {
		t.Fatal("shared view contains private writing")
	}
	n.shared.viewport.SetYOffset(29)
	n.Update(privacyKey("c"))
	if n.sharedActive() || n.viewport.YOffset != 17 || n.positions[n.GetCurrentPage()] != 17 {
		t.Fatal("return did not preserve private numeric offset")
	}
	n.Update(privacyKey("c"))
	if n.shared.viewport.YOffset != 29 {
		t.Fatal("shared reading position was not independently restored")
	}
	n.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if n.IsPopping() || n.IsQuitting() || n.viewport.YOffset != 17 {
		t.Fatal("shared Escape escaped the notebook or lost its position")
	}
	n.SetCurrentDate("2026-10-30")
	n.viewport.SetYOffset(0)
	n.SetCurrentDate("2026-10-31")
	if n.viewport.YOffset != 17 {
		t.Fatal("shared toggling damaged date-keyed private positions")
	}
}

func TestSharedUpdatesIgnoreEditPublicationAndPastes(t *testing.T) {
	n := sharedNotebook(t)
	calls := 0
	n.loader = func(string) (string, error) { calls++; return "PRIVATE_LOADED", nil }
	n.save = func(string, string, string) error { calls++; return nil }
	n.editorProvider = func() (*editor.Editor, error) { calls++; return nil, nil }
	n.Update(privacyKey("c"))
	date := n.GetCurrentPage()
	for _, msg := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEnter}, privacyKey("e"), privacyKey("i"),
		privacyKey("u"), privacyKey("a"), privacyKey("d"),
		tea.KeyMsg{Type: tea.KeyCtrlS}, privacyKey("enter"),
		privacyKey("left"), privacyKey("right"),
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Paste: true},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}, Alt: true},
		privatePasteMsg("e\na\nd\nu\nc\nq"), privatePasteMsg("c"),
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true},
	} {
		_, cmd := n.Update(msg)
		if cmd != nil || !n.sharedActive() || n.IsQuitting() || n.IsPopping() || n.GetSelectedDate() != "" || n.GetCurrentPage() != date {
			t.Fatalf("shared read-only layer forwarded action: %#v", msg)
		}
	}
	if cmd := n.startEdit(date); cmd != nil || calls != 0 {
		t.Fatal("shared view reached private loader/editor/publication")
	}
	if strings.Contains(n.shared.viewport.View(), "PRIVATE_ONLY") || strings.Contains(n.sharedText(date), "PRIVATE_ONLY") {
		t.Fatal("private text entered shared projection")
	}
}

func TestSharedUpdatesAvailabilityRequiresNonWhitespaceAndTruthfulAbsence(t *testing.T) {
	for _, text := range []string{"", " \n\t\r ", "SHARED_ONLY"} {
		t.Run(fmt.Sprintf("text=%q", text), func(t *testing.T) {
			n := sharedNotebook(t)
			date := n.GetCurrentPage()
			n.contents[date] = ""
			n.personalDates[date] = false // Explicit shared-only/absent metadata.
			n.sharedContents[date] = text
			n.updateViewportContent()
			view := ansi.Strip(n.View())
			available := strings.TrimSpace(text) != ""
			if strings.Contains(view, "Shared updates available · c view") != available || !strings.Contains(view, "No personal note for this day") {
				t.Fatal("absence or shared availability is misleading")
			}
			n.Update(privacyKey("c"))
			if n.sharedActive() != available {
				t.Fatal("empty shared pane opened, or nonempty updates unavailable")
			}
		})
	}
}

func TestSharedUpdatesRefreshRemovalAndDateChangesStayShared(t *testing.T) {
	n := sharedNotebook(t)
	n.viewport.SetYOffset(12)
	n.Update(privacyKey("c"))
	n.shared.viewport.SetYOffset(18)
	n.SetCurrentDate("2026-10-30")
	if !n.sharedActive() || n.shared.date != "2026-10-30" || n.sharedPositions["2026-10-31"] != 18 || n.positions["2026-10-31"] != 12 {
		t.Fatal("date selection did not retain shared mode and independent positions")
	}
	n.SetCurrentDate("2026-10-31")
	if n.shared.viewport.YOffset != 18 || n.viewport.YOffset != 12 {
		t.Fatal("date selection mixed private/shared reading positions")
	}
	n.sharedContents[n.GetCurrentPage()] = " \n\t "
	n.updateViewportContent()
	if !n.sharedActive() || !strings.Contains(ansi.Strip(n.View()), "No shared updates for this day.") || strings.Contains(ansi.Strip(n.View()), "SHARED_ONLY") {
		t.Fatal("removing updates did not show truthful absence in shared mode")
	}
}

func TestSharedHelpNeverProjectsPrivateDiagnosticsAndHardQuit(t *testing.T) {
	for _, quit := range []tea.KeyMsg{privacyKey("q"), {Type: tea.KeyCtrlC}} {
		n := sharedNotebook(t)
		n.diagnostics = "PRIVATE_DIAGNOSTIC"
		n.theme.SetStatus("PRIVATE_ERROR", 0)
		n.Update(privacyKey("c"))
		n.Update(tea.KeyMsg{Type: tea.KeyF1})
		view := ansi.Strip(n.View())
		for _, secret := range []string{"PRIVATE_ONLY", "PRIVATE_ERROR", "PRIVATE_DIAGNOSTIC"} {
			if strings.Contains(view, secret) {
				t.Fatalf("shared help leaked %s", secret)
			}
		}
		n.Update(quit)
		if n.IsQuitting() || !n.sharedActive() || n.shared.help != nil {
			t.Fatal("help did not own quit before underlying view")
		}
		n.Update(quit)
		if !n.IsQuitting() || n.GetSelectedDate() != "" {
			t.Fatal("ordinary shared view did not honor hard quit")
		}
	}
}

func TestSharedPrivacyRelockClearsHiddenViewportAndKeepsPositions(t *testing.T) {
	n := sharedNotebook(t)
	date := n.GetCurrentPage()
	n.privacyEnabled = true
	n.contents[date] = "SHARED_ONLY"
	n.privateDate = date
	n.privatePage = &PrivatePage{Private: strings.Repeat("PRIVATE_DECRYPTED\n\n", 100), Shared: "SHARED_ONLY", HasPrivate: true}
	n.updateViewportContent()
	n.viewport.SetYOffset(23)
	n.Update(privacyKey("c"))
	n.privatePage = &PrivatePage{Locked: true, Shared: "SHARED_ONLY", HasPrivate: true}
	n.updateViewportContent()
	if !n.sharedActive() || strings.Contains(n.viewport.View(), "PRIVATE_DECRYPTED") || n.viewportReady || n.positions[date] != 23 {
		t.Fatal("relock retained hidden plaintext or lost the private numeric position")
	}
	if strings.Contains(ansi.Strip(n.shared.viewport.View()), "PRIVATE_DECRYPTED") || !strings.Contains(ansi.Strip(n.shared.viewport.View()), "SHARED_ONLY") {
		t.Fatal("relock shared projection was contaminated")
	}
	n.Update(tea.KeyMsg{Type: tea.KeyEsc})
	n.updateViewportContent()
	if n.positions[date] != 23 {
		t.Fatal("locked placeholder overwrote remembered private offset")
	}
	n.privatePage = &PrivatePage{Private: strings.Repeat("PRIVATE_DECRYPTED\n\n", 100), HasPrivate: true}
	n.updateViewportContent()
	if n.viewport.YOffset != 23 {
		t.Fatal("unlock did not restore original private reading position")
	}
}
