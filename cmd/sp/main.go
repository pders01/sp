package main

import (
	"context"
	"errors"
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

var rootCmd = newRootCommand(scratchpad.NewManager)

func newRootCommand(factory managerFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "sp",
		Version: VersionInfo(),
		Short:   "A daily scratchpad for notes and todos",
		Example: `  sp      # Edit today
  sp -n   # Notebook (Enter/e: edit)
  sp -c   # Calendar (Enter: notebook; e: edit directly)`,
		Args: cobra.NoArgs,
		RunE: runScratchpad,
	}
	cmd.Flags().BoolVarP(&calendarFlag, "calendar", "c", false, "Browse dates")
	cmd.Flags().BoolVarP(&notebookFlag, "notebook", "n", false, "Browse notes")
	cmd.AddGroup(
		&cobra.Group{ID: "shared", Title: "Shared notes:"},
		&cobra.Group{ID: "privacy", Title: "Privacy:"},
		&cobra.Group{ID: "utilities", Title: "Utilities:"},
	)
	appendCmd, sharedCmd, privacyCmd := newAppendCommand(factory), newSharedCommand(factory), newPrivacyCommand(factory)
	appendCmd.GroupID, sharedCmd.GroupID, privacyCmd.GroupID = "shared", "shared", "privacy"
	cmd.AddCommand(appendCmd, sharedCmd, privacyCmd)
	cmd.SetHelpCommandGroupID("utilities")
	cmd.SetCompletionCommandGroupID("utilities")
	return cmd
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
	defer mgr.Lock()
	private, err := mgr.PrivacyEnabled()
	if err != nil {
		return fmt.Errorf("failed to inspect privacy settings: %w", err)
	}
	if private {
		status, err := mgr.PrivacyStatus()
		if err != nil {
			return fmt.Errorf("failed to inspect privacy status: %w", err)
		}
		if len(status.PlaintextDates) > 0 {
			fmt.Fprintln(os.Stderr, "sp: WARNING: plaintext pages remain; stop old sp instances and run sp privacy enable to finish migration. See sp privacy status.")
		}
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

	// Encrypted pages must never pass through an external editor or its
	// plaintext temporary files, including during editor resolution.
	if private {
		if !calendarFlag && !notebookFlag {
			return editPrivateAndSave(mgr, "")
		}
		return runApp(mgr, nil, icons, cfg)
	}
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
	private, err := mgr.PrivacyEnabled()
	if err != nil {
		return err
	}
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
	if !private {
		cal.SetEditor(ed, makeSaver(mgr), makeLoader(mgr))
	}

	nb := tui.NewNotebook(dates)
	nb.SetIcons(icons)
	nb.SetThemePref(cfg.UI.Theme)
	nb.SetContents(contents)
	if !private {
		nb.SetEditor(ed, makeSaver(mgr), makeLoader(mgr))
	}

	mode := tui.ModeCalendar
	if notebookFlag && !calendarFlag {
		mode = tui.ModeNotebook
	}
	app := tui.NewApp(cal, nb, mode)
	if private {
		app.SetPrivacy(makePrivacyHooks(mgr))
	} else {
		sharedContents := make(map[string]string, len(dates))
		for _, d := range dates {
			shared, readErr := mgr.ReadShared(d)
			if readErr != nil {
				continue // Legacy loadAll already retains the per-page error preview.
			}
			sharedContents[d] = shared.Content
		}
		app.SetSharedContents(sharedContents)
	}
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
		if recoveryErr := app.RecoverPrivateEdit(); recoveryErr != nil {
			return fmt.Errorf("failed to run TUI: %w; %v", rerr, recoveryErr)
		}
		return fmt.Errorf("failed to run TUI: %w", rerr)
	}

	if private {
		// The private TUI owns its in-memory editor and recovery flow. Never
		// fall back to an external editor, even if it exits with a selection.
		return nil
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
	return func(date, original, content string) error {
		sp, err := mgr.GetByDate(date)
		if err != nil {
			return err
		}
		if sp.Content != original {
			return fmt.Errorf("%w: %s; reload before retrying", scratchpad.ErrConflict, date)
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
		private, err := mgr.PrivacyEnabled()
		if err != nil {
			return tui.TemplateApplyResult{}, err
		}
		if private && !mgr.IsUnlocked() {
			// Check before rendering: command templates must not run while
			// private notes are locked, even if command execution is trusted.
			return tui.TemplateApplyResult{}, scratchpad.ErrLocked
		}
		sections := make([]templates.Section, 0, len(selections))
		for _, selection := range selections {
			definition, ok := byID[selection.ID]
			if !ok {
				return tui.TemplateApplyResult{}, fmt.Errorf("unknown template %q", selection.ID)
			}
			section, renderErr := templates.RenderContext(ctx, definition, date)
			if renderErr != nil {
				return tui.TemplateApplyResult{}, renderErr
			}
			section.Force = selection.Force
			sections = append(sections, section)
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return tui.TemplateApplyResult{}, contextErr
		}
		sp, err := mgr.ApplyTemplateSections(date, sections)
		if err != nil && (!errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) || sp == nil) {
			return tui.TemplateApplyResult{}, err
		}
		return tui.TemplateApplyResult{Content: sp.Content, Applied: sp.AppliedTemplates, Published: err != nil}, err
	}
}

// loadAll reads every saved scratchpad and returns dates (descending), their
// contents, and template metadata used by the chooser.
func loadAll(mgr *scratchpad.Manager) (dates []string, contents map[string]string, applied map[string][]string, err error) {
	private, err := mgr.PrivacyEnabled()
	if err != nil {
		return nil, nil, nil, err
	}
	dates, err = mgr.ListDates()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list dates: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	contents = make(map[string]string, len(dates))
	applied = make(map[string][]string, len(dates))
	for _, d := range dates {
		if private {
			// Never decrypt the archive for previews, even after unlocking.
			shared, err := mgr.ReadShared(d)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to load shared page %s: %w", d, err)
			}
			contents[d] = shared.Content
			continue
		}
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
	private, err := mgr.PrivacyEnabled()
	if err != nil {
		return err
	}
	if private {
		return fmt.Errorf("private notes require the built-in in-memory editor")
	}
	var sp *scratchpad.Scratchpad
	if pickedDate == "" {
		sp, err = mgr.GetToday()
	} else {
		sp, err = mgr.GetByDate(pickedDate)
	}
	if err != nil {
		return fmt.Errorf("failed to load scratchpad: %w", err)
	}

	cmd, path, cleanup, err := ed.Prepare(sp.Content)
	if err != nil {
		return fmt.Errorf("failed to prepare editor: %w", err)
	}
	newContent, err := ed.RunPrepared(cmd, path)
	if err != nil {
		return fmt.Errorf("failed to edit scratchpad: %w; buffer path: %s", err, path)
	}

	if newContent != sp.Content {
		sp.Content = newContent
		if err := mgr.Save(sp); err != nil {
			if errors.Is(err, scratchpad.ErrDurabilityUnconfirmed) {
				return fmt.Errorf("scratchpad saved, but durability is unconfirmed: %w; buffer path: %s", err, path)
			}
			return fmt.Errorf("failed to save scratchpad: %w; buffer path: %s", err, path)
		}
		fmt.Println("Scratchpad saved!")
	}
	cleanup()
	return nil
}
