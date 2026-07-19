package tui

import "strings"

type helpEntry struct {
	keys    string
	label   string
	visible bool
}

func renderHelp(entries []helpEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.visible {
			continue
		}
		parts = append(parts, entry.keys+": "+entry.label)
	}
	return strings.Join(parts, " • ")
}
