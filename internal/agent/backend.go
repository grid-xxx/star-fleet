package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type Backend interface {
	Run(ctx context.Context, workdir string, prompt string) error
}

func NewBackend(name string) (Backend, error) {
	switch name {
	case "claude-code":
		return &ClaudeBackend{}, nil
	case "cursor":
		return &CursorBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q", name)
	}
}

const reviewOutputFile = ".fleet-review-output.md"

// RunForReview runs the backend with a prompt that instructs it to write output
// to a known file, then reads and returns that file's contents.
func RunForReview(ctx context.Context, b Backend, workdir, prompt string) (string, error) {
	outputPath := filepath.Join(workdir, reviewOutputFile)
	os.Remove(outputPath)

	augmented := prompt + fmt.Sprintf("\n\nIMPORTANT: Write your complete review output to the file %s in the repository root. Do not commit this file.", reviewOutputFile)

	if err := b.Run(ctx, workdir, augmented); err != nil {
		return "", err
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading review output: %w", err)
	}
	os.Remove(outputPath)
	return string(data), nil
}
