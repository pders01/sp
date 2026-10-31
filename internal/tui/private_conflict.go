package tui

func (e *privateEditor) openConflictReview() {
	if e.load == nil {
		e.status = "Conflict review is unavailable; keep the draft or leave with encrypted recovery, then reopen the saved note."
		return
	}
	current, err := e.load(e.date)
	if err != nil {
		e.status = "Cannot load current note for review: " + err.Error() + "; draft retained in this editor."
		return
	}
	probe := newPrivateInput()
	probe.SetValue(current)
	if probe.err != "" {
		e.status = "Cannot reload current note: " + probe.err + " Draft retained in this editor."
		return
	}
	e.review = newDraftReview(e.date, current, e.input.Value(), e.width, e.height)
}
