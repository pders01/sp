package main

import (
	"testing"

	"github.com/pders01/sp/internal/config"
)

func TestTemplateDefinitionsRequireCommandOptIn(t *testing.T) {
	cfg := config.Default()
	cfg.Templates.Items = []config.TemplateConfig{{Name: "Script", Command: []string{"script"}}}
	if _, err := templateDefinitions(cfg); err == nil {
		t.Fatal("command template was accepted without allow_commands")
	}

	cfg.Templates.AllowCommands = true
	definitions, err := templateDefinitions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 2 { // built-in plus configured command
		t.Errorf("definitions = %+v", definitions)
	}
}
