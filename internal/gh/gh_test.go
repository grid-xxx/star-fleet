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

func TestGetAuthenticatedUser(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		return "octocat\n", nil
	}

	user, err := GetAuthenticatedUser(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != "octocat" {
		t.Errorf("user = %q, want %q", user, "octocat")
	}
}

func TestGetPRAuthor(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		return "pr-author\n", nil
	}

	author, err := GetPRAuthor(context.Background(), "owner", "repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if author != "pr-author" {
		t.Errorf("author = %q, want %q", author, "pr-author")
	}
}

func TestSubmitReview_FallbackToCommentOnOwnPR(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	var reviewCalls [][]string

	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		// GetAuthenticatedUser call
		if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
			return "octocat\n", nil
		}
		// GetPRAuthor call
		if len(args) >= 2 && args[0] == "pr" && args[1] == "view" && containsArg(args, "--json") {
			return "octocat\n", nil
		}
		// pr review call
		if len(args) >= 2 && args[0] == "pr" && args[1] == "review" {
			reviewCalls = append(reviewCalls, args)
			return "", nil
		}
		return "", fmt.Errorf("unexpected call: %s", joined)
	}

	err := SubmitReview(context.Background(), "owner", "repo", 42, "REQUEST_CHANGES", "needs work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviewCalls) != 1 {
		t.Fatalf("expected 1 review call, got %d", len(reviewCalls))
	}
	if !containsArg(reviewCalls[0], "--comment") {
		t.Errorf("expected --comment flag in review call, got %v", reviewCalls[0])
	}
	if containsArg(reviewCalls[0], "--request-changes") {
		t.Errorf("should NOT have --request-changes flag when reviewing own PR, got %v", reviewCalls[0])
	}
}

func TestSubmitReview_RequestChangesOnDifferentAuthor(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	var reviewCalls [][]string

	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
			return "reviewer-bot\n", nil
		}
		if len(args) >= 2 && args[0] == "pr" && args[1] == "view" && containsArg(args, "--json") {
			return "pr-author\n", nil
		}
		if len(args) >= 2 && args[0] == "pr" && args[1] == "review" {
			reviewCalls = append(reviewCalls, args)
			return "", nil
		}
		return "", fmt.Errorf("unexpected call: %s", joined)
	}

	err := SubmitReview(context.Background(), "owner", "repo", 42, "REQUEST_CHANGES", "needs work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviewCalls) != 1 {
		t.Fatalf("expected 1 review call, got %d", len(reviewCalls))
	}
	if !containsArg(reviewCalls[0], "--request-changes") {
		t.Errorf("expected --request-changes flag, got %v", reviewCalls[0])
	}
}

func TestSubmitReview_RuntimeFallbackOnOwnPRError(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	var reviewCalls [][]string
	callCount := 0

	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
			return "", fmt.Errorf("api error")
		}
		if len(args) >= 2 && args[0] == "pr" && args[1] == "review" {
			callCount++
			reviewCalls = append(reviewCalls, args)
			if callCount == 1 && containsArg(args, "--request-changes") {
				return "", fmt.Errorf("Review Can not request changes on your own pull request")
			}
			return "", nil
		}
		return "", fmt.Errorf("unexpected call: %s", joined)
	}

	err := SubmitReview(context.Background(), "owner", "repo", 42, "REQUEST_CHANGES", "needs work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviewCalls) != 2 {
		t.Fatalf("expected 2 review calls (original + fallback), got %d", len(reviewCalls))
	}
	if !containsArg(reviewCalls[0], "--request-changes") {
		t.Errorf("first call should use --request-changes, got %v", reviewCalls[0])
	}
	if !containsArg(reviewCalls[1], "--comment") {
		t.Errorf("second call should use --comment, got %v", reviewCalls[1])
	}
}

func TestSubmitReview_CommentEventUnchanged(t *testing.T) {
	origRunFn := runFn
	t.Cleanup(func() { runFn = origRunFn })

	var reviewCalls [][]string

	runFn = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "pr" && args[1] == "review" {
			reviewCalls = append(reviewCalls, args)
			return "", nil
		}
		return "", nil
	}

	err := SubmitReview(context.Background(), "owner", "repo", 42, "COMMENT", "looks good")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviewCalls) != 1 {
		t.Fatalf("expected 1 review call, got %d", len(reviewCalls))
	}
	if !containsArg(reviewCalls[0], "--comment") {
		t.Errorf("expected --comment flag, got %v", reviewCalls[0])
	}
}

func TestIsOwnPRError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("Review Can not request changes on your own pull request"), true},
		{fmt.Errorf("review can not request changes on your own pull request"), true},
		{fmt.Errorf("some other error"), false},
		{nil, false},
	}
	for _, tt := range tests {
		got := isOwnPRError(tt.err)
		if got != tt.want {
			t.Errorf("isOwnPRError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
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
