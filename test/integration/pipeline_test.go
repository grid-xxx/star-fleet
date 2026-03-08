// Integration tests for the full Star Fleet pipeline.
//
// Prerequisites:
//
//	gh CLI installed and authenticated (with delete_repo scope for auto-cleanup)
//	claude (claude-code backend) or cursor CLI installed
//
// Setup (one-time):
//
//	gh auth refresh -s delete_repo    # needed for auto-cleanup of test repos
//
// Run:
//
//	# Smoke test — verify tools (~5s, no tokens used)
//	go test -v -run TestIntegrationSmoke ./test/integration/
//
//	# Full pipeline — auto-creates a GitHub repo, runs the agent (~5-20 min)
//	go test -v -count=1 -timeout=30m -run TestIntegrationFullPipeline ./test/integration/
//
//	# Keep repo after test for manual inspection
//	FLEET_TEST_KEEP=1 go test -v -count=1 -timeout=30m ./test/integration/
//
// Environment variables:
//
//	FLEET_TEST_BACKEND   "claude-code" (default) or "cursor"
//	FLEET_TEST_KEEP      "1" to preserve the test repo and all artifacts for inspection
//	FLEET_TEST_REPO      Use an existing repo (owner/repo) instead of creating one
//	FLEET_TEST_ISSUE     Override the default test issue body
package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/orchestrator"
	"github.com/nullne/star-fleet/internal/ui"
)

const defaultIssueTitle = "[fleet-test] Add string utility"

const defaultIssueBody = `## Task

Add a simple string utility package.

### Requirements

1. Create a new file ` + "`pkg/strutil/strutil.go`" + ` (package ` + "`strutil`" + `)
2. Implement ` + "`Reverse(s string) string`" + ` that reverses a UTF-8 string correctly (rune-aware)
3. Implement ` + "`IsPalindrome(s string) bool`" + ` that checks if a string reads the same forwards and backwards (case-insensitive)

### Acceptance Criteria

- ` + "`Reverse(\"hello\")`" + ` returns ` + "`\"olleh\"`" + `
- ` + "`Reverse(\"世界\")`" + ` returns ` + "`\"界世\"`" + `
- ` + "`IsPalindrome(\"racecar\")`" + ` returns ` + "`true`" + `
- ` + "`IsPalindrome(\"hello\")`" + ` returns ` + "`false`" + `
- ` + "`IsPalindrome(\"Racecar\")`" + ` returns ` + "`true`" + ` (case-insensitive)
`

// ---------------------------------------------------------------------------
// TestIntegrationSmoke — quick check that all tools are available.
// No repo needed, no tokens used. ~5 seconds.
// ---------------------------------------------------------------------------

func TestIntegrationSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	requireCommand(t, "gh")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ghMust(t, ctx, "", "auth", "status")
	t.Log("gh auth: OK")

	user := ghUser(t, ctx)
	t.Logf("gh user: %s", user)

	backend := envOr("FLEET_TEST_BACKEND", "claude-code")
	bin := backendCommand(backend)
	requireCommand(t, bin)

	out, _ := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	t.Logf("%s: %s", bin, strings.TrimSpace(string(out)))

	t.Log("smoke: all prerequisites met ✓")
}

// ---------------------------------------------------------------------------
// TestIntegrationFullPipeline — end-to-end pipeline test.
//
// Creates a fresh GitHub repo (or uses FLEET_TEST_REPO), creates a test issue,
// runs the single agent, creates PR, and verifies delivery.
//
// Set FLEET_TEST_KEEP=1 to preserve the repo and PRs for manual inspection.
// ---------------------------------------------------------------------------

func TestIntegrationFullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	backend := envOr("FLEET_TEST_BACKEND", "claude-code")
	keep := shouldKeep()

	requireCommand(t, "gh")
	requireCommand(t, backendCommand(backend))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// --- Setup repo ---
	owner, repo, repoDir, repoCreated := setupRepo(t, ctx, backend)
	nwo := owner + "/" + repo
	t.Logf("repo: https://github.com/%s", nwo)
	t.Logf("local clone: %s", repoDir)

	// --- Create test issue ---
	issueBody := defaultIssueBody
	if custom := os.Getenv("FLEET_TEST_ISSUE"); custom != "" {
		issueBody = custom
	}
	issueNum := createIssue(t, ctx, nwo, defaultIssueTitle, issueBody)
	t.Logf("issue: https://github.com/%s/issues/%d", nwo, issueNum)

	// --- Register cleanup ---
	t.Cleanup(func() {
		cctx, cc := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cc()
		teardown(t, cctx, owner, repo, repoDir, issueNum, repoCreated, keep)
	})

	// --- Build config ---
	cfg := &config.Config{
		Agent: config.AgentConfig{Backend: backend},
		Watch: config.WatchConfig{MaxFixRounds: 2},
		CI:    config.CIConfig{Enabled: true},
		Test:  config.TestConfig{Command: "go test ./..."},
	}

	// --- Run pipeline (no-watch for integration tests) ---
	t.Log("─── pipeline start ───")
	start := time.Now()

	o := &orchestrator.Orchestrator{
		Owner:    owner,
		Repo:     repo,
		Number:   issueNum,
		Config:   cfg,
		Display:  ui.New(),
		RepoRoot: repoDir,
		NoWatch:  true,
	}
	err := o.Run(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("pipeline failed after %s: %v", elapsed.Round(time.Second), err)
	}
	t.Logf("─── pipeline completed in %s ───", elapsed.Round(time.Second))

	// --- Verify ---
	verifyIssue(t, ctx, nwo, issueNum)
	prNum := verifyPR(t, ctx, nwo, issueNum)

	if keep {
		t.Log("")
		t.Log("═══ FLEET_TEST_KEEP: artifacts preserved ═══")
		t.Logf("  Repo:   https://github.com/%s", nwo)
		t.Logf("  Issue:  https://github.com/%s/issues/%d", nwo, issueNum)
		if prNum > 0 {
			t.Logf("  PR:     https://github.com/%s/pull/%d", nwo, prNum)
		}
		if repoCreated {
			t.Logf("")
			t.Logf("  To clean up:")
			t.Logf("    gh repo delete %s --yes", nwo)
		}
		t.Log("═══════════════════════════════════════════")
	}
}

// ===========================================================================
// Repo setup
// ===========================================================================

// setupRepo either uses an existing repo (FLEET_TEST_REPO) or creates a fresh
// one via gh. Returns owner, repo name, local clone path, and whether the repo
// was created by us.
func setupRepo(t *testing.T, ctx context.Context, backend string) (owner, repo, repoDir string, created bool) {
	t.Helper()

	if nwo := os.Getenv("FLEET_TEST_REPO"); nwo != "" {
		parts := strings.SplitN(nwo, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			t.Fatalf("FLEET_TEST_REPO must be owner/repo, got %q", nwo)
		}
		owner, repo = parts[0], parts[1]
		repoDir = cloneRepo(t, ctx, nwo)
		return owner, repo, repoDir, false
	}

	// Auto-create a test repo
	owner = ghUser(t, ctx)
	repo = fmt.Sprintf("fleet-test-%d", time.Now().Unix())
	nwo := owner + "/" + repo

	t.Logf("creating repo %s ...", nwo)
	ghMust(t, ctx, "", "repo", "create", nwo,
		"--public",
		"--add-readme",
		"--description", "Star Fleet integration test (auto-generated)")

	// Brief pause for GitHub to provision the repo
	time.Sleep(2 * time.Second)

	repoDir = cloneRepo(t, ctx, nwo)
	initGoProject(t, ctx, repoDir, nwo)

	return owner, repo, repoDir, true
}

func initGoProject(t *testing.T, ctx context.Context, dir, nwo string) {
	t.Helper()

	goMod := fmt.Sprintf("module github.com/%s\n\ngo 1.22\n", nwo)
	writeFile(t, filepath.Join(dir, "go.mod"), goMod)

	mainGo := "package main\n\nfunc main() {}\n"
	writeFile(t, filepath.Join(dir, "main.go"), mainGo)

	gitRun(t, ctx, dir, "add", "-A")
	gitRun(t, ctx, dir, "commit", "-m", "init: Go project skeleton")
	gitRun(t, ctx, dir, "push")

	t.Log("initialized Go project skeleton")
}

// ===========================================================================
// Verification
// ===========================================================================

func verifyIssue(t *testing.T, ctx context.Context, nwo string, issueNum int) {
	t.Helper()
	type issueInfo struct {
		State string `json:"state"`
	}
	s := ghJSON[issueInfo](t, ctx, "issue", "view", strconv.Itoa(issueNum),
		"--repo", nwo, "--json", "state")
	// Issue should still be OPEN — it auto-closes when the PR is merged
	if s.State != "OPEN" {
		t.Logf("verify: issue #%d state = %s (expected OPEN until PR is merged)", issueNum, s.State)
	} else {
		t.Logf("verify: issue #%d OPEN (will close on PR merge) ✓", issueNum)
	}
}

// verifyPR checks the PR exists and has meaningful content.
// Returns the PR number (0 if not found).
func verifyPR(t *testing.T, ctx context.Context, nwo string, issueNum int) int {
	t.Helper()
	branch := fmt.Sprintf("fleet/%d", issueNum)

	type prInfo struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Body   string `json:"body"`
	}
	prs := ghJSON[[]prInfo](t, ctx, "pr", "list",
		"--repo", nwo, "--head", branch, "--json", "number,state,body")

	if len(prs) == 0 {
		t.Fatalf("no PR found for branch %s", branch)
	}
	pr := prs[0]
	t.Logf("verify: PR #%d (state: %s) ✓", pr.Number, pr.State)

	// Body should reference the issue for auto-close
	closesRef := fmt.Sprintf("Closes #%d", issueNum)
	if !strings.Contains(pr.Body, closesRef) {
		t.Errorf("PR body missing %q for auto-close", closesRef)
	} else {
		t.Logf("verify: PR body contains %q ✓", closesRef)
	}

	// Diff should have substance
	diff := ghMust(t, ctx, "", "pr", "diff", strconv.Itoa(pr.Number), "--repo", nwo)
	added := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		}
	}
	if added < 5 {
		t.Errorf("PR diff has only %d added lines, expected substantial changes", added)
	} else {
		t.Logf("verify: PR diff — %d added lines ✓", added)
	}

	return pr.Number
}

// ===========================================================================
// Teardown
// ===========================================================================

func teardown(t *testing.T, ctx context.Context, owner, repo, repoDir string, issueNum int, repoCreated, keep bool) {
	t.Helper()
	nwo := owner + "/" + repo

	if keep {
		t.Logf("FLEET_TEST_KEEP: skipping cleanup for %s", nwo)
		return
	}

	t.Log("─── cleanup ───")

	if repoCreated {
		// Deleting the repo removes everything (branches, PRs, issues)
		t.Logf("  deleting repo %s", nwo)
		cmd := exec.CommandContext(ctx, "gh", "repo", "delete", nwo, "--yes")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("  ⚠ could not delete repo: %s", strings.TrimSpace(string(out)))
			t.Logf("  to grant delete_repo scope: gh auth refresh -s delete_repo")
			t.Logf("  to delete manually:          gh repo delete %s --yes", nwo)
		}
		t.Log("─── cleanup done ───")
		return
	}

	// Existing repo: surgical cleanup
	branch := fmt.Sprintf("fleet/%d", issueNum)
	closePRsForBranch(t, ctx, nwo, branch)

	t.Logf("  deleting remote branch %s", branch)
	ghQuiet(t, ctx, "", "api",
		fmt.Sprintf("repos/%s/git/refs/heads/%s", nwo, branch),
		"-X", "DELETE")

	for _, name := range []string{"impl"} {
		dir := filepath.Join(repoDir, "worktrees", name)
		if _, err := os.Stat(dir); err == nil {
			t.Logf("  removing worktree %s", name)
			cmd := exec.CommandContext(ctx, "git", "worktree", "remove", dir, "--force")
			cmd.Dir = repoDir
			_ = cmd.Run()
		}
	}

	t.Logf("  closing issue #%d", issueNum)
	ghQuiet(t, ctx, "", "issue", "close", strconv.Itoa(issueNum), "--repo", nwo)

	t.Log("─── cleanup done ───")
}

func closePRsForBranch(t *testing.T, ctx context.Context, nwo, branch string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
		"--repo", nwo, "--head", branch, "--json", "number")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	var prs []struct{ Number int `json:"number"` }
	if json.Unmarshal(out, &prs) != nil {
		return
	}
	for _, pr := range prs {
		t.Logf("  closing PR #%d (branch %s)", pr.Number, branch)
		ghQuiet(t, ctx, "", "pr", "close", strconv.Itoa(pr.Number),
			"--repo", nwo, "--delete-branch")
	}
}

// ===========================================================================
// Low-level helpers
// ===========================================================================

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func shouldKeep() bool {
	v := os.Getenv("FLEET_TEST_KEEP")
	return v == "1" || strings.EqualFold(v, "true")
}

func requireCommand(t *testing.T, name string) {
	t.Helper()
	if name == "" {
		return
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found: %v", name, err)
	}
}

func backendCommand(backend string) string {
	switch backend {
	case "claude-code":
		return "claude"
	case "mock":
		return "" // no external CLI needed
	default:
		return backend
	}
}

// gh wrappers

func ghUser(t *testing.T, ctx context.Context) string {
	t.Helper()
	return ghMust(t, ctx, "", "api", "user", "-q", ".login")
}

func ghMust(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gh %s: %s\n%v", strings.Join(args, " "), out, err)
	}
	return strings.TrimSpace(string(out))
}

func ghQuiet(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("  gh %s (ignored): %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
}

func ghJSON[T any](t *testing.T, ctx context.Context, args ...string) T {
	t.Helper()
	raw := ghMust(t, ctx, "", args...)
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("gh JSON parse: %v\nraw: %s", err, raw)
	}
	return v
}

// git wrappers

func gitRun(t *testing.T, ctx context.Context, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s\n%v", strings.Join(args, " "), out, err)
	}
}

// file / repo helpers

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cloneRepo(t *testing.T, ctx context.Context, nwo string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	ghMust(t, ctx, "", "repo", "clone", nwo, dir)
	return dir
}

func createIssue(t *testing.T, ctx context.Context, nwo, title, body string) int {
	t.Helper()
	out := ghMust(t, ctx, "", "issue", "create",
		"--repo", nwo,
		"--title", title,
		"--body", body)
	parts := strings.Split(strings.TrimSpace(out), "/")
	num, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatalf("cannot parse issue number from %q: %v", out, err)
	}
	return num
}
