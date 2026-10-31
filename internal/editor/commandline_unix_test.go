//go:build !windows

package editor

import (
	"reflect"
	"strings"
	"testing"
)

func editorTestCommand(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, "'"+strings.ReplaceAll(part, "'", "'\\''")+"'")
	}
	return strings.Join(quoted, " ")
}

func TestSplitEditorCommandPOSIXQuoting(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		want          []string
	}{
		{"simple", "nvim", []string{"nvim"}},
		{"spaces", `nvim -u 'config dir/init.vim' -c "set number"`, []string{"nvim", "-u", "config dir/init.vim", "-c", "set number"}},
		{"executable", `"/editor dir/nvim" --clean`, []string{"/editor dir/nvim", "--clean"}},
		{"escaped spaces", `editor\ path --arg=some\ value`, []string{"editor path", "--arg=some value"}},
		{"empty args", `nvim "" ''`, []string{"nvim", "", ""}},
		{"segments", `nvim pre"middle word"post`, []string{"nvim", "premiddle wordpost"}},
		{"escapes", `nvim "a\"b" "c\\d" "a\qb"`, []string{"nvim", `a"b`, `c\d`, `a\qb`}},
		{"unicode", `"笔记 editor" -u "🙂 config/init.vim"`, []string{"笔记 editor", "-u", "🙂 config/init.vim"}},
		{"literal syntax", "nvim '$HOME' '$(touch marker)' '`touch marker`' '*.md' '|' ';' '&&'", []string{"nvim", "$HOME", "$(touch marker)", "`touch marker`", "*.md", "|", ";", "&&"}},
		{"line continuation", "nvim \\\n--clean", []string{"nvim", "--clean"}},
		{"quoted continuation", "nvim \"a\\\nb\"", []string{"nvim", "ab"}},
		{"whitespace", " \t\r\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitEditorCommand(tc.command)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("split(%q) = %q, %v; want %q", tc.command, got, err, tc.want)
			}
		})
	}
}

func TestSplitEditorCommandRejectsMalformedPOSIXInput(t *testing.T) {
	for _, command := range []string{`nvim 'unfinished`, `nvim "unfinished`, "nvim trailing\\", "nvim \"trailing\\", "nvim \x00", "nvim 'bad\x00arg'"} {
		if _, err := splitEditorCommand(command); err == nil {
			t.Fatalf("accepted malformed command %q", command)
		}
	}
}
