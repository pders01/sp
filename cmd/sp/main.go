package main

import (
	"fmt"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/config"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

// Version information - set during build via -ldflags. commit and date
// are surfaced through VersionInfo so they don't get flagged as unused;
// goreleaser populates them on every release.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// VersionInfo formats the build metadata for display. Pure helper; the
// underlying vars are kept package-level so the linker can patch them.
func VersionInfo() string {
	return fmt.Sprintf("sp %s (commit %s, built %s)", version, commit, date)
}

var (
	calendarFlag bool
	notebookFlag bool
)

func main() {
	Execute()
}

var rootCmd = &cobra.Command{
	Use:     "sp",
	Version: VersionInfo(),
	Short:   "A daily scratchpad for quick notes and todos",
	Long: `sp is a CLI/TUI-based scratchpad application for quickly storing notes,
todos, and thoughts. It automatically creates a new scratchpad for each day
and allows you to browse historical entries through a calendar interface.

Flow:
  sp        edit today's page directly
  sp -n     notebook viewer; Enter/e drills into the editor
  sp -c     calendar; Enter drills into the notebook at that day, then
            Enter again drills into the editor. Press 'e' in the calendar
            to skip the notebook preview and edit immediately.`,
	RunE: runScratchpad,
}

func init() {
	rootCmd.Flags().BoolVarP(&calendarFlag, "calendar", "c", false, "Open calendar view to select a date")
	rootCmd.Flags().BoolVarP(&notebookFlag, "notebook", "n", false, "Open notebook view to browse all notes")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runScratchpad(_ *cobra.Command, _ []string) error {
	mgr, err := scratchpad.NewManager()
	if err != nil {
		return fmt.Errorf("failed to initialize scratchpad manager: %w", err)
	}

	cfg := config.Default()
	if path, perr := config.DefaultPath(); perr == nil {
		if loaded, lerr := config.Load(path); lerr == nil {
			cfg = loaded
		} else {
			fmt.Fprintf(os.Stderr, "sp: %v\n", lerr)
		}
	}
	icons := tui.NewIconSet(cfg.UI.Icons)

	pickedDate, ok, err := pickDate(mgr, icons, cfg)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	return editAndSave(mgr, pickedDate)
}

// pickDate runs the appropriate TUI chain and returns the date the user
// committed to. ok is false when the user quit out without picking.
func pickDate(mgr *scratchpad.Manager, icons tui.IconSet, cfg *config.Config) (date string, ok bool, err error) {
	switch {
	case calendarFlag:
		return runCalendarChain(mgr, icons, cfg)
	case notebookFlag:
		return runNotebookChain(mgr, icons, cfg, "")
	default:
		// Default flow: edit today.
		return "", true, nil
	}
}

func runCalendarChain(mgr *scratchpad.Manager, icons tui.IconSet, cfg *config.Config) (date string, ok bool, err error) {
	dates, contents, err := loadAll(mgr)
	if err != nil {
		return "", false, err
	}
	cal := tui.NewCalendar(dates)
	cal.SetIcons(icons)
	cal.SetThemePref(cfg.UI.Theme)
	cal.SetContents(contents)
	defer cal.Close()

	if _, rerr := tea.NewProgram(cal, tea.WithAltScreen()).Run(); rerr != nil {
		return "", false, fmt.Errorf("failed to run calendar TUI: %w", rerr)
	}

	picked := cal.GetSelectedDate()
	if picked == "" {
		fmt.Println("No date selected. Exiting.")
		return "", false, nil
	}
	if cal.IsDirectEdit() {
		return picked, true, nil
	}
	// Drill into the notebook positioned on the picked day.
	return runNotebookChain(mgr, icons, cfg, picked)
}

// runNotebookChain runs the notebook view. When startDate is non-empty,
// the cursor positions on it and the date is added to the page list if
// missing so brand-new days are reachable.
func runNotebookChain(mgr *scratchpad.Manager, icons tui.IconSet, cfg *config.Config, startDate string) (date string, ok bool, err error) {
	dates, contents, err := loadAll(mgr)
	if err != nil {
		return "", false, err
	}
	if startDate != "" {
		if _, exists := contents[startDate]; !exists {
			contents[startDate] = ""
			dates = append(dates, startDate)
		}
	}
	if len(dates) == 0 {
		fmt.Println("No scratchpad pages found.")
		return "", false, nil
	}

	nb := tui.NewNotebook(dates)
	nb.SetIcons(icons)
	nb.SetThemePref(cfg.UI.Theme)
	nb.SetContents(contents)
	if startDate != "" {
		nb.SetCurrentDate(startDate)
	}
	defer nb.Close()

	if _, rerr := tea.NewProgram(nb, tea.WithAltScreen()).Run(); rerr != nil {
		return "", false, fmt.Errorf("failed to run notebook TUI: %w", rerr)
	}

	picked := nb.GetSelectedDate()
	if picked == "" {
		return "", false, nil
	}
	return picked, true, nil
}

// loadAll reads every saved scratchpad and returns dates (descending)
// plus their contents.
func loadAll(mgr *scratchpad.Manager) (dates []string, contents map[string]string, err error) {
	dates, err = mgr.ListDates()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list dates: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	contents = make(map[string]string, len(dates))
	for _, d := range dates {
		sp, gerr := mgr.GetByDate(d)
		if gerr != nil {
			contents[d] = fmt.Sprintf("Error loading: %v", gerr)
			continue
		}
		contents[d] = sp.Content
	}
	return dates, contents, nil
}

// editAndSave opens the picked date (or today, when empty) in $EDITOR
// and persists changes when the user actually edited something.
func editAndSave(mgr *scratchpad.Manager, pickedDate string) error {
	var sp *scratchpad.Scratchpad
	var err error
	if pickedDate == "" {
		sp, err = mgr.GetToday()
	} else {
		sp, err = mgr.GetByDate(pickedDate)
	}
	if err != nil {
		return fmt.Errorf("failed to load scratchpad: %w", err)
	}

	ed, err := editor.NewEditor()
	if err != nil {
		return fmt.Errorf("failed to initialize editor: %w", err)
	}

	newContent, err := ed.Edit(sp.Content, sp.Date+".md")
	if err != nil {
		return fmt.Errorf("failed to edit scratchpad: %w", err)
	}

	if newContent != sp.Content {
		sp.Content = newContent
		if err := mgr.Save(sp); err != nil {
			return fmt.Errorf("failed to save scratchpad: %w", err)
		}
		fmt.Println("Scratchpad saved!")
	}
	return nil
}
