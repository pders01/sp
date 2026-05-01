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
// are surfaced through the rootCmd long description so they don't get
// flagged as unused; goreleaser populates them on every release.
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

Features:
- Daily scratchpad with automatic daily clearing
- TUI calendar view for browsing historical entries
- Notebook view for browsing all notes with markdown rendering
- External editor integration (uses $EDITOR)
- Markdown support for rich formatting
- Clean, distraction-free interface`,
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

	if notebookFlag {
		// Load all scratchpad files
		dates, lerr := mgr.ListDates()
		if lerr != nil {
			return fmt.Errorf("failed to list dates: %w", lerr)
		}
		if len(dates) == 0 {
			fmt.Println("No scratchpad pages found.")
			return nil
		}
		// Sort dates in descending order (most recent first)
		sort.Sort(sort.Reverse(sort.StringSlice(dates)))
		// Load content for each date
		contents := make(map[string]string)
		for _, d := range dates {
			sp, gerr := mgr.GetByDate(d)
			if gerr != nil {
				contents[d] = fmt.Sprintf("Error loading: %v", gerr)
			} else {
				contents[d] = sp.Content
			}
		}
		notebook := tui.NewNotebook(dates)
		notebook.SetIcons(icons)
		notebook.SetThemePref(cfg.UI.Theme)
		notebook.SetContents(contents)
		defer notebook.Close()
		p := tea.NewProgram(notebook, tea.WithAltScreen())
		if _, rerr := p.Run(); rerr != nil {
			return fmt.Errorf("failed to run notebook TUI: %w", rerr)
		}
		return nil
	}

	var date string
	if calendarFlag {
		// Show calendar view
		dates, lerr := mgr.ListDates()
		if lerr != nil {
			return fmt.Errorf("failed to list dates: %w", lerr)
		}
		contents := make(map[string]string, len(dates))
		for _, d := range dates {
			sp, gerr := mgr.GetByDate(d)
			if gerr != nil {
				continue
			}
			contents[d] = sp.Content
		}
		calendar := tui.NewCalendar(dates)
		calendar.SetIcons(icons)
		calendar.SetThemePref(cfg.UI.Theme)
		calendar.SetContents(contents)
		defer calendar.Close()
		p := tea.NewProgram(calendar, tea.WithAltScreen())
		if _, rerr := p.Run(); rerr != nil {
			return fmt.Errorf("failed to run calendar TUI: %w", rerr)
		}
		if calendar.GetSelectedDate() == "" {
			fmt.Println("No date selected. Exiting.")
			return nil
		}
		date = calendar.GetSelectedDate()
	} else {
		// Default: today
		date = ""
	}

	var sp *scratchpad.Scratchpad
	if date == "" {
		sp, err = mgr.GetToday()
	} else {
		sp, err = mgr.GetByDate(date)
	}
	if err != nil {
		return fmt.Errorf("failed to load scratchpad: %w", err)
	}

	// Use external editor
	ed, err := editor.NewEditor()
	if err != nil {
		return fmt.Errorf("failed to initialize editor: %w", err)
	}

	// Edit the content
	newContent, err := ed.Edit(sp.Content, sp.Date+".md")
	if err != nil {
		return fmt.Errorf("failed to edit scratchpad: %w", err)
	}

	// Save if content changed
	if newContent != sp.Content {
		sp.Content = newContent
		if err := mgr.Save(sp); err != nil {
			return fmt.Errorf("failed to save scratchpad: %w", err)
		}
		fmt.Println("Scratchpad saved!")
	}

	return nil
}
