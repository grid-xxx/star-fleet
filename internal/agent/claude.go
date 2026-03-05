package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

type ClaudeBackend struct {
	Output io.Writer
}

func (c *ClaudeBackend) Run(ctx context.Context, workdir string, prompt string) error {
	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--dangerously-skip-permissions",
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
		return fmt.Errorf("claude-code: %s: %w", stderr.String(), err)
	}
	return nil
}
