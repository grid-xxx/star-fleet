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
	gitRunFn func(ctx context.Context, dir string, env []string, args ...string) (string, error)
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

// credentialEnv returns env vars that configure git to use an inline credential
// helper, keeping the token out of URLs, command args, and .git/config.
func credentialEnv(token string) []string {
	helper := fmt.Sprintf("!f() { echo username=x-access-token; echo password=%s; }; f", token)
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=" + helper,
	}
}

func (c *Cache) clone(ctx context.Context, owner, repo, token, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	authEnv := credentialEnv(token)
	if _, err := c.gitRunFn(ctx, "", authEnv, "clone", cloneURL, dest); err != nil {
		return err
	}

	if _, err := c.gitRunFn(ctx, dest, nil, "config", "user.name", "star-fleets[bot]"); err != nil {
		return fmt.Errorf("setting git user.name: %w", err)
	}
	if _, err := c.gitRunFn(ctx, dest, nil, "config", "user.email", "star-fleets[bot]@users.noreply.github.com"); err != nil {
		return fmt.Errorf("setting git user.email: %w", err)
	}

	return nil
}

func (c *Cache) fetch(ctx context.Context, dir, owner, repo, token string) error {
	authEnv := credentialEnv(token)

	if _, err := c.gitRunFn(ctx, dir, authEnv, "fetch", "origin"); err != nil {
		return err
	}

	branch, err := c.gitRunFn(ctx, dir, nil, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		log.Printf("repocache: rev-parse HEAD failed for %s/%s (non-fatal): %v", owner, repo, err)
		return nil
	}
	branch = strings.TrimSpace(branch)
	if branch != "" && branch != "HEAD" {
		if _, err := c.gitRunFn(ctx, dir, nil, "reset", "--hard", "origin/"+branch); err != nil {
			log.Printf("repocache: reset to origin/%s failed (non-fatal): %v", branch, err)
		}
	}

	return nil
}

func defaultGitRun(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", redactArgs(args), stderr.String(), err)
	}
	return stdout.String(), nil
}

// redactArgs returns args joined as a string with any token-bearing URLs sanitized.
func redactArgs(args []string) string {
	redacted := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(a, "x-access-token:") {
			if idx := strings.Index(a, "x-access-token:"); idx >= 0 {
				end := strings.Index(a[idx:], "@")
				if end > 0 {
					a = a[:idx] + "x-access-token:REDACTED" + a[idx+end:]
				}
			}
		}
		redacted[i] = a
	}
	return strings.Join(redacted, " ")
}
