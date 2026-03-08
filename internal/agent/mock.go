package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MockBackend simulates agent work by creating real Go source files and
// committing them. This lets you test the full GitHub pipeline (push, PR,
// watch loop) without calling any LLM.
//
// It is hardcoded for the default integration-test issue (strutil package).
// Use backend = "mock" in config or FLEET_TEST_BACKEND=mock.
type MockBackend struct{}

func (m *MockBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	log := func(msg string) {
		if output != nil {
			fmt.Fprintln(output, msg)
		}
	}

	switch {
	case strings.Contains(prompt, reviewOutputFile):
		// Watch handler asking for event response
		if strings.Contains(prompt, "CI Failure") {
			log("mock: writing event response → fix")
			return os.WriteFile(
				filepath.Join(workdir, reviewOutputFile),
				[]byte("Fixed the CI failure in the latest push."),
				0o644)
		}
		log("mock: writing event response → NO_ACTION")
		return os.WriteFile(
			filepath.Join(workdir, reviewOutputFile),
			[]byte("NO_ACTION"),
			0o644)

	case strings.Contains(prompt, "Write both the implementation AND tests"):
		log("mock: creating implementation + tests...")
		time.Sleep(500 * time.Millisecond)
		if err := m.writeImpl(workdir); err != nil {
			return err
		}
		if err := m.writeTests(workdir); err != nil {
			return err
		}
		log("mock: committed implementation + tests")
		return m.commitAll(ctx, workdir, "feat: add strutil package with tests")

	case strings.Contains(prompt, "fixing issues found"):
		log("mock: no-op (fix prompt)")
		return nil

	default:
		log("mock: no-op (unknown prompt)")
		return nil
	}
}

func (m *MockBackend) writeImpl(workdir string) error {
	dir := filepath.Join(workdir, "pkg", "strutil")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	src := `package strutil

import "strings"

// Reverse returns the input string reversed, handling multi-byte runes correctly.
func Reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// IsPalindrome reports whether s reads the same forwards and backwards (case-insensitive).
func IsPalindrome(s string) bool {
	lower := strings.ToLower(s)
	return lower == Reverse(lower)
}
`
	return os.WriteFile(filepath.Join(dir, "strutil.go"), []byte(src), 0o644)
}

func (m *MockBackend) writeTests(workdir string) error {
	dir := filepath.Join(workdir, "pkg", "strutil")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	src := `package strutil

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "olleh"},
		{"世界", "界世"},
		{"", ""},
		{"a", "a"},
	}
	for _, tt := range tests {
		if got := Reverse(tt.input); got != tt.want {
			t.Errorf("Reverse(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsPalindrome(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"racecar", true},
		{"hello", false},
		{"Racecar", true},
		{"", true},
		{"a", true},
	}
	for _, tt := range tests {
		if got := IsPalindrome(tt.input); got != tt.want {
			t.Errorf("IsPalindrome(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
`
	return os.WriteFile(filepath.Join(dir, "strutil_test.go"), []byte(src), 0o644)
}

func (m *MockBackend) commitAll(ctx context.Context, workdir, message string) error {
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-m", message},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %s: %w", args[0], out, err)
		}
	}
	return nil
}
