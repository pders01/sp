package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestTruncateUsesDisplayCellsAndPreservesGraphemes(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		width            int
	}{
		{"wide", "笔记abcd", "笔…", 3},
		{"emoji", "🙂abc", "🙂…", 3},
		{"combining fits", "e\u0301", "e\u0301", 1},
		{"combining cut", "e\u0301xy", "e\u0301…", 2},
		{"joined emoji fits", "👩‍👩‍👧‍👦", "👩‍👩‍👧‍👦", 2},
		{"joined emoji cut", "👩‍👩‍👧‍👦x", "…", 1},
		{"zero", "visible", "", 0},
		{"negative", "visible", "", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.text, tc.width)
			if got != tc.want || lipgloss.Width(got) > max(0, tc.width) {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

func TestTruncatePreservesANSIWithoutLeakingControlFragments(t *testing.T) {
	text := "\x1b[31m笔记abcdef\x1b[0m"
	for width := 0; width <= 10; width++ {
		got := truncate(text, width)
		if lipgloss.Width(got) > width {
			t.Fatalf("ANSI line exceeds width %d: %q", width, got)
		}
		plain := ansi.Strip(got)
		if strings.ContainsAny(plain, "\x1b[") {
			t.Fatalf("broken ANSI sequence at width %d: %q", width, got)
		}
	}
}
