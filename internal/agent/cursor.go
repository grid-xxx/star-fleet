package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type CursorBackend struct{}

func (c *CursorBackend) Run(ctx context.Context, workdir string, prompt string) error {
	cmd := exec.CommandContext(ctx, "cursor",
		"agent", "run",
		"--prompt", prompt,
		"--repo", workdir,
	)
	cmd.Dir = workdir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cursor: %s: %w", stderr.String(), err)
	}
	return nil
}
