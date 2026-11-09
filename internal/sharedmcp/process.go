package sharedmcp

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pders01/sp/internal/scratchpad"
)

// Main owns the entire process and NEVER returns. Do not embed this adapter in
// a long-lived host. The existing storage API has noncancellable operations;
// fatal transport/deadline paths must end every storage worker with os.Exit.
// No shutdown hook may resume the host, retry writes, or wait without a bound.
func Main() {
	defer func() {
		if recover() != nil {
			exitProcess(1, "sp-shared: session ended; append outcomes may be unconfirmed.\n")
		}
	}()
	runProcess()
}

// Keep immediate exit outside the recovery boundary; no shutdown defers may
// run between serveProcess returning and process termination.
func runProcess() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		exitProcess(2, "sp-shared: experimental; require existing absolute --store and explicit --read-dates and --append-dates.\n")
	}
	manager, err := scratchpad.NewManagerAt(cfg.store)
	if err != nil {
		exitProcess(2, "sp-shared: store unavailable.\n")
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	code := serveProcess(cfg, manager, os.Stdin, os.Stdout, signals, operationBudget, drainGrace)
	// Exit immediately, including when a worker is blocked. Do NOT add deferred
	// manager/key operations, a logger drain, or another blocking shutdown step.
	os.Exit(code)
}

func exitProcess(code int, diagnostic string) {
	// A blocked stderr pipe must not prevent process termination either.
	done := make(chan struct{})
	go func() { _, _ = os.Stderr.WriteString(diagnostic); close(done) }()
	select {
	case <-done:
	case <-time.After(drainGrace):
	}
	os.Exit(code)
}
