package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
	"github.com/nullne/star-fleet/internal/tester"
)

var (
	testPRNumber   int
	testNoAgent    bool
	testRepoDir    string
	testCmdOverride string
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "AI-driven hierarchical test generation and execution",
	Long: `Scan the repository for testable modules, generate missing tests using a
Code Agent, execute them, and produce a structured report.

A "testable module" is any directory containing both:
  - tests.md       — test requirements / specifications
  - skills/usage.md — module usage documentation

The command will:
  1. Recursively scan the repo for testable modules.
  2. For each module, spawn a Code Agent to read tests.md, skills/usage.md,
     and existing test files, then write/update tests to cover requirements.
  3. Execute the tests and aggregate results.
  4. Print a report. If --pr is provided, also post it as a PR comment.

Use --no-agent to skip the AI test generation and only run existing tests.`,
	RunE: runTest,
}

func init() {
	testCmd.Flags().IntVar(&testPRNumber, "pr", 0, "PR number to post the report as a comment")
	testCmd.Flags().BoolVar(&testNoAgent, "no-agent", false, "skip AI test generation, only run existing tests")
	testCmd.Flags().StringVar(&testRepoDir, "dir", "", "repository root directory (defaults to current repo)")
	testCmd.Flags().StringVar(&testCmdOverride, "command", "", "override the test command (e.g. \"go test ./...\")")

	rootCmd.AddCommand(testCmd)
}

// ghCommenter wraps the package-level gh.PostComment for the tester interface.
type ghCommenter struct{}

func (ghCommenter) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	return gh.PostComment(ctx, owner, repo, number, body)
}

// cliLogger adapts terminal output for the tester.
type cliLogger struct{}

func (cliLogger) Info(msg string)    { fmt.Fprintf(os.Stderr, "  %s\n", msg) }
func (cliLogger) Success(msg string) { fmt.Fprintf(os.Stderr, "  ✓  %s\n", msg) }
func (cliLogger) Warn(msg string)    { fmt.Fprintf(os.Stderr, "  ⚠  %s\n", msg) }
func (cliLogger) Fail(msg string)    { fmt.Fprintf(os.Stderr, "  ✗  %s\n", msg) }

func runTest(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Determine repo root
	repoRoot := testRepoDir
	if repoRoot == "" {
		var err error
		repoRoot, err = git.RepoRoot(ctx)
		if err != nil {
			return fmt.Errorf("must be run inside a git repository: %w", err)
		}
	}

	// Load config
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	testCommand := testCmdOverride
	if testCommand == "" {
		testCommand = cfg.Test.Command
	}

	// Resolve owner/repo for PR commenting
	var owner, repo string
	if testPRNumber > 0 {
		owner, repo, err = resolveOwnerRepo(ctx)
		if err != nil {
			return fmt.Errorf("cannot determine repo for PR comment: %w", err)
		}
	}

	// Set up agent backend (unless --no-agent)
	var backend agent.Backend
	if !testNoAgent {
		backend, err = agent.NewBackend(cfg.Agent.Backend)
		if err != nil {
			return fmt.Errorf("creating agent backend: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "\n  ● Fleet Test — %s\n\n", repoRoot)

	testerCfg := &tester.Config{
		RepoRoot:    repoRoot,
		TestCommand: testCommand,
		Owner:       owner,
		Repo:        repo,
		PRNumber:    testPRNumber,
		Backend:     backend,
		GH:          ghCommenter{},
		Log:         cliLogger{},
	}

	report, err := tester.Run(ctx, testerCfg)
	if err != nil {
		return err
	}

	// Print terminal report
	fmt.Fprint(os.Stderr, "\n"+report.FormatTerminal()+"\n")

	if !report.AllPassed {
		return fmt.Errorf("test failures detected: %d/%d modules failed",
			report.FailedModules+report.ErrorModules, report.TotalModules)
	}

	return nil
}

// resolveOwnerRepo determines the GitHub owner/repo from the environment
// or the current repository.
func resolveOwnerRepo(ctx context.Context) (string, string, error) {
	// Check environment variables first (common in CI)
	if owner := os.Getenv("GITHUB_REPOSITORY_OWNER"); owner != "" {
		if repo := os.Getenv("GITHUB_REPOSITORY"); repo != "" {
			// GITHUB_REPOSITORY is "owner/repo"
			parts := splitNWO(repo)
			if parts != nil {
				return parts[0], parts[1], nil
			}
		}
	}

	// Fall back to gh CLI
	info, err := gh.CurrentRepo(ctx)
	if err != nil {
		return "", "", err
	}
	return info.Owner, info.Repo, nil
}

// splitNWO splits "owner/repo" into [owner, repo].
func splitNWO(nwo string) []string {
	for i, c := range nwo {
		if c == '/' {
			if i > 0 && i < len(nwo)-1 {
				return []string{nwo[:i], nwo[i+1:]}
			}
			return nil
		}
	}
	return nil
}

// prNumberFromEnv reads the PR number from CI environment variables.
func prNumberFromEnv() int {
	// GitHub Actions
	if v := os.Getenv("GITHUB_PR_NUMBER"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}
