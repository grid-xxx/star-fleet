package agent

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

type ClaudeBackend struct{}

func (c *ClaudeBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--dangerously-skip-permissions",
	)
	cmd.Dir = workdir
	if err := runWithPTY(cmd, output); err != nil {
		return fmt.Errorf("claude-code: %w", err)
	}
	return nil
}
