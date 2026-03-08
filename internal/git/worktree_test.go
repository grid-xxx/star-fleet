package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a bare-bones git repo with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		if _, err := runGit(ctx, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, dir, "commit", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCreateBranch(t *testing.T) {
	tests := []struct {
		name       string
		branch     string
		startPoint string
		wantErr    bool
	}{
		{
			name:       "new branch from main",
			branch:     "feature/foo",
			startPoint: "main",
		},
		{
			name:       "new branch with slashes",
			branch:     "fleet/42",
			startPoint: "main",
		},
		{
			name:       "invalid start point",
			branch:     "feature/bad",
			startPoint: "nonexistent",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			ctx := context.Background()

			err := CreateBranch(ctx, dir, tt.branch, tt.startPoint)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got, err := CurrentBranch(ctx, dir)
			if err != nil {
				t.Fatalf("CurrentBranch: %v", err)
			}
			if got != tt.branch {
				t.Errorf("CurrentBranch = %q, want %q", got, tt.branch)
			}
		})
	}
}

func TestDeleteBranch(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr bool
	}{
		{
			name:   "delete existing branch",
			branch: "to-delete",
		},
		{
			name:    "delete nonexistent branch",
			branch:  "ghost",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := initRepo(t)
			ctx := context.Background()

			if !tt.wantErr {
				if err := CreateBranch(ctx, dir, tt.branch, "main"); err != nil {
					t.Fatalf("setup CreateBranch: %v", err)
				}
				if err := Checkout(ctx, dir, "main"); err != nil {
					t.Fatalf("setup Checkout: %v", err)
				}
			}

			err := DeleteBranch(ctx, dir, tt.branch)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			out, _ := runGit(ctx, dir, "branch", "--list", tt.branch)
			if strings.TrimSpace(out) != "" {
				t.Errorf("branch %q still exists after deletion", tt.branch)
			}
		})
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	got, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "main" {
		t.Errorf("CurrentBranch = %q, want %q", got, "main")
	}

	if err := CreateBranch(ctx, dir, "feature/x", "main"); err != nil {
		t.Fatal(err)
	}
	got, err = CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "feature/x" {
		t.Errorf("CurrentBranch = %q, want %q", got, "feature/x")
	}
}

func TestCurrentBranchInvalidDir(t *testing.T) {
	_, err := CurrentBranch(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestCurrentHead(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	head, err := CurrentHead(ctx, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(head) != 40 {
		t.Errorf("expected 40-char SHA, got %q (len %d)", head, len(head))
	}
}

func TestCurrentHeadInvalidDir(t *testing.T) {
	_, err := CurrentHead(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestHasChanges(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	has, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected no changes in clean repo")
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	has, err = HasChanges(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("expected changes after creating a file")
	}
}

func TestHasChangesInvalidDir(t *testing.T) {
	_, err := HasChanges(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestDiffNames(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	base, err := CurrentHead(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := CreateBranch(ctx, dir, "feature/diff", "main"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommitAll(ctx, dir, "add files"); err != nil {
		t.Fatal(err)
	}

	head, err := CurrentHead(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("all files", func(t *testing.T) {
		files, err := DiffNames(ctx, dir, base, head)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Fatalf("got %d files, want 2: %v", len(files), files)
		}
	})

	t.Run("with pattern filter", func(t *testing.T) {
		files, err := DiffNames(ctx, dir, base, head, "*.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1: %v", len(files), files)
		}
		if files[0] != "a.go" {
			t.Errorf("got %q, want %q", files[0], "a.go")
		}
	})

	t.Run("no diff", func(t *testing.T) {
		files, err := DiffNames(ctx, dir, head, head)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if files != nil {
			t.Errorf("expected nil for no diff, got %v", files)
		}
	})
}

func TestCommitAll(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CommitAll(ctx, dir, "add file"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	has, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("expected clean working tree after CommitAll")
	}

	out, err := runGit(ctx, dir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "add file") {
		t.Errorf("commit message not found in log: %s", out)
	}
}

func TestCommitAllEmpty(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	headBefore, err := CurrentHead(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := CommitAll(ctx, dir, "empty commit"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headAfter, err := CurrentHead(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if headBefore == headAfter {
		t.Error("expected new commit even with --allow-empty")
	}
}

func TestCheckout(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	if err := CreateBranch(ctx, dir, "other", "main"); err != nil {
		t.Fatal(err)
	}

	if err := Checkout(ctx, dir, "main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Errorf("CurrentBranch = %q, want %q", got, "main")
	}
}

func TestCheckoutInvalidRef(t *testing.T) {
	dir := initRepo(t)
	err := Checkout(context.Background(), dir, "nonexistent-ref")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

func TestRemoveFiles(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	files := []string{"a.txt", "b.txt"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("data\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CommitAll(ctx, dir, "add files"); err != nil {
		t.Fatal(err)
	}

	if err := RemoveFiles(ctx, dir, files); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
			t.Errorf("file %q still exists after RemoveFiles", f)
		}
	}

	has, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("expected clean tree after RemoveFiles commit")
	}
}

func TestRemoveFilesEmpty(t *testing.T) {
	dir := initRepo(t)
	headBefore, err := CurrentHead(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := RemoveFiles(context.Background(), dir, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headAfter, err := CurrentHead(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if headBefore != headAfter {
		t.Error("RemoveFiles with empty slice should be a no-op")
	}
}

func TestMerge(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	if err := CreateBranch(ctx, dir, "feature", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommitAll(ctx, dir, "feature work"); err != nil {
		t.Fatal(err)
	}
	if err := Checkout(ctx, dir, "main"); err != nil {
		t.Fatal(err)
	}

	if err := Merge(ctx, dir, "feature"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Errorf("merged file not present: %v", err)
	}

	out, err := runGit(ctx, dir, "log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merge feature") {
		t.Errorf("expected merge commit message, got: %s", out)
	}
}

func TestMergeInvalidBranch(t *testing.T) {
	dir := initRepo(t)
	err := Merge(context.Background(), dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for invalid branch")
	}
}

func TestRepoRoot(t *testing.T) {
	dir := initRepo(t)

	// RepoRoot uses cwd-relative ".", so we need to run from inside the repo.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	root, err := RepoRoot(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Resolve symlinks for macOS /private/tmp vs /tmp
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(root)
	if gotResolved != wantResolved {
		t.Errorf("RepoRoot = %q, want %q", gotResolved, wantResolved)
	}
}

func TestRepoRootNotARepo(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, err = RepoRoot(context.Background())
	if err == nil {
		t.Fatal("expected error for non-repo directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error should mention 'not a git repository', got: %v", err)
	}
}

func TestCreateWorktree(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	wtDir, err := CreateWorktree(ctx, dir, "test-wt", "wt-branch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(dir, "worktrees", "test-wt")
	if wtDir != expected {
		t.Errorf("worktree dir = %q, want %q", wtDir, expected)
	}

	if _, err := os.Stat(wtDir); err != nil {
		t.Errorf("worktree directory does not exist: %v", err)
	}

	branch, err := CurrentBranch(ctx, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "wt-branch" {
		t.Errorf("worktree branch = %q, want %q", branch, "wt-branch")
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()

	_, err := CreateWorktree(ctx, dir, "removable", "rm-branch")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := RemoveWorktree(ctx, dir, "removable"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wtDir := filepath.Join(dir, "worktrees", "removable")
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Errorf("worktree directory should not exist after removal")
	}
}

func TestRemoveWorktreeNonexistent(t *testing.T) {
	dir := initRepo(t)
	err := RemoveWorktree(context.Background(), dir, "nope")
	if err == nil {
		t.Fatal("expected error for nonexistent worktree")
	}
}

func TestRunGitContextCanceled(t *testing.T) {
	dir := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CurrentBranch(ctx, dir)
	if err == nil {
		t.Fatal("expected error with canceled context")
	}
}

func TestRunGitErrorFormat(t *testing.T) {
	dir := initRepo(t)
	_, err := runGit(context.Background(), dir, "checkout", "nonexistent-branch-xyz")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "git checkout nonexistent-branch-xyz") {
		t.Errorf("error should contain the git command, got: %v", err)
	}
}
