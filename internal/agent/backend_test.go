package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNewBackend(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"claude-code", false},
		{"cursor", false},
		{"unknown", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := NewBackend(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewBackend(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
				return
			}
			if !tt.wantErr && b == nil {
				t.Error("NewBackend returned nil backend")
			}
		})
	}
}

func TestNewBackend_ClaudeType(t *testing.T) {
	b, err := NewBackend("claude-code")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(*ClaudeBackend); !ok {
		t.Errorf("NewBackend(\"claude-code\") returned %T, want *ClaudeBackend", b)
	}
}

func TestNewBackend_CursorType(t *testing.T) {
	b, err := NewBackend("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.(*CursorBackend); !ok {
		t.Errorf("NewBackend(\"cursor\") returned %T, want *CursorBackend", b)
	}
}

func TestRunForReview(t *testing.T) {
	workdir := t.TempDir()

	fake := &fakeBackend{
		runFunc: func(ctx context.Context, workdir string, prompt string) error {
			return os.WriteFile(
				filepath.Join(workdir, reviewOutputFile),
				[]byte("looks good"),
				0644,
			)
		},
	}

	result, err := RunForReview(context.Background(), fake, workdir, "review this")
	if err != nil {
		t.Fatalf("RunForReview() error = %v", err)
	}
	if result != "looks good" {
		t.Errorf("RunForReview() = %q, want %q", result, "looks good")
	}

	if _, err := os.Stat(filepath.Join(workdir, reviewOutputFile)); !os.IsNotExist(err) {
		t.Error("RunForReview should clean up the output file")
	}
}

func TestRunForReview_NoOutput(t *testing.T) {
	fake := &fakeBackend{
		runFunc: func(ctx context.Context, workdir string, prompt string) error {
			return nil
		},
	}

	result, err := RunForReview(context.Background(), fake, t.TempDir(), "review this")
	if err != nil {
		t.Fatalf("RunForReview() error = %v", err)
	}
	if result != "" {
		t.Errorf("RunForReview() = %q, want empty string", result)
	}
}

func TestRunForReview_BackendError(t *testing.T) {
	fake := &fakeBackend{
		runFunc: func(ctx context.Context, workdir string, prompt string) error {
			return fmt.Errorf("backend failed")
		},
	}

	_, err := RunForReview(context.Background(), fake, t.TempDir(), "review this")
	if err == nil {
		t.Fatal("RunForReview() expected error, got nil")
	}
}

func TestRunForReview_PromptContainsOutputInstruction(t *testing.T) {
	var capturedPrompt string
	fake := &fakeBackend{
		runFunc: func(ctx context.Context, workdir string, prompt string) error {
			capturedPrompt = prompt
			return os.WriteFile(
				filepath.Join(workdir, reviewOutputFile),
				[]byte("ok"),
				0644,
			)
		},
	}

	_, err := RunForReview(context.Background(), fake, t.TempDir(), "review this")
	if err != nil {
		t.Fatal(err)
	}
	if capturedPrompt == "review this" {
		t.Error("RunForReview should augment the prompt with output file instruction")
	}
}

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
}

const testFileName = "fleet-integration-test.txt"
const testFileContent = "fleet-test-ok"

var createFilePrompt = fmt.Sprintf(
	"Create a file called %q in the current directory containing exactly this text and nothing else: %s\nDo not add any extra whitespace, newlines, or explanation. Just create the file.",
	testFileName, testFileContent,
)

func verifyFileCreated(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, testFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file %s to be created, but got error: %v", testFileName, err)
	}
	got := string(data)
	if got != testFileContent && got != testFileContent+"\n" {
		t.Errorf("file content = %q, want %q", got, testFileContent)
	}
}

func verboseWriter(t *testing.T) io.Writer {
	t.Helper()
	if testing.Verbose() {
		return os.Stderr
	}
	return nil
}

func TestClaudeBackendRun(t *testing.T) {
	if !commandAvailable("claude") {
		t.Skip("claude CLI not installed")
	}
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	workdir := t.TempDir()
	t.Logf("workdir: %s", workdir)

	t.Log("initializing git repo...")
	initGitRepo(t, workdir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("running claude backend...")
	start := time.Now()
	backend := &ClaudeBackend{Output: verboseWriter(t)}
	if err := backend.Run(ctx, workdir, createFilePrompt); err != nil {
		t.Fatalf("ClaudeBackend.Run() error = %v", err)
	}
	t.Logf("claude backend finished in %s", time.Since(start))

	t.Log("verifying file was created...")
	verifyFileCreated(t, workdir)
}

func TestCursorBackendRun(t *testing.T) {
	if !commandAvailable("cursor") {
		t.Skip("cursor CLI not installed")
	}
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	workdir := t.TempDir()
	t.Logf("workdir: %s", workdir)

	t.Log("initializing git repo...")
	initGitRepo(t, workdir)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Log("running cursor backend...")
	start := time.Now()
	backend := &CursorBackend{Output: verboseWriter(t)}
	if err := backend.Run(ctx, workdir, createFilePrompt); err != nil {
		t.Fatalf("CursorBackend.Run() error = %v", err)
	}
	t.Logf("cursor backend finished in %s", time.Since(start))

	t.Log("verifying file was created...")
	verifyFileCreated(t, workdir)
}

type fakeBackend struct {
	runFunc func(ctx context.Context, workdir string, prompt string) error
}

func (f *fakeBackend) Run(ctx context.Context, workdir string, prompt string) error {
	return f.runFunc(ctx, workdir, prompt)
}
