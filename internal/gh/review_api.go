package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// TokenFunc returns a GitHub installation token for the given owner.
type TokenFunc func(owner string) (string, error)

// APIReviewClient submits PR reviews via the GitHub REST API
// using an installation token obtained from a GitHub App.
type APIReviewClient struct {
	Token   TokenFunc
	BaseURL string // defaults to "https://api.github.com"
	Client  *http.Client
}

// reviewRequest is the JSON body for creating a PR review.
type reviewRequest struct {
	Body     string           `json:"body"`
	Event    string           `json:"event"`
	Comments []ReviewComment  `json:"comments,omitempty"`
}

// ReviewComment is an inline comment on a specific file/line.
type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Body string `json:"body"`
	Side string `json:"side,omitempty"` // "RIGHT" (default) or "LEFT"
}

func (c *APIReviewClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.github.com"
}

func (c *APIReviewClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

// SubmitReview posts a formal PR review via the GitHub API.
// event must be "APPROVE", "REQUEST_CHANGES", or "COMMENT".
func (c *APIReviewClient) SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
	return c.SubmitReviewWithComments(ctx, owner, repo, prNumber, event, body, nil)
}

// SubmitReviewWithComments posts a formal PR review with optional inline comments.
func (c *APIReviewClient) SubmitReviewWithComments(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []ReviewComment) error {
	token, err := c.Token(owner)
	if err != nil {
		return fmt.Errorf("obtaining installation token: %w", err)
	}

	reqBody := reviewRequest{
		Body:     body,
		Event:    event,
		Comments: comments,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling review request: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", c.baseURL(), owner, repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("creating review request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("submitting review: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		errMsg := string(respBody)

		// If the app is the PR author and tried REQUEST_CHANGES, fall back to COMMENT.
		if event == "REQUEST_CHANGES" && isOwnPRAPIError(errMsg) {
			return c.SubmitReviewWithComments(ctx, owner, repo, prNumber, "COMMENT", body, comments)
		}

		return fmt.Errorf("review API HTTP %d: %s", resp.StatusCode, errMsg)
	}

	return nil
}

// GetPRBranches fetches the base and head branch names via the GitHub API.
func (c *APIReviewClient) GetPRBranches(ctx context.Context, owner, repo string, prNumber int) (*PRBranches, error) {
	token, err := c.Token(owner)
	if err != nil {
		return nil, fmt.Errorf("obtaining installation token: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL(), owner, repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching PR: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("PR API HTTP %d: %s", resp.StatusCode, body)
	}

	var pr struct {
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, fmt.Errorf("decoding PR response: %w", err)
	}

	return &PRBranches{Base: pr.Base.Ref, Head: pr.Head.Ref}, nil
}

// PostComment posts an issue comment via the GitHub API.
func (c *APIReviewClient) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	token, err := c.Token(owner)
	if err != nil {
		return fmt.Errorf("obtaining installation token: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"body": body})
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.baseURL(), owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating comment request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("posting comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("comment API HTTP %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// CheckRunOutput holds the output fields for a check run.
type CheckRunOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text,omitempty"`
}

// createCheckRunRequest is the JSON body for POST /repos/{owner}/{repo}/check-runs.
type createCheckRunRequest struct {
	Name    string          `json:"name"`
	HeadSHA string          `json:"head_sha"`
	Status  string          `json:"status,omitempty"`
	Output  *CheckRunOutput `json:"output,omitempty"`
}

// updateCheckRunRequest is the JSON body for PATCH /repos/{owner}/{repo}/check-runs/{id}.
type updateCheckRunRequest struct {
	Status     string          `json:"status,omitempty"`
	Conclusion string          `json:"conclusion,omitempty"`
	Output     *CheckRunOutput `json:"output,omitempty"`
}

// createCheckRunResponse captures the id returned by the API.
type createCheckRunResponse struct {
	ID int64 `json:"id"`
}

// CreateCheckRun creates a new check run on the given commit SHA.
// It returns the check run ID. status is typically "in_progress" or "queued".
func (c *APIReviewClient) CreateCheckRun(ctx context.Context, owner, repo, name, headSHA, status string) (int64, error) {
	token, err := c.Token(owner)
	if err != nil {
		return 0, fmt.Errorf("obtaining installation token: %w", err)
	}

	reqBody := createCheckRunRequest{
		Name:    name,
		HeadSHA: headSHA,
		Status:  status,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshaling check run request: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/check-runs", c.baseURL(), owner, repo)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return 0, fmt.Errorf("creating check run request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("creating check run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("check run API HTTP %d: %s", resp.StatusCode, respBody)
	}

	var result createCheckRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decoding check run response: %w", err)
	}
	return result.ID, nil
}

// UpdateCheckRun updates an existing check run to the given status/conclusion.
// conclusion should be set when status is "completed" (e.g. "success", "failure").
// output is optional and can embed a Markdown summary.
func (c *APIReviewClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, status, conclusion string, output *CheckRunOutput) error {
	token, err := c.Token(owner)
	if err != nil {
		return fmt.Errorf("obtaining installation token: %w", err)
	}

	reqBody := updateCheckRunRequest{
		Status:     status,
		Conclusion: conclusion,
		Output:     output,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling check run update: %w", err)
	}

	url := fmt.Sprintf("%s/repos/%s/%s/check-runs/%d", c.baseURL(), owner, repo, checkRunID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", url, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("creating check run update request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("updating check run: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("check run update API HTTP %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// isOwnPRAPIError checks if an API error message indicates that the bot
// tried to request changes on its own PR.
func isOwnPRAPIError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "can not request changes on your own pull request") ||
		strings.Contains(lower, "cannot request changes on your own pull request")
}
