package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pders01/sp/internal/config"
	"github.com/pders01/sp/internal/editor"
	"github.com/pders01/sp/internal/scratchpad"
	"github.com/pders01/sp/internal/templates"
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

	ed, eerr := editor.NewEditor()
	if eerr != nil {
		return fmt.Errorf("failed to initialize editor: %w", eerr)
	}

	if !calendarFlag && !notebookFlag {
		return editAndSave(mgr, ed, "")
	}
	return runApp(mgr, ed, icons, cfg)
}

func runApp(mgr *scratchpad.Manager, ed *editor.Editor, icons tui.IconSet, cfg *config.Config) error {
	dates, contents, applied, err := loadAll(mgr)
	if err != nil {
		return err
	}
	if len(dates) == 0 && notebookFlag && !calendarFlag {
		fmt.Println("No scratchpad pages found.")
		return nil
	}

	cal := tui.NewCalendar(dates)
	cal.SetIcons(icons)
	cal.SetThemePref(cfg.UI.Theme)
	cal.SetContents(contents)
	cal.SetEditor(ed, makeSaver(mgr), makeLoader(mgr))

	nb := tui.NewNotebook(dates)
	nb.SetIcons(icons)
	nb.SetThemePref(cfg.UI.Theme)
	nb.SetContents(contents)
	nb.SetEditor(ed, makeSaver(mgr))

	mode := tui.ModeCalendar
	if notebookFlag && !calendarFlag {
		mode = tui.ModeNotebook
	}
	app := tui.NewApp(cal, nb, mode)
	definitions, err := templateDefinitions(cfg)
	if err != nil {
		return err
	}
	options := make([]tui.DayTemplate, 0, len(definitions))
	for _, definition := range definitions {
		options = append(options, tui.DayTemplate{ID: definition.ID, Name: definition.Name})
	}
	app.SetTemplates(options, applied, makeTemplateApplier(mgr, definitions))
	defer app.Close()

	if _, rerr := tea.NewProgram(app, tea.WithAltScreen()).Run(); rerr != nil {
		return fmt.Errorf("failed to run TUI: %w", rerr)
	}

	// Fallback path for unwired editor: app exits with calendar's
	// directEdit/selected set or notebook's selected set, expecting
	// main to run the editor.
	if cal.GetSelectedDate() != "" && cal.IsDirectEdit() {
		return editAndSave(mgr, ed, cal.GetSelectedDate())
	}
	if d := nb.GetSelectedDate(); d != "" {
		return editAndSave(mgr, ed, d)
	}
	return nil
}

func makeSaver(mgr *scratchpad.Manager) tui.Saver {
	return func(date, content string) error {
		sp, err := mgr.GetByDate(date)
		if err != nil {
			return err
		}
		sp.Content = content
		return mgr.Save(sp)
	}
}

func makeLoader(mgr *scratchpad.Manager) func(string) (string, error) {
	return func(date string) (string, error) {
		sp, err := mgr.GetByDate(date)
		if err != nil {
			return "", err
		}
		return sp.Content, nil
	}
}

func templateDefinitions(cfg *config.Config) ([]templates.Definition, error) {
	definitions := templates.Builtins()
	for _, configured := range cfg.Templates.Items {
		if len(configured.Command) > 0 && !cfg.Templates.AllowCommands {
			return nil, fmt.Errorf(
				"command template %q requires templates.allow_commands = true",
				configured.Name,
			)
		}
		definitions = append(definitions, templates.Definition{
			ID:      configured.ID,
			Name:    configured.Name,
			File:    configured.File,
			Command: configured.Command,
		})
	}
	return templates.Normalize(definitions)
}

func makeTemplateApplier(mgr *scratchpad.Manager, definitions []templates.Definition) tui.TemplateApplier {
	byID := make(map[string]templates.Definition, len(definitions))
	for _, definition := range definitions {
		byID[definition.ID] = definition
	}
	return func(ctx context.Context, date string, selections []tui.TemplateSelection) (tui.TemplateApplyResult, error) {
		sections := make([]templates.Section, 0, len(selections))
		for _, selection := range selections {
			definition, ok := byID[selection.ID]
			if !ok {
				return tui.TemplateApplyResult{}, fmt.Errorf("unknown template %q", selection.ID)
			}
			section, err := templates.RenderContext(ctx, definition, date)
			if err != nil {
				return tui.TemplateApplyResult{}, err
			}
			section.Force = selection.Force
			sections = append(sections, section)
		}
		if err := ctx.Err(); err != nil {
			return tui.TemplateApplyResult{}, err
		}
		sp, err := mgr.ApplyTemplateSections(date, sections)
		if err != nil {
			return tui.TemplateApplyResult{}, err
		}
		return tui.TemplateApplyResult{Content: sp.Content, Applied: sp.AppliedTemplates}, nil
	}
}

// loadAll reads every saved scratchpad and returns dates (descending), their
// contents, and template metadata used by the chooser.
func loadAll(mgr *scratchpad.Manager) (dates []string, contents map[string]string, applied map[string][]string, err error) {
	dates, err = mgr.ListDates()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list dates: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	contents = make(map[string]string, len(dates))
	applied = make(map[string][]string, len(dates))
	for _, d := range dates {
		sp, gerr := mgr.GetByDate(d)
		if gerr != nil {
			contents[d] = fmt.Sprintf("Error loading: %v", gerr)
			continue
		}
		contents[d] = sp.Content
		applied[d] = append([]string(nil), sp.AppliedTemplates...)
	}
	return dates, contents, applied, nil
}

// editAndSave opens the picked date (or today, when empty) in $EDITOR
// and persists changes when the user actually edited something. Used
// for the bare `sp` flow and as a fallback when the TUI couldn't wire
// the editor inline.
func editAndSave(mgr *scratchpad.Manager, ed *editor.Editor, pickedDate string) error {
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
