package tui

func privacyBadge(enabled bool, state func() string) string {
	if !enabled || state == nil {
		return ""
	}
	return " · " + state()
}

func privacyIsUnlocked(state func() string) bool { return state != nil && state() == "unlocked" }
