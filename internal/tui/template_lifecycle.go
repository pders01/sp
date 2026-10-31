package tui

import "fmt"

// Cancellation does not roll back publication. Wait for owned work before
// dropping private keys or exiting, including an unstarted completion command.
func (a *App) closeTemplateWork() {
	chooser := a.templateChooser
	if chooser == nil || !chooser.applying {
		return
	}
	if chooser.cancel != nil {
		chooser.cancel()
	}
	if chooser.attempt == nil || chooser.attempt.done == nil {
		return
	}
	<-chooser.attempt.done
	result := chooser.attempt.result
	if result.err == nil || result.result.Published {
		if a.recoveryWarnings == nil {
			a.recoveryWarnings = make(map[string]string)
		}
		a.recoveryWarnings["template-outcome"] = fmt.Sprintf("Templates for %s were published before shutdown; cancellation did not undo insertion. Review the saved note.", chooser.date)
	}
	chooser.applying = false
}
