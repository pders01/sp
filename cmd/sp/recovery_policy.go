package main

import (
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
)

func makePolicySaver(mgr *scratchpad.Manager, encrypted bool) tui.Saver {
	return func(date, original, content string) error {
		return mgr.SaveContentWithPrivacyPolicy(date, original, content, encrypted)
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
