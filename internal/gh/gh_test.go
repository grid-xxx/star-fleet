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
