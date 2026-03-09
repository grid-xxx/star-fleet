package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nullne/star-fleet/internal/retry"
)

func CreateWorktree(ctx context.Context, repoRoot, name, branch string) (string, error) {
	dir := filepath.Join(repoRoot, "worktrees", name)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("creating worktrees dir: %w", err)
	}

	EnsureGitignore(repoRoot, "worktrees/", ".fleet/")

	if _, err := runGit(ctx, repoRoot, "worktree", "add", "-b", branch, dir); err != nil {
		// Branch may already exist from a prior run; try without -b
		if _, err2 := runGit(ctx, repoRoot, "worktree", "add", dir, branch); err2 != nil {
			// Branch may be checked out by another worktree; use detached HEAD
			if _, err3 := runGit(ctx, repoRoot, "worktree", "add", "--detach", dir, branch); err3 != nil {
				return "", fmt.Errorf("creating worktree %s: %w (also tried existing branch: %v)", name, err, err2)
			}
		}
	}
	return dir, nil
}

// EnsureGitignore adds the given patterns to the repo's .gitignore if they
// are not already present. This prevents fleet runtime artifacts from
// polluting `git status` in the target repo.
func EnsureGitignore(repoRoot string, patterns ...string) {
	gi := filepath.Join(repoRoot, ".gitignore")

	existing := ""
	if data, err := os.ReadFile(gi); err == nil {
		existing = string(data)
	}

	lines := strings.Split(existing, "\n")
	present := make(map[string]bool, len(lines))
	for _, l := range lines {
		present[strings.TrimSpace(l)] = true
	}

	var toAdd []string
	for _, p := range patterns {
		if !present[p] {
			toAdd = append(toAdd, p)
		}
	}

	if len(toAdd) == 0 {
		return
	}

	content := existing
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n# Fleet runtime (auto-added)\n"
	}
	for _, p := range toAdd {
		content += p + "\n"
	}

	_ = os.WriteFile(gi, []byte(content), 0o644)
}

func RemoveWorktree(ctx context.Context, repoRoot, name string) error {
	dir := filepath.Join(repoRoot, "worktrees", name)
	_, err := runGit(ctx, repoRoot, "worktree", "remove", dir, "--force")
	return err
}

func Checkout(ctx context.Context, dir, ref string) error {
	_, err := runGit(ctx, dir, "checkout", ref)
	return err
}

func CreateBranch(ctx context.Context, dir, branch, startPoint string) error {
	_, err := runGit(ctx, dir, "checkout", "-b", branch, startPoint)
	return err
}

func Merge(ctx context.Context, dir, branch string) error {
	_, err := runGit(ctx, dir, "merge", "--no-ff", branch, "-m", fmt.Sprintf("Merge %s", branch))
	return err
}

func Push(ctx context.Context, dir, remote, branch string) error {
	return retry.Do(ctx, func() error {
		_, err := runGit(ctx, dir, "push", "-u", remote, branch)
		return err
	})
}

func ForcePush(ctx context.Context, dir, remote, branch string) error {
	_, err := runGit(ctx, dir, "push", "--force-with-lease", "-u", remote, branch)
	return err
}

func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func CurrentHead(ctx context.Context, dir string) (string, error) {
	out, err := runGit(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func DiffNames(ctx context.Context, dir, base, head string, patterns ...string) ([]string, error) {
	args := []string{"diff", "--name-only", base + ".." + head, "--"}
	args = append(args, patterns...)
	out, err := runGit(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func RemoveFiles(ctx context.Context, dir string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f", "--"}, files...)
	_, err := runGit(ctx, dir, args...)
	if err != nil {
		return err
	}
	_, err = runGit(ctx, dir, "commit", "-m", "Strip dev-authored test files for cross-validation")
	return err
}

func HasChanges(ctx context.Context, dir string) (bool, error) {
	out, err := runGit(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func CommitAll(ctx context.Context, dir, message string) error {
	if _, err := runGit(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	_, err := runGit(ctx, dir, "commit", "-m", message, "--allow-empty")
	return err
}

func RepoRoot(ctx context.Context) (string, error) {
	out, err := runGit(ctx, ".", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func DeleteBranch(ctx context.Context, dir, branch string) error {
	_, err := runGit(ctx, dir, "branch", "-D", branch)
	return err
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), stderr.String(), err)
	}
	return stdout.String(), nil
}
