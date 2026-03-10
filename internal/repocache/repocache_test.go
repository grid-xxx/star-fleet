package repocache

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func initBareRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init", "--bare")
}

func initRepoWithCommit(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-m", "init")
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s in %s failed: %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func TestEnsure_ClonesNewRepo(t *testing.T) {
	t.Parallel()

	upstream := t.TempDir()
	initRepoWithCommit(t, upstream)

	workdir := t.TempDir()
	cache := New(workdir, func(owner string) (string, error) {
		return "fake-token", nil
	})
	// Override gitRunFn to use local path instead of GitHub URL
	cache.gitRunFn = fakeGitRun(t, upstream)

	repoDir, err := cache.Ensure(context.Background(), "myorg", "myrepo")
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	want := filepath.Join(workdir, "myorg", "myrepo")
	if repoDir != want {
		t.Errorf("repoDir = %q, want %q", repoDir, want)
	}

	if !isGitRepo(repoDir) {
		t.Error("repo dir is not a git repo after clone")
	}

	readme := filepath.Join(repoDir, "README.md")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("README content = %q, want %q", data, "hello")
	}
}

func TestEnsure_FetchesExistingRepo(t *testing.T) {
	t.Parallel()

	upstream := t.TempDir()
	initRepoWithCommit(t, upstream)

	workdir := t.TempDir()
	cache := New(workdir, func(owner string) (string, error) {
		return "fake-token", nil
	})
	cache.gitRunFn = fakeGitRun(t, upstream)

	// First clone
	repoDir, err := cache.Ensure(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	// Add a new commit upstream
	if err := os.WriteFile(filepath.Join(upstream, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, upstream, "git", "add", "-A")
	run(t, upstream, "git", "commit", "-m", "add new.txt")

	// Second call should fetch
	repoDir2, err := cache.Ensure(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if repoDir2 != repoDir {
		t.Errorf("path changed: %q vs %q", repoDir, repoDir2)
	}

	newFile := filepath.Join(repoDir, "new.txt")
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		t.Error("new.txt not present after fetch — fetch did not update")
	}
}

func TestEnsure_TokenError(t *testing.T) {
	t.Parallel()

	workdir := t.TempDir()
	cache := New(workdir, func(owner string) (string, error) {
		return "", fmt.Errorf("auth failure")
	})

	_, err := cache.Ensure(context.Background(), "org", "repo")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "auth failure") {
		t.Errorf("error = %v, want to contain 'auth failure'", err)
	}
}

func TestEnsure_PerRepoLocking(t *testing.T) {
	t.Parallel()

	upstream := t.TempDir()
	initRepoWithCommit(t, upstream)

	workdir := t.TempDir()
	cache := New(workdir, func(owner string) (string, error) {
		return "token", nil
	})
	cache.gitRunFn = fakeGitRun(t, upstream)

	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range 5 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = cache.Ensure(context.Background(), "org", "repo")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

func TestEnsure_DifferentReposParallel(t *testing.T) {
	t.Parallel()

	upstream := t.TempDir()
	initRepoWithCommit(t, upstream)

	workdir := t.TempDir()
	var cloneCount atomic.Int32
	cache := New(workdir, func(owner string) (string, error) {
		return "token", nil
	})
	cache.gitRunFn = func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "clone" {
			cloneCount.Add(1)
		}
		return fakeGitRun(t, upstream)(ctx, dir, args...)
	}

	var wg sync.WaitGroup
	repos := []string{"repo-a", "repo-b", "repo-c"}
	errs := make([]error, len(repos))
	for i, name := range repos {
		wg.Add(1)
		go func(idx int, repoName string) {
			defer wg.Done()
			_, errs[idx] = cache.Ensure(context.Background(), "org", repoName)
		}(i, name)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("repo %s: %v", repos[i], err)
		}
	}

	if got := cloneCount.Load(); got != 3 {
		t.Errorf("clone count = %d, want 3 (one per repo)", got)
	}
}

func TestRepoMutex_SameForSameRepo(t *testing.T) {
	t.Parallel()
	cache := New("/tmp/test", func(string) (string, error) { return "", nil })

	mu1 := cache.RepoMutex("org", "repo")
	mu2 := cache.RepoMutex("org", "repo")
	if mu1 != mu2 {
		t.Error("expected same mutex for same repo")
	}
}

func TestRepoMutex_DifferentForDifferentRepos(t *testing.T) {
	t.Parallel()
	cache := New("/tmp/test", func(string) (string, error) { return "", nil })

	mu1 := cache.RepoMutex("org", "repo-a")
	mu2 := cache.RepoMutex("org", "repo-b")
	if mu1 == mu2 {
		t.Error("expected different mutexes for different repos")
	}
}

func TestIsGitRepo(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		dir := t.TempDir()
		initRepoWithCommit(t, dir)
		if !isGitRepo(dir) {
			t.Error("expected true for git repo")
		}
	})

	t.Run("empty dir", func(t *testing.T) {
		dir := t.TempDir()
		if isGitRepo(dir) {
			t.Error("expected false for empty dir")
		}
	})

	t.Run("nonexistent", func(t *testing.T) {
		if isGitRepo("/nonexistent/path") {
			t.Error("expected false for nonexistent path")
		}
	})
}

// fakeGitRun intercepts "git clone" commands to clone from a local path
// instead of a GitHub URL. All other git commands pass through normally.
func fakeGitRun(t *testing.T, upstream string) func(ctx context.Context, dir string, args ...string) (string, error) {
	t.Helper()
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) >= 3 && args[0] == "clone" {
			dest := args[2]
			return defaultGitRun(ctx, "", "clone", upstream, dest)
		}
		if len(args) >= 4 && args[0] == "remote" && args[1] == "set-url" {
			return defaultGitRun(ctx, dir, "remote", "set-url", args[2], upstream)
		}
		return defaultGitRun(ctx, dir, args...)
	}
}
