package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/pders01/sp/internal/scratchpad"
)

// Only synthetic applier commands are executed here, never terminal/editor,
// clipboard, timer, or filesystem commands.
func templateWorkflowApp(t *testing.T, mode AppMode, apply TemplateApplier) *App {
	t.Helper()
	app := newTestApp(mode)
	t.Cleanup(app.Close)
	app.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "Section one"}, {ID: "two", Name: "Section two"}}, nil, apply)
	app.Update(privacyKey("a"))
	if app.templateChooser == nil {
		t.Fatal("chooser did not open")
	}
	return app
}

func templateWorkflowStart(t *testing.T, app *App) tea.Cmd {
	t.Helper()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !app.templateChooser.applying {
		t.Fatal("selected sections did not start applying")
	}
	return cmd
}

func templateWorkflowStatus(app *App) *themeWatcher {
	if app.mode == ModeNotebook {
		return app.nb.theme
	}
	return app.cal.theme
}

func TestTemplateWorkflowSaveNowSelectionAndHistory(t *testing.T) {
	calls := 0
	app := templateWorkflowApp(t, ModeCalendar, func(_ context.Context, _ string, selections []TemplateSelection) (TemplateApplyResult, error) {
		calls++
		if len(selections) != 1 || selections[0].ID != "one" || !selections[0].Force {
			t.Fatalf("append-again selections = %+v", selections)
		}
		return TemplateApplyResult{Content: "another copy", Applied: []string{"one"}}, nil
	})
	date := app.templateChooser.date
	app.appliedTemplates[date] = map[string]bool{"one": true}
	view := ansi.Strip(app.View())
	for _, want := range []string{"SAVE NOW", "saves before editing", "Editor cancel does not undo insertion", "[ ] [applied] Section one", "[ ] Section two"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in chooser: %s", want, view)
		}
	}
	if strings.Contains(view, "[✓]") {
		t.Fatal("history looks selected")
	}
	if _, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || calls != 0 || app.templateChooser.applying {
		t.Fatal("empty selection started work")
	}
	if !strings.Contains(app.View(), "Nothing selected") {
		t.Fatal("empty selection feedback is missing")
	}
	app.Update(privacyKey(" "))
	if view = ansi.Strip(app.View()); !strings.Contains(view, "[✓] [applied] [append again] Section one") {
		t.Fatalf("selection and history are not independent: %s", view)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	for _, want := range []string{"SAVE NOW", "another copy", "existing writing stays unchanged", "Cancelling the editor does not undo"} {
		if !strings.Contains(app.templateChooser.keyHelp.content, want) {
			t.Errorf("help is missing %q", want)
		}
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc}) // Close help, not the chooser.
	cmd := templateWorkflowStart(t, app)
	app.Update(cmd())
	if calls != 1 || !app.templateApplied(date, "one") || app.cal.contents[date] != "another copy" || app.nb.contents[date] != "another copy" {
		t.Fatal("append-again outcome did not propagate")
	}
}

func TestTemplateWorkflowFailuresRetainActionableRetryContext(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		for _, applyErr := range []error{errors.New("synthetic storage unavailable"), scratchpad.ErrLocked, scratchpad.ErrDurabilityUnconfirmed} {
			t.Run(fmt.Sprintf("%d/%v", mode, applyErr), func(t *testing.T) {
				calls := 0
				app := templateWorkflowApp(t, mode, func(_ context.Context, _ string, selections []TemplateSelection) (TemplateApplyResult, error) {
					calls++
					if len(selections) != 1 || selections[0].ID != "two" || selections[0].Force {
						t.Fatalf("retry lost selection: %+v", selections)
					}
					if calls == 1 {
						return TemplateApplyResult{Content: "unpublished text", Applied: []string{"two"}}, applyErr
					}
					return TemplateApplyResult{Content: "persisted retry", Applied: []string{"two"}}, nil
				})
				date := app.templateChooser.date
				app.Update(tea.KeyMsg{Type: tea.KeyDown})
				app.Update(privacyKey(" "))
				chooser := app.templateChooser
				cmd := templateWorkflowStart(t, app)
				// Completion must reveal the error even if help was opened while saving.
				app.Update(tea.KeyMsg{Type: tea.KeyF1})
				_, next := app.Update(cmd())
				if next != nil || app.templateChooser != chooser || chooser.applying || chooser.cancel != nil || chooser.keyHelp != nil || chooser.cursor != 1 || !chooser.selected["two"] {
					t.Fatal("failure discarded retry context or hid its outcome")
				}
				if app.templateApplied(date, "two") || app.cal.contents[date] == "unpublished text" || app.nb.contents[date] == "unpublished text" {
					t.Fatal("unpublished failure mutated saved state")
				}
				status := templateWorkflowStatus(app)
				if !status.statusExpiry.IsZero() || !strings.Contains(status.StatusText(), applyErr.Error()) || !strings.Contains(chooser.feedback, "Enter retries") {
					t.Fatal("failure is transient or not actionable")
				}
				app.Update(statusExpireMsg{})
				if status.StatusText() == "" || chooser.feedback == "" {
					t.Fatal("unrelated expiration erased the failure")
				}
				app.Update(privacyKey(" ")) // Clearing selection must not erase the error.
				if _, next := app.Update(tea.KeyMsg{Type: tea.KeyEnter}); next != nil || !strings.Contains(chooser.feedback, applyErr.Error()) || !strings.Contains(chooser.feedbackText(), "Nothing selected") {
					t.Fatal("empty selection erased the persistent diagnostic")
				}
				app.Update(privacyKey(" "))
				if strings.Contains(chooser.feedbackText(), "Nothing selected") {
					t.Fatal("selection feedback remained stale after selecting a section")
				}
				app.Update(tea.KeyMsg{Type: tea.KeyF1})
				if !strings.Contains(chooser.keyHelp.content, applyErr.Error()) {
					t.Fatal("full diagnostic unavailable in help")
				}
				app.Update(tea.KeyMsg{Type: tea.KeyEsc})
				cmd = templateWorkflowStart(t, app)
				app.Update(cmd())
				if calls != 2 || app.templateChooser != nil || app.cal.contents[date] != "persisted retry" || app.nb.contents[date] != "persisted retry" {
					t.Fatal("retry did not publish through both views")
				}
			})
		}
	}
}

func TestTemplateWorkflowEscapeCancelsAndWaitsWithoutLeavingApp(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			app := templateWorkflowApp(t, mode, func(ctx context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
				<-ctx.Done() // Accepted work may start before the UI sends Escape.
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("Escape did not cancel the applier context")
				}
				return TemplateApplyResult{}, ctx.Err()
			})
			date, page := app.cal.CursorDate(), app.nb.GetCurrentPage()
			app.Update(privacyKey(" "))
			cmd := templateWorkflowStart(t, app)
			chooser := app.templateChooser
			for range 2 {
				if _, next := app.Update(tea.KeyMsg{Type: tea.KeyEsc}); next != nil {
					t.Fatal("task cancellation returned an exit command")
				}
			}
			app.Update(tea.KeyMsg{Type: tea.KeyEnter})
			app.Update(privacyKey(" "))
			app.Update(tea.KeyMsg{Type: tea.KeyDown})
			if app.templateChooser != chooser || !chooser.applying || !chooser.cancelRequested || !chooser.selected["one"] || chooser.cursor != 0 || app.IsQuitting() {
				t.Fatal("cancellation did not retain and freeze the pending workflow")
			}
			if view := app.View(); !strings.Contains(view, "Cancellation requested") || !strings.Contains(view, "waiting for outcome") {
				t.Fatal("pending cancellation is not visible")
			}
			if _, next := app.Update(cmd()); next != nil || chooser.applying || chooser.cancel != nil || app.templateChooser != chooser {
				t.Fatal("cancelled completion lost retry context or launched an editor")
			}
			if !strings.Contains(chooser.feedback, "cancelled before publication") || !chooser.selected["one"] || app.templateApplied(chooser.date, "one") {
				t.Fatal("prepublication cancellation outcome was misreported")
			}
			app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if app.templateChooser != nil || app.IsQuitting() || app.Mode() != mode || app.cal.CursorDate() != date || app.nb.GetCurrentPage() != page {
				t.Fatal("cancelling templates changed the surrounding app")
			}
		})
	}
}

func TestTemplateWorkflowPublishedCancellationAndWarnings(t *testing.T) {
	for _, mode := range []AppMode{ModeCalendar, ModeNotebook} {
		for _, private := range []bool{false, true} {
			for _, cancel := range []bool{false, true} {
				for _, applyErr := range []error{nil, scratchpad.ErrDurabilityUnconfirmed, context.Canceled, errors.New("synthetic post-publication error")} {
					if !cancel && applyErr == nil {
						continue // Normal editor launch is covered separately.
					}
					t.Run(fmt.Sprintf("mode=%d/private=%t/cancel=%t/err=%v", mode, private, cancel, applyErr), func(t *testing.T) {
						const content = "PRIVATE_SYNTHETIC_TEMPLATE"
						app := templateWorkflowApp(t, mode, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
							return TemplateApplyResult{Content: content, Applied: []string{"one"}, Published: true}, applyErr
						})
						date := app.templateChooser.date
						page, cursor := app.nb.GetCurrentPage(), app.cal.CursorDate()
						if private {
							app.SetPrivacy(PrivacyHooks{
								Inspect: func(string) (PublicPage, error) { return PublicPage{Shared: "SHARED", HasPrivate: true}, nil },
								// Deliberately omit template metadata from the reload: the
								// authoritative completion must not lose its applied IDs.
								Load: func(string) (PrivatePage, error) {
									return PrivatePage{Private: content, Shared: "SHARED", HasPrivate: true}, nil
								},
							})
						}
						app.Update(privacyKey(" "))
						cmd := templateWorkflowStart(t, app)
						if cancel {
							app.Update(tea.KeyMsg{Type: tea.KeyEsc})
						}
						if _, next := app.Update(cmd()); next != nil {
							t.Fatal("cancelled/warning outcome launched an editor or expiration command")
						}
						if !app.cal.HasData(date) || !app.templateApplied(date, "one") || app.nb.GetCurrentPage() != page || app.cal.CursorDate() != cursor || app.IsQuitting() {
							t.Fatal("published outcome lost cross-view state or changed focus")
						}
						if app.privacy != nil && (app.privacy.editor != nil || app.privacy.prompt != nil) {
							t.Fatal("cancellation/warning started private editing or unlock")
						}
						status := templateWorkflowStatus(app)
						if !status.statusExpiry.IsZero() {
							t.Fatal("publication/cancellation warning expires")
						}
						if cancel && !strings.Contains(status.StatusText(), "Cancellation after commit") {
							t.Fatal("cancellation after commit was reported as a rollback")
						}
						if errors.Is(applyErr, scratchpad.ErrDurabilityUnconfirmed) && !strings.Contains(status.StatusText(), "durability is unconfirmed") {
							t.Fatal("durability uncertainty is hidden")
						}
						if applyErr != nil {
							if app.templateChooser == nil || !app.templateChooser.selected["one"] || !strings.Contains(app.templateChooser.feedback, "Enter appends another copy") {
								t.Fatal("published warning lost selection or invites an unsafe blind retry")
							}
						} else if app.templateChooser != nil {
							t.Fatal("successful cancellation outcome did not return to the surrounding app")
						}
						if private {
							for _, cache := range []map[string]string{app.cal.contents, app.cal.previews, app.nb.contents, app.nb.sharedContents} {
								for _, text := range cache {
									if strings.Contains(text, content) {
										t.Fatal("private template entered a shared/calendar cache")
									}
								}
							}
						} else if app.cal.contents[date] != content || app.nb.contents[date] != content {
							t.Fatal("published plaintext was not projected to both views")
						}
					})
				}
			}
		}
	}
}

func TestTemplateWorkflowEditorCancellationDoesNotUndoSavedInsertion(t *testing.T) {
	const content = "PRIVATE_SYNTHETIC_TEMPLATE"
	persisted, saves := "", 0
	app := templateWorkflowApp(t, ModeNotebook, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		persisted = content
		return TemplateApplyResult{Content: persisted, Applied: []string{"one"}}, nil
	})
	date := app.templateChooser.date
	app.SetPrivacy(PrivacyHooks{
		Inspect: func(string) (PublicPage, error) { return PublicPage{HasPrivate: persisted != ""}, nil },
		Load: func(string) (PrivatePage, error) {
			return PrivatePage{Private: persisted, Applied: []string{"one"}}, nil
		},
		Save: func(_, _, _ string) error { saves++; return nil },
	})
	app.Update(privacyKey(" "))
	cmd := templateWorkflowStart(t, app)
	app.Update(cmd()) // Do not execute the editor's blink command.
	if app.templateChooser != nil || app.privacy.editor == nil || app.privacy.editor.input.Value() != content || persisted != content {
		t.Fatal("editor did not start from already-persisted template content")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.privacy.editor != nil || persisted != content || saves != 0 || !app.templateApplied(date, "one") {
		t.Fatal("editor cancellation rolled back or rewrote the insertion")
	}
	if strings.Contains(app.nb.contents[date], content) || strings.Contains(app.cal.contents[date], content) {
		t.Fatal("private insertion leaked on editor cancellation")
	}
}

func TestTemplateWorkflowStaleResultsCannotMutateNewChooserOrRetry(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprintf("reopen=%t", reopen), func(t *testing.T) {
			calls := 0
			app := templateWorkflowApp(t, ModeCalendar, func(ctx context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
				calls++
				if ctx.Err() != nil {
					t.Fatal("stale result cancelled the newer attempt")
				}
				if calls == 1 {
					return TemplateApplyResult{}, errors.New("synthetic first failure")
				}
				return TemplateApplyResult{Content: "current result", Applied: []string{"one"}}, nil
			})
			app.Update(privacyKey(" "))
			old := templateWorkflowStart(t, app)().(templateAppliedMsg)
			app.Update(old)
			if reopen {
				app.Update(tea.KeyMsg{Type: tea.KeyEsc})
				app.Update(privacyKey("a"))
				app.Update(privacyKey(" "))
			}
			cmd := templateWorkflowStart(t, app)
			chooser, attempt := app.templateChooser, app.templateChooser.attempt
			// A delayed duplicate with the old identity must be harmless even if
			// it reports publication, and even for the exact same date.
			old.err = nil
			old.result = TemplateApplyResult{Content: "STALE", Applied: []string{"two"}, Published: true}
			app.Update(old)
			app.Update(templateAppliedMsg{date: chooser.date, result: old.result}) // Legacy, unidentifiable result.
			if app.templateChooser != chooser || !chooser.applying || chooser.attempt != attempt || app.templateApplied(chooser.date, "two") || app.cal.contents[chooser.date] == "STALE" {
				t.Fatal("stale completion mutated a newer chooser or its saved state")
			}
			current := cmd()
			app.Update(current)
			app.Update(old)
			app.Update(current) // Completed attempts cannot be replayed either.
			if app.templateChooser != nil || app.cal.contents[chooser.date] != "current result" || app.nb.contents[chooser.date] != "current result" || app.templateApplied(chooser.date, "two") {
				t.Fatal("late completion mutated state after the current chooser closed")
			}
		})
	}
}

func TestTemplateWorkflowPrivateLoadFailureNeverFallsBackToPlaintext(t *testing.T) {
	for _, missingLoader := range []bool{false, true} {
		t.Run(fmt.Sprintf("missingLoader=%t", missingLoader), func(t *testing.T) {
			const content = "PRIVATE_SYNTHETIC_TEMPLATE"
			app := templateWorkflowApp(t, ModeCalendar, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
				return TemplateApplyResult{Content: content, Applied: []string{"one"}}, nil
			})
			hooks := PrivacyHooks{}
			if !missingLoader {
				hooks.Load = func(string) (PrivatePage, error) {
					return PrivatePage{}, errors.New("synthetic encrypted load failure")
				}
			}
			app.SetPrivacy(hooks)
			app.Update(privacyKey(" "))
			cmd := templateWorkflowStart(t, app)
			if _, next := app.Update(cmd()); next != nil {
				t.Fatal("failed/missing private loader launched a fallback editor")
			}
			if app.privacy.editor != nil || app.cal.editor != nil || app.nb.editor != nil || app.cal.loader != nil || app.nb.loader != nil {
				t.Fatal("a plaintext editing path remained available")
			}
			for _, cache := range []map[string]string{app.cal.contents, app.cal.previews, app.nb.contents, app.nb.sharedContents} {
				for _, text := range cache {
					if strings.Contains(text, content) {
						t.Fatal("private load failure leaked template text into shared caches")
					}
				}
			}
		})
	}
}

func TestTemplateWorkflowCancelledAttemptCanRetryAndEdit(t *testing.T) {
	for _, startFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("start-before-cancel=%t", startFirst), func(t *testing.T) {
			calls := 0
			allowStart := make(chan struct{})
			started := make(chan error, 1)
			app := templateWorkflowApp(t, ModeNotebook, func(ctx context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
				calls++
				if calls == 1 {
					// Force both legal orderings. Accepted work can enter before
					// Escape; it must await cancellation, not assume it on entry.
					select {
					case <-allowStart:
					case <-ctx.Done(): // Also let cleanup join a failed test.
					}
					started <- ctx.Err()
					<-ctx.Done()
					return TemplateApplyResult{}, ctx.Err()
				}
				if ctx.Err() != nil {
					t.Error("retry reused the cancelled context")
					return TemplateApplyResult{}, ctx.Err()
				}
				return TemplateApplyResult{Applied: []string{"one"}}, nil
			})
			app.SetPrivacy(PrivacyHooks{Load: func(string) (PrivatePage, error) {
				return PrivatePage{Private: "synthetic persisted template", Applied: []string{"one"}}, nil
			}})
			app.Update(privacyKey(" "))
			first := templateWorkflowStart(t, app)
			awaitStart := func(wantCanceled bool) {
				select {
				case err := <-started:
					if errors.Is(err, context.Canceled) != wantCanceled {
						t.Fatalf("entry context = %v, want canceled=%t", err, wantCanceled)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("first applier did not start")
				}
			}
			if startFirst {
				close(allowStart)
				awaitStart(false)
			}
			app.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if !startFirst {
				close(allowStart)
				awaitStart(true)
			}
			app.Update(first())
			second := templateWorkflowStart(t, app)
			if app.templateChooser.cancelRequested {
				t.Fatal("retry retained the previous cancellation request")
			}
			app.Update(second())
			if calls != 2 || app.templateChooser != nil || app.privacy.editor == nil {
				t.Fatal("successful explicit retry was incorrectly prevented from editing")
			}
		})
	}
}

func TestTemplateWorkflowLegacyStandaloneResultRemainsSupported(t *testing.T) {
	app := newTestApp(ModeCalendar)
	t.Cleanup(app.Close)
	date := app.cal.CursorDate()
	app.finishTemplateApply(templateAppliedMsg{date: date, result: TemplateApplyResult{Content: "legacy", Applied: []string{"one"}}})
	if app.cal.contents[date] != "legacy" || app.nb.contents[date] != "legacy" || !app.templateApplied(date, "one") {
		t.Fatal("untagged standalone result lost backwards compatibility")
	}
}

func TestTemplateWorkflowLongErrorAndResizeRetainDetails(t *testing.T) {
	diagnostic := strings.Repeat("synthetic diagnostic ", 80) + "END_OF_ERROR"
	app := templateWorkflowApp(t, ModeCalendar, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		return TemplateApplyResult{}, errors.New(diagnostic)
	})
	app.Update(privacyKey(" "))
	app.Update(templateWorkflowStart(t, app)())
	chooser := app.templateChooser
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 12}, {Width: 30, Height: 10}, {Width: 0, Height: 0}, {Width: 120, Height: 40}} {
		app.Update(size)
		assertFrameFits(t, app.View(), size)
		if !chooser.selected["one"] || !strings.Contains(chooser.feedback, "END_OF_ERROR") {
			t.Fatal("resize lost selection or diagnostic")
		}
	}
	if !strings.Contains(app.View(), "F1: full details") {
		t.Fatal("truncated diagnostic has no route to full details")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyF1})
	if !strings.Contains(chooser.keyHelp.content, diagnostic) {
		t.Fatal("full diagnostic was not retained in scrollable help")
	}
}
