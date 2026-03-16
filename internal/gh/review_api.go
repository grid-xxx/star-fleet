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

// isOwnPRAPIError checks if an API error message indicates that the bot
// tried to request changes on its own PR.
func isOwnPRAPIError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "can not request changes on your own pull request") ||
		strings.Contains(lower, "cannot request changes on your own pull request")
}
