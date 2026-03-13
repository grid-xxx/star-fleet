package review

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
)

// GHReview abstracts the GitHub operations needed for code review.
type GHReview interface {
	GetPRBranches(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error)
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

// Review fetches the PR branch names, instructs the review agent to inspect
// the diff between base and head itself, and returns the review feedback.
// Returns the number of issues found.
func (r *Reviewer) Review(ctx context.Context, owner, repo string, prNumber int, cfg *config.ReviewConfig) (string, int, error) {
	branches, err := r.GH.GetPRBranches(ctx, owner, repo, prNumber)
	if err != nil {
		return "", 0, fmt.Errorf("fetching PR branches: %w", err)
	}

	prompt := buildReviewPrompt(branches.Base, branches.Head, cfg)

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

func buildReviewPrompt(base, head string, cfg *config.ReviewConfig) string {
	if cfg != nil && cfg.PromptFile != "" {
		if data, err := os.ReadFile(cfg.PromptFile); err == nil {
			return fmt.Sprintf("%s\n\n## Branches\n\nReview the changes between base branch `%s` and head branch `%s`.\nUse `git diff %s...%s` to obtain the diff yourself.", string(data), base, head, base, head)
		}
	}

	return fmt.Sprintf(`You are a senior software engineer performing a code review.

## Task

Review the changes between the base branch %[1]s and the head branch %[2]s.

Use %[3]s to obtain the diff, then review each changed file. For each issue found, describe the problem clearly, referencing the specific file and line.

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
`, "`"+base+"`", "`"+head+"`", "`git diff "+base+"..."+head+"`")
}
