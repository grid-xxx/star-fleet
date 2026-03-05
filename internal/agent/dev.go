package agent

import (
	"context"
	"fmt"

	"github.com/nullne/star-fleet/internal/gh"
)

type DevAgent struct {
	Backend  Backend
	Owner    string
	Repo     string
	Issue    *gh.Issue
	Workdir  string
	Branch   string
	BaseBranch string
}

func (d *DevAgent) Run(ctx context.Context) error {
	prompt := buildDevPrompt(d.Issue)
	return d.Backend.Run(ctx, d.Workdir, prompt)
}

func (d *DevAgent) Fix(ctx context.Context, feedback string) error {
	prompt := buildDevFixPrompt(d.Issue, feedback)
	return d.Backend.Run(ctx, d.Workdir, prompt)
}

func buildDevPrompt(issue *gh.Issue) string {
	return fmt.Sprintf(`You are a senior software engineer implementing a feature.

## Task

Implement the changes described in the following GitHub issue. Write clean, production-ready code. Do NOT write tests — a separate agent handles testing.

## Issue #%d: %s

%s

## Instructions

1. Read the existing codebase to understand the project structure, conventions, and patterns.
2. Implement the feature or fix described in the issue.
3. Follow existing code style and conventions.
4. Commit your changes with clear, descriptive commit messages.
5. Do NOT add test files — testing is handled independently.
`, issue.Number, issue.Title, issue.Body)
}

func buildDevFixPrompt(issue *gh.Issue, feedback string) string {
	return fmt.Sprintf(`You are a senior software engineer fixing issues found during code review.

## Original Issue #%d: %s

%s

## Review Feedback

The following issues were found during review. Fix all of them:

%s

## Instructions

1. Address every piece of feedback above.
2. Commit your fixes with clear commit messages.
3. Do NOT add test files.
`, issue.Number, issue.Title, issue.Body, feedback)
}
