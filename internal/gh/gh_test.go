package gh

import (
	"context"
	"fmt"
	"strings"
	"strconv"
	"testing"
)

func TestCurrentRepo(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		err       error
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "single remote",
			output:    "grid-xxx/star-fleet\n",
			wantOwner: "grid-xxx",
			wantRepo:  "star-fleet",
		},
		{
			name:      "multiple remotes returns nameWithOwner correctly",
			output:    "upstream-org/star-fleet\n",
			wantOwner: "upstream-org",
			wantRepo:  "star-fleet",
		},
		{
			name:      "no trailing newline",
			output:    "owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:    "gh command fails",
			output:  "",
			err:     fmt.Errorf("gh repo view --json nameWithOwner -q .nameWithOwner: : exit status 1"),
			wantErr: true,
		},
		{
			name:    "unexpected format without slash",
			output:  "noslash\n",
			wantErr: true,
		},
		{
			name:    "empty output",
			output:  "\n",
			wantErr: true,
		},
		{
			name:      "repo name with dots",
			output:    "my-org/my.dotted.repo\n",
			wantOwner: "my-org",
			wantRepo:  "my.dotted.repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRunFn := runFn
			t.Cleanup(func() { runFn = origRunFn })

			runFn = func(_ context.Context, _ string, args ...string) (string, error) {
				return tt.output, tt.err
			}

			info, err := CurrentRepo(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", info.Owner, tt.wantOwner)
			}
			if info.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", info.Repo, tt.wantRepo)
			}
		})
	}
}

func TestCurrentRepoUsesNameWithOwner(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	var capturedArgs []string
	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		capturedArgs = args
		return "owner/repo\n", nil
	}

	_, err := CurrentRepo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner"}
	if len(capturedArgs) != len(expected) {
		t.Fatalf("args = %v, want %v", capturedArgs, expected)
	}
	for i, arg := range expected {
		if capturedArgs[i] != arg {
			t.Errorf("arg[%d] = %q, want %q", i, capturedArgs[i], arg)
		}
	}
}

func TestCheckCIStatus(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		runErr    error
		wantGreen bool
		wantTotal int
		wantErr   bool
	}{
		{
			name:      "all checks green",
			output:    `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"},{"name":"lint","state":"COMPLETED","conclusion":"SUCCESS"}]`,
			wantGreen: true,
			wantTotal: 2,
		},
		{
			name:      "one check failed",
			output:    `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"},{"name":"lint","state":"COMPLETED","conclusion":"FAILURE"}]`,
			wantGreen: false,
			wantTotal: 2,
		},
		{
			name:      "check still in progress",
			output:    `[{"name":"build","state":"IN_PROGRESS","conclusion":""},{"name":"lint","state":"COMPLETED","conclusion":"SUCCESS"}]`,
			wantGreen: false,
			wantTotal: 2,
		},
		{
			name:      "no checks",
			output:    `[]`,
			wantGreen: false,
			wantTotal: 0,
		},
		{
			name:      "neutral and skipped count as green",
			output:    `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"},{"name":"optional","state":"COMPLETED","conclusion":"NEUTRAL"},{"name":"skip","state":"COMPLETED","conclusion":"SKIPPED"}]`,
			wantGreen: true,
			wantTotal: 3,
		},
		{
			name:      "empty output from gh",
			output:    "",
			wantGreen: false,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origRunFn := runFn
			t.Cleanup(func() { runFn = origRunFn })

			runFn = func(_ context.Context, _ string, args ...string) (string, error) {
				if tt.runErr != nil {
					return "", tt.runErr
				}
				return tt.output, nil
			}

			status, err := CheckCIStatus(context.Background(), "owner", "repo", 1)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status.AllGreen != tt.wantGreen {
				t.Errorf("AllGreen = %v, want %v", status.AllGreen, tt.wantGreen)
			}
			if status.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", status.Total, tt.wantTotal)
			}
		})
	}
}

func TestSubmitPRReview_NoComments_FallsBack(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	var capturedArgs []string
	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		capturedArgs = args
		return "", nil
	}

	err := SubmitPRReview(context.Background(), "owner", "repo", 42, "APPROVE", "Looks good", nil)
	if err != nil {
		t.Fatalf("SubmitPRReview() error = %v", err)
	}
	// Should fall back to SubmitReview (gh pr review)
	if len(capturedArgs) == 0 {
		t.Fatal("expected captured args from SubmitReview fallback")
	}
	found := false
	for _, a := range capturedArgs {
		if a == "--approve" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --approve flag in args, got %v", capturedArgs)
	}
}

func TestSubmitPRReview_WithComments(t *testing.T) {
	origFn := submitReviewFn
	t.Cleanup(func() { submitReviewFn = origFn })

	var capturedEndpoint string
	var capturedPayload []byte
	submitReviewFn = func(_ context.Context, endpoint string, payload []byte) (string, error) {
		capturedEndpoint = endpoint
		capturedPayload = payload
		return "", nil
	}

	comments := []InlineComment{
		{Path: "main.go", Line: 10, Body: "Missing nil check"},
		{Path: "util.go", Line: 25, Body: "Unused variable"},
	}

	err := SubmitPRReview(context.Background(), "owner", "repo", 42, "REQUEST_CHANGES", "Issues found", comments)
	if err != nil {
		t.Fatalf("SubmitPRReview() error = %v", err)
	}

	if capturedEndpoint != "repos/owner/repo/pulls/42/reviews" {
		t.Errorf("endpoint = %q, want %q", capturedEndpoint, "repos/owner/repo/pulls/42/reviews")
	}

	// Verify payload contains the comments
	payloadStr := string(capturedPayload)
	if !strings.Contains(payloadStr, "main.go") {
		t.Errorf("payload should contain 'main.go', got %q", payloadStr)
	}
	if !strings.Contains(payloadStr, "REQUEST_CHANGES") {
		t.Errorf("payload should contain 'REQUEST_CHANGES', got %q", payloadStr)
	}
}

func TestSubmitPRReview_InvalidEvent(t *testing.T) {
	err := SubmitPRReview(context.Background(), "owner", "repo", 42, "INVALID", "body", nil)
	if err == nil {
		t.Fatal("expected error for invalid event")
	}
	if !strings.Contains(err.Error(), "unknown review event") {
		t.Errorf("expected 'unknown review event' error, got %v", err)
	}
}

func TestSubmitPRReview_APIError(t *testing.T) {
	origFn := submitReviewFn
	t.Cleanup(func() { submitReviewFn = origFn })

	submitReviewFn = func(_ context.Context, endpoint string, payload []byte) (string, error) {
		return "", fmt.Errorf("gh api %s: 422: %w", endpoint, fmt.Errorf("unprocessable entity"))
	}

	comments := []InlineComment{{Path: "x.go", Line: 1, Body: "bug"}}
	err := SubmitPRReview(context.Background(), "owner", "repo", 1, "COMMENT", "body", comments)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "submitting PR review") {
		t.Errorf("expected 'submitting PR review' error, got %v", err)
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		url    string
		wantN  int
		wantOK bool
	}{
		{"https://github.com/owner/repo/pull/42", 42, true},
		{"https://github.com/org/my-repo/pull/1", 1, true},
		{"https://github.com/org/repo/pull/999", 999, true},
		{"https://github.com/org/repo/pull/abc", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		parts := strings.Split(tt.url, "/")
		if len(parts) < 2 {
			if tt.wantOK {
				t.Errorf("URL %q: split too short", tt.url)
			}
			continue
		}
		num, err := strconv.Atoi(parts[len(parts)-1])
		if tt.wantOK {
			if err != nil {
				t.Errorf("URL %q: unexpected error %v", tt.url, err)
			}
			if num != tt.wantN {
				t.Errorf("URL %q: got %d, want %d", tt.url, num, tt.wantN)
			}
		} else {
			if err == nil {
				t.Errorf("URL %q: expected error, got %d", tt.url, num)
			}
		}
	}
}
