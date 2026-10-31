//go:build !windows

package editor

import (
	"errors"
	"strings"
)

// splitEditorCommand recognizes quoting/escaping, not shell syntax or expansion.
// It preserves empty quoted arguments and concatenated quoted/unquoted segments.
func splitEditorCommand(command string) ([]string, error) {
	if strings.IndexByte(command, 0) >= 0 {
		return nil, errors.New("editor command contains NUL")
	}
	var args []string
	var word strings.Builder
	quote := byte(0)
	started := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if quote == '"' {
			switch c {
			case '"':
				quote = 0
			case '\\':
				if i+1 < len(command) && strings.ContainsRune("\"\\$`\n", rune(command[i+1])) {
					i++
					if command[i] != '\n' {
						word.WriteByte(command[i])
					}
				} else {
					word.WriteByte(c)
				}
			default:
				word.WriteByte(c)
			}
			continue
		}
		switch c {
		case ' ', '\t', '\r', '\n':
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		case '\'', '"':
			quote, started = c, true
		case '\\':
			if i+1 == len(command) {
				return nil, errors.New("trailing backslash in editor command")
			}
			i++
			if command[i] != '\n' {
				word.WriteByte(command[i])
				started = true
			}
		default:
			word.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in editor command")
	}
	if started {
		args = append(args, word.String())
	}
	return args, nil
}
