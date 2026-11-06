package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSharedBrowsingPublicOnlyAcrossDatesAndExplicitReturn(t *testing.T) {
	for _, unlocked := range []bool{false, true} {
		for _, exit := range []tea.KeyMsg{privacyKey("c"), {Type: tea.KeyEsc}} {
			t.Run(fmt.Sprintf("unlocked=%t/exit=%s", unlocked, exit.String()), func(t *testing.T) {
				const current = "2026-10-31"
				app := workflowApp(t, ModeNotebook, map[string]string{
					current: "", "2026-10-30": "", "2026-10-29": "", "2026-10-28": "",
				})
				loads, inspections, actions := []string{}, []string{}, 0
				public := map[string]string{current: strings.Repeat("PUBLIC_CURRENT\n\n", 100), "2026-10-28": strings.Repeat("PUBLIC_EARLIER\n\n", 100)}
				app.SetPrivacy(PrivacyHooks{
					Inspect: func(date string) (PublicPage, error) {
						inspections = append(inspections, date)
						if date == "2026-10-29" {
							return PublicPage{}, errors.New("PRIVATE_DIAGNOSTIC")
						}
						return PublicPage{Shared: public[date], HasPrivate: true}, nil
					},
					IsUnlocked: func() bool { return unlocked },
					Load: func(date string) (PrivatePage, error) {
						loads = append(loads, date)
						p := PrivatePage{Locked: !unlocked, HasPrivate: true, Shared: "STALE_SHARED"}
						if unlocked {
							p.Private = strings.Repeat("PRIVATE_CANARY_"+date+"\n\n", 100)
						}
						return p, nil
					},
					Unlock: func([]byte) error { actions++; return nil },
					Save:   func(string, string, string) error { actions++; return nil },
				})
				if unlocked {
					app.nb.viewport.SetYOffset(17)
				}
				before := len(loads)
				app.Update(privacyKey("c"))
				app.nb.shared.viewport.SetYOffset(19)
				for _, step := range []struct {
					key  tea.KeyMsg
					date string
					want string
				}{
					{tea.KeyMsg{Type: tea.KeyRight}, current, "PUBLIC_CURRENT"}, // Latest boundary.
					{privacyKey("h"), "2026-10-30", "No shared updates for this day."},
					{tea.KeyMsg{Type: tea.KeyLeft}, "2026-10-29", "Shared updates unavailable for this day."},
					{privacyKey("h"), "2026-10-28", "PUBLIC_EARLIER"},
					{tea.KeyMsg{Type: tea.KeyLeft}, "2026-10-28", "PUBLIC_EARLIER"}, // Earliest boundary.
				} {
					app.Update(step.key)
					view := ansi.Strip(app.View())
					if !app.nb.sharedActive() || app.nb.shared.date != step.date || app.nb.GetCurrentPage() != step.date || !strings.Contains(view, step.want) {
						t.Fatalf("shared navigation lost date/state: %s -> %s", step.key, step.date)
					}
					for _, forbidden := range []string{"PRIVATE_CANARY", "PRIVATE_DIAGNOSTIC", "STALE_SHARED", "enter/e", "unlock"} {
						if strings.Contains(view, forbidden) {
							t.Fatalf("shared view exposed %q", forbidden)
						}
					}
					if len(loads) != before || actions != 0 {
						t.Fatal("public browsing reached private hooks")
					}
				}
				app.nb.shared.viewport.SetYOffset(11)
				app.Update(tea.KeyMsg{Type: tea.KeyF1})
				app.Update(privacyKey("l"))
				app.Update(tea.KeyMsg{Type: tea.KeyRight})
				if app.nb.GetCurrentPage() != "2026-10-28" || strings.Contains(app.View(), "PRIVATE_") || !strings.Contains(ansi.Strip(app.nb.shared.help.content), "earlier/later saved date") {
					t.Fatal("help leaked private diagnostics or forwarded date keys")
				}
				app.Update(tea.KeyMsg{Type: tea.KeyEsc})
				for range 3 {
					app.Update(privacyKey("l"))
				}
				if app.nb.shared.viewport.YOffset != 19 || len(loads) != before || inspections[len(inspections)-1] != current {
					t.Fatal("return visit did not inspect publicly or restore shared position")
				}
				app.Update(exit) // Explicit same-date personal return must refresh, not reuse stale data.
				if app.nb.sharedActive() || len(loads) != before+1 || loads[len(loads)-1] != current || actions != 0 {
					t.Fatal("explicit personal return did not load exactly the selected date")
				}
				if unlocked && app.nb.viewport.YOffset != 17 {
					t.Fatal("personal offset was not independent of shared navigation")
				}
				app.Update(privacyKey("c"))
				for range 3 {
					app.Update(privacyKey("h"))
				}
				if app.nb.shared.viewport.YOffset != 11 || len(loads) != before+1 {
					t.Fatal("earlier shared offset was not restored independently")
				}
				app.Update(exit)
				if loads[len(loads)-1] != "2026-10-28" || len(loads) != before+2 || actions != 0 {
					t.Fatal("return from another shared date did not select its personal note")
				}
			})
		}
	}
}

func TestSharedFreshInspectionOverridesPrivateCacheRemovalAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			app := newTestApp(ModeNotebook)
			t.Cleanup(app.Close)
			text, fail, loads := "PUBLIC_OLD", false, 0
			app.SetPrivacy(PrivacyHooks{
				Inspect: func(string) (PublicPage, error) {
					if fail {
						return PublicPage{Shared: "PUBLIC_OLD"}, errors.New("PRIVATE_ERROR_CANARY")
					}
					return PublicPage{Shared: text}, nil
				},
				Load: func(string) (PrivatePage, error) {
					loads++
					return PrivatePage{Shared: "PUBLIC_OLD", Private: "PRIVATE_CANARY"}, nil
				},
			})
			text, fail = "", failure
			app.Update(privacyKey("c")) // Fresh inspect after the cached availability hint.
			want := "No shared updates for this day."
			if failure {
				want = "Shared updates unavailable for this day."
			}
			if !app.nb.sharedActive() || !strings.Contains(ansi.Strip(app.View()), want) || strings.Contains(app.View(), "PUBLIC_OLD") || loads != 1 {
				t.Fatal("fresh absence/failure reused a stale private-page shared cache")
			}
			app.Update(tea.KeyMsg{Type: tea.KeyF1})
			if strings.Contains(app.View(), "PRIVATE_") {
				t.Fatal("public help exposed private text or inspection diagnostics")
			}
			app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			text, fail = "PUBLIC_NEW", false
			app.refreshPublicPage(app.nb.GetCurrentPage())
			if !app.nb.sharedActive() || !strings.Contains(app.View(), "PUBLIC_NEW") || loads != 1 {
				t.Fatal("public refresh did not recover without private loads")
			}
		})
	}
}

func TestSharedBrowsingWithoutInspectorUsesOnlyPublicCaches(t *testing.T) {
	app := newTestApp(ModeNotebook)
	t.Cleanup(app.Close)
	app.nb.AddPage("2024-01-14")
	loads := 0
	app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) {
		loads++
		return PrivatePage{Shared: "PUBLIC_CURRENT", Private: "PRIVATE_CANARY"}, nil
	}})
	app.nb.contents["2024-01-14"] = "PUBLIC_EARLIER"
	app.Update(privacyKey("c"))
	if !app.nb.sharedActive() || !strings.Contains(app.View(), "PUBLIC_CURRENT") {
		t.Fatal("hooks without Inspect lost their existing shared-page fallback")
	}
	app.Update(privacyKey("h"))
	if !app.nb.sharedActive() || !strings.Contains(app.View(), "PUBLIC_EARLIER") || strings.Contains(app.View(), "PRIVATE_CANARY") || loads != 1 {
		t.Fatal("navigation without Inspect did not use only cached public projections")
	}
}

func TestSharedUnavailableRefreshRetainsReadingOffset(t *testing.T) {
	n := sharedNotebook(t)
	date := n.GetCurrentPage()
	n.privacyEnabled = true
	n.contents[date] = strings.Repeat("PUBLIC_LINE\n\n", 100)
	n.sharedInspections[date] = true
	n.openShared()
	n.shared.viewport.SetYOffset(23)
	n.sharedInspections[date] = false
	n.refreshShared()
	if !n.sharedActive() || n.shared.ready || n.sharedPositions[date] != 23 {
		t.Fatal("unavailable projection erased the last public reading position")
	}
	n.Update(tea.KeyMsg{Type: tea.KeyDown})
	n.closeShared()
	n.sharedInspections[date] = true
	n.openShared()
	if n.shared.viewport.YOffset != 23 {
		t.Fatal("successful public read did not restore its pre-error position")
	}
}

func TestSharedHintAndDateStripCenteredWithinCommonGutters(t *testing.T) {
	for _, theme := range []string{ThemePrefDark, ThemePrefLight} {
		for _, width := range []int{24, 32, 40, 48, 56, 80, 100} {
			t.Run(fmt.Sprintf("%s/%d", theme, width), func(t *testing.T) {
				n := sharedNotebook(t)
				n.SetThemePref(theme)
				n.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				footer := ansi.Strip(n.renderFooter())
				strip := ansi.Strip(n.renderDateStrip())
				hint := strings.Split(footer, strip)[0]
				for _, line := range strings.Split(strings.TrimSuffix(hint, "\n")+"\n"+strip, "\n") {
					left := len(line) - len(strings.TrimLeft(line, " "))
					right := width - left - ansi.StringWidth(strings.TrimSpace(line))
					if left < viewGutter || right < viewGutter || left-right < -1 || left-right > 1 {
						t.Fatalf("not centered inside gutters at width %d: %q", width, line)
					}
				}
				n.Update(privacyKey("c"))
				if !strings.Contains(ansi.Strip(n.sharedFooter()), strip) || strings.Contains(n.sharedFooter(), "enter/e") {
					t.Fatal("shared footer omitted dates or reused personal editing actions")
				}
				view := n.View()
				if width >= 48 {
					for _, label := range []string{"Read-only", "Not encrypted", "self-reported"} {
						if !strings.Contains(ansi.Strip(view), label) {
							t.Fatalf("shared policy label clipped at width %d: %s", width, label)
						}
					}
				}
				if viewHeight(view) > n.height {
					t.Fatal("shared frame exceeded terminal height")
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > width {
						t.Fatal("shared frame exceeded terminal width")
					}
				}
			})
		}
	}
}
