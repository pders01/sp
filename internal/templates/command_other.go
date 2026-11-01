//go:build !darwin && !linux

package templates

import (
	"context"
	"os/exec"
)

// Other platforms retain CommandContext's immediate-process cancellation.
// WaitDelay still bounds inherited-pipe draining; this is not a process-tree
// termination guarantee (in particular, no Windows Job Object is installed).
func runTemplateProcess(_ context.Context, cmd *exec.Cmd) error {
	return cmd.Run()
}
