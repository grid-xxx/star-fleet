package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

type CursorBackend struct {
	Output io.Writer
}

func (c *CursorBackend) Run(ctx context.Context, workdir string, prompt string) error {
	cmd := exec.CommandContext(ctx, "cursor",
		"agent",
		"-p", prompt,
		"--trust", "--yolo",
	)
	cmd.Dir = workdir
	var stderr bytes.Buffer
	if c.Output != nil {
		cmd.Stdout = c.Output
		cmd.Stderr = io.MultiWriter(&stderr, c.Output)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cursor: %s: %w", stderr.String(), err)
	}
	return nil
}
