// Package sharedmcp implements an experimental, bounded, shared-only stdio
// protocol subset. It is not an externally verified MCP implementation.
package sharedmcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pders01/sp/internal/scratchpad"
)

const (
	ProtocolVersion = "2025-06-18"
	AdapterVersion  = "0.1.0-experimental"
	maxFrame        = 64 << 10
	maxOutput       = 1 << 20
	maxAppend       = 32 << 10
	maxRead         = 64 << 10
)

var errInvalid = errors.New("invalid input")

type config struct {
	store        string
	read, append map[string]bool
}

// Parse every grant before touching the store. No defaults, environment, HOME,
// relative paths, duplicate options, or implicit read-from-write authority.
func parseConfig(args []string) (config, error) {
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		key, value, equals := strings.Cut(args[i], "=")
		if key != "--store" && key != "--read-dates" && key != "--append-dates" {
			return config{}, errInvalid
		}
		if _, exists := values[key]; exists {
			return config{}, errInvalid
		}
		if !equals {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") {
				return config{}, errInvalid
			}
			value = args[i]
		}
		values[key] = value
	}
	if len(values) != 3 {
		return config{}, errInvalid
	}
	r, err := parseDates(values["--read-dates"])
	if err != nil {
		return config{}, err
	}
	a, err := parseDates(values["--append-dates"])
	if err != nil || len(r)+len(a) == 0 {
		return config{}, errInvalid
	}
	store := values["--store"]
	if !filepath.IsAbs(store) {
		return config{}, errInvalid
	}
	store = filepath.Clean(store)
	info, err := os.Lstat(store)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return config{}, errInvalid
	}
	return config{store: store, read: r, append: a}, nil
}

func parseDates(value string) (map[string]bool, error) {
	out := map[string]bool{}
	if value == "" {
		return out, nil
	}
	dates := strings.Split(value, ",")
	if len(dates) > 31 {
		return nil, errInvalid
	}
	for _, date := range dates {
		if scratchpad.ValidateSharedDate(date) != nil || out[date] {
			return nil, errInvalid
		}
		out[date] = true
	}
	return out, nil
}
