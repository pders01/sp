package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/scratchpad"
)

func TestPrivateEditorSignalQuitRecoversBeforeExit(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			calls := 0
			e := newPrivateEditor("2026-02-14", "saved", nil, func(date, text string) (string, error) {
				calls++
				if date != "2026-02-14" || text != "unsaved draft" {
					t.Errorf("recovery changed the draft")
				}
				return "synthetic-recovery.age", nil
			})
			if changed {
				e.input.SetValue("unsaved draft")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			p := tea.NewProgram(e, tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithFilter(privateEditorSignalFilter))
			go p.Send(tea.QuitMsg{}) // Exactly the graceful SIGINT/SIGTERM message.
			_, err := p.Run()
			if err != nil {
				t.Fatal(err)
			}
			if !e.done || !errors.Is(e.finishRun(nil), ErrPrivateEditorInterrupted) {
				t.Fatal("signal reported successful completion")
			}
			wantCalls := 0
			if changed {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("recovery calls=%d want=%d", calls, wantCalls)
			}
			if changed && (len(e.recoveries) != 1 || !strings.Contains(e.err.Error(), "synthetic-recovery.age")) {
				t.Fatal("recovery artifact/path missing")
			}
		})
	}
}

func TestPrivateEditorFailedSignalRecoveryRetainsUsableDraft(t *testing.T) {
	for _, layer := range []string{"draft", "help", "find", "review"} {
		t.Run(layer, func(t *testing.T) {
			fail := true
			calls := 0
			e := newPrivateEditor("2026-02-14", "saved", nil, func(_, text string) (string, error) {
				calls++
				if text != "exact\r\ndraft\t\ufffd\n" {
					t.Error("changed recovery bytes")
				}
				if fail {
					return "partial.age", scratchpad.ErrDurabilityUnconfirmed
				}
				return "retained.age", nil
			})
			e.input.SetValue("exact\r\ndraft\t\ufffd\n")
			switch layer {
			case "help":
				e.keyHelp = newKeyHelp("Write", nil, "", 80, 24)
			case "find":
				e.input.findActive = true
			case "review":
				e.review = newDraftReview(e.date, "new saved", e.input.Value(), 80, 24)
			}
			_, cmd := e.Update(privateEditorInterruptedMsg{})
			if e.done || cmd != nil || e.input.Value() != "exact\r\ndraft\t\ufffd\n" || e.original != "saved" || !strings.Contains(e.status, "keep it open") {
				t.Fatal("failed signal recovery discarded/closed/changed the draft")
			}
			if e.keyHelp != nil || e.review != nil || e.input.Finding() {
				t.Fatal("shutdown failure remained hidden behind a layer")
			}
			fail = false
			_, cmd = e.Update(privateEditorInterruptedMsg{})
			if !e.done || cmd == nil || calls != 2 || !errors.Is(e.err, ErrPrivateEditorInterrupted) {
				t.Fatal("signal recovery could not be retried")
			}
		})
	}
}

func TestPrivateEditorUnfinishedNilExitIsNotSuccessful(t *testing.T) {
	calls := 0
	e := newPrivateEditor("2026-02-14", "saved", nil, func(_, text string) (string, error) { calls++; return "retained.age", nil })
	e.input.SetValue("new draft")
	if err := e.finishRun(nil); !errors.Is(err, ErrPrivateEditorInterrupted) || !strings.Contains(err.Error(), "retained.age") || calls != 1 {
		t.Fatalf("unfinished exit: calls=%d err=%v", calls, err)
	}
}

func TestPrivateEditorCompletedSaveDoesNotRecoverAgain(t *testing.T) {
	calls := 0
	e := newPrivateEditor("2026-02-14", "saved", func(_, _, _ string) error { return nil }, func(_, _ string) (string, error) { calls++; return "unexpected.age", nil })
	e.input.SetValue("saved draft")
	_, cmd := e.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil || !e.done || e.finishRun(nil) != nil || calls != 0 {
		t.Fatal("normal save became an interruption/recovery")
	}
	if _, ok := privateEditorSignalFilter(e, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Fatal("completed editor exit was intercepted")
	}
}
