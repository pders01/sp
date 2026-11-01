package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

var testOutputSGR = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// Independently check the output alphabet: only SGR and LF may be controls.
// In particular ansi.Strip alone is not an assertion that an output was safe.
func assertTerminalOutput(t *testing.T, output string) {
	t.Helper()
	plain := testOutputSGR.ReplaceAllString(output, "")
	for _, r := range plain {
		if unicode.IsControl(r) && r != '\n' || strings.ContainsRune("\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069", r) {
			t.Fatalf("unsafe output rune U+%04X in %q", r, output)
		}
	}
}

func assertRenderedTerminalOutput(t *testing.T, output string) {
	t.Helper()
	assertTerminalOutput(t, output)
	if strings.Contains(output, "\x1b[8m") {
		t.Fatalf("note-generated conceal SGR survived: %q", output)
	}
}

func assertOutputBounds(t *testing.T, output string, size tea.WindowSizeMsg) {
	t.Helper()
	if size.Width <= 0 || size.Height <= 0 {
		if output != "" {
			t.Fatalf("nonempty zero-sized output: %q", output)
		}
		return
	}
	lines := strings.Split(output, "\n")
	if len(lines) > size.Height {
		t.Fatalf("output has %d rows, limit %d", len(lines), size.Height)
	}
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > size.Width {
			t.Fatalf("output has %d cells, limit %d: %q", w, size.Width, line)
		}
	}
}

var terminalAttackSamples = []string{
	"literal \x1b[2J\x1b[H ERASE-END",
	"literal \x1b]52;c;U1lOVEhFVElD\a OSC-END",
	"literal \x1b]0;SYNTHETIC\x1b\\ TITLE-END",
	"literal \x1b[?1049h\x1b[?25l MODE-END",
	"literal \x1b[8mHIDDEN\x1b[0m SGR-END",
	"literal \a\x00\x7f\u0081\u0090\u009b2J\u009d52;c;U1lO\u009c C1-END",
	"entity &#27;[2J&#x1B;[H ERASE-END",
	"entity &#27[2J&#x1b[H NO-SEMICOLON-END",
	"entity &#x1b;]52;c;U1lOVEhFVElD&#7; OSC-END",
	"entity &#27;]0;SYNTHETIC&#27;\\ TITLE-END",
	"entity &#27;[?1049h&#27;[?25l MODE-END",
	"entity &#27;[8mHIDDEN&#x1b;[0m SGR-END",
	"entity &#0;&#7;&#127;&#129;&#144;&#155;&#157;&#156; C1-END",
	"bidi \u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069 BIDI-END",
	"bidi &#1564;&#x200e;&rlm;&#8234;&#8235;&#8236;&#8237;&#8238;&#8294;&#8295;&#8296;&#8297; BIDI-END",
	"nested &amp;#27;[8m &#38;#27;[2J &#x26;#7; &amp;amp;#27;]52;c;U1lO NESTED-END",
	"entity &#13;RETURN&#10;NEWLINE&Tab;TAB ENTITY-END",
	"<div>&<i></i>#27;[8mHIDDEN&<i></i>#27;[0m HTML-ASSEMBLED-END</div>",
	"<div>&#<i></i>27;[8mHIDDEN HTML-ASSEMBLED-END</div>",
}

func TestTerminalOutputGateRetainsOnlyStyling(t *testing.T) {
	trusted := "\x1b[m\x1b[0m\x1b[1;3;4m\x1b[38;5;42m\x1b[48;2;1;2;3m\x1b[38:2::1:2:3mStyled\x1b[0m\n"
	if got := safeTerminalOutput(trusted); got != trusted {
		t.Fatalf("trusted SGR changed: %q", got)
	}
	for _, input := range append(terminalAttackSamples, "\x1b[?1m\x1b[1 m\x1b[\x1b]8;;https://example.invalid\aLINK\x1b]8;;\a", "\r\t\n") {
		got := safeTerminalOutput(input)
		assertTerminalOutput(t, got)
		if safeTerminalOutput(got) != got {
			t.Fatal("output gate is not idempotent")
		}
	}
}

func TestTerminalOutputMarkdownControlsAndTrustedTheme(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	for _, style := range []string{styles.DarkStyle, styles.LightStyle, styles.NoTTYStyle} {
		for _, source := range []string{
			"# Heading\n\n**Bold** and *italic* and [link](https://example.invalid)",
			"literal \x1b[8mHIDDEN\x1b[0m",
			"entity &#27;[8mHIDDEN&#x1b;[0m",
			"nested &amp;#27;[8mHIDDEN&#38;#27;[0m",
		} {
			got := renderDocument(source, style, 100)
			assertTerminalOutput(t, got)
			if strings.Contains(got, "\x1b[8m") {
				t.Fatalf("note-generated conceal SGR survived: %q", got)
			}
			if style == styles.NoTTYStyle && strings.Contains(got, "\x1b") {
				t.Fatalf("notty acquired styling from note: %q", got)
			}
			if style != styles.NoTTYStyle && !testOutputSGR.MatchString(got) {
				t.Fatalf("trusted theme styling disappeared: %q", got)
			}
		}
	}
	for _, source := range []string{"&#27;[8m", "&#x1b[8m", "\x1b[8m"} {
		if got := ansi.Strip(renderDocument(source, styles.DarkStyle, 100)); !strings.Contains(got, "\\u{1B}[8m") {
			t.Fatalf("note SGR was not visibly projected: %q", got)
		}
	}
	if got := ansi.Strip(renderDocument("&#8238; &rlm; &#7; &#13; &Tab;", styles.NoTTYStyle, 100)); !strings.Contains(got, "\\u{202E} \\u{200F} \\u{7} \\u{D} \\u{9}") {
		t.Fatalf("decoded controls/bidi were not visibly projected: %q", got)
	}
	for _, source := range []string{"&amp;", "&#38;", "&#x26;"} {
		got := ansi.Strip(renderDocument("ENTITY "+source+" END &lt;span&gt; &ast;plain&ast; \ue000", styles.NoTTYStyle, 100))
		if !strings.Contains(got, "ENTITY & END <span> *plain* \ue000") {
			t.Fatalf("ordinary entities or private-use text changed: %q", got)
		}
	}
	for _, target := range []string{"relative&path?q=1&b=2", "https://example.invalid/a&b?q=1&b=2"} {
		got := ansi.Strip(renderDocument("[Link]("+target+") %EE%80%80 \ue000 &#57345;", styles.NoTTYStyle, 200))
		if !strings.Contains(got, target) || !strings.Contains(got, "%EE%80%80 \ue000 \ue001") {
			t.Fatalf("ampersand deferral changed URLs or collided with note text: %q", got)
		}
	}
}

func TestTerminalOutputNotebookAndSharedViews(t *testing.T) {
	const date = "2026-02-14"
	for i, payload := range terminalAttackSamples {
		for _, wrap := range []struct{ name, format string }{
			{"text", "%s"}, {"heading", "# %s"}, {"code-span", "`%s`"},
			{"code-block", "```text\n%s\n```"}, {"html", "<span>%s</span>"},
			{"link", "[SYNTHETIC](https://example.invalid/%s)"}, {"table", "| Synthetic |\n| --- |\n| %s |"},
		} {
			t.Run(fmt.Sprintf("%d/%s", i, wrap.name), func(t *testing.T) {
				source := fmt.Sprintf(wrap.format, payload)
				// Without renderer styling, *any* remaining ESC would be
				// note-generated, even if it happens to match a theme SGR.
				if got := renderDocument(source, styles.NoTTYStyle, 100); strings.Contains(got, "\x1b") {
					t.Fatalf("note-generated ANSI survived Markdown: %q", got)
				}
				n := newNotebookAt([]string{date}, date)
				defer n.Close()
				n.SetThemePref(ThemePrefDark)
				n.SetContents(map[string]string{date: source})
				n.sharedContents = map[string]string{date: source}
				for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 48, Height: 15}, {Width: 17, Height: 5}, {Width: 1, Height: 1}, {}} {
					n.Update(size)
					assertRenderedTerminalOutput(t, n.View())
					assertOutputBounds(t, n.View(), size)
					n.openShared()
					if !n.sharedActive() {
						t.Fatal("synthetic shared document did not open")
					}
					assertRenderedTerminalOutput(t, n.View())
					assertOutputBounds(t, n.View(), size)
					if size.Width == 100 {
						plain := ansi.Strip(n.View())
						for _, label := range []string{"Read-only", "Not encrypted", "self-reported"} {
							if !strings.Contains(plain, label) {
								t.Fatalf("lost provenance %q in %q", label, plain)
							}
						}
					}
					n.closeShared()
				}
				if n.contents[date] != source || n.sharedContents[date] != source {
					t.Fatal("display projection changed source bytes")
				}
			})
		}
	}
}

func TestTerminalOutputLockedCalendarPublicAnnotations(t *testing.T) {
	const date = "2026-02-14"
	for i, source := range terminalAttackSamples {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			cal := NewCalendar([]string{date})
			cal.SetCursor(date)
			n := newNotebookAt([]string{date}, date)
			app := NewApp(cal, n, ModeCalendar)
			defer app.Close()
			app.SetPrivacy(PrivacyHooks{
				Inspect: func(string) (PublicPage, error) {
					return PublicPage{Shared: source, HasPrivate: true}, nil
				},
				IsUnlocked: func() bool { return false },
				Load: func(string) (PrivatePage, error) {
					t.Fatal("locked calendar attempted a private load")
					return PrivatePage{}, nil
				},
			})
			for _, size := range []tea.WindowSizeMsg{{Width: 140, Height: 60}, {Width: 80, Height: 24}, {Width: 42, Height: 19}, {Width: 20, Height: 10}, {Width: 1, Height: 1}, {}} {
				app.Update(size)
				view := cal.View()
				assertRenderedTerminalOutput(t, view)
				assertOutputBounds(t, view, size)
				// The same public-only cache can be opened read-only while
				// locked; it must not trigger Load or acquire private bytes.
				n.Update(size)
				n.openShared()
				if !n.sharedActive() {
					t.Fatal("locked public shared view did not open")
				}
				assertRenderedTerminalOutput(t, n.View())
				assertOutputBounds(t, n.View(), size)
				n.closeShared()
				if size.Width == 140 {
					if !strings.Contains(ansi.Strip(view), "shared") {
						t.Fatal("lost shared preview label")
					}
					// Annotations are independently tested before composition, so
					// truncation or a final gate cannot hide a raw preview bypass.
					annotation := cal.cellAnnotation(cal.cursor, date, 500)
					assertRenderedTerminalOutput(t, annotation)
					if strings.Contains(source, "\x1b") && !strings.Contains(ansi.Strip(annotation), "\\u{1B}") {
						t.Fatalf("annotation did not project literal ESC: %q", annotation)
					}
				}
			}
			if cal.contents[date] != source || cal.previews[date] != extractPreview(source) {
				t.Fatal("calendar display changed public source bytes")
			}
		})
	}
}
