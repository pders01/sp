//go:build !darwin && !linux

package editorterminal

import "os"

func platformSupported() bool              { return false }
func terminationSignals() []os.Signal      { return nil }
func sameTerminal(*os.File, *os.File) bool { return false }
func foregroundTerminal(*os.File) bool     { return false }
