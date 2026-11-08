package editorcore

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSharedProductionLayersDoNotImportEditorVariants(t *testing.T) {
	for _, dir := range []string{".", "../editorsandbox", "../privateedit", "../editorterminal"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			for _, spec := range file.Imports {
				path, unquoteErr := strconv.Unquote(spec.Path.Value)
				if unquoteErr != nil {
					t.Fatal(unquoteErr)
				}
				if strings.Contains(path, "/internal/nvim") {
					t.Fatalf("shared production layer %s imports variant %s", filepath.Join(dir, name), path)
				}
			}
		}
	}
}
