package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type ClaudeBackend struct{}

func (c *ClaudeBackend) Run(ctx context.Context, workdir string, prompt string) error {
	cmd := exec.CommandContext(ctx, "claude",
		"--print", prompt,
		"--output-format", "stream-json",
	)
	cmd.Dir = workdir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude-code: %s: %w", stderr.String(), err)
	}
	return nil
}
