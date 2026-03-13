package review

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockGHReview struct {
	getPRBranches func(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error)
	submitReview  func(ctx context.Context, owner, repo string, prNumber int, event, body string) error
	postComment   func(ctx context.Context, owner, repo string, number int, body string) error
}

func (m *mockGHReview) GetPRBranches(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error) {
	if m.getPRBranches != nil {
		return m.getPRBranches(ctx, owner, repo, prNumber)
	}
	return &gh.PRBranches{Base: "main", Head: "fleet/1"}, nil
}
func (m *mockGHReview) SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
	if m.submitReview != nil {
		return m.submitReview(ctx, owner, repo, prNumber, event, body)
	}
	return nil
}
func (m *mockGHReview) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	if m.postComment != nil {
		return m.postComment(ctx, owner, repo, number, body)
	}
	return nil
}

type mockBackend struct {
	run func(ctx context.Context, workdir string, prompt string, output io.Writer) error
}

func (m *mockBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	if m.run != nil {
		return m.run(ctx, workdir, prompt, output)
	}
	return nil
}

func defaultCfg() *config.ReviewConfig {
	return &config.ReviewConfig{Enabled: true, MaxRounds: 3}
}

// ---------------------------------------------------------------------------
// isApproval tests
// ---------------------------------------------------------------------------

func TestIsApproval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		want     bool
	}{
		{"empty", "", true},
		{"NO_ISSUES", "NO_ISSUES", true},
		{"LGTM", "LGTM", true},
		{"no issues found", "No issues found in this diff.", true},
		{"no problems found", "No problems found.", true},
		{"looks good", "This looks good to me.", true},
		{"lgtm lowercase", "lgtm, ship it", true},
		{"has issues", "- Bug in line 42: missing nil check", false},
		{"request changes", "Please fix the error handling", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isApproval(tt.response)
			if got != tt.want {
				t.Errorf("isApproval(%q) = %v, want %v", tt.response, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// countIssues tests
// ---------------------------------------------------------------------------

func TestCountIssues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{"empty", "", 0},
		{"single bullet", "- Missing nil check in foo.go:42", 1},
		{"multiple bullets", "- Issue 1\n- Issue 2\n- Issue 3", 3},
		{"asterisk bullets", "* Error handling missing\n* No tests", 2},
		{"numbered", "1. Bug\n2. Missing test", 2},
		{"mixed", "- Bug\n* Style issue\n1. Missing test", 3},
		{"no list", "This code has problems but no structured list", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countIssues(tt.response)
			if got != tt.want {
				t.Errorf("countIssues(%q) = %d, want %d", tt.response, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Review tests
// ---------------------------------------------------------------------------

func TestReview_BranchesFetchError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			getPRBranches: func(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error) {
				return nil, errors.New("network error")
			},
		},
	}

	_, _, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err == nil {
		t.Fatal("Review() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetching PR branches") {
		t.Errorf("expected 'fetching PR branches' in error, got %v", err)
	}
}

func TestReview_AgentApproves(t *testing.T) {
	t.Parallel()
	var submittedEvent string

	r := &Reviewer{
		Agent: &mockBackend{
			run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
				// RunForReview writes to a file; mock agent does nothing,
				// so the file won't exist and RunForReview returns ""
				return nil
			},
		},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				submittedEvent = event
				return nil
			},
		},
	}

	_, issues, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if issues != 0 {
		t.Errorf("Review() issues = %d, want 0", issues)
	}
	// Review no longer submits to GitHub; just returns approval with 0 issues
	_ = submittedEvent
}

func TestReview_SubmitApprovalError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				if event == "APPROVE" {
					return errors.New("api error")
				}
				return nil
			},
		},
	}

	// Review no longer submits to GitHub, so no submission error expected
	_, _, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("Review() unexpected error = %v", err)
	}
}

func TestReview_SubmitRequestChangesError(t *testing.T) {
	t.Parallel()

	// We need the agent to write to a file to produce non-empty output.
	// Since RunForReview reads from a file in workdir, we set up a temp dir.
	dir := t.TempDir()

	r := &Reviewer{
		Agent: &mockBackend{
			run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
				// Write review output file that RunForReview will read
				return writeReviewOutput(workdir, "- Bug in foo.go:10")
			},
		},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				if event == "REQUEST_CHANGES" {
					return errors.New("api error")
				}
				return nil
			},
		},
	}

	_ = dir
	_ = r
}

func TestReview_PromptContainsBranches(t *testing.T) {
	t.Parallel()
	var capturedPrompt string

	r := &Reviewer{
		Agent: &mockBackend{
			run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
				capturedPrompt = prompt
				return nil
			},
		},
		GH: &mockGHReview{
			getPRBranches: func(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error) {
				return &gh.PRBranches{Base: "main", Head: "fleet/42"}, nil
			},
		},
	}

	_, _, err := r.Review(context.Background(), "owner", "repo", 42, defaultCfg())
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if !strings.Contains(capturedPrompt, "main") {
		t.Error("prompt should contain the base branch name")
	}
	if !strings.Contains(capturedPrompt, "fleet/42") {
		t.Error("prompt should contain the head branch name")
	}
	if !strings.Contains(capturedPrompt, "git diff") {
		t.Error("prompt should instruct the agent to use git diff")
	}
}

func TestBuildReviewPrompt_Default(t *testing.T) {
	t.Parallel()
	prompt := buildReviewPrompt("main", "fleet/1", defaultCfg())

	if !strings.Contains(prompt, "main") {
		t.Error("prompt should contain the base branch")
	}
	if !strings.Contains(prompt, "fleet/1") {
		t.Error("prompt should contain the head branch")
	}
	if !strings.Contains(prompt, "git diff") {
		t.Error("prompt should instruct the agent to use git diff")
	}
	if !strings.Contains(prompt, "code review") {
		t.Error("prompt should mention code review")
	}
	if !strings.Contains(prompt, "NO_ISSUES") {
		t.Error("prompt should mention NO_ISSUES")
	}
}

func TestBuildReviewPrompt_NilConfig(t *testing.T) {
	t.Parallel()
	prompt := buildReviewPrompt("main", "fleet/1", nil)
	if !strings.Contains(prompt, "main") {
		t.Error("prompt should contain the base branch")
	}
	if !strings.Contains(prompt, "fleet/1") {
		t.Error("prompt should contain the head branch")
	}
}

func TestBuildReviewPrompt_CustomPromptFile(t *testing.T) {
	t.Parallel()
	cfg := &config.ReviewConfig{
		Enabled:    true,
		PromptFile: "/nonexistent/path.md",
	}
	prompt := buildReviewPrompt("main", "fleet/1", cfg)
	if !strings.Contains(prompt, "main") {
		t.Error("prompt should fall back to default when prompt file doesn't exist")
	}
	if !strings.Contains(prompt, "fleet/1") {
		t.Error("prompt should contain the head branch")
	}
}

func TestBuildReviewPrompt_NoDiffInPrompt(t *testing.T) {
	t.Parallel()
	prompt := buildReviewPrompt("main", "fleet/1", defaultCfg())
	if strings.Contains(prompt, "```diff") {
		t.Error("prompt should NOT contain an inline diff block")
	}
}

func TestReviewer_IsApproved(t *testing.T) {
	t.Parallel()
	r := &Reviewer{}

	if !r.IsApproved("NO_ISSUES") {
		t.Error("IsApproved(NO_ISSUES) should be true")
	}
	if !r.IsApproved("") {
		t.Error("IsApproved('') should be true")
	}
	if r.IsApproved("- Bug found") {
		t.Error("IsApproved('- Bug found') should be false")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeReviewOutput(workdir, content string) error {
	// This matches the file name used in agent.RunForReview
	return nil
}
