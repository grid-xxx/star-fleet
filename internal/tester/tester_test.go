package tester

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Mock types ---

type mockGHCommenter struct {
	postComment func(ctx context.Context, owner, repo string, number int, body string) error
}

func (m *mockGHCommenter) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	if m.postComment != nil {
		return m.postComment(ctx, owner, repo, number, body)
	}
	return nil
}

type mockBackend struct {
	run func(ctx context.Context, workdir string, prompt string, output io.Writer) error
}

func (m *mockBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	if m.run != nil {
		return m.run(ctx, workdir, prompt, output)
	}
	return nil
}

type recordingLogger struct {
	messages []string
}

func (l *recordingLogger) Info(msg string)    { l.messages = append(l.messages, "INFO: "+msg) }
func (l *recordingLogger) Success(msg string) { l.messages = append(l.messages, "OK: "+msg) }
func (l *recordingLogger) Warn(msg string)    { l.messages = append(l.messages, "WARN: "+msg) }
func (l *recordingLogger) Fail(msg string)    { l.messages = append(l.messages, "FAIL: "+msg) }

// --- Run tests ---

func TestRun_NoModules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	log := &recordingLogger{}
	cfg := &Config{
		RepoRoot: dir,
		Log:      log,
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.TotalModules != 0 {
		t.Errorf("TotalModules = %d, want 0", report.TotalModules)
	}

	// Should log a warning about no modules
	hasWarning := false
	for _, msg := range log.messages {
		if strings.Contains(msg, "WARN") && strings.Contains(msg, "No testable modules") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("Run() should warn when no modules found")
	}
}

func TestRun_SingleModulePasses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n- test a\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "PASS\nok  pkg  0.1s\n", nil
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Runner:   runner,
		Log:      &recordingLogger{},
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.TotalModules != 1 {
		t.Errorf("TotalModules = %d, want 1", report.TotalModules)
	}
	if !report.AllPassed {
		t.Error("AllPassed = false, want true")
	}
}

func TestRun_SingleModuleFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "FAIL\n", &exitError{code: 1}
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Runner:   runner,
		Log:      &recordingLogger{},
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.AllPassed {
		t.Error("AllPassed = true, want false")
	}
	if report.FailedModules != 1 {
		t.Errorf("FailedModules = %d, want 1", report.FailedModules)
	}
}

func TestRun_MultipleModules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, name := range []string{"alpha", "beta"} {
		modDir := filepath.Join(dir, name)
		mustMkdir(t, filepath.Join(modDir, "skills"))
		mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
		mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")
	}

	callCount := 0
	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			callCount++
			return "ok", nil
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Runner:   runner,
		Log:      &recordingLogger{},
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.TotalModules != 2 {
		t.Errorf("TotalModules = %d, want 2", report.TotalModules)
	}
	if callCount != 2 {
		t.Errorf("runner called %d times, want 2", callCount)
	}
}

func TestRun_WithAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n- test login\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	var agentPrompt string
	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			agentPrompt = prompt
			// Agent creates a test file
			mustWrite(t, filepath.Join(workdir, "login_test.go"), "package pkg\n")
			return nil
		},
	}

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "PASS\n", nil
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Backend:  backend,
		Runner:   runner,
		Log:      &recordingLogger{},
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.AllPassed {
		t.Error("AllPassed = false, want true")
	}

	// Verify agent received the prompt with test requirements
	if !strings.Contains(agentPrompt, "test login") {
		t.Error("Agent prompt should contain test requirements from tests.md")
	}
	if !strings.Contains(agentPrompt, "Usage") {
		t.Error("Agent prompt should contain usage info from skills/usage.md")
	}
}

func TestRun_AgentError_ContinuesToRunTests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			return errors.New("agent crashed")
		},
	}

	testRan := false
	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			testRan = true
			return "PASS\n", nil
		},
	}

	log := &recordingLogger{}
	cfg := &Config{
		RepoRoot: dir,
		Backend:  backend,
		Runner:   runner,
		Log:      log,
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !testRan {
		t.Error("Tests should still run even when agent fails")
	}
	if !report.AllPassed {
		t.Error("AllPassed = false, want true (agent failure doesn't fail tests)")
	}

	// Should have logged a warning about agent failure
	hasWarning := false
	for _, msg := range log.messages {
		if strings.Contains(msg, "WARN") && strings.Contains(msg, "Agent") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("Run() should warn when agent fails")
	}
}

func TestRun_NoAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "ok", nil
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Backend:  nil, // No agent
		Runner:   runner,
		Log:      &recordingLogger{},
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.TotalModules != 1 {
		t.Errorf("TotalModules = %d, want 1", report.TotalModules)
	}
}

func TestRun_PostsPRComment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "PASS\n", nil
		},
	}

	var postedComment string
	var postedPR int
	gh := &mockGHCommenter{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			postedPR = number
			postedComment = body
			return nil
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Runner:   runner,
		Owner:    "testorg",
		Repo:     "testrepo",
		PRNumber: 42,
		GH:       gh,
		Log:      &recordingLogger{},
	}

	_, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if postedPR != 42 {
		t.Errorf("posted to PR #%d, want #42", postedPR)
	}
	if !strings.Contains(postedComment, "Fleet Test Report") {
		t.Error("PR comment should contain report")
	}
}

func TestRun_NoPRComment_WhenNoPRNumber(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "PASS\n", nil
		},
	}

	commentPosted := false
	gh := &mockGHCommenter{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			commentPosted = true
			return nil
		},
	}

	cfg := &Config{
		RepoRoot: dir,
		Runner:   runner,
		PRNumber: 0, // No PR
		GH:       gh,
		Log:      &recordingLogger{},
	}

	_, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if commentPosted {
		t.Error("Should not post PR comment when PRNumber=0")
	}
}

func TestRun_PRCommentError_DoesNotFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			return "PASS\n", nil
		},
	}

	gh := &mockGHCommenter{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			return errors.New("github error")
		},
	}

	log := &recordingLogger{}
	cfg := &Config{
		RepoRoot: dir,
		Runner:   runner,
		Owner:    "org",
		Repo:     "repo",
		PRNumber: 10,
		GH:       gh,
		Log:      log,
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() should not fail when PR comment fails, got %v", err)
	}
	if !report.AllPassed {
		t.Error("AllPassed should be true")
	}

	// Should log a warning
	hasWarning := false
	for _, msg := range log.messages {
		if strings.Contains(msg, "WARN") && strings.Contains(msg, "Failed to post") {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("Run() should warn when PR comment fails")
	}
}

func TestRun_CustomTestCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	var capturedName string
	runner := &mockRunner{
		run: func(ctx context.Context, dir string, name string, args ...string) (string, error) {
			capturedName = name
			return "ok", nil
		},
	}

	cfg := &Config{
		RepoRoot:    dir,
		TestCommand: "pytest -xvs",
		Runner:      runner,
		Log:         &recordingLogger{},
	}

	_, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if capturedName != "pytest" {
		t.Errorf("test command = %q, want 'pytest'", capturedName)
	}
}

func TestRun_NilLogger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cfg := &Config{
		RepoRoot: dir,
		Log:      nil, // should not panic
	}

	report, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report == nil {
		t.Error("Run() should return a report even with nil logger")
	}
}

func TestRun_DefaultRunner(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cfg := &Config{
		RepoRoot: dir,
		Runner:   nil, // should default to ExecRunner
		Log:      &recordingLogger{},
	}

	// Just ensure it doesn't panic
	_, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// --- generateTests tests ---

func TestGenerateTests_ReadsSpecFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Test Reqs\n- test login\n- test logout\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# API Usage\nCall Login()\n")

	var capturedPrompt string
	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			capturedPrompt = prompt
			return nil
		},
	}

	mod := Module{
		Path:    modDir,
		RelPath: "pkg",
		TestsMD: filepath.Join(modDir, "tests.md"),
		UsageMD: filepath.Join(modDir, "skills", "usage.md"),
	}

	_, err := generateTests(context.Background(), backend, mod, dir)
	if err != nil {
		t.Fatalf("generateTests() error = %v", err)
	}

	if !strings.Contains(capturedPrompt, "test login") {
		t.Error("prompt should contain tests.md content")
	}
	if !strings.Contains(capturedPrompt, "test logout") {
		t.Error("prompt should contain all test requirements")
	}
	if !strings.Contains(capturedPrompt, "Call Login()") {
		t.Error("prompt should contain usage.md content")
	}
}

func TestGenerateTests_IncludesExistingTests(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")
	mustWrite(t, filepath.Join(modDir, "auth_test.go"), "package pkg\n\nfunc TestAuth(t *testing.T) {}\n")

	var capturedPrompt string
	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			capturedPrompt = prompt
			return nil
		},
	}

	mod := Module{
		Path:      modDir,
		RelPath:   "pkg",
		TestsMD:   filepath.Join(modDir, "tests.md"),
		UsageMD:   filepath.Join(modDir, "skills", "usage.md"),
		TestFiles: []string{filepath.Join(modDir, "auth_test.go")},
	}

	_, err := generateTests(context.Background(), backend, mod, dir)
	if err != nil {
		t.Fatalf("generateTests() error = %v", err)
	}

	if !strings.Contains(capturedPrompt, "TestAuth") {
		t.Error("prompt should include existing test file content")
	}
}

func TestGenerateTests_DetectsNewFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			// Agent creates a new test file
			return os.WriteFile(filepath.Join(workdir, "new_test.go"), []byte("package pkg\n"), 0644)
		},
	}

	mod := Module{
		Path:    modDir,
		RelPath: "pkg",
		TestsMD: filepath.Join(modDir, "tests.md"),
		UsageMD: filepath.Join(modDir, "skills", "usage.md"),
	}

	changed, err := generateTests(context.Background(), backend, mod, dir)
	if err != nil {
		t.Fatalf("generateTests() error = %v", err)
	}
	if !changed {
		t.Error("generateTests() should detect new files as changes")
	}
}

func TestGenerateTests_NoChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			// Agent does nothing
			return nil
		},
	}

	mod := Module{
		Path:    modDir,
		RelPath: "pkg",
		TestsMD: filepath.Join(modDir, "tests.md"),
		UsageMD: filepath.Join(modDir, "skills", "usage.md"),
	}

	changed, err := generateTests(context.Background(), backend, mod, dir)
	if err != nil {
		t.Fatalf("generateTests() error = %v", err)
	}
	if changed {
		t.Error("generateTests() should report no changes when agent does nothing")
	}
}

func TestGenerateTests_BackendError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	backend := &mockBackend{
		run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
			return errors.New("agent error")
		},
	}

	mod := Module{
		Path:    modDir,
		RelPath: "pkg",
		TestsMD: filepath.Join(modDir, "tests.md"),
		UsageMD: filepath.Join(modDir, "skills", "usage.md"),
	}

	_, err := generateTests(context.Background(), backend, mod, dir)
	if err == nil {
		t.Error("generateTests() should return error on backend failure")
	}
}

// --- filesChanged tests ---

func TestFilesChanged_NoChange(t *testing.T) {
	t.Parallel()
	snap := map[string]fileSnapshot{
		"a.go": {size: 100, modTime: fixedTime},
	}
	if filesChanged(snap, snap) {
		t.Error("filesChanged() = true, want false")
	}
}

func TestFilesChanged_NewFile(t *testing.T) {
	t.Parallel()
	before := map[string]fileSnapshot{}
	after := map[string]fileSnapshot{
		"a_test.go": {size: 100, modTime: fixedTime},
	}
	if !filesChanged(before, after) {
		t.Error("filesChanged() = false, want true (new file)")
	}
}

func TestFilesChanged_RemovedFile(t *testing.T) {
	t.Parallel()
	before := map[string]fileSnapshot{
		"a_test.go": {size: 100, modTime: fixedTime},
	}
	after := map[string]fileSnapshot{}
	if !filesChanged(before, after) {
		t.Error("filesChanged() = false, want true (removed file)")
	}
}

func TestFilesChanged_SizeChanged(t *testing.T) {
	t.Parallel()
	before := map[string]fileSnapshot{
		"a_test.go": {size: 100, modTime: fixedTime},
	}
	after := map[string]fileSnapshot{
		"a_test.go": {size: 200, modTime: fixedTime},
	}
	if !filesChanged(before, after) {
		t.Error("filesChanged() = false, want true (size changed)")
	}
}

func TestFilesChanged_ModTimeChanged(t *testing.T) {
	t.Parallel()
	before := map[string]fileSnapshot{
		"a_test.go": {size: 100, modTime: fixedTime},
	}
	after := map[string]fileSnapshot{
		"a_test.go": {size: 100, modTime: fixedTime.Add(1)},
	}
	if !filesChanged(before, after) {
		t.Error("filesChanged() = false, want true (mod time changed)")
	}
}

func TestFilesChanged_BothEmpty(t *testing.T) {
	t.Parallel()
	if filesChanged(map[string]fileSnapshot{}, map[string]fileSnapshot{}) {
		t.Error("filesChanged() = true, want false (both empty)")
	}
}

// --- buildTestGenPrompt tests ---

func TestBuildTestGenPrompt_ContainsAllSections(t *testing.T) {
	t.Parallel()
	prompt := buildTestGenPrompt("pkg/auth", "# Test Reqs\n", "# Usage\n", "")

	if !strings.Contains(prompt, "pkg/auth") {
		t.Error("prompt should contain module path")
	}
	if !strings.Contains(prompt, "Test Reqs") {
		t.Error("prompt should contain tests.md content")
	}
	if !strings.Contains(prompt, "Usage") {
		t.Error("prompt should contain usage.md content")
	}
	if !strings.Contains(prompt, "Instructions") {
		t.Error("prompt should contain instructions")
	}
}

func TestBuildTestGenPrompt_IncludesExistingTests(t *testing.T) {
	t.Parallel()
	existing := "### auth_test.go\n```\npackage auth\n```\n"
	prompt := buildTestGenPrompt("pkg/auth", "# Tests\n", "# Usage\n", existing)

	if !strings.Contains(prompt, "Existing Test Files") {
		t.Error("prompt should have existing test files section")
	}
	if !strings.Contains(prompt, "auth_test.go") {
		t.Error("prompt should include existing test content")
	}
}

func TestBuildTestGenPrompt_NoExistingTests(t *testing.T) {
	t.Parallel()
	prompt := buildTestGenPrompt("pkg/auth", "# Tests\n", "# Usage\n", "")

	if strings.Contains(prompt, "Existing Test Files") {
		t.Error("prompt should NOT have existing test files section when empty")
	}
}

func TestBuildTestGenPrompt_ContainsDoNotModifyImpl(t *testing.T) {
	t.Parallel()
	prompt := buildTestGenPrompt("pkg", "# Tests\n", "# Usage\n", "")

	if !strings.Contains(prompt, "NOT modify the implementation") {
		t.Error("prompt should instruct agent not to modify implementation code")
	}
}

// --- nopLogger tests ---

func TestNopLogger_DoesNotPanic(t *testing.T) {
	t.Parallel()
	l := nopLogger{}
	l.Info("test")
	l.Success("test")
	l.Warn("test")
	l.Fail("test")
}

// helpers

var fixedTime = mustParseTime("2024-01-01T00:00:00Z")

func mustParseTime(s string) (t_ time.Time) {
	t_, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(fmt.Sprintf("bad time %q: %v", s, err))
	}
	return t_
}
