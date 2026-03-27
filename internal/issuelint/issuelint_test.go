package issuelint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nullne/star-fleet/internal/config"
)

type mockGHClient struct {
	issueTitle  string
	issueBody   string
	fileContent string
	fileErr     error
	comments    []string
}

func (m *mockGHClient) FetchIssue(ctx context.Context, owner, repo string, number int) (string, string, error) {
	return m.issueTitle, m.issueBody, nil
}

func (m *mockGHClient) FetchFileContent(ctx context.Context, owner, repo, path string) (string, error) {
	return m.fileContent, m.fileErr
}

func (m *mockGHClient) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	m.comments = append(m.comments, body)
	return nil
}

func TestParseResponse_Pass(t *testing.T) {
	t.Parallel()
	result := parseResponse(`{"pass": true}`)
	if !result.Pass {
		t.Error("expected pass=true")
	}
	if result.Comment != "" {
		t.Errorf("expected empty comment, got %q", result.Comment)
	}
}

func TestParseResponse_Fail(t *testing.T) {
	t.Parallel()
	result := parseResponse(`{"pass": false, "issues": ["Missing acceptance criteria", "No Why section"]}`)
	if result.Pass {
		t.Error("expected pass=false")
	}
	if result.Comment == "" {
		t.Error("expected non-empty comment")
	}
	if !contains(result.Comment, "Missing acceptance criteria") {
		t.Errorf("comment should mention first issue, got %q", result.Comment)
	}
	if !contains(result.Comment, "No Why section") {
		t.Errorf("comment should mention second issue, got %q", result.Comment)
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	t.Parallel()
	result := parseResponse("not json at all")
	if !result.Pass {
		t.Error("expected pass=true for unparseable response (fail-open)")
	}
}

func TestBuildPrompt(t *testing.T) {
	t.Parallel()
	prompt := buildPrompt("guideline content", "My Title", "My Body")
	if !contains(prompt, "guideline content") {
		t.Error("prompt should contain guideline")
	}
	if !contains(prompt, "My Title") {
		t.Error("prompt should contain title")
	}
	if !contains(prompt, "My Body") {
		t.Error("prompt should contain body")
	}
}

func TestLint_Pass(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: `{"pass": true}`},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gh := &mockGHClient{
		issueTitle:  "feat: add logging",
		issueBody:   "## What\nAdd logging\n## Why\nObservability\n## Acceptance Criteria\n- [ ] Logs exist",
		fileContent: "guideline here",
	}

	linter := &Linter{
		GH:         gh,
		HTTPClient: server.Client(),
	}

	// Override the API URL by using a custom transport
	linter.HTTPClient = &http.Client{
		Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL},
	}

	cfg := &config.IssueLintConfig{
		Enabled:       true,
		GuidelineFile: "docs/ISSUE-SPEC.md",
		APIKey:        "test-key",
		Model:         "claude-sonnet-4-20250514",
	}

	result, err := linter.Lint(context.Background(), "owner", "repo", 1, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pass {
		t.Error("expected pass")
	}
}

func TestLint_Fail(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: `{"pass": false, "issues": ["Missing Why section"]}`},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gh := &mockGHClient{
		issueTitle:  "fix something",
		issueBody:   "just fix it",
		fileContent: "guideline here",
	}

	linter := &Linter{
		GH:         gh,
		HTTPClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
	}

	cfg := &config.IssueLintConfig{
		Enabled:       true,
		GuidelineFile: "docs/ISSUE-SPEC.md",
		APIKey:        "test-key",
		Model:         "claude-sonnet-4-20250514",
	}

	result, err := linter.Lint(context.Background(), "owner", "repo", 1, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pass {
		t.Error("expected fail")
	}
	if !contains(result.Comment, "Missing Why section") {
		t.Errorf("comment should contain issue, got %q", result.Comment)
	}
}

func TestLint_MissingGuideline_UsesDefault(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Content: []struct {
				Text string `json:"text"`
			}{
				{Text: `{"pass": true}`},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gh := &mockGHClient{
		issueTitle: "feat: something",
		issueBody:  "body",
		fileErr:    fmt.Errorf("not found"),
	}

	linter := &Linter{
		GH:         gh,
		HTTPClient: &http.Client{Transport: &rewriteTransport{base: server.Client().Transport, url: server.URL}},
	}

	cfg := &config.IssueLintConfig{
		Enabled:       true,
		GuidelineFile: "nonexistent.md",
		APIKey:        "test-key",
		Model:         "claude-sonnet-4-20250514",
	}

	result, err := linter.Lint(context.Background(), "owner", "repo", 1, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pass {
		t.Error("expected pass")
	}
}

// rewriteTransport redirects all requests to the test server URL.
type rewriteTransport struct {
	base http.RoundTripper
	url  string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.url[len("http://"):]
	if t.base != nil {
		return t.base.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
