package tui

func recoveredPrivateEditor(date, current, recovered string, save Saver, sealRecovery func(string, string) (string, error), load func(string) (string, error), policy string) *privateEditor {
	model := newPrivateEditor(date, current, save, sealRecovery)
	model.load, model.savePolicy = load, policy
	model.input.OpenValue(recovered)
	model.status = "Recovery: saving replaces current private Markdown; shared contributions and the recovery artifact are retained."
	return model
}

// EditRecoveredPrivateWithPolicy keeps an explicit save-policy caption visible
// independently of transient errors: encrypted artifacts can be reviewed after
// disabling privacy, when the resulting daily note will be plaintext.
func EditRecoveredPrivateWithPolicy(date, current, recovered string, save Saver, sealRecovery func(string, string) (string, error), load func(string) (string, error), policy string, preference ...string) error {
	model := recoveredPrivateEditor(date, current, recovered, save, sealRecovery, load, policy)
	if len(preference) > 0 {
		model.theme.SetPref(preference[0])
	}
	return runPrivateEditor(model)
}
