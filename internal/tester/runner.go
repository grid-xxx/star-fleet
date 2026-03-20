package tester

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// TestResult captures the outcome of running tests for a single module.
type TestResult struct {
	// Module is the module that was tested.
	Module Module

	// Passed is true if all tests passed.
	Passed bool

	// Output is the combined stdout+stderr from the test command.
	Output string

	// Duration is how long the test execution took.
	Duration time.Duration

	// Error is set if the test command could not be started (not test failures).
	Error error

	// AgentGenerated is true if the agent generated or updated test files.
	AgentGenerated bool
}

// CommandRunner executes a shell command and returns its output.
// Abstracted for testing.
type CommandRunner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (output string, err error)
}

// ExecRunner is the default CommandRunner that shells out.
type ExecRunner struct{}

// Run executes the command and returns combined stdout+stderr.
func (ExecRunner) Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// RunTests executes the test suite for a module. It uses the provided test
// command (from config) or auto-detects based on project files.
func RunTests(ctx context.Context, runner CommandRunner, mod Module, testCommand string) TestResult {
	start := time.Now()

	name, args := resolveTestCommand(mod, testCommand)

	output, err := runner.Run(ctx, mod.Path, name, args...)
	duration := time.Since(start)

	if err != nil {
		// Distinguish between test failure (exit code != 0) and execution error.
		// Test failures produce output but the command runs fine; execution errors
		// mean the binary couldn't be found or similar.
		if isExitError(err) {
			return TestResult{
				Module:   mod,
				Passed:   false,
				Output:   output,
				Duration: duration,
			}
		}
		return TestResult{
			Module:   mod,
			Passed:   false,
			Output:   output,
			Duration: duration,
			Error:    err,
		}
	}

	return TestResult{
		Module:   mod,
		Passed:   true,
		Output:   output,
		Duration: duration,
	}
}

// isExitError returns true if the error represents a process that ran but
// exited with a non-zero status code. This covers both *exec.ExitError
// and any error type that implements ExitCode() int.
func isExitError(err error) bool {
	if _, ok := err.(*exec.ExitError); ok {
		return true
	}
	// Support interface-based detection for testability
	type exitCoder interface {
		ExitCode() int
	}
	if _, ok := err.(exitCoder); ok {
		return true
	}
	return false
}

// resolveTestCommand determines what test command to run for a module.
// If a custom command is configured, it parses it. Otherwise, it auto-detects
// based on files present in the module directory.
func resolveTestCommand(mod Module, testCommand string) (string, []string) {
	if testCommand != "" {
		return parseCommand(testCommand)
	}

	// Auto-detect by looking at existing test files and project markers
	for _, f := range mod.TestFiles {
		if strings.HasSuffix(f, "_test.go") {
			return "go", []string{"test", "-v", "-count=1", "./..."}
		}
	}

	// Fall back to go test if we see any .go files
	if hasGoFiles(mod.Path) {
		return "go", []string{"test", "-v", "-count=1", "./..."}
	}

	// Default to go test
	return "go", []string{"test", "-v", "-count=1", "./..."}
}

// hasGoFiles returns true if the directory contains any .go files.
func hasGoFiles(dir string) bool {
	entries, err := exec.Command("find", dir, "-name", "*.go", "-maxdepth", "3", "-print", "-quit").Output()
	if err != nil {
		return false
	}
	return len(bytes.TrimSpace(entries)) > 0
}

// parseCommand splits a command string into name and args.
func parseCommand(cmd string) (string, []string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "go", []string{"test", "-v", "-count=1", "./..."}
	}
	return parts[0], parts[1:]
}

// SummarizeOutput trims test output to a reasonable length for display.
func SummarizeOutput(output string, maxLines int) string {
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}

	// Keep the first few and last few lines
	head := maxLines / 3
	tail := maxLines - head - 1

	var b strings.Builder
	for i := 0; i < head; i++ {
		fmt.Fprintln(&b, lines[i])
	}
	fmt.Fprintf(&b, "\n... (%d lines omitted) ...\n\n", len(lines)-head-tail)
	for i := len(lines) - tail; i < len(lines); i++ {
		fmt.Fprintln(&b, lines[i])
	}
	return b.String()
}
