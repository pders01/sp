// Package editorsandbox owns editor-independent OS enforcement requirements.
// An editor adapter, nofile option, empty configuration, child acknowledgement
// or process group is NOT enforcement. No native backend is approved yet: all
// protected admission fails closed. This package does not probe or execute OS
// sandbox mechanisms; native proof must be performed manually outside pi.
package editorsandbox

import (
	"context"
	"errors"
	"runtime"
)

var (
	ErrUnavailable   = errors.New("editor sandbox: OS enforcement unavailable or not validated")
	ErrInvalidPolicy = errors.New("editor sandbox: complete no-persistence policy required")
)

// Policy has no writable-directory exceptions or trusted-editor fallback. Only
// the parent may persist the draft, after encryption. Existing parent-owned
// stdio IPC is required; new network/IPC/processes must not bypass write denial.
// OS paging/core dumps, display capture and compromised OS remain exclusions.
type Policy struct{ version uint8 }

func NoPersistence() Policy  { return Policy{version: 1} }
func (p Policy) Valid() bool { return p.version == 1 }

type Requirements struct {
	DenyFilesystemWrites         bool
	DenyNewProcesses             bool
	DenyNetwork                  bool
	DenyNewIPC                   bool
	RestrictInheritedDescriptors bool
	ApplyBeforePrivateHandoff    bool
}

func (p Policy) Requirements() (Requirements, error) {
	if !p.Valid() {
		return Requirements{}, ErrInvalidPolicy
	}
	return Requirements{true, true, true, true, true, true}, nil
}

type Status struct {
	Platform  string
	Available bool
}

// Current is capability reporting, not a child-supplied or caller-set receipt.
// Add an available backend only with native enforcement/escape-path evidence.
func Current() Status { return Status{Platform: runtime.GOOS, Available: false} }

// Require is the common admission gate. Missing/unsupported/unvalidated native
// enforcement never silently becomes a synthetic or trusted-editor launch.
func Require(ctx context.Context, policy Policy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !policy.Valid() {
		return ErrInvalidPolicy
	}
	return ErrUnavailable
}
