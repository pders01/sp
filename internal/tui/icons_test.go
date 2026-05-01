package tui

import (
	"testing"
)

func TestNewIconSetNerd(t *testing.T) {
	got := NewIconSet("nerd")
	if got != nerdIcons {
		t.Errorf("NewIconSet(\"nerd\") = %+v, want nerdIcons", got)
	}
}

func TestNewIconSetUnicodeFallback(t *testing.T) {
	got := NewIconSet("")
	if got != unicodeIcons {
		t.Errorf("NewIconSet(\"\") = %+v, want unicodeIcons", got)
	}
	got = NewIconSet("garbage")
	if got != unicodeIcons {
		t.Errorf("NewIconSet(\"garbage\") = %+v, want unicodeIcons", got)
	}
}

func TestDefaultIconSetReadsEnv(t *testing.T) {
	t.Setenv("SP_ICONS", "nerd")
	if got := DefaultIconSet(); got != nerdIcons {
		t.Errorf("DefaultIconSet() with SP_ICONS=nerd = %+v, want nerdIcons", got)
	}

	t.Setenv("SP_ICONS", "unicode")
	if got := DefaultIconSet(); got != unicodeIcons {
		t.Errorf("DefaultIconSet() with SP_ICONS=unicode = %+v, want unicodeIcons", got)
	}
}

func TestWithIcon(t *testing.T) {
	if got := withIcon("", "label"); got != "label" {
		t.Errorf("withIcon(\"\", \"label\") = %q, want %q", got, "label")
	}
	if got := withIcon("", ""); got != "" {
		t.Errorf("withIcon(\"\", \"\") = %q, want %q", got, "")
	}
	if got := withIcon("", "name"); got != "name" {
		t.Errorf("empty glyph should pass name through, got %q", got)
	}
	if got := withIcon("X", "name"); got != "X name" {
		t.Errorf("withIcon(%q, %q) = %q, want %q", "X", "name", got, "X name")
	}
}
