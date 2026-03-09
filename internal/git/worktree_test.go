package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "initial commit"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	writeFile(t, dir, name, content)
	for _, args := range [][]string{
		{"add", name},
		{"commit", "-m", msg},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCurrentHead(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	ctx := context.Background()
	head, err := CurrentHead(ctx, dir)
	if err != nil {
		t.Fatalf("CurrentHead: %v", err)
	}
	if len(head) != 40 {
		t.Errorf("expected 40-char SHA, got %q (len %d)", head, len(head))
	}

	want := gitRun(t, dir, "rev-parse", "HEAD")
	if head != want {
		t.Errorf("CurrentHead = %q, want %q", head, want)
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	ctx := context.Background()
	branch, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}

	want := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != want {
		t.Errorf("CurrentBranch = %q, want %q", branch, want)
	}
}

func TestHasChanges(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	ctx := context.Background()

	t.Run("clean repo", func(t *testing.T) {
		has, err := HasChanges(ctx, dir)
		if err != nil {
			t.Fatalf("HasChanges: %v", err)
		}
		if has {
			t.Error("expected no changes in fresh repo")
		}
	})

	t.Run("untracked file", func(t *testing.T) {
		writeFile(t, dir, "untracked.txt", "hello")
		has, err := HasChanges(ctx, dir)
		if err != nil {
			t.Fatalf("HasChanges: %v", err)
		}
		if !has {
			t.Error("expected changes with untracked file")
		}
	})

	t.Run("staged file", func(t *testing.T) {
		gitRun(t, dir, "add", "untracked.txt")
		has, err := HasChanges(ctx, dir)
		if err != nil {
			t.Fatalf("HasChanges: %v", err)
		}
		if !has {
			t.Error("expected changes with staged file")
		}
	})

	t.Run("committed clears changes", func(t *testing.T) {
		gitRun(t, dir, "commit", "-m", "add file")
		has, err := HasChanges(ctx, dir)
		if err != nil {
			t.Fatalf("HasChanges: %v", err)
		}
		if has {
			t.Error("expected no changes after commit")
		}
	})
}

func TestCommitAll(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	ctx := context.Background()

	writeFile(t, dir, "a.txt", "aaa")
	writeFile(t, dir, "sub/b.txt", "bbb")

	if err := CommitAll(ctx, dir, "add files"); err != nil {
		t.Fatalf("CommitAll: %v", err)
	}

	has, err := HasChanges(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("expected clean working tree after CommitAll")
	}

	msg := gitRun(t, dir, "log", "-1", "--format=%s")
	if msg != "add files" {
		t.Errorf("commit message = %q, want %q", msg, "add files")
	}

	// Verify both files were committed
	files := gitRun(t, dir, "diff", "--name-only", "HEAD~1..HEAD")
	for _, want := range []string{"a.txt", "sub/b.txt"} {
		if !strings.Contains(files, want) {
			t.Errorf("committed files %q missing %q", files, want)
		}
	}
}

func TestCommitAll_AllowsEmpty(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	ctx := context.Background()

	headBefore := gitRun(t, dir, "rev-parse", "HEAD")

	if err := CommitAll(ctx, dir, "empty commit"); err != nil {
		t.Fatalf("CommitAll with no changes: %v", err)
	}

	headAfter := gitRun(t, dir, "rev-parse", "HEAD")
	if headBefore == headAfter {
		t.Error("expected new commit even with no changes (--allow-empty)")
	}
}

func TestWorktreeCreateRemove(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "file.txt", "content", "add file")
	ctx := context.Background()

	wtDir, err := CreateWorktree(ctx, dir, "test-wt", "feature-branch")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if _, err := os.Stat(wtDir); err != nil {
		t.Fatalf("worktree dir should exist: %v", err)
	}

	branch, err := CurrentBranch(ctx, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature-branch" {
		t.Errorf("worktree branch = %q, want %q", branch, "feature-branch")
	}

	// Verify file from main branch is present
	data, err := os.ReadFile(filepath.Join(wtDir, "file.txt"))
	if err != nil {
		t.Fatalf("reading file in worktree: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("file content = %q, want %q", data, "content")
	}

	if err := RemoveWorktree(ctx, dir, "test-wt"); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Error("worktree dir should not exist after removal")
	}
}

func TestWorktreeCreateExistingBranch(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "file.txt", "content", "add file")
	ctx := context.Background()

	gitRun(t, dir, "branch", "existing-branch")

	wtDir, err := CreateWorktree(ctx, dir, "test-wt", "existing-branch")
	if err != nil {
		t.Fatalf("CreateWorktree with existing branch: %v", err)
	}
	defer RemoveWorktree(ctx, dir, "test-wt")

	branch, err := CurrentBranch(ctx, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "existing-branch" {
		t.Errorf("worktree branch = %q, want %q", branch, "existing-branch")
	}
}

func TestPush(t *testing.T) {
	ctx := context.Background()

	// Create a bare "remote" repo
	bareDir := t.TempDir()
	gitRun(t, bareDir, "init", "--bare")

	// Create a "local" repo that pushes to the bare one
	localDir := t.TempDir()
	initRepo(t, localDir)
	commitFile(t, localDir, "pushed.txt", "data", "add file")
	gitRun(t, localDir, "remote", "add", "origin", bareDir)

	branch := gitRun(t, localDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err := Push(ctx, localDir, "origin", branch); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Verify the bare repo received the commit
	remoteHead := gitRun(t, bareDir, "rev-parse", "HEAD")
	localHead := gitRun(t, localDir, "rev-parse", "HEAD")
	if remoteHead != localHead {
		t.Errorf("remote HEAD %q != local HEAD %q", remoteHead, localHead)
	}
}

func TestForcePush(t *testing.T) {
	ctx := context.Background()

	bareDir := t.TempDir()
	gitRun(t, bareDir, "init", "--bare")

	localDir := t.TempDir()
	initRepo(t, localDir)
	commitFile(t, localDir, "file.txt", "v1", "first commit")
	gitRun(t, localDir, "remote", "add", "origin", bareDir)

	branch := gitRun(t, localDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err := Push(ctx, localDir, "origin", branch); err != nil {
		t.Fatalf("initial Push: %v", err)
	}

	// Amend the last commit (rewrites history)
	writeFile(t, localDir, "file.txt", "v2")
	gitRun(t, localDir, "add", "file.txt")
	gitRun(t, localDir, "commit", "--amend", "-m", "amended commit")

	// Normal push should fail
	if err := Push(ctx, localDir, "origin", branch); err == nil {
		t.Fatal("expected push to fail after amend")
	}

	// Force push should succeed
	if err := ForcePush(ctx, localDir, "origin", branch); err != nil {
		t.Fatalf("ForcePush: %v", err)
	}

	remoteHead := gitRun(t, bareDir, "rev-parse", "HEAD")
	localHead := gitRun(t, localDir, "rev-parse", "HEAD")
	if remoteHead != localHead {
		t.Errorf("after force push: remote HEAD %q != local HEAD %q", remoteHead, localHead)
	}
}

func TestMerge(t *testing.T) {
	ctx := context.Background()

	t.Run("clean merge", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		commitFile(t, dir, "base.txt", "base", "base commit")

		gitRun(t, dir, "checkout", "-b", "feature")
		commitFile(t, dir, "feature.txt", "feature", "feature commit")

		defaultBranch := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
		gitRun(t, dir, "checkout", "-")
		mainBranch := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
		_ = defaultBranch

		if err := Merge(ctx, dir, "feature"); err != nil {
			t.Fatalf("Merge: %v", err)
		}

		// feature.txt should now exist on the main branch
		if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
			t.Errorf("feature.txt should exist after merge: %v", err)
		}

		// Verify merge commit message
		msg := gitRun(t, dir, "log", "-1", "--format=%s")
		if !strings.Contains(msg, "Merge feature") {
			t.Errorf("merge commit message = %q, want to contain %q", msg, "Merge feature")
		}

		// Verify it's a merge commit (two parents)
		parents := gitRun(t, dir, "log", "-1", "--format=%P")
		if len(strings.Fields(parents)) < 2 {
			t.Errorf("expected merge commit with 2 parents on %s, got %q", mainBranch, parents)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		dir := t.TempDir()
		initRepo(t, dir)
		commitFile(t, dir, "conflict.txt", "original", "base")

		gitRun(t, dir, "checkout", "-b", "branch-a")
		commitFile(t, dir, "conflict.txt", "version-a", "change on branch-a")

		gitRun(t, dir, "checkout", "-")
		commitFile(t, dir, "conflict.txt", "version-main", "change on main")

		err := Merge(ctx, dir, "branch-a")
		if err == nil {
			t.Fatal("expected merge conflict error")
		}

		// Abort the failed merge so temp dir cleanup works
		cmd := exec.Command("git", "merge", "--abort")
		cmd.Dir = dir
		_ = cmd.Run()
	})
}

func TestDiffNames(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "existing.txt", "keep", "base commit")
	ctx := context.Background()

	base := gitRun(t, dir, "rev-parse", "HEAD")

	commitFile(t, dir, "src/main.go", "package main", "add go file")
	commitFile(t, dir, "src/util.go", "package util", "add util")
	commitFile(t, dir, "README.md", "# README", "add readme")

	head := gitRun(t, dir, "rev-parse", "HEAD")

	t.Run("all changed files", func(t *testing.T) {
		files, err := DiffNames(ctx, dir, base, head)
		if err != nil {
			t.Fatalf("DiffNames: %v", err)
		}
		sort.Strings(files)
		want := []string{"README.md", "src/main.go", "src/util.go"}
		if len(files) != len(want) {
			t.Fatalf("got %v, want %v", files, want)
		}
		for i, f := range files {
			if f != want[i] {
				t.Errorf("files[%d] = %q, want %q", i, f, want[i])
			}
		}
	})

	t.Run("with pattern filter", func(t *testing.T) {
		files, err := DiffNames(ctx, dir, base, head, "*.go")
		if err != nil {
			t.Fatalf("DiffNames with pattern: %v", err)
		}
		sort.Strings(files)
		want := []string{"src/main.go", "src/util.go"}
		if len(files) != len(want) {
			t.Fatalf("got %v, want %v", files, want)
		}
		for i, f := range files {
			if f != want[i] {
				t.Errorf("files[%d] = %q, want %q", i, f, want[i])
			}
		}
	})

	t.Run("no changes", func(t *testing.T) {
		files, err := DiffNames(ctx, dir, head, head)
		if err != nil {
			t.Fatalf("DiffNames same ref: %v", err)
		}
		if files != nil {
			t.Errorf("expected nil for no changes, got %v", files)
		}
	})
}

func TestRemoveFiles(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "keep.txt", "keep", "keep file")
	commitFile(t, dir, "remove-me.txt", "gone", "to be removed")
	ctx := context.Background()

	if err := RemoveFiles(ctx, dir, []string{"remove-me.txt"}); err != nil {
		t.Fatalf("RemoveFiles: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "remove-me.txt")); !os.IsNotExist(err) {
		t.Error("remove-me.txt should not exist after RemoveFiles")
	}

	// keep.txt should still exist
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Errorf("keep.txt should still exist: %v", err)
	}

	// Verify a commit was created
	msg := gitRun(t, dir, "log", "-1", "--format=%s")
	if !strings.Contains(msg, "Strip dev-authored test files") {
		t.Errorf("commit message = %q, expected removal message", msg)
	}
}

func TestRemoveFiles_Empty(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	ctx := context.Background()

	headBefore := gitRun(t, dir, "rev-parse", "HEAD")
	if err := RemoveFiles(ctx, dir, nil); err != nil {
		t.Fatalf("RemoveFiles(nil): %v", err)
	}
	headAfter := gitRun(t, dir, "rev-parse", "HEAD")
	if headBefore != headAfter {
		t.Error("RemoveFiles with empty list should not create a commit")
	}
}

func TestCheckout(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "file.txt", "content", "add file")
	ctx := context.Background()

	gitRun(t, dir, "branch", "other-branch")

	if err := Checkout(ctx, dir, "other-branch"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	branch, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "other-branch" {
		t.Errorf("branch = %q, want %q", branch, "other-branch")
	}
}

func TestCreateBranch(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "file.txt", "content", "add file")
	ctx := context.Background()

	head := gitRun(t, dir, "rev-parse", "HEAD")

	if err := CreateBranch(ctx, dir, "new-branch", head); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	branch, err := CurrentBranch(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "new-branch" {
		t.Errorf("branch = %q, want %q", branch, "new-branch")
	}

	branchHead := gitRun(t, dir, "rev-parse", "HEAD")
	if branchHead != head {
		t.Errorf("new branch HEAD = %q, want %q", branchHead, head)
	}
}

func TestDeleteBranch(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	commitFile(t, dir, "file.txt", "content", "add file")
	ctx := context.Background()

	gitRun(t, dir, "branch", "doomed")

	if err := DeleteBranch(ctx, dir, "doomed"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	// Verify branch is gone
	branches := gitRun(t, dir, "branch", "--list", "doomed")
	if strings.TrimSpace(branches) != "" {
		t.Errorf("branch 'doomed' should be deleted, got %q", branches)
	}
}

func TestEnsureGitignore_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	EnsureGitignore(dir, "worktrees/", ".fleet/")

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "worktrees/") {
		t.Errorf(".gitignore should contain 'worktrees/', got %q", content)
	}
	if !strings.Contains(content, ".fleet/") {
		t.Errorf(".gitignore should contain '.fleet/', got %q", content)
	}
}

func TestEnsureGitignore_NoDoubleAdd(t *testing.T) {
	dir := t.TempDir()
	initial := "worktrees/\n.fleet/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureGitignore(dir, "worktrees/", ".fleet/")

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != initial {
		t.Errorf("expected no changes, .gitignore = %q, want %q", data, initial)
	}
}

func TestEnsureGitignore_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	initial := "*.log\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureGitignore(dir, "worktrees/", ".fleet/")

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "*.log") {
		t.Error("should preserve existing patterns")
	}
	if !strings.Contains(content, "worktrees/") {
		t.Error("should add worktrees/")
	}
	if !strings.Contains(content, ".fleet/") {
		t.Error("should add .fleet/")
	}
	if !strings.Contains(content, "# Fleet runtime") {
		t.Error("should include Fleet comment header")
	}
}

func TestEnsureGitignore_PartiallyPresent(t *testing.T) {
	dir := t.TempDir()
	initial := "worktrees/\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	EnsureGitignore(dir, "worktrees/", ".fleet/")

	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, ".fleet/") {
		t.Error("should add missing .fleet/ pattern")
	}
	// worktrees/ should appear only once
	count := strings.Count(content, "worktrees/")
	if count != 1 {
		t.Errorf("worktrees/ appears %d times, want 1", count)
	}
}

func TestRepoRoot(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	subdir := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// runGit uses cmd.Dir so we need to call from the subdir;
	// RepoRoot always uses "." as dir, so we test via the low-level helper.
	out, err := runGit(ctx, subdir, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatalf("rev-parse --show-toplevel from subdir: %v", err)
	}
	got := strings.TrimSpace(out)

	// Resolve symlinks for comparison (t.TempDir may use symlinks)
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("repo root = %q, want %q", gotResolved, wantResolved)
	}
}
