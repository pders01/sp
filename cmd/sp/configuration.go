package main

import (
	"fmt"
	"io"

	"github.com/pders01/sp/internal/config"
	"github.com/pders01/sp/internal/scratchpad"
)

type appStartup struct {
	recovery *scratchpad.RecoveryReference
	notices  []string
}

func loadConfiguration(path string) (*config.Config, []string) {
	cfg, warnings, err := config.LoadWithDiagnostics(path)
	if err != nil {
		return config.Default(), []string{fmt.Sprintf("Configuration ignored: %v; using safe defaults.", err)}
	}
	notices := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		notices = append(notices, warning.Message)
	}
	return cfg, notices
}

func loadUIConfiguration() (*config.Config, []string) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Default(), []string{fmt.Sprintf("Configuration path unavailable: %v; using safe defaults.", err)}
	}
	return loadConfiguration(path)
}

func printConfigurationNotices(out io.Writer, notices []string) {
	for _, notice := range notices {
		fmt.Fprintln(out, "sp:", notice)
	}
}
