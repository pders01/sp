package main

import (
	"fmt"
	"io"

	"github.com/pders01/sp/internal/scratchpad"
)

// Bound terminal output for large archives: stage transitions, each hundred
// dates, and the final count. No date names, keys, passphrases or content.
func privacyProgressReporter(out io.Writer) func(scratchpad.PrivacyProgress) {
	lastStage, lastCount := "", -1
	return func(p scratchpad.PrivacyProgress) {
		if p.Stage == lastStage && (p.Completed == lastCount || p.Completed != p.Total && p.Completed%100 != 0) {
			return
		}
		lastStage, lastCount = p.Stage, p.Completed
		if p.Total > 0 {
			fmt.Fprintf(out, "Privacy %s: %d/%d dates\n", p.Stage, p.Completed, p.Total)
		} else {
			fmt.Fprintf(out, "Privacy %s…\n", p.Stage)
		}
	}
}
