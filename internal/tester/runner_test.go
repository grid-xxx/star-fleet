package tester

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// mockRunner is a test double for CommandRunner.
type mockRunner struct {
	run func(ctx context.Context, dir string, name string, args ...string) (string, error)
}

func (m *mockRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	if m.run != nil {
		return m.run(ctx, dir, name, args...)
	}
	return "ok", nil
}

func TestRunTests_Pass(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "PASS\nok  pkg/auth  0.5s\n", nil
		},
	}

	mod := Module{
		Path:    "/tmp/pkg/auth",
		RelPath: "pkg/auth",
	}

	result := RunTests(context.Background(), runner, mod, "")
	if !result.Passed {
		t.Error("RunTests() Passed = false, want true")
	}
	if result.Error != nil {
		t.Errorf("RunTests() Error = %v, want nil", result.Error)
	}
	if !strings.Contains(result.Output, "PASS") {
		t.Errorf("RunTests() Output should contain 'PASS', got %q", result.Output)
	}
}

func TestRunTests_Fail(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "FAIL\n", &exitError{code: 1}
		},
	}

	mod := Module{
		Path:    "/tmp/pkg/auth",
		RelPath: "pkg/auth",
	}

	result := RunTests(context.Background(), runner, mod, "")
	if result.Passed {
		t.Error("RunTests() Passed = true, want false")
	}
	if result.Error != nil {
		t.Errorf("RunTests() Error = %v, want nil (test failures are not errors)", result.Error)
	}
}

func TestRunTests_CommandError(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "", fmt.Errorf("command not found: go")
		},
	}

	mod := Module{
		Path:    "/tmp/pkg/auth",
		RelPath: "pkg/auth",
	}

	result := RunTests(context.Background(), runner, mod, "")
	if result.Passed {
		t.Error("RunTests() Passed = true, want false")
	}
	if result.Error == nil {
		t.Error("RunTests() Error = nil, want non-nil")
	}
}

func TestRunTests_CustomCommand(t *testing.T) {
	t.Parallel()

	var capturedName string
	var capturedArgs []string

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			capturedName = name
			capturedArgs = args
			return "ok", nil
		},
	}

	mod := Module{
		Path:    "/tmp/pkg",
		RelPath: "pkg",
	}

	RunTests(context.Background(), runner, mod, "pytest -xvs tests/")
	if capturedName != "pytest" {
		t.Errorf("command name = %q, want %q", capturedName, "pytest")
	}
	want := []string{"-xvs", "tests/"}
	if len(capturedArgs) != len(want) {
		t.Fatalf("args = %v, want %v", capturedArgs, want)
	}
	for i := range want {
		if capturedArgs[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, capturedArgs[i], want[i])
		}
	}
}

func TestRunTests_DefaultGoCommand(t *testing.T) {
	t.Parallel()

	var capturedName string
	var capturedArgs []string

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			capturedName = name
			capturedArgs = args
			return "ok", nil
		},
	}

	mod := Module{
		Path:    "/tmp/pkg",
		RelPath: "pkg",
	}

	RunTests(context.Background(), runner, mod, "")
	if capturedName != "go" {
		t.Errorf("command name = %q, want %q", capturedName, "go")
	}
	if len(capturedArgs) < 1 || capturedArgs[0] != "test" {
		t.Errorf("expected 'go test ...', got %v", capturedArgs)
	}
}

func TestRunTests_GoTestDetectedByTestFiles(t *testing.T) {
	t.Parallel()

	var capturedName string
	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			capturedName = name
			return "ok", nil
		},
	}

	mod := Module{
		Path:      "/tmp/pkg",
		RelPath:   "pkg",
		TestFiles: []string{"/tmp/pkg/foo_test.go"},
	}

	RunTests(context.Background(), runner, mod, "")
	if capturedName != "go" {
		t.Errorf("auto-detected command = %q, want 'go'", capturedName)
	}
}

func TestRunTests_Duration(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "ok", nil
		},
	}

	mod := Module{Path: "/tmp/pkg", RelPath: "pkg"}
	result := RunTests(context.Background(), runner, mod, "")
	if result.Duration < 10*time.Millisecond {
		t.Errorf("Duration = %v, want >= 10ms", result.Duration)
	}
}

func TestRunTests_ContextDir(t *testing.T) {
	t.Parallel()

	var capturedDir string
	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			capturedDir = dir
			return "ok", nil
		},
	}

	mod := Module{Path: "/repo/src/pkg", RelPath: "src/pkg"}
	RunTests(context.Background(), runner, mod, "")
	if capturedDir != "/repo/src/pkg" {
		t.Errorf("working dir = %q, want %q", capturedDir, "/repo/src/pkg")
	}
}

func TestParseCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		wantName string
		wantArgs []string
	}{
		{"go test ./...", "go", []string{"test", "./..."}},
		{"pytest -xvs", "pytest", []string{"-xvs"}},
		{"npm test", "npm", []string{"test"}},
		{"make check", "make", []string{"check"}},
		{"single", "single", nil},
		{"", "go", []string{"test", "-v", "-count=1", "./..."}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			name, args := parseCommand(tt.input)
			if name != tt.wantName {
				t.Errorf("parseCommand(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
			if len(args) != len(tt.wantArgs) {
				t.Errorf("parseCommand(%q) args = %v, want %v", tt.input, args, tt.wantArgs)
				return
			}
			for i := range tt.wantArgs {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("parseCommand(%q) args[%d] = %q, want %q", tt.input, i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestSummarizeOutput_Short(t *testing.T) {
	t.Parallel()
	output := "line1\nline2\nline3\n"
	got := SummarizeOutput(output, 10)
	if got != output {
		t.Errorf("SummarizeOutput() should return full output when under limit")
	}
}

func TestSummarizeOutput_Long(t *testing.T) {
	t.Parallel()
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	output := strings.Join(lines, "\n")

	got := SummarizeOutput(output, 10)
	if len(strings.Split(got, "\n")) >= 100 {
		t.Error("SummarizeOutput() should truncate long output")
	}
	if !strings.Contains(got, "omitted") {
		t.Error("SummarizeOutput() should indicate omitted lines")
	}
	if !strings.Contains(got, "line 0") {
		t.Error("SummarizeOutput() should include first lines")
	}
	if !strings.Contains(got, "line 99") {
		t.Error("SummarizeOutput() should include last lines")
	}
}

// exitError implements the error interface and os/exec.ExitError behavior
// for testing purposes.
type exitError struct {
	code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

// ExitCode satisfies the exec.ExitError interface pattern.
func (e *exitError) ExitCode() int {
	return e.code
}
