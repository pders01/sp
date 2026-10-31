package main

import (
	"fmt"

	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
)

func makePolicySaver(mgr *scratchpad.Manager, encrypted bool) tui.Saver {
	return func(date, original, content string) error {
		actual, err := mgr.PrivacyEnabled()
		if err != nil {
			return err
		}
		if actual != encrypted {
			return scratchpad.ErrPrivacyPolicyChanged
		}
		note, err := mgr.GetByDate(date)
		if err != nil {
			return err
		}
		if note.Content != original {
			return fmt.Errorf("%w: %s; reload before retrying", scratchpad.ErrConflict, date)
		}
		note.Content = content
		return mgr.SaveWithPrivacyPolicy(note, encrypted)
	}
}

func recoverySavePolicy(mgr *scratchpad.Manager) (string, error) {
	enabled, err := mgr.PrivacyEnabled()
	if err != nil {
		return "", err
	}
	if enabled {
		return "Saving encrypted", nil
	}
	return "Saving as plaintext", nil
}
