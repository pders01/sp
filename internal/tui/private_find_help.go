package tui

func privateFindBindings() []helpEntry {
	return []helpEntry{
		{keys: "Enter", label: "accept find and return to draft", visible: true},
		{keys: "Esc", label: "close find (draft unchanged)", visible: true},
		{keys: "F3", label: "next match in this draft", visible: true},
		{keys: "Ctrl+U", label: "clear query", visible: true},
		{keys: "F1", label: "help", visible: true},
	}
}
