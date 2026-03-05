package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/gh"
)

type Result struct {
	Clean    bool
	Feedback string
}

// ReviewPR uses the code agent to review a PR diff and posts findings as a comment.
// reviewDir should be a git-initialized directory where the agent can run.
func ReviewPR(ctx context.Context, backend agent.Backend, reviewDir, owner, repo string, prNumber int, role string) (*Result, error) {
	diff, err := gh.GetPRDiff(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("getting PR diff: %w", err)
	}

	prompt := buildReviewPrompt(diff, role)
	reviewText, err := agent.RunForReview(ctx, backend, reviewDir, prompt)
	if err != nil {
		return nil, fmt.Errorf("review agent: %w", err)
	}

	if reviewText == "" || isCleanReview(reviewText) {
		return &Result{Clean: true}, nil
	}

	if err := gh.PostReviewComment(ctx, owner, repo, prNumber, reviewText); err != nil {
		return nil, fmt.Errorf("posting review: %w", err)
	}

	return &Result{Clean: false, Feedback: reviewText}, nil
}

func CountIssues(feedback string) int {
	count := 0
	for _, line := range strings.Split(feedback, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			count++
		}
	}
	if count == 0 && feedback != "" {
		count = 1
	}
	return count
}

func isCleanReview(text string) bool {
	lower := strings.ToLower(text)
	cleanIndicators := []string{
		"lgtm",
		"looks good",
		"no issues",
		"no problems",
		"approved",
		"no changes needed",
	}
	for _, indicator := range cleanIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

func buildReviewPrompt(diff, role string) string {
	roleDesc := "implementation"
	if role == "test" {
		roleDesc = "test"
	}
	return fmt.Sprintf(`You are a senior engineer reviewing a %s pull request.

## PR Diff

%s

## Instructions

Review the code changes above. Look for:
- Bugs, logic errors, or incorrect behavior
- Missing error handling
- Security issues
- Code style or convention violations
- Performance concerns

If the code looks good, write exactly: LGTM

If there are issues, list each issue as a bullet point with:
- The file and approximate location
- What the problem is
- How to fix it

Be specific and actionable. Do not nitpick style preferences.
`, roleDesc, "```diff\n"+diff+"\n```")
}
