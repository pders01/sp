package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTemplateWorkflowLiteralKeyNamesCannotPublish(t *testing.T) {
	app := workflowApp(t, ModeCalendar, map[string]string{"2026-01-01": "saved"})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(context.Context, string, []TemplateSelection) (TemplateApplyResult, error) {
		t.Fatal("literal key-name text started work")
		return TemplateApplyResult{}, nil
	})
	app.Update(privacyKey("a"))
	app.Update(tea.KeyMsg{Type: tea.KeySpace})
	for _, text := range []string{"enter", "esc", "ctrl+c"} {
		app.Update(privacyKey(text))
	}
	if app.templateChooser == nil || app.templateChooser.applying || app.IsQuitting() {
		t.Fatal("literal text published/cancelled/quit")
	}
}

func TestTemplateWorkflowCloseOwnsUnstartedCompletionAndReportsPublication(t *testing.T) {
	app := workflowApp(t, ModeCalendar, map[string]string{"2026-01-01": "saved"})
	entered := make(chan struct{})
	app.SetTemplates([]DayTemplate{{ID: "one", Name: "One"}}, nil, func(ctx context.Context, _ string, _ []TemplateSelection) (TemplateApplyResult, error) {
		close(entered)
		<-ctx.Done()
		return TemplateApplyResult{Content: "PRIVATE_CANARY", Published: true}, ctx.Err()
	})
	app.Update(privacyKey("a"))
	app.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no completion command")
	}
	<-entered // Accepted work runs even if cmd is never invoked.
	attempt := app.templateChooser.attempt
	app.Close()
	select {
	case <-attempt.done:
	default:
		t.Fatal("Close returned while work was active")
	}
	var report bytes.Buffer
	app.ReportRecoveries(&report)
	if !strings.Contains(report.String(), "published before shutdown") || strings.Contains(report.String(), "PRIVATE_CANARY") {
		t.Fatalf("unsafe/missing publication report: %q", report.String())
	}
}
