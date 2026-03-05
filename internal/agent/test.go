package agent

import (
	"context"
	"fmt"

	"github.com/nullne/star-fleet/internal/gh"
)

type TestAgent struct {
	Backend  Backend
	Owner    string
	Repo     string
	Issue    *gh.Issue
	Workdir  string
	Branch   string
	BaseBranch string
}

func (t *TestAgent) Run(ctx context.Context) error {
	prompt := buildTestPrompt(t.Issue)
	return t.Backend.Run(ctx, t.Workdir, prompt)
}

func (t *TestAgent) Fix(ctx context.Context, feedback string) error {
	prompt := buildTestFixPrompt(t.Issue, feedback)
	return t.Backend.Run(ctx, t.Workdir, prompt)
}

func buildTestPrompt(issue *gh.Issue) string {
	return fmt.Sprintf(`You are a senior QA engineer writing tests for a feature.

## Task

Write comprehensive tests for the feature described in the following GitHub issue. Write tests ONLY — do NOT implement the feature itself. Your tests should verify the behavior described in the spec.

## Issue #%d: %s

%s

## Instructions

1. Read the existing codebase to understand the project structure, testing conventions, and patterns.
2. Write tests that verify the behavior described in the issue.
3. Follow existing testing conventions and framework usage.
4. Tests should be thorough: cover happy paths, edge cases, and error conditions.
5. Do NOT implement the feature — only write tests that define the expected behavior.
6. Commit your tests with clear, descriptive commit messages.
`, issue.Number, issue.Title, issue.Body)
}

func buildTestFixPrompt(issue *gh.Issue, feedback string) string {
	return fmt.Sprintf(`You are a senior QA engineer fixing test issues found during review.

## Original Issue #%d: %s

%s

## Review Feedback

The following issues were found in your tests. Fix all of them:

%s

## Instructions

1. Address every piece of feedback above.
2. Do NOT implement the feature — only fix the tests.
3. Commit your fixes with clear commit messages.
`, issue.Number, issue.Title, issue.Body, feedback)
}
