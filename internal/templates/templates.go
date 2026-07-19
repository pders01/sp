// Package templates renders opt-in Markdown sections for scratchpad days.
package templates

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Definition describes one chooser entry. Exactly one of Body, File, or
// Command should provide the Markdown section body.
type Definition struct {
	ID      string
	Name    string
	Body    string
	File    string
	Command []string
}

// Section is rendered output ready to append to a scratchpad.
type Section struct {
	ID    string
	Title string
	Body  string
	Force bool
}

//go:embed builtin/workday-timebox.md
var workdayTimebox string

// Builtins returns templates available without configuration.
func Builtins() []Definition {
	return []Definition{{
		ID:   "workday-timebox",
		Name: "Workday timebox",
		Body: workdayTimebox,
	}}
}

const (
	commandTimeout   = 10 * time.Second
	maxCommandOutput = 1 << 20 // 1 MiB
)

var (
	nonID                  = regexp.MustCompile(`[^a-z0-9]+`)
	errCommandOutputTooBig = errors.New("template command output exceeds 1 MiB")
)

// Normalize fills missing IDs and names and rejects ambiguous definitions.
func Normalize(defs []Definition) ([]Definition, error) {
	out := make([]Definition, 0, len(defs))
	seen := make(map[string]bool, len(defs))
	for i, def := range defs {
		def.Name = strings.TrimSpace(def.Name)
		def.ID = strings.Trim(nonID.ReplaceAllString(strings.ToLower(def.ID), "-"), "-")
		if def.ID == "" {
			def.ID = strings.Trim(nonID.ReplaceAllString(strings.ToLower(def.Name), "-"), "-")
		}
		if def.Name == "" {
			def.Name = def.ID
		}
		if def.ID == "" {
			return nil, fmt.Errorf("template %d requires a name or id", i+1)
		}
		sources := 0
		if def.Body != "" {
			sources++
		}
		if def.File != "" {
			sources++
		}
		if len(def.Command) > 0 {
			sources++
		}
		if sources != 1 {
			return nil, fmt.Errorf("template %q requires exactly one body, file, or command source", def.Name)
		}
		if seen[def.ID] {
			return nil, fmt.Errorf("duplicate template id %q", def.ID)
		}
		seen[def.ID] = true
		out = append(out, def)
	}
	return out, nil
}

// Render resolves a definition into a Markdown section for date.
func Render(def Definition, date string) (Section, error) {
	return RenderContext(context.Background(), def, date)
}

// RenderContext resolves a definition and cancels command templates when ctx
// is canceled.
func RenderContext(ctx context.Context, def Definition, date string) (Section, error) {
	if err := ctx.Err(); err != nil {
		return Section{}, err
	}
	body := def.Body
	switch {
	case def.File != "":
		path, err := expandHome(def.File)
		if err != nil {
			return Section{}, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return Section{}, fmt.Errorf("read template %q: %w", def.Name, err)
		}
		body = string(data)
	case len(def.Command) > 0:
		output, err := runCommand(ctx, def, date, commandTimeout, maxCommandOutput)
		if err != nil {
			return Section{}, err
		}
		body = output
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Section{}, fmt.Errorf("template %q produced no Markdown", def.Name)
	}
	return Section{ID: def.ID, Title: def.Name, Body: body}, nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining < len(p) {
		b.overflow = true
		if remaining > 0 {
			_, _ = b.buffer.Write(p[:remaining])
		}
		// Report the whole chunk consumed so the child can finish while
		// retaining no more than the configured amount in memory.
		return len(p), nil
	}
	return b.buffer.Write(p)
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func runCommand(parent context.Context, def Definition, date string, timeout time.Duration, maxOutput int) (string, error) {
	args := append([]string(nil), def.Command...)
	path, err := expandHome(args[0])
	if err != nil {
		return "", err
	}
	args[0] = path

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) // #nosec G204 -- explicit, opt-in user configuration.
	cmd.Env = commandEnvironment(date)
	stdout := &limitedBuffer{limit: maxOutput}
	stderr := &limitedBuffer{limit: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	switch {
	case stdout.overflow || stderr.overflow:
		return "", fmt.Errorf("run template %q: %w", def.Name, errCommandOutputTooBig)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "", fmt.Errorf("run template %q: timed out after %s", def.Name, timeout)
	case errors.Is(ctx.Err(), context.Canceled):
		return "", fmt.Errorf("run template %q: %w", def.Name, context.Canceled)
	case runErr != nil:
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("run template %q: %w: %s", def.Name, runErr, detail)
		}
		return "", fmt.Errorf("run template %q: %w", def.Name, runErr)
	}
	return stdout.String(), nil
}

func commandEnvironment(date string) []string {
	env := []string{"SP_DATE=" + date}
	for _, name := range []string{
		"PATH", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "TEMP", "TMP",
		"SYSTEMROOT", "WINDIR",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand %q: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}
