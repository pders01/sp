//go:build !darwin && !linux

package main

import "os"

func memoryEditSignals() []os.Signal { return []os.Signal{os.Interrupt} }
