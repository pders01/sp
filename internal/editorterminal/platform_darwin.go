//go:build darwin

package editorterminal

// Darwin PTY device identities are per endpoint; the shared Rdev check remains
// deliberately strict and does not resolve alternative /dev/tty aliases.
func terminalEndpoint(int) bool { return true }
