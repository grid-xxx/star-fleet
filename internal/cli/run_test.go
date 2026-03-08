package cli

import (
	"context"
	"testing"
)

func TestParseIssueRef_NWO(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantErr   bool
	}{
		{
			name:      "simple",
			input:     "grid-xxx/star-fleet#42",
			wantOwner: "grid-xxx",
			wantRepo:  "star-fleet",
			wantNum:   42,
		},
		{
			name:      "single digit",
			input:     "owner/repo#1",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantNum:   1,
		},
		{
			name:      "large number",
			input:     "my-org/my-repo#99999",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
			wantNum:   99999,
		},
		{
			name:      "repo with dots",
			input:     "org/repo.go#7",
			wantOwner: "org",
			wantRepo:  "repo.go",
			wantNum:   7,
		},
		{
			name:    "missing number",
			input:   "org/repo#",
			wantErr: true,
		},
		{
			name:    "missing hash",
			input:   "org/repo",
			wantErr: true,
		},
		{
			name:    "no slash",
			input:   "repo#42",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "hash only",
			input:   "#42",
			wantErr: true,
		},
		{
			name:    "non-numeric issue number",
			input:   "org/repo#abc",
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := parseIssueRef(ctx, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got %+v", tt.input, ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", ref.Owner, tt.wantOwner)
			}
			if ref.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", ref.Repo, tt.wantRepo)
			}
			if ref.Number != tt.wantNum {
				t.Errorf("Number = %d, want %d", ref.Number, tt.wantNum)
			}
		})
	}
}

func TestParseIssueRef_URL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantNum   int
		wantErr   bool
	}{
		{
			name:      "standard URL",
			input:     "https://github.com/grid-xxx/star-fleet/issues/42",
			wantOwner: "grid-xxx",
			wantRepo:  "star-fleet",
			wantNum:   42,
		},
		{
			name:      "URL without scheme",
			input:     "github.com/org/repo/issues/7",
			wantOwner: "org",
			wantRepo:  "repo",
			wantNum:   7,
		},
		{
			name:      "URL with query params",
			input:     "https://github.com/org/repo/issues/10?foo=bar",
			wantOwner: "org",
			wantRepo:  "repo",
			wantNum:   10,
		},
		{
			name:      "URL with fragment",
			input:     "https://github.com/org/repo/issues/5#issuecomment-123",
			wantOwner: "org",
			wantRepo:  "repo",
			wantNum:   5,
		},
		{
			name:    "URL to pull request not issues",
			input:   "https://github.com/org/repo/pull/42",
			wantErr: true,
		},
		{
			name:    "URL missing issue number",
			input:   "https://github.com/org/repo/issues/",
			wantErr: true,
		},
		{
			name:    "URL with non-numeric issue",
			input:   "https://github.com/org/repo/issues/abc",
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := parseIssueRef(ctx, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got %+v", tt.input, ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ref.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", ref.Owner, tt.wantOwner)
			}
			if ref.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", ref.Repo, tt.wantRepo)
			}
			if ref.Number != tt.wantNum {
				t.Errorf("Number = %d, want %d", ref.Number, tt.wantNum)
			}
		})
	}
}

func TestParseIssueRef_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"random text", "just-some-text"},
		{"special characters", "!@#$%"},
		{"double hash", "org/repo##42"},
		{"spaces", "org / repo # 42"},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseIssueRef(ctx, tt.input)
			if err == nil {
				t.Fatalf("expected error for input %q", tt.input)
			}
		})
	}
}

func TestIsInteractive(t *testing.T) {
	// isInteractive should return a bool without panicking. The actual
	// value depends on how tests are invoked (piped vs terminal), so we
	// just verify it runs and returns a consistent result.
	got := isInteractive()
	if got != isInteractive() {
		t.Error("isInteractive() returned inconsistent results across calls")
	}
}
