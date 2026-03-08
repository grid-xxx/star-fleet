package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/nullne/star-fleet/internal/gh"
)

// CodeAgent implements a single agent that writes both implementation and tests.
type CodeAgent struct {
	Backend    Backend
	Owner      string
	Repo       string
	Issue      *gh.Issue
	Workdir    string
	Branch     string
	BaseBranch string
}

func (a *CodeAgent) Run(ctx context.Context, output io.Writer) error {
	prompt := buildImplementPrompt(a.Issue)
	return a.Backend.Run(ctx, a.Workdir, prompt, output)
}

func (a *CodeAgent) Fix(ctx context.Context, feedback string) error {
	prompt := buildFixPrompt(a.Issue, feedback)
	return a.Backend.Run(ctx, a.Workdir, prompt, nil)
}

// HandleEvent runs the agent with context about a PR event so it can decide
// how to respond (fix code, reply, or do nothing). Returns the agent's
// written response from the response file.
func (a *CodeAgent) HandleEvent(ctx context.Context, eventContext string) (string, error) {
	prompt := buildEventPrompt(a.Issue, eventContext)
	response, err := RunForReview(ctx, a.Backend, a.Workdir, prompt)
	if err != nil {
		return "", fmt.Errorf("handling event: %w", err)
	}
	return response, nil
}

func buildImplementPrompt(issue *gh.Issue) string {
	return fmt.Sprintf(`You are a senior software engineer implementing a feature.

## Task

Implement the changes described in the following GitHub issue. Write clean, production-ready code. Write both the implementation AND tests.

## Issue #%d: %s

%s

## Instructions

1. Read the existing codebase to understand the project structure, conventions, and patterns.
2. Implement the feature or fix described in the issue.
3. Write tests that verify the behavior described in the issue.
4. Follow existing code style and conventions.
5. Commit your changes with clear, descriptive commit messages.
`, issue.Number, issue.Title, issue.Body)
}

func buildFixPrompt(issue *gh.Issue, feedback string) string {
	return fmt.Sprintf(`You are a senior software engineer fixing issues found during review.

## Original Issue #%d: %s

%s

## Feedback

The following issues were found. Fix all of them:

%s

## Instructions

1. Address every piece of feedback above.
2. Commit your fixes with clear commit messages.
`, issue.Number, issue.Title, issue.Body, feedback)
}

func buildEventPrompt(issue *gh.Issue, eventContext string) string {
	return fmt.Sprintf(`You are a senior software engineer responding to feedback on your pull request.

## Original Issue #%d: %s

%s

## PR Event

%s

## Instructions

Think carefully before acting. Like a real developer:

- **Reasonable suggestion** → Fix the code and note what you fixed
- **Discussion or question** → Write a thoughtful reply explaining your reasoning
- **You disagree** → Explain your reasoning clearly
- **Unclear feedback** → Ask for clarification
- **CI failure from your code** → Fix it
- **CI failure from flaky test or environment** → Explain, don't change code
- **Unrelated CI failure** → Note it's unrelated

If you need to fix code:
1. Make the changes in the codebase
2. Commit with a clear message

Write your response (what you'd reply to the reviewer) to the output file.
If no action or reply is needed, write: NO_ACTION
`, issue.Number, issue.Title, issue.Body, eventContext)
}
