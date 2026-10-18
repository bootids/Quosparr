package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

// Runner executes the ssh binary. Tests supply a fake. The real runner
// discards stderr so failure errors can carry only the exit code and
// category, never remote output.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, exitCode int, err error)
}

func newSSHRunner() Runner { return execRunner{} }

// execRunner starts the resolved ssh binary. A cancelled context kills
// the process and surfaces the context error.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err == nil {
		return out, 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out, exit.ExitCode(), nil
	}
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	return nil, 0, err
}
