package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/pders01/sp/internal/scratchpad"
)

const (
	designCaptureDate   = "2026-10-31"
	designCaptureSecret = "synthetic-input-secret"
	designCaptureNote   = "# Saturday field notes\n\nA quiet morning to review the October prototype.\n\n## Design audit\n\n- [x] Review the calendar and notebook\n- [ ] Check encrypted writing on a small terminal\n- [ ] Share the accessibility findings\n\nKeep private reflections separate from shared updates.\n\n## Next week\n\nTry a calmer hierarchy, clearer save feedback, and fewer distractions.\n"
	designCaptureShared = "## Team updates\n\n- Mira: keyboard navigation review is ready.\n- Jules: contrast checks passed for the new palette.\n- Next: test the narrow-terminal layouts.\n"
)

type designCaptureFrame struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Group string `json:"group"`
	Theme string `json:"theme"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	ANSI  string `json:"ansi"`
}

type designCaptureScene struct {
	id, title, group, want string
	compact                bool
}

// TestDesignCapture is an opt-in visual-audit fixture, not a second renderer.
// It never calls Init or executes returned tea commands: no watchers, clipboard,
// timers, external editor, crypto, Manager, store, or user configuration is used.
// Every ANSI payload comes directly from the production App.View method.
func TestDesignCapture(t *testing.T) {
	dir := os.Getenv("SP_DESIGN_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set SP_DESIGN_CAPTURE_DIR to export synthetic design frames")
	}
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	scenes := []designCaptureScene{
		{"month", "Month calendar", "calendar", "2026-10", false},
		{"year", "Year calendar", "calendar", "2026", false},
		{"plain", "Plain notebook", "notebook", "Saturday field notes", false},
		{"empty", "Empty notebook", "notebook", "Start today's note", true},
		{"plain-empty", "Saved empty personal note", "notebook", "This note is empty", true},
		{"plain-shared", "Plain note with secondary shared updates available", "notebook", "Shared updates available", true},
		{"private-month", "Locked private month calendar", "encrypted-calendar", "2026-10", false},
		{"private-year", "Locked private year calendar", "encrypted-calendar", "2026", false},
		{"locked", "Locked private note", "encrypted-notebook", "Note locked.", true},
		{"locked-shared", "Locked private note with shared updates", "encrypted-notebook", "Shared updates available", true},
		{"shared-updates-open", "Read-only shared updates", "shared-updates", "Not encrypted", true},
		{"locked-with-shared-open", "Shared updates while private note is locked", "shared-updates", "Not encrypted", true},
		{"shared-updates-help", "Shared updates key reference", "shared-updates", "Shared updates", true},
		{"locked-with-shared-help", "Locked shared updates key reference", "shared-updates", "Shared updates", true},
		{"unlocked", "Unlocked private note", "encrypted-notebook", "Saturday field notes", true},
		{"unlocked-shared", "Unlocked private note with shared updates", "encrypted-notebook", "Shared updates available", true},
		{"shared-only", "Shared updates without a private note", "encrypted-notebook", "No personal note", true},
		{"private-empty", "Unlocked empty private note", "encrypted-notebook", "empty", true},
		{"load-error", "Private note load error", "encrypted-notebook", "unavailable", true},
		{"load-error-help", "Full private load error details", "encrypted-notebook", "synthetic archive unavailable", true},
		{"unlock", "Unlock prompt", "unlock", "Passphrase:", true},
		{"unlock-masked", "Unlock prompt with synthetic masked input", "unlock", "Passphrase:", true},
		{"unlock-error", "Unlock rejected", "unlock", "Unlock failed", true},
		{"unlock-busy", "Unlock in progress", "unlock", "Unlocking your notes", true},
		{"unlock-cancelling", "Unlock cancellation pending", "unlock", "Cancelling unlock", true},
		{"unlock-help", "Unlock key reference", "unlock", "Unlock notes", true},
		{"editor", "Private Markdown editor", "private-editor", "Write · " + designCaptureDate, true},
		{"editor-empty", "Empty private writer", "private-editor", "Write · " + designCaptureDate, true},
		{"editor-dirty-cancel", "Private editor with unsaved cancellation", "private-editor", "Unsaved changes", true},
		{"editor-find", "Find in private draft", "private-editor", "prototype", true},
		{"editor-save-error", "Private save failed with encrypted recovery", "private-editor", "Not saved", true},
		{"editor-save-error-help", "Complete save error and recovery path", "private-editor", "synthetic storage unavailable", true},
		{"editor-recovery-error", "Save and encrypted recovery both failed", "private-editor", "Recovery failed", true},
		{"editor-recovery-error-help", "Complete recovery failure details", "private-editor", "synthetic recovery unavailable", true},
		{"editor-durability-error", "Published save with unconfirmed durability", "private-editor", "durability", true},
		{"editor-conflict", "Private save conflict", "private-editor", "F2: compare", true},
		{"editor-review", "Private conflict comparison", "private-editor", "", true},
		{"editor-help", "Private editor key reference", "private-editor", "Write · " + designCaptureDate, true},
		{"recovery-encrypted", "Encrypted recovery review", "recovery", "Saving encrypted", true},
		{"recovery-plaintext", "Recovery under plaintext save policy", "recovery", "Saving as plaintext", true},
		{"recovery-plaintext-save-error", "Plaintext recovery save failed", "recovery", "Saving as plaintext", true},
		{"recovery-encrypted-save-error", "Encrypted recovery save failed", "recovery", "Saving encrypted", true},
		{"templates", "Template chooser", "templates", "SAVE NOW", false},
		{"templates-selected", "Selected templates", "templates", "SAVE NOW", false},
		{"templates-applied", "Previously applied template selected again", "templates", "append again", false},
		{"templates-retry", "Template failure ready for retry", "templates", "Templates not saved", false},
		{"templates-applying", "Template application in progress", "templates", "Saving templates", true},
		{"templates-cancelling", "Template cancellation pending", "templates", "", true},
		{"templates-help", "Template key reference", "templates", "Templates", false},
		{"date-jump", "Jump to date", "date-jump", "YYYY-MM-DD", false},
		{"date-invalid", "Invalid date", "date-jump", "Enter a valid date", false},
		{"date-help", "Date jump key reference", "date-jump", "Jump to date", false},
		{"calendar-help", "Calendar key reference", "help", "", false},
		{"notebook-help", "Notebook key reference", "help", "", false},
	}
	var frames []designCaptureFrame
	ids := make(map[string]bool)
	for _, theme := range []string{ThemePrefDark, ThemePrefLight} {
		// Set before constructors, including their initial auto resolution.
		t.Setenv("GLAMOUR_STYLE", theme)
		for _, scene := range scenes {
			sizes := []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 80, Height: 24}, {Width: 56, Height: 16}}
			if scene.compact {
				sizes = append(sizes, tea.WindowSizeMsg{Width: 40, Height: 12})
			}
			for _, size := range sizes {
				id := fmt.Sprintf("%s-%s-%dx%d", scene.id, theme, size.Width, size.Height)
				t.Run(id, func(t *testing.T) {
					app := designCaptureApp(t, scene, theme, size)
					view := app.View()
					if view == "" || strings.Contains(view, designCaptureSecret) {
						t.Fatal("empty frame or synthetic secret exposed")
					}
					if scene.id == "unlock-masked" && !strings.Contains(view, "Resize terminal") && !strings.Contains(view, "•") {
						t.Fatal("synthetic input was not visibly masked")
					}
					plain := ansi.Strip(view)
					if (strings.HasPrefix(scene.id, "locked") || scene.group == "unlock" || scene.group == "shared-updates") && strings.Contains(plain, "Saturday field notes") {
						t.Fatal("locked or secondary view exposed private note text")
					}
					if scene.group == "shared-updates" && !strings.HasSuffix(scene.id, "help") && !strings.Contains(plain, "Resize terminal") {
						for _, label := range []string{"Not encrypted", "self-reported", "read-only"} {
							if !strings.Contains(strings.ToLower(plain), strings.ToLower(label)) {
								t.Fatalf("shared view missing provenance label %q", label)
							}
						}
					}
					if scene.group == "recovery" && !strings.Contains(plain, "Resize terminal") {
						policy := "Saving encrypted"
						if strings.HasPrefix(scene.id, "recovery-plaintext") {
							policy = "Saving as plaintext"
						}
						if !strings.Contains(plain, policy) {
							t.Fatalf("recovery save policy hidden: %q", policy)
						}
					}
					if size.Width >= 56 && !strings.Contains(plain, "Resize terminal") && !strings.Contains(plain, "sp / ") {
						t.Fatal("view lost common branded location chrome")
					}
					if strings.HasSuffix(scene.id, "help") && !strings.Contains(view, "\x1b[38;2;") && !strings.Contains(view, "\x1b[48;2;") {
						t.Fatal("help lost its themed palette")
					}
					if view != app.View() {
						t.Fatal("View changed without an update")
					}
					lines := strings.Split(view, "\n")
					if len(lines) > size.Height {
						t.Fatalf("frame height %d exceeds %d", len(lines), size.Height)
					}
					for line, text := range lines {
						if width := ansi.StringWidth(text); width > size.Width {
							t.Fatalf("line %d display width %d exceeds %d", line+1, width, size.Width)
						}
					}
					// Narrow views may deliberately use the production small-terminal fallback.
					if size.Width == 100 && !strings.Contains(strings.ToLower(strings.Join(strings.Fields(plain), " ")), strings.ToLower(strings.Join(strings.Fields(scene.want), " "))) {
						t.Fatalf("frame missing scene marker %q", scene.want)
					}
					if ids[id] {
						t.Fatalf("duplicate frame id %q", id)
					}
					ids[id] = true
					frames = append(frames, designCaptureFrame{id, scene.title, scene.group, theme, size.Width, size.Height, view})
				})
			}
		}
	}
	if t.Failed() {
		t.Fatal("capture failed; manifest not written")
	}
	colored := false
	for _, frame := range frames {
		colored = colored || strings.Contains(frame.ANSI, "\x1b[38;2;") || strings.Contains(frame.ANSI, "\x1b[48;2;")
	}
	if !colored {
		t.Fatal("capture has no truecolor ANSI styling")
	}
	data, err := json.MarshalIndent(frames, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), designCaptureSecret) {
		t.Fatal("synthetic secret exposed in manifest")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "frames.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("exported %d synthetic production-view frames to %s", len(frames), path)
}

func designCaptureApp(t *testing.T, scene designCaptureScene, theme string, size tea.WindowSizeMsg) *App {
	t.Helper()
	mode := ModeNotebook
	if scene.group == "calendar" || scene.group == "encrypted-calendar" || scene.id == "calendar-help" {
		mode = ModeCalendar
	}
	dates := []string{designCaptureDate, "2026-10-30", "2026-10-28", "2026-10-22", "2026-09-30"}
	if scene.id == "empty" {
		dates = nil
	}
	cal := NewCalendar(dates)
	cal.SetThemePref(theme)
	cal.today = time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC)
	cal.cursor = cal.today
	nb := newNotebookAt(dates, designCaptureDate)
	nb.SetThemePref(theme)
	if scene.id != "empty" {
		contents := map[string]string{
			designCaptureDate: designCaptureNote,
			"2026-10-30":      "# Friday review\n\nTested the prototype and collected keyboard feedback.\n",
			"2026-10-28":      "# Midweek\n\nA smaller scope makes room for careful work.\n",
			"2026-10-22":      "# Research\n\nCompare notebook navigation patterns.\n",
			"2026-09-30":      "# September retrospective\n\nCarry the useful ideas forward.\n",
		}
		if scene.id == "plain-empty" {
			contents[designCaptureDate] = ""
		}
		cal.SetContents(contents)
		nb.SetContents(contents)
		nb.SetCurrentDate(designCaptureDate)
	}
	app := NewApp(cal, nb, mode)
	t.Cleanup(func() {
		// These are display-only jobs with no worker. Never let Close wait for
		// a fake unlock or consume a fake template completion as real work.
		if app.privacy != nil {
			app.privacy.job = nil
		}
		if app.templateChooser != nil {
			app.templateChooser.attempt = nil
			app.templateChooser.applying = false
		}
		app.Close()
	})
	app.Update(size) // No Init: in particular, no appearance subscription.
	if scene.id == "plain-shared" {
		app.SetSharedContents(map[string]string{designCaptureDate: designCaptureShared})
	}
	if scene.id == "year" || scene.id == "private-year" {
		cal.view = ViewYear
	}

	privateLoads, privateSaves, privateUnlocks := 0, 0, 0
	private := scene.group == "encrypted-calendar" || scene.group == "encrypted-notebook" ||
		scene.group == "unlock" || scene.group == "private-editor" || scene.group == "recovery" || scene.group == "templates" || scene.group == "shared-updates"
	if private {
		locked := scene.group == "unlock" || scene.group == "encrypted-calendar" || strings.HasPrefix(scene.id, "locked")
		page := PrivatePage{HasPrivate: true, Locked: locked}
		if !locked {
			page.Private = designCaptureNote
		}
		if strings.HasSuffix(scene.id, "-shared") || scene.id == "shared-only" || scene.group == "shared-updates" {
			page.Shared = designCaptureShared
		}
		if scene.id == "shared-only" {
			page.Private, page.HasPrivate = "", false
		}
		if scene.id == "private-empty" || scene.id == "editor-empty" {
			page.Private = ""
		}
		app.SetPrivacy(PrivacyHooks{
			Inspect: func(string) (PublicPage, error) {
				return PublicPage{Shared: page.Shared, HasPrivate: page.HasPrivate}, nil
			},
			IsUnlocked: func() bool { return !locked },
			Unlock: func([]byte) error {
				privateUnlocks++
				return errors.New("synthetic passphrase rejected")
			},
			Lock: func() {},
			Load: func(string) (PrivatePage, error) {
				privateLoads++
				if strings.HasPrefix(scene.id, "load-error") {
					return PrivatePage{}, errors.New("synthetic archive unavailable")
				}
				return page, nil
			},
			Save: func(string, string, string) error {
				privateSaves++
				if strings.Contains(scene.id, "save-error") || strings.HasPrefix(scene.id, "editor-recovery-error") {
					return errors.New("synthetic storage unavailable")
				}
				if scene.id == "editor-durability-error" {
					return scratchpad.ErrDurabilityUnconfirmed
				}
				if scene.id == "editor-conflict" || scene.id == "editor-review" {
					return scratchpad.ErrConflict
				}
				return nil
			},
			Recover: func(string, string) (string, error) {
				if strings.HasPrefix(scene.id, "editor-recovery-error") {
					return "", errors.New("synthetic recovery unavailable")
				}
				return "/synthetic/recovery.age", nil
			},
		})
	}
	key := func(kind tea.KeyType) { app.Update(tea.KeyMsg{Type: kind}) }
	runes := func(text string) { app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}) }
	switch scene.group {
	case "shared-updates":
		loadsBefore := privateLoads
		runes("c")
		for _, blocked := range []string{"u", "e", "i", "a", "d", "h", "l"} {
			runes(blocked)
		}
		key(tea.KeyEnter)
		key(tea.KeyCtrlS)
		if !nb.sharedActive() || privateLoads != loadsBefore || privateSaves != 0 || privateUnlocks != 0 || app.privacy.prompt != nil || app.privacy.editor != nil || app.templateChooser != nil || app.dateJump != nil || nb.GetCurrentPage() != designCaptureDate {
			t.Fatal("secondary shared viewer forwarded a private load, edit, unlock, template, or date action")
		}
		if strings.HasSuffix(scene.id, "help") {
			key(tea.KeyF1)
		}
	case "encrypted-notebook":
		if scene.id == "load-error-help" {
			key(tea.KeyF1)
		}
	case "unlock":
		runes("u")
		switch scene.id {
		case "unlock-masked":
			runes(designCaptureSecret)
		case "unlock-error":
			runes(designCaptureSecret)
			key(tea.KeyEnter)
		case "unlock-busy", "unlock-cancelling":
			// A closed done channel is safe even if cleanup changes later. No
			// goroutine or KDF is started; the production busy View is used.
			done := make(chan struct{})
			close(done)
			app.privacy.job = &privateUnlockJob{done: done, cancelRequested: scene.id == "unlock-cancelling"}
		case "unlock-help":
			key(tea.KeyF1)
		}
	case "private-editor":
		key(tea.KeyEnter)
		e := app.privacy.editor
		if e == nil {
			t.Fatal("private editor did not open")
		}
		switch scene.id {
		case "editor-dirty-cancel", "editor-save-error", "editor-save-error-help", "editor-recovery-error", "editor-recovery-error-help", "editor-durability-error", "editor-conflict", "editor-review":
			e.input.SetValue(designCaptureNote + "\nDraft: simplify the save feedback.\n")
			if scene.id == "editor-dirty-cancel" {
				key(tea.KeyEsc)
			} else {
				key(tea.KeyCtrlS)
				if scene.id == "editor-review" {
					key(tea.KeyF2)
					if e.review == nil {
						t.Fatal("conflict review did not open")
					}
				}
				if strings.HasSuffix(scene.id, "help") {
					key(tea.KeyF1)
					key(tea.KeyEnd) // Full outcome/path details remain reachable.
				}
			}
		case "editor-find":
			key(tea.KeyCtrlF)
			runes("prototype")
		case "editor-help":
			key(tea.KeyF1)
		}
	case "recovery":
		policy := "Saving encrypted"
		if strings.HasPrefix(scene.id, "recovery-plaintext") {
			policy = "Saving as plaintext"
		}
		hooks := app.privacy.hooks
		e := recoveredPrivateEditor(designCaptureDate, designCaptureNote,
			designCaptureNote+"\nRecovered draft: keep the hierarchy calm.\n", hooks.Save, hooks.Recover,
			func(string) (string, error) { return designCaptureNote, nil }, policy)
		e.resize(size.Width, size.Height)
		app.privacy.editor = e
		if strings.HasSuffix(scene.id, "save-error") {
			key(tea.KeyCtrlS)
		}
	case "templates":
		app.SetTemplates([]DayTemplate{
			{ID: "daily", Name: "Daily reflection"},
			{ID: "work", Name: "Work log"},
			{ID: "review", Name: "Weekly review"},
		}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
			t.Error("capture must not launch template work")
			return TemplateApplyResult{}, errors.New("unexpected synthetic application")
		})
		runes("a")
		c := app.templateChooser
		if c == nil {
			t.Fatal("template chooser did not open")
		}
		if scene.id != "templates" && scene.id != "templates-help" {
			key(tea.KeySpace)
		}
		switch scene.id {
		case "templates-applied":
			app.setPrivateApplied(designCaptureDate, []string{"daily"})
		case "templates-retry", "templates-applying", "templates-cancelling":
			done := make(chan struct{})
			close(done)
			c.applying, c.attempt = true, &templateApplyAttempt{done: done}
			switch scene.id {
			case "templates-retry":
				app.finishTemplateApply(templateAppliedMsg{date: designCaptureDate, attempt: c.attempt, err: errors.New("synthetic storage unavailable")})
			case "templates-cancelling":
				key(tea.KeyEsc)
			}
		case "templates-help":
			key(tea.KeyF1)
		}
	case "date-jump":
		runes("d")
		switch scene.id {
		case "date-invalid":
			runes("2026-02-30")
			key(tea.KeyEnter)
		case "date-help":
			key(tea.KeyF1)
		default:
			runes(designCaptureDate)
		}
	case "help":
		key(tea.KeyF1)
	}
	return app
}
