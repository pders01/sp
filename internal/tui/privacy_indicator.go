package tui

func (a *App) installPrivacyIndicators() {
	p := a.privacy
	state := func() string {
		if p.job != nil {
			return "unlocking"
		}
		if p.closed {
			return "locked"
		}
		if p.hooks.IsUnlocked == nil {
			return "private"
		}
		if p.hooks.IsUnlocked() {
			return "unlocked"
		}
		return "locked"
	}
	a.cal.privacyState, a.nb.privacyState = state, state
	a.cal.canRelock, a.nb.canRelock = p.hooks.Lock != nil, p.hooks.Lock != nil
}

func privacyBadge(enabled bool, state func() string) string {
	if !enabled || state == nil {
		return ""
	}
	return " · encrypted · " + state()
}

func privacyIsUnlocked(state func() string) bool { return state != nil && state() == "unlocked" }
