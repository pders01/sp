package tui

import (
	"strings"
	"testing"
)

func TestRecoveryPolicyCaptionSurvivesStatusChanges(t *testing.T) {
	model := recoveredPrivateEditor("2026-01-01", "saved", "recovered", nil, nil, nil, "Saving as plaintext")
	model.status = "unrelated status"
	if !strings.Contains(model.View(), "Saving as plaintext") || model.input.Value() != "recovered" || model.input.pos != 0 {
		t.Fatal("recovery policy or exact draft/orientation was hidden")
	}
	if strings.Contains(model.input.Value(), "Saving as plaintext") {
		t.Fatal("policy became note text")
	}
	for _, size := range layoutSizes {
		model.Update(size)
		assertFrameFits(t, model.View(), size)
	}
}
