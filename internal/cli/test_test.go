package cli

import (
	"testing"
)

func TestSplitNWO(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantNil   bool
	}{
		{"standard", "org/repo", "org", "repo", false},
		{"with-dashes", "my-org/my-repo", "my-org", "my-repo", false},
		{"empty", "", "", "", true},
		{"no-slash", "repo", "", "", true},
		{"leading-slash", "/repo", "", "", true},
		{"trailing-slash", "org/", "", "", true},
		{"only-slash", "/", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitNWO(tt.input)
			if tt.wantNil {
				if result != nil {
					t.Errorf("splitNWO(%q) = %v, want nil", tt.input, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("splitNWO(%q) = nil, want [%q, %q]", tt.input, tt.wantOwner, tt.wantRepo)
			}
			if result[0] != tt.wantOwner {
				t.Errorf("splitNWO(%q)[0] = %q, want %q", tt.input, result[0], tt.wantOwner)
			}
			if result[1] != tt.wantRepo {
				t.Errorf("splitNWO(%q)[1] = %q, want %q", tt.input, result[1], tt.wantRepo)
			}
		})
	}
}

func TestPRNumberFromEnv(t *testing.T) {
	// With no env set, should return 0
	got := prNumberFromEnv()
	if got != 0 {
		t.Errorf("prNumberFromEnv() = %d, want 0 (no env set)", got)
	}

	// With env set
	t.Setenv("GITHUB_PR_NUMBER", "42")
	got = prNumberFromEnv()
	if got != 42 {
		t.Errorf("prNumberFromEnv() = %d, want 42", got)
	}

	// With invalid env
	t.Setenv("GITHUB_PR_NUMBER", "abc")
	got = prNumberFromEnv()
	if got != 0 {
		t.Errorf("prNumberFromEnv() = %d, want 0 (invalid env)", got)
	}
}
