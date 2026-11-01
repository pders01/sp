package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// A small, synthetic reproduction of the startup-notice/document boundary.
// The user's screenshot/note contents are not copied into fixtures or artifacts.
func TestNotebookNoticeCapture(t *testing.T) {
	dir := os.Getenv("SP_DESIGN_NOTICE_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set SP_DESIGN_NOTICE_CAPTURE_DIR to export the notice layout")
	}
	t.Setenv("GLAMOUR_STYLE", "light")
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	app := workflowApp(t, ModeNotebook, map[string]string{"2026-02-14": "# Project notes\n\nGoal: keep the page easy to read.\n\n- Clear hierarchy\n- Consistent alignment\n"})
	app.nb.theme.SetPref(ThemePrefLight)
	app.nb.theme.SetStatus("No page for today; opened the newest earlier page.", 0)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	frames := []designCaptureFrame{{ID: "notebook-notice-light-80x24", Title: "Notebook with startup notice", Group: "notebook", Theme: "light", Cols: 80, Rows: 24, ANSI: app.View()}}
	data, err := json.MarshalIndent(frames, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "frames.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
