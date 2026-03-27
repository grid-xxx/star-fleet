package issuelint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nullne/star-fleet/internal/config"
)

// GHClient abstracts GitHub operations needed by the linter.
type GHClient interface {
	FetchFileContent(ctx context.Context, owner, repo, path string) (string, error)
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
	FetchIssue(ctx context.Context, owner, repo string, number int) (string, string, error)
}

// Linter evaluates issues against project-specific guidelines using an LLM.
type Linter struct {
	GH         GHClient
	HTTPClient *http.Client
}

// LintResult holds the outcome of an issue review.
type LintResult struct {
	Pass    bool   // true if the issue meets the guidelines
	Comment string // feedback to post (empty if pass and silent)
}

// Lint evaluates the given issue against the project's guideline file.
func (l *Linter) Lint(ctx context.Context, owner, repo string, issueNumber int, cfg *config.IssueLintConfig) (*LintResult, error) {
	issueTitle, issueBody, err := l.GH.FetchIssue(ctx, owner, repo, issueNumber)
	if err != nil {
		return nil, fmt.Errorf("fetching issue #%d: %w", issueNumber, err)
	}

	guideline, err := l.GH.FetchFileContent(ctx, owner, repo, cfg.GuidelineFile)
	if err != nil {
		// If guideline file doesn't exist, use a sensible default
		guideline = defaultGuideline
	}

	prompt := buildPrompt(guideline, issueTitle, issueBody)

	response, err := l.callLLM(ctx, cfg.APIKey, cfg.Model, prompt)
	if err != nil {
		return nil, fmt.Errorf("calling LLM: %w", err)
	}

	return parseResponse(response), nil
}

const defaultGuideline = `## Issue Guidelines

A well-written issue should contain:

1. **What** — A clear description of what needs to be done or what the problem is.
2. **Why** — The motivation or context behind the change.
3. **Acceptance Criteria** — A checklist of conditions that must be met for the issue to be considered complete.

A good issue should NOT:
- Dictate specific implementation details (the "How") unless there are hard constraints.
- Be vague or ambiguous about what success looks like.
- Lack testable acceptance criteria.
`

func buildPrompt(guideline, title, body string) string {
	return fmt.Sprintf(`You are an issue quality reviewer for a software project. Your job is to evaluate whether a GitHub issue meets the project's guidelines.

## Project Guidelines

%s

## Issue to Review

**Title:** %s

**Body:**
%s

## Instructions

Evaluate the issue against the guidelines above. Respond in the following JSON format:

{"pass": true}

or

{"pass": false, "issues": ["issue 1 description", "issue 2 description"]}

Only output the JSON, nothing else. Be strict but fair — minor style issues are acceptable, but missing sections or vague requirements are not.`, guideline, title, body)
}

// anthropic API types
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type llmOutput struct {
	Pass   bool     `json:"pass"`
	Issues []string `json:"issues"`
}

func (l *Linter) callLLM(ctx context.Context, apiKey, model, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 1024,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := l.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	return apiResp.Content[0].Text, nil
}

func parseResponse(response string) *LintResult {
	response = strings.TrimSpace(response)

	var output llmOutput
	if err := json.Unmarshal([]byte(response), &output); err != nil {
		// If we can't parse, treat as pass to avoid blocking
		return &LintResult{Pass: true}
	}

	if output.Pass {
		return &LintResult{Pass: true}
	}

	var sb strings.Builder
	sb.WriteString("## 📋 Issue Review\n\n")
	sb.WriteString("This issue does not yet meet the project's guidelines. Please address the following:\n\n")
	for i, issue := range output.Issues {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, issue))
	}
	sb.WriteString("\n---\n*🤖 Automated review by Star Fleet*")

	return &LintResult{
		Pass:    false,
		Comment: sb.String(),
	}
}
