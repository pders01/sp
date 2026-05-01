package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/styles"
)

func TestResolveGlamourStyle_ExplicitPrefWins(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "")
	t.Setenv("COLORFGBG", "0;15") // would normally select light

	if got := resolveGlamourStyle(ThemePrefDark); got != styles.DarkStyle {
		t.Errorf("dark pref: got %q want %q", got, styles.DarkStyle)
	}
	if got := resolveGlamourStyle(ThemePrefLight); got != styles.LightStyle {
		t.Errorf("light pref: got %q want %q", got, styles.LightStyle)
	}
}

func TestResolveGlamourStyle_PrefIsCaseInsensitive(t *testing.T) {
	for _, in := range []string{"LIGHT", "Light", "  light  "} {
		if got := resolveGlamourStyle(in); got != styles.LightStyle {
			t.Errorf("input %q: got %q want %q", in, got, styles.LightStyle)
		}
	}
}

func TestResolveGlamourStyle_AutoHonorsGlamourStyleEnv(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "ascii")
	t.Setenv("COLORFGBG", "")
	if got := resolveGlamourStyle(ThemePrefAuto); got != "ascii" {
		t.Errorf("got %q want %q", got, "ascii")
	}
}

func TestResolveGlamourStyle_AutoHonorsCOLORFGBG(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "")

	cases := []struct {
		fgbg string
		want string
	}{
		{"15;0", styles.DarkStyle},
		{"0;15", styles.LightStyle},
		{"7;8", styles.DarkStyle},
		{"15;7", styles.LightStyle},
	}
	for _, tc := range cases {
		t.Run(tc.fgbg, func(t *testing.T) {
			t.Setenv("COLORFGBG", tc.fgbg)
			if got := resolveGlamourStyle(ThemePrefAuto); got != tc.want {
				t.Errorf("COLORFGBG=%q got %q want %q", tc.fgbg, got, tc.want)
			}
		})
	}
}

func TestResolveGlamourStyle_UnknownPrefFallsThrough(t *testing.T) {
	t.Setenv("GLAMOUR_STYLE", "ascii")
	t.Setenv("COLORFGBG", "")
	for _, in := range []string{"", "weird", "AUTO"} {
		if got := resolveGlamourStyle(in); got == "" {
			t.Errorf("input %q: got empty string", in)
		}
	}
}

func TestNextThemePref_Cycle(t *testing.T) {
	cases := []struct{ in, want string }{
		{ThemePrefAuto, ThemePrefLight},
		{ThemePrefLight, ThemePrefDark},
		{ThemePrefDark, ThemePrefAuto},
		{"", ThemePrefAuto},
		{"junk", ThemePrefAuto},
		{"  Light  ", ThemePrefDark},
	}
	for _, tc := range cases {
		if got := nextThemePref(tc.in); got != tc.want {
			t.Errorf("next(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestMsgThemeApplied_Format(t *testing.T) {
	got := MsgThemeApplied("auto", styles.LightStyle)
	if !strings.Contains(got, "auto") || !strings.Contains(got, styles.LightStyle) {
		t.Errorf("auto label: got %q", got)
	}
	got = MsgThemeApplied("light", styles.LightStyle)
	if !strings.Contains(got, "light") {
		t.Errorf("light label: got %q", got)
	}
	got = MsgThemeApplied("", styles.DarkStyle)
	if !strings.Contains(got, "auto") {
		t.Errorf("empty pref should display as auto: %q", got)
	}
}

func TestThemeWatcher_CycleAndApply(t *testing.T) {
	w := newThemeWatcher(ThemePrefAuto)

	if w.Pref() != ThemePrefAuto {
		t.Fatalf("initial pref = %q, want %q", w.Pref(), ThemePrefAuto)
	}

	// Cycle drains exactly one event each call; consume to keep channel empty.
	wants := []string{ThemePrefLight, ThemePrefDark, ThemePrefAuto}
	for i, want := range wants {
		w.Cycle()
		<-w.events
		if w.Pref() != want {
			t.Errorf("step %d: pref = %q, want %q", i, w.Pref(), want)
		}
	}
}

func TestThemeWatcher_ApplyResolved(t *testing.T) {
	w := newThemeWatcher(ThemePrefDark)

	if changed := w.applyResolved(); changed {
		t.Error("applyResolved should be a no-op when style is unchanged")
	}

	w.SetPref(ThemePrefLight)
	// SetPref already updates resolved, so applyResolved is now a no-op.
	if changed := w.applyResolved(); changed {
		t.Error("applyResolved after SetPref should not report change")
	}
	if w.Style() != styles.LightStyle {
		t.Errorf("style = %q, want %q", w.Style(), styles.LightStyle)
	}
}
