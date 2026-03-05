package validate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nullne/star-fleet/internal/git"
)

type Result struct {
	Passed     bool
	Output     string
	FailCount  int
	TotalCount int
	// Attribution is "dev" or "test" indicating which agent likely caused failures
	Attribution string
}

func CrossValidate(ctx context.Context, repoRoot, devBranch, testBranch, baseBranch, testCmd string) (*Result, error) {
	valDir, err := git.CreateWorktree(ctx, repoRoot, "validate", baseBranch)
	if err != nil {
		return nil, fmt.Errorf("creating validate worktree: %w", err)
	}

	if err := git.Merge(ctx, valDir, devBranch); err != nil {
		return nil, fmt.Errorf("merging dev branch: %w", err)
	}

	if err := git.Merge(ctx, valDir, testBranch); err != nil {
		return nil, fmt.Errorf("merging test branch: %w", err)
	}

	devTestFiles, err := git.DiffNames(ctx, valDir, baseBranch, devBranch,
		"*_test.*", "test_*", "*_test/*", "tests/*")
	if err != nil {
		// Non-fatal: maybe no test files from dev
		devTestFiles = nil
	}

	if len(devTestFiles) > 0 {
		if err := git.RemoveFiles(ctx, valDir, devTestFiles); err != nil {
			return nil, fmt.Errorf("stripping dev test files: %w", err)
		}
	}

	if testCmd == "" {
		testCmd = detectTestCommand(valDir)
	}

	output, passed, err := runTests(ctx, valDir, testCmd)
	if err != nil && passed {
		return nil, fmt.Errorf("running tests: %w", err)
	}

	result := &Result{
		Passed: passed,
		Output: output,
	}

	if !passed {
		result.FailCount, result.TotalCount = parseTestCounts(output)
		result.Attribution = attributeFailures(output, devTestFiles)
	}

	return result, nil
}

func Cleanup(ctx context.Context, repoRoot string) {
	_ = git.RemoveWorktree(ctx, repoRoot, "validate")
}

func detectTestCommand(dir string) string {
	checks := []struct {
		file string
		cmd  string
	}{
		{"go.mod", "go test ./..."},
		{"package.json", "npm test"},
		{"Cargo.toml", "cargo test"},
		{"pyproject.toml", "python -m pytest"},
		{"setup.py", "python -m pytest"},
		{"requirements.txt", "python -m pytest"},
		{"Makefile", "make test"},
		{"Gemfile", "bundle exec rspec"},
		{"build.gradle", "./gradlew test"},
		{"pom.xml", "mvn test"},
	}

	for _, c := range checks {
		cmd := exec.Command("test", "-f", c.file)
		cmd.Dir = dir
		if cmd.Run() == nil {
			return c.cmd
		}
	}
	return "make test"
}

func runTests(ctx context.Context, dir, testCmd string) (string, bool, error) {
	parts := strings.Fields(testCmd)
	if len(parts) == 0 {
		return "", false, fmt.Errorf("empty test command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = dir
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined

	err := cmd.Run()
	output := combined.String()
	passed := err == nil

	return output, passed, nil
}

func parseTestCounts(output string) (failed, total int) {
	// Best-effort parsing across common test frameworks.
	// Go: "FAIL" lines, "ok" lines
	// pytest: "X failed, Y passed"
	// jest: "Tests: X failed, Y passed"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fail") {
			failed++
		}
		total++
	}
	if failed == 0 && !strings.Contains(strings.ToLower(output), "pass") {
		failed = 1
	}
	return failed, total
}

func attributeFailures(output string, devTestFiles []string) string {
	// If dev authored test files that were stripped but tests still fail,
	// the failures are in the test agent's tests running against dev code.
	// Heuristic: if the failure output mentions test file names from the
	// test branch, attribute to "dev" (implementation bug). Otherwise "test".
	lower := strings.ToLower(output)
	if strings.Contains(lower, "undefined") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "cannot find") ||
		strings.Contains(lower, "import") {
		return "dev"
	}
	if strings.Contains(lower, "expected") ||
		strings.Contains(lower, "assert") {
		return "dev"
	}
	return "test"
}
