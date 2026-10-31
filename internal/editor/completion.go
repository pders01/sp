package editor

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// editorBaseName recognizes native paths and Windows launcher names, even in
// platform-independent policy tests. This does not resolve symlinks or wrappers.
func editorBaseName(command string) string {
	name := strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/")))
	return strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".cmd"), ".bat")
}

type guiCompletionPolicy struct {
	flag    string
	aliases []string
}

// A recognized GUI launcher with no flag is deliberately unsupported: process
// exit cannot reliably mean editing has finished (e.g. modern Notepad handoff).
func guiPolicy(command string) (guiCompletionPolicy, bool) {
	switch editorBaseName(command) {
	case "code", "code-insiders", "code - insiders", "code-oss", "codium", "vscodium", "cursor", "windsurf", "subl", "sublime_text", "atom", "gedit":
		return guiCompletionPolicy{"--wait", []string{"-w"}}, true
	case "kate":
		return guiCompletionPolicy{"--block", []string{"-b"}}, true
	case "gvim", "mvim":
		return guiCompletionPolicy{"--nofork", []string{"-f"}}, true
	case "notepad++":
		// A separate process, not a request handed off to an existing instance.
		return guiCompletionPolicy{flag: "-multiInst"}, true
	case "notepad", "textedit", "open", "gnome-text-editor", "electron":
		return guiCompletionPolicy{}, true
	default:
		return guiCompletionPolicy{}, false
	}
}

func isGUIEditor(command string) bool {
	_, known := guiPolicy(command)
	return known
}

// editorCompletionArgs leaves supplied options intact and adds the launcher's
// supported wait/foreground option before any end-of-options marker. Unknown
// commands are trusted to honor the documented foreground-process contract.
func editorCompletionArgs(command string, supplied []string) ([]string, error) {
	args := slices.Clone(supplied)
	policy, known := guiPolicy(command)
	if !known {
		return args, nil
	}
	if policy.flag == "" {
		return nil, fmt.Errorf("editor launcher %q has no supported process-finished contract; configure EDITOR (or VISUAL) to a blocking editor such as 'code --wait' or 'gvim --nofork' instead of a detached launcher", command)
	}
	end := slices.Index(args, "--")
	if end < 0 {
		end = len(args)
	}
	name := editorBaseName(command)
	rawBase := strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/")))
	// Electron GUI executables do not implement the CLI wrapper's wait-marker
	// lifecycle. A --wait flag alone is not proof that a GUI handoff blocks.
	if strings.HasSuffix(rawBase, ".exe") {
		switch name {
		case "code", "code-insiders", "code - insiders", "code-oss", "codium", "vscodium", "cursor", "windsurf", "atom", "sublime_text":
			return nil, fmt.Errorf("GUI executable %q can hand off before editing finishes; configure EDITOR (or VISUAL) to a blocking CLI launcher or terminal editor, not the direct GUI executable", command)
		}
	}
	for _, arg := range args[:end] {
		if (name == "gvim" || name == "mvim") && strings.HasPrefix(arg, "--remote") && !strings.HasPrefix(arg, "--remote-wait") && !strings.HasPrefix(arg, "--remote-tab-wait") {
			return nil, fmt.Errorf("editor launcher %q option %q can return before editing finishes; configure EDITOR (or VISUAL) without remote options or use --remote-wait", command, arg)
		}
	}
	for _, arg := range args[:end] {
		if arg == policy.flag || slices.Contains(policy.aliases, arg) {
			return args, nil
		}
	}
	return slices.Insert(args, end, policy.flag), nil
}
