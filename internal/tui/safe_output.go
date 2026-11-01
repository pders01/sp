package tui

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func unsafeDisplayRune(r rune) bool {
	return unicode.IsControl(r) || r == '\u061c' || r == '\u200e' || r == '\u200f' || r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069'
}

// safeMarkdownDisplay projects untrusted plain text before application styling.
// It never changes the stored document or editor buffer.
func safeMarkdownDisplay(text string) string {
	var out strings.Builder
	for _, r := range text {
		if unsafeDisplayRune(r) && r != '\n' && r != '\r' && r != '\t' {
			fmt.Fprintf(&out, "\\u{%X}", r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Match references html.UnescapeString accepts, including numeric references
// without semicolons, plus bare/unknown ampersands. Ordinary entities stay
// encoded so they cannot become Markdown syntax (e.g. &lt; or &ast;) before
// parsing. All other ampersands are deferred, including ones that HTML removal
// could otherwise join to a new entity.
var displayEntity = regexp.MustCompile(`&(?:#[xX][0-9a-fA-F]+;?|#[0-9]+;?|[A-Za-z][A-Za-z0-9]*;?)?`)

// markdownDisplaySource neutralizes entity-generated controls *including SGR*
// before Glamour can mistake them for renderer styling. Untrusted ampersands are
// deferred until after rendering, preventing nested references from becoming
// active through successive HTML/Markdown transformations.
func markdownDisplaySource(text string) (string, func(string) string) {
	if !strings.ContainsRune(text, '&') {
		return safeMarkdownDisplay(text), func(rendered string) string { return rendered }
	}
	decoded := html.UnescapeString(text)
	// A one-cell private-use marker keeps wrapping intact. Pick one absent from
	// both literal text and decoded references, so genuine note text survives.
	used := make(map[rune]bool)
	for _, r := range text + decoded {
		used[r] = true
	}
	marker := ""
	for r := rune(0xe000); r <= 0xf8ff; r++ {
		if !used[r] && !strings.Contains(text, url.PathEscape(string(r))) && !strings.Contains(decoded, url.PathEscape(string(r))) {
			marker = string(r)
			break
		}
	}
	if marker == "" {
		// Even a document containing every BMP private-use rune has a finite
		// length; this fallback remains collision-free without a random token.
		marker = "\ue000"
		for strings.Contains(text, marker) || strings.Contains(decoded, marker) || strings.Contains(text, url.PathEscape(marker)) || strings.Contains(decoded, url.PathEscape(marker)) {
			marker += "\ue001"
		}
	}
	text = displayEntity.ReplaceAllStringFunc(text, func(entity string) string {
		value := html.UnescapeString(entity)
		var out strings.Builder
		changed := false
		for _, r := range value {
			switch {
			case r == '&':
				out.WriteString(marker)
				changed = true
			case unsafeDisplayRune(r):
				fmt.Fprintf(&out, "\\u{%X}", r)
				changed = true
			default:
				out.WriteRune(r)
			}
		}
		if changed {
			return out.String()
		}
		return entity
	})
	// Relative link resolution can URL-encode the marker. Restore that form
	// too, but only after all parsing/decoding (and before the output gate).
	restore := strings.NewReplacer(marker, "&", url.PathEscape(marker), "&")
	return safeMarkdownDisplay(text), restore.Replace
}

// stylingSequenceLen recognizes only ordinary 7-bit SGR. No private parameters,
// intermediate bytes, OSC (including hyperlinks/clipboard), cursor/erase CSI,
// terminal modes, C1 forms, or incomplete escape sequences cross this boundary.
// Source controls/references must be projected before styling: an output-only
// gate cannot distinguish a forged SGR from an identical renderer-owned SGR.
func stylingSequenceLen(text string) int {
	if !strings.HasPrefix(text, "\x1b[") {
		return 0
	}
	for i := 2; i < len(text); i++ {
		switch c := text[i]; {
		case c == 'm':
			return i + 1
		case c >= '0' && c <= '9', c == ';', c == ':':
		default:
			return 0
		}
	}
	return 0
}

// safeTerminalOutput is the post-transformation terminal boundary. Retain trusted
// renderer/theme SGR, visibly project every other control and bidi format rune,
// and normalize CR/tabs before cell-width accounting. Never use on editor bytes.
func safeTerminalOutput(text string) string {
	text = displayText(text)
	var out strings.Builder
	for len(text) > 0 {
		if n := stylingSequenceLen(text); n > 0 {
			out.WriteString(text[:n])
			text = text[n:]
			continue
		}
		r, n := utf8.DecodeRuneInString(text)
		if unsafeDisplayRune(r) && r != '\n' {
			fmt.Fprintf(&out, "\\u{%X}", r)
		} else {
			out.WriteRune(r)
		}
		text = text[n:]
	}
	return out.String()
}
