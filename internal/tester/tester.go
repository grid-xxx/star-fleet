package tester

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nullne/star-fleet/internal/agent"
)

// GHCommenter posts comments on GitHub issues/PRs.
type GHCommenter interface {
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
}

// CheckRunUpdater updates a GitHub Check Run status and output.
type CheckRunUpdater interface {
	// UpdateCheckRun updates a check run to the given status/conclusion with
	// an optional output payload. conclusion should be set when status is
	// "completed" (e.g. "success" or "failure").
	UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, status, conclusion string, output *CheckRunOutput) error
}

// CheckRunOutput holds the output fields for a GitHub Check Run.
type CheckRunOutput struct {
	Title   string
	Summary string
	Text    string
}

// Logger abstracts log output for the tester.
type Logger interface {
	Info(msg string)
	Success(msg string)
	Warn(msg string)
	Fail(msg string)
}

// Config holds configuration for the test orchestrator.
type Config struct {
	// RepoRoot is the root of the git repository to scan.
	RepoRoot string

	// TestCommand overrides auto-detected test commands.
	TestCommand string

	// Owner and Repo identify the GitHub repository (for PR comments).
	Owner string
	Repo  string

	// PRNumber, if non-zero, triggers a PR comment with the report.
	PRNumber int

	// Backend is the agent backend for test generation.
	Backend agent.Backend

	// GH is the GitHub client for posting comments.
	GH GHCommenter

	// Runner executes test commands. If nil, defaults to ExecRunner.
	Runner CommandRunner

	// CheckRun optionally updates a GitHub Check Run on completion.
	CheckRun CheckRunUpdater

	// CheckRunID is the ID of the check run to update. Only used when CheckRun is set.
	CheckRunID int64

	// Logger for status output. If nil, output is suppressed.
	Log Logger
}

// Run executes the full test orchestration pipeline:
//  1. Scan for testable modules
//  2. For each module, optionally generate/update tests via agent
//  3. Execute tests
//  4. Build and output report
//  5. Optionally post report as PR comment
func Run(ctx context.Context, cfg *Config) (*Report, error) {
	if cfg.Runner == nil {
		cfg.Runner = ExecRunner{}
	}

	log := cfg.Log
	if log == nil {
		log = nopLogger{}
	}

	// Step 1: Scan for modules
	log.Info("Scanning for testable modules...")
	modules, err := ScanModules(cfg.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("scanning modules: %w", err)
	}

	if len(modules) == 0 {
		log.Warn("No testable modules found (need tests.md + skills/usage.md)")
		report := BuildReport(nil, 0)
		return report, nil
	}

	log.Info(fmt.Sprintf("Found %d testable module(s)", len(modules)))

	start := time.Now()
	var results []TestResult

	for i, mod := range modules {
		log.Info(fmt.Sprintf("[%d/%d] Processing %s", i+1, len(modules), mod.RelPath))

		// Step 2: Agent-driven test generation
		agentGenerated := false
		if cfg.Backend != nil {
			generated, err := generateTests(ctx, cfg.Backend, mod, cfg.RepoRoot)
			if err != nil {
				log.Warn(fmt.Sprintf("  Agent test generation failed: %v", err))
			} else if generated {
				agentGenerated = true
				log.Info("  Agent updated test files")
			} else {
				log.Info("  Tests are up-to-date")
			}
		}

		// Step 3: Execute tests
		result := RunTests(ctx, cfg.Runner, mod, cfg.TestCommand)
		result.AgentGenerated = agentGenerated

		if result.Error != nil {
			log.Fail(fmt.Sprintf("  Error: %v", result.Error))
		} else if result.Passed {
			log.Success(fmt.Sprintf("  Passed (%s)", result.Duration.Round(time.Millisecond)))
		} else {
			log.Fail(fmt.Sprintf("  Failed (%s)", result.Duration.Round(time.Millisecond)))
		}

		results = append(results, result)
	}

	totalDuration := time.Since(start)

	// Step 4: Build report
	report := BuildReport(results, totalDuration)

	// Step 5: Post PR comment if configured
	if cfg.PRNumber > 0 && cfg.GH != nil && cfg.Owner != "" && cfg.Repo != "" {
		comment := report.FormatMarkdown()
		if err := cfg.GH.PostComment(ctx, cfg.Owner, cfg.Repo, cfg.PRNumber, comment); err != nil {
			log.Warn(fmt.Sprintf("Failed to post PR comment: %v", err))
		} else {
			log.Info(fmt.Sprintf("Posted report to PR #%d", cfg.PRNumber))
		}
	}

	// Step 6: Update Check Run if configured
	if cfg.CheckRun != nil && cfg.CheckRunID != 0 && cfg.Owner != "" && cfg.Repo != "" {
		conclusion := "success"
		title := "Fleet Test — All Passed"
		if !report.AllPassed {
			conclusion = "failure"
			title = fmt.Sprintf("Fleet Test — %d/%d modules failed",
				report.FailedModules+report.ErrorModules, report.TotalModules)
		}

		markdown := report.FormatMarkdown()
		output := &CheckRunOutput{
			Title:   title,
			Summary: title,
			Text:    markdown,
		}

		if err := cfg.CheckRun.UpdateCheckRun(ctx, cfg.Owner, cfg.Repo, cfg.CheckRunID, "completed", conclusion, output); err != nil {
			log.Warn(fmt.Sprintf("Failed to update check run: %v", err))
		} else {
			log.Info(fmt.Sprintf("Updated check run to %s", conclusion))
		}
	}

	return report, nil
}

// generateTests invokes the code agent to read tests.md, skills/usage.md, and
// existing test files, then generate or update tests as needed. Returns true
// if the agent wrote/modified any test files.
func generateTests(ctx context.Context, backend agent.Backend, mod Module, repoRoot string) (bool, error) {
	testsMD, err := os.ReadFile(mod.TestsMD)
	if err != nil {
		return false, fmt.Errorf("reading tests.md: %w", err)
	}

	usageMD, err := os.ReadFile(mod.UsageMD)
	if err != nil {
		return false, fmt.Errorf("reading skills/usage.md: %w", err)
	}

	// Collect existing test file contents for context
	var existingTests strings.Builder
	for _, tf := range mod.TestFiles {
		data, err := os.ReadFile(tf)
		if err != nil {
			continue
		}
		relPath := tf
		if rel, err := relPathFrom(repoRoot, tf); err == nil {
			relPath = rel
		}
		fmt.Fprintf(&existingTests, "\n### %s\n```\n%s\n```\n", relPath, string(data))
	}

	prompt := buildTestGenPrompt(mod.RelPath, string(testsMD), string(usageMD), existingTests.String())

	// Snapshot test files before agent runs
	beforeFiles := snapshotTestFiles(mod)

	if err := backend.Run(ctx, mod.Path, prompt, io.Discard); err != nil {
		return false, fmt.Errorf("agent run: %w", err)
	}

	// Check if any test files changed
	afterFiles := snapshotTestFiles(mod)
	return filesChanged(beforeFiles, afterFiles), nil
}

func buildTestGenPrompt(modPath, testsMD, usageMD, existingTests string) string {
	var b strings.Builder

	fmt.Fprintf(&b, `You are a senior QA engineer generating tests for the module at %q.

## Test Requirements (tests.md)

%s

## Module Usage Documentation (skills/usage.md)

%s
`, modPath, testsMD, usageMD)

	if existingTests != "" {
		fmt.Fprintf(&b, `
## Existing Test Files

%s
`, existingTests)
	}

	fmt.Fprintf(&b, `
## Instructions

1. Read the test requirements in tests.md carefully.
2. Read the usage documentation to understand the module's API and behavior.
3. Examine the existing test files (if any) and the module source code.
4. If the existing tests already fully cover the requirements in tests.md, do nothing.
5. If coverage is incomplete, write or update test files to cover all requirements.
6. Follow the existing code style and testing conventions of the project.
7. Commit any new or updated test files with a clear message like "test: add/update tests for <module>".
8. Do NOT modify the implementation code — only write tests.
`)

	return b.String()
}

// relPathFrom returns the relative path from base to target.
func relPathFrom(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

// snapshotTestFiles captures file sizes and modification times of test files.
func snapshotTestFiles(mod Module) map[string]fileSnapshot {
	snap := make(map[string]fileSnapshot)

	// Re-scan to pick up any new files the agent might have created
	files := findTestFiles(mod.Path)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		snap[f] = fileSnapshot{
			size:    info.Size(),
			modTime: info.ModTime(),
		}
	}
	return snap
}

type fileSnapshot struct {
	size    int64
	modTime time.Time
}

// filesChanged returns true if any files were added, removed, or modified.
func filesChanged(before, after map[string]fileSnapshot) bool {
	if len(before) != len(after) {
		return true
	}
	for path, bSnap := range before {
		aSnap, ok := after[path]
		if !ok {
			return true
		}
		if bSnap.size != aSnap.size || !bSnap.modTime.Equal(aSnap.modTime) {
			return true
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			return true
		}
	}
	return false
}

type nopLogger struct{}

func (nopLogger) Info(string)    {}
func (nopLogger) Success(string) {}
func (nopLogger) Warn(string)    {}
func (nopLogger) Fail(string)    {}
