package agent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

type CursorBackend struct{}

func (c *CursorBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	cmd := exec.CommandContext(ctx, "cursor",
		"agent",
		"-p", prompt,
		"--trust", "--yolo",
	)
	cmd.Dir = workdir
	if err := runWithPTY(cmd, output); err != nil {
		return fmt.Errorf("cursor: %w", err)
	}
	return nil
}
