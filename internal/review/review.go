package review

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
)

// GHReview abstracts the GitHub operations needed for code review.
type GHReview interface {
	GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error)
	SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
}

// Reviewer performs automated code review on a pull request.
type Reviewer struct {
	Agent agent.Backend
	GH    GHReview
}

// ReviewResult contains the outcome of a single review pass.
type ReviewResult struct {
	Approved bool
	Comments string
}

// Review fetches the PR diff, sends it to the review agent, and posts the review.
// Returns the number of issues found.
func (r *Reviewer) Review(ctx context.Context, owner, repo string, prNumber int, cfg *config.ReviewConfig) (string, int, error) {
	diff, err := r.GH.GetPRDiff(ctx, owner, repo, prNumber)
	if err != nil {
		return "", 0, fmt.Errorf("fetching PR diff: %w", err)
	}

	if strings.TrimSpace(diff) == "" {
		return "", 0, nil
	}

	prompt := buildReviewPrompt(diff, cfg)

	response, err := agent.RunForReview(ctx, r.Agent, "", prompt)
	if err != nil {
		return "", 0, fmt.Errorf("running review agent: %w", err)
	}

	response = strings.TrimSpace(response)

	if isApproval(response) {
		return response, 0, nil
	}

	issues := countIssues(response)
	if issues == 0 {
		issues = 1
	}
	return response, issues, nil
}

// IsApproved checks if the latest review from fleet is an approval.
func (r *Reviewer) IsApproved(response string) bool {
	return isApproval(response)
}

func isApproval(response string) bool {
	if response == "" || response == "NO_ISSUES" || response == "LGTM" {
		return true
	}
	lower := strings.ToLower(response)
	return strings.Contains(lower, "no issues found") ||
		strings.Contains(lower, "no problems found") ||
		strings.Contains(lower, "lgtm") ||
		strings.Contains(lower, "looks good")
}

func countIssues(response string) int {
	count := 0
	for _, line := range strings.Split(response, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			count++
		}
		if len(trimmed) > 2 && trimmed[0] >= '1' && trimmed[0] <= '9' && (trimmed[1] == '.' || trimmed[1] == ')') {
			count++
		}
	}
	return count
}

func buildReviewPrompt(diff string, cfg *config.ReviewConfig) string {
	if cfg != nil && cfg.PromptFile != "" {
		if data, err := os.ReadFile(cfg.PromptFile); err == nil {
			return fmt.Sprintf("%s\n\n## Diff\n\n```diff\n%s\n```", string(data), diff)
		}
	}

	return fmt.Sprintf(`You are a senior software engineer performing a code review.

## Task

Review the following pull request diff. For each issue found, describe the problem clearly, referencing the specific file and line.

Only comment on real problems:
- Logic errors or bugs
- Missing error handling
- Security issues
- Style/convention violations
- Test coverage gaps
- Unnecessary changes or leftover debug code

Do NOT comment on things that are fine. Do NOT add praise or filler.

If there are no issues, respond with exactly: NO_ISSUES

If there are issues, list each one as a bullet point with the file path and line number.

## Diff

`+"```diff\n%s\n```", diff)
}
