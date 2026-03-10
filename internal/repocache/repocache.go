package repocache

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// TokenFunc returns a fresh installation access token for the given owner.
type TokenFunc func(owner string) (string, error)

// Cache manages local clones of repositories under a workdir.
// Each repo is stored at <workdir>/<owner>/<repo>/ and protected by a per-repo mutex.
type Cache struct {
	workdir  string
	tokenFn  TokenFunc
	mu       sync.Mutex
	repoMu   map[string]*sync.Mutex
	gitRunFn func(ctx context.Context, dir string, args ...string) (string, error)
}

// New creates a Cache rooted at workdir.
func New(workdir string, tokenFn TokenFunc) *Cache {
	return &Cache{
		workdir:  workdir,
		tokenFn:  tokenFn,
		repoMu:   make(map[string]*sync.Mutex),
		gitRunFn: defaultGitRun,
	}
}

// Ensure guarantees a local clone of owner/repo exists and is up-to-date.
// It acquires a per-repo lock, so concurrent calls for the same repo are serialized,
// while different repos can proceed in parallel.
// Returns the absolute path to the repo directory.
func (c *Cache) Ensure(ctx context.Context, owner, repo string) (string, error) {
	mu := c.repoMutex(owner, repo)
	mu.Lock()
	defer mu.Unlock()

	repoDir := filepath.Join(c.workdir, owner, repo)

	token, err := c.tokenFn(owner)
	if err != nil {
		return "", fmt.Errorf("getting token for %s/%s: %w", owner, repo, err)
	}

	if isGitRepo(repoDir) {
		log.Printf("repocache: fetching %s/%s", owner, repo)
		if err := c.fetch(ctx, repoDir, owner, repo, token); err != nil {
			return "", fmt.Errorf("fetching %s/%s: %w", owner, repo, err)
		}
		return repoDir, nil
	}

	log.Printf("repocache: cloning %s/%s", owner, repo)
	if err := c.clone(ctx, owner, repo, token, repoDir); err != nil {
		return "", fmt.Errorf("cloning %s/%s: %w", owner, repo, err)
	}
	return repoDir, nil
}

// RepoMutex returns the per-repo mutex for the given owner/repo.
// Exposed for the handler to serialize webhook processing per-repo.
func (c *Cache) RepoMutex(owner, repo string) *sync.Mutex {
	return c.repoMutex(owner, repo)
}

func (c *Cache) repoMutex(owner, repo string) *sync.Mutex {
	key := owner + "/" + repo
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.repoMu[key]; !ok {
		c.repoMu[key] = &sync.Mutex{}
	}
	return c.repoMu[key]
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

func (c *Cache) clone(ctx context.Context, owner, repo, token, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}

	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)
	_, err := c.gitRunFn(ctx, "", "clone", cloneURL, dest)
	if err != nil {
		return err
	}

	if _, err := c.gitRunFn(ctx, dest, "config", "user.name", "star-fleets[bot]"); err != nil {
		return fmt.Errorf("setting git user.name: %w", err)
	}
	if _, err := c.gitRunFn(ctx, dest, "config", "user.email", "star-fleets[bot]@users.noreply.github.com"); err != nil {
		return fmt.Errorf("setting git user.email: %w", err)
	}

	return nil
}

func (c *Cache) fetch(ctx context.Context, dir, owner, repo, token string) error {
	remoteURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)
	if _, err := c.gitRunFn(ctx, dir, "remote", "set-url", "origin", remoteURL); err != nil {
		return fmt.Errorf("setting remote URL: %w", err)
	}

	if _, err := c.gitRunFn(ctx, dir, "fetch", "origin"); err != nil {
		return err
	}

	branch, err := c.gitRunFn(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil
	}
	branch = strings.TrimSpace(branch)
	if branch != "" && branch != "HEAD" {
		if _, err := c.gitRunFn(ctx, dir, "reset", "--hard", "origin/"+branch); err != nil {
			log.Printf("repocache: reset to origin/%s failed (non-fatal): %v", branch, err)
		}
	}

	return nil
}

func defaultGitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), stderr.String(), err)
	}
	return stdout.String(), nil
}
