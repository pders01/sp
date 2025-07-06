package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"
	"github.com/spf13/cobra"
)

var (
	calendarFlag bool
)

func main() {
	Execute()
}

var rootCmd = &cobra.Command{
	Use:   "sp",
	Short: "A daily scratchpad for quick notes and todos",
	Long: `sp is a CLI/TUI-based scratchpad application for quickly storing notes, 
todos, and thoughts. It automatically creates a new scratchpad for each day 
and allows you to browse historical entries through a calendar interface.

Features:
- Daily scratchpad with automatic daily clearing
- TUI calendar view for browsing historical entries
- External editor integration (uses $EDITOR)
- Markdown support for rich formatting
- Clean, distraction-free interface`,
	RunE: runScratchpad,
}

func init() {
	rootCmd.Flags().BoolVarP(&calendarFlag, "calendar", "c", false, "Open calendar view to select a date")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runScratchpad(cmd *cobra.Command, args []string) error {
	mgr, err := scratchpad.NewManager()
	if err != nil {
		return fmt.Errorf("failed to initialize scratchpad manager: %w", err)
	}

	var date string
	if calendarFlag {
		// Show calendar view
		dates, err := mgr.ListDates()
		if err != nil {
			return fmt.Errorf("failed to list dates: %w", err)
		}
		calendar := tui.NewCalendar(dates)
		p := tea.NewProgram(calendar, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run calendar TUI: %w", err)
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
