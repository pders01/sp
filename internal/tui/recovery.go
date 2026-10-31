package tui

import (
	"fmt"
	"io"

	"github.com/pders01/sp/internal/scratchpad"
)

// WriteRecoveryReport is called after the alternate screen is restored. Paths
// are quoted (including controls); private text and passphrases never appear.
func WriteRecoveryReport(out io.Writer, refs []scratchpad.RecoveryReference) {
	if len(refs) == 0 {
		return
	}
	fmt.Fprintln(out, "Retained recovery artifacts (may be earlier drafts; not permanent backups):")
	for _, ref := range refs {
		kind := "temporary editor buffer"
		if ref.Encrypted {
			kind = "encrypted recovery"
		}
		fmt.Fprintf(out, "  %s %s: %q\n", ref.Date, kind, ref.Path)
	}
	fmt.Fprintln(out, "Discover recorded paths later: sp privacy recover --list")
	fmt.Fprintln(out, "Review before saving; no automatic merge. Temporary buffers may be removed by the OS.")
}

// SetRecoveryRecorder persists only legacy buffer discovery metadata. Encrypted
// recovery is already discoverable inside the store, without an extra index.
func (a *App) SetRecoveryRecorder(record, forget func(date, path string) error) {
	a.recordRecovery, a.forgetRecovery = record, forget
}

func (a *App) trackRecovery(ref scratchpad.RecoveryReference) {
	for _, existing := range a.recoveries {
		if existing == ref {
			return
		}
	}
	a.recoveries = append(a.recoveries, ref)
	if !ref.Encrypted && a.recordRecovery != nil {
		if err := a.recordRecovery(ref.Date, ref.Path); err != nil {
			a.recoveryWarnings = append(a.recoveryWarnings, fmt.Sprintf("could not index retained buffer %q: %v; keep the reported path", ref.Path, err))
		}
	}
}

func (a *App) consumedRecovery(date, path string) {
	found := false
	for i := len(a.recoveries) - 1; i >= 0; i-- {
		if a.recoveries[i].Date == date && a.recoveries[i].Path == path && !a.recoveries[i].Encrypted {
			found = true
			a.recoveries = append(a.recoveries[:i], a.recoveries[i+1:]...)
		}
	}
	if found && a.forgetRecovery != nil {
		if err := a.forgetRecovery(date, path); err != nil {
			a.recoveryWarnings = append(a.recoveryWarnings, fmt.Sprintf("could not remove stale buffer reference %q: %v", path, err))
		}
	}
}

// ReportRecoveries never depends on the current status banner or active view.
func (a *App) ReportRecoveries(out io.Writer) {
	WriteRecoveryReport(out, a.recoveries)
	for _, warning := range a.recoveryWarnings {
		fmt.Fprintln(out, "sp:", warning)
	}
}
