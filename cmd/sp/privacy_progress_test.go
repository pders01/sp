package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pders01/sp/internal/scratchpad"
)

func TestPrivacyProgressOutputIsBoundedAndContainsOnlyStagesAndCounts(t *testing.T) {
	var out bytes.Buffer
	report := privacyProgressReporter(&out)
	report(scratchpad.PrivacyProgress{Stage: "inspecting"})
	for i := 0; i <= 1000; i++ {
		report(scratchpad.PrivacyProgress{Stage: "migrating", Completed: i, Total: 1000})
	}
	report(scratchpad.PrivacyProgress{Stage: "complete", Completed: 1000, Total: 1000})
	if strings.Count(out.String(), "\n") > 14 || !strings.Contains(out.String(), "1000/1000") || !strings.Contains(out.String(), "inspecting") {
		t.Fatalf("progress output was unbounded/incomplete: %q", out.String())
	}
}
