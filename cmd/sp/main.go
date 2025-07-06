package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	calendarFlag := flag.Bool("calendar", false, "Open calendar view")
	flag.Parse()

	mgr, err := scratchpad.NewManager()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing scratchpad manager: %v\n", err)
		os.Exit(1)
	}

	var date string
	if *calendarFlag {
		// Show calendar view
		dates, err := mgr.ListDates()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing dates: %v\n", err)
			os.Exit(1)
		}
		calendar := tui.NewCalendar(dates)
		p := tea.NewProgram(calendar)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running calendar TUI: %v\n", err)
			os.Exit(1)
		}
		if calendar.GetSelectedDate() == "" {
			fmt.Println("No date selected. Exiting.")
			return
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
		fmt.Fprintf(os.Stderr, "Error loading scratchpad: %v\n", err)
		os.Exit(1)
	}

	// Use external editor
	ed, err := editor.NewEditor()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing editor: %v\n", err)
		os.Exit(1)
	}

	// Edit the content
	newContent, err := ed.Edit(sp.Content, sp.Date+".md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error editing scratchpad: %v\n", err)
		os.Exit(1)
	}

	// Save if content changed
	if newContent != sp.Content {
		sp.Content = newContent
		if err := mgr.Save(sp); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving scratchpad: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Scratchpad saved!")
	}
}
