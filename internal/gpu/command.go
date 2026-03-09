package gpu

import (
	"context"
	"os/exec"
)

// CommandRunner abstracts shell command execution for testability.
type CommandRunner interface {
	// Run executes a command and returns its combined stdout output.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner implements CommandRunner using os/exec.
type ExecRunner struct{}

// Run executes the command and returns stdout.
func (e *ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output() // #nosec G204 -- BONNIE is designed to execute host commands
}
