package editorterminal

import (
	"context"
	"errors"
	"testing"

	"github.com/pders01/sp/internal/editorcore"
	"github.com/pders01/sp/internal/editorsandbox"
)

type forbiddenDriver struct{ called *bool }

func (forbiddenDriver) ID() string { return "forbidden-test" }
func (d forbiddenDriver) OpenProtected(context.Context, editorcore.Options, string) (editorcore.UI, error) {
	*d.called = true
	return nil, editorcore.ErrUnsupported
}

func TestProtectedTerminalRejectsBeforeTTYManagerOrEitherFactory(t *testing.T) {
	called := false
	options := Options{Driver: forbiddenDriver{called: &called}, InMemory: func(context.Context, editorcore.Options, string) (editorcore.UI, error) {
		called = true
		return nil, editorcore.ErrUnsupported
	}, Experiment: func(context.Context, editorcore.Options, string) (editorcore.UI, error) {
		called = true
		return nil, editorcore.ErrUnsupported
	}}
	report, err := Run(context.Background(), options)
	if !errors.Is(err, editorsandbox.ErrUnavailable) || called || report.Editor != nil || report.PendingInput || !report.Outcome.Closed {
		t.Fatal("protected terminal touched a driver/TTY or used an unconfined fallback")
	}
}

func TestExperimentMustBeExplicitAndCannotSelectProtectedDriver(t *testing.T) {
	called := false
	if _, err := RunExperiment(context.Background(), Options{Driver: forbiddenDriver{called: &called}}); !errors.Is(err, ErrRetained) || called {
		t.Fatal("experiment implicitly selected another driver or mode")
	}
}
