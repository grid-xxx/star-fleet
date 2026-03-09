package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
)

// GHReview abstracts the GitHub operations needed for code review.
type GHReview interface {
	GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error)
	SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error
	SubmitPRReview(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
}

// Reviewer performs automated code review on a pull request.
type Reviewer struct {
	Agent agent.Backend
	GH    GHReview
}

// ReviewResult contains the outcome of a local review pass. The feedback
// field carries the full agent response so the orchestrator can feed it
// directly to the coding agent without a GitHub API roundtrip.
type ReviewResult struct {
	Approved bool
	Feedback string
	Issues   int
}

// structuredReview is the JSON format the review agent is asked to produce.
type structuredReview struct {
	Summary  string          `json:"summary"`
	Verdict  string          `json:"verdict"` // "approve" or "request_changes"
	Comments []reviewComment `json:"comments"`
}

type reviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// ReviewLocal fetches the PR diff, sends it to the review agent, and returns
// the result without submitting anything to GitHub. This enables fast local
// review-fix cycles where the orchestrator feeds feedback directly to the
// coding agent.
func (r *Reviewer) ReviewLocal(ctx context.Context, owner, repo string, prNumber int, cfg *config.ReviewConfig) (*ReviewResult, error) {
	diff, err := r.GH.GetPRDiff(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("fetching PR diff: %w", err)
	}

	if strings.TrimSpace(diff) == "" {
		return &ReviewResult{Approved: true}, nil
	}

	prompt := buildReviewPrompt(diff, cfg)
	response, err := agent.RunForReview(ctx, r.Agent, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("running review agent: %w", err)
	}

	response = strings.TrimSpace(response)
	return parseReviewResponse(response), nil
}

// parseReviewResponse converts raw agent output into a ReviewResult.
func parseReviewResponse(response string) *ReviewResult {
	if sr, ok := parseStructuredReview(response); ok {
		verdict := strings.ToLower(sr.Verdict)
		approved := verdict == "approve" || verdict == "approved" || verdict == "lgtm"
		issues := len(sr.Comments)
		feedback := formatLocalFeedback(sr)
		if approved {
			return &ReviewResult{Approved: true, Feedback: feedback}
		}
		if issues == 0 {
			issues = 1
		}
		return &ReviewResult{Approved: false, Feedback: feedback, Issues: issues}
	}

	if isApproval(response) {
		return &ReviewResult{Approved: true, Feedback: response}
	}

	issues := countIssues(response)
	if issues == 0 {
		issues = 1
	}
	return &ReviewResult{Approved: false, Feedback: response, Issues: issues}
}

// formatLocalFeedback builds a human-readable feedback string from a
// structured review so the coding agent receives actionable instructions.
func formatLocalFeedback(sr *structuredReview) string {
	var b strings.Builder
	b.WriteString(sr.Summary)
	for i, c := range sr.Comments {
		fmt.Fprintf(&b, "\n\n%d. %s:%d — %s", i+1, c.Path, c.Line, c.Body)
	}
	return b.String()
}

// Review fetches the PR diff, sends it to the review agent, and posts the
// review to GitHub as a PR Review. Returns the number of issues found.
func (r *Reviewer) Review(ctx context.Context, owner, repo string, prNumber int, cfg *config.ReviewConfig) (int, error) {
	diff, err := r.GH.GetPRDiff(ctx, owner, repo, prNumber)
	if err != nil {
		return 0, fmt.Errorf("fetching PR diff: %w", err)
	}

	if strings.TrimSpace(diff) == "" {
		return 0, nil
	}

	prompt := buildReviewPrompt(diff, cfg)

	response, err := agent.RunForReview(ctx, r.Agent, "", prompt)
	if err != nil {
		return 0, fmt.Errorf("running review agent: %w", err)
	}

	response = strings.TrimSpace(response)

	displayName := "Code Review"
	if cfg != nil && cfg.Name != "" {
		displayName = cfg.Name
	}

	// Try to parse structured JSON response
	if sr, ok := parseStructuredReview(response); ok {
		return r.submitStructured(ctx, owner, repo, prNumber, sr, displayName)
	}

	// Fall back to legacy plain-text handling
	if isApproval(response) {
		body := formatReviewBody(displayName, "approved", "No issues found.", 0)
		if err := r.submitWithFallback(ctx, owner, repo, prNumber, "APPROVE", body, nil); err != nil {
			return 0, fmt.Errorf("submitting approval: %w", err)
		}
		return 0, nil
	}

	body := formatReviewBody(displayName, "request_changes", response, countIssues(response))
	if err := r.GH.SubmitReview(ctx, owner, repo, prNumber, "REQUEST_CHANGES", body); err != nil {
		return 0, fmt.Errorf("submitting review: %w", err)
	}

	issues := countIssues(response)
	if issues == 0 {
		issues = 1
	}
	return issues, nil
}

func (r *Reviewer) submitStructured(ctx context.Context, owner, repo string, prNumber int, sr *structuredReview, displayName string) (int, error) {
	verdict := strings.ToLower(sr.Verdict)
	approved := verdict == "approve" || verdict == "approved" || verdict == "lgtm"

	event := "REQUEST_CHANGES"
	if approved {
		event = "APPROVE"
	}

	body := formatReviewBody(displayName, verdict, sr.Summary, len(sr.Comments))

	var inlineComments []gh.InlineComment
	for _, c := range sr.Comments {
		inlineComments = append(inlineComments, gh.InlineComment{
			Path: c.Path,
			Line: c.Line,
			Body: c.Body,
		})
	}

	if err := r.submitWithFallback(ctx, owner, repo, prNumber, event, body, inlineComments); err != nil {
		return 0, fmt.Errorf("submitting review: %w", err)
	}

	if approved {
		return 0, nil
	}

	issues := len(sr.Comments)
	if issues == 0 {
		issues = 1
	}
	return issues, nil
}

// submitWithFallback submits a PR review and gracefully handles self-review
// rejection. If the event is APPROVE and the API rejects it (HTTP 422 — GitHub
// prohibits authors from approving their own PRs), it retries as a COMMENT.
func (r *Reviewer) submitWithFallback(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
	var err error
	if len(comments) > 0 {
		err = r.GH.SubmitPRReview(ctx, owner, repo, prNumber, event, body, comments)
	} else {
		err = r.GH.SubmitReview(ctx, owner, repo, prNumber, event, body)
	}

	if err != nil && event == "APPROVE" && isSelfReviewError(err) {
		fallbackEvent := "COMMENT"
		body = strings.Replace(body, "(approved)", "(approved — self-review fallback)", 1)
		body = strings.Replace(body, "(approve)", "(approve — self-review fallback)", 1)
		if len(comments) > 0 {
			return r.GH.SubmitPRReview(ctx, owner, repo, prNumber, fallbackEvent, body, comments)
		}
		return r.GH.SubmitReview(ctx, owner, repo, prNumber, fallbackEvent, body)
	}

	return err
}

// isSelfReviewError returns true if the error indicates GitHub rejected an
// approval because the reviewer is the PR author.
func isSelfReviewError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "can not approve your own pull request") ||
		strings.Contains(msg, "cannot approve your own pull request") ||
		strings.Contains(msg, "422") ||
		strings.Contains(msg, "self-review")
}

// formatReviewBody builds the standardized review body with display name header.
func formatReviewBody(displayName, verdict, summary string, commentCount int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## 🤖 %s (%s)\n\n", displayName, verdict)
	b.WriteString(summary)
	if commentCount > 0 {
		fmt.Fprintf(&b, "\n\n📝 %d inline comment(s) below.", commentCount)
	}
	return b.String()
}

// parseStructuredReview attempts to parse the agent response as structured JSON.
// The agent may wrap JSON in a markdown code fence.
func parseStructuredReview(response string) (*structuredReview, bool) {
	raw := extractJSON(response)
	if raw == "" {
		return nil, false
	}
	var sr structuredReview
	if err := json.Unmarshal([]byte(raw), &sr); err != nil {
		return nil, false
	}
	if sr.Verdict == "" {
		return nil, false
	}
	return &sr, true
}

// extractJSON strips markdown code fences and extracts the first JSON object.
func extractJSON(s string) string {
	// Try stripping ```json ... ``` fences
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + len("```json")
		if end := strings.Index(s[start:], "```"); end >= 0 {
			return strings.TrimSpace(s[start : start+end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + len("```")
		if end := strings.Index(s[start:], "```"); end >= 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if len(candidate) > 0 && candidate[0] == '{' {
				return candidate
			}
		}
	}
	// Try the raw string itself
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '{' {
		return s
	}
	return ""
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

## Output Format

Respond with a JSON object (no markdown fence) in this exact format:

{
  "summary": "Brief overall summary of the review",
  "verdict": "approve" or "request_changes",
  "comments": [
    {"path": "file/path.go", "line": 42, "body": "Description of the issue"}
  ]
}

If there are no issues, use verdict "approve" and an empty comments array.

## Diff

`+"```diff\n%s\n```", diff)
}
