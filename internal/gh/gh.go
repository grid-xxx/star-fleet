package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

type RepoInfo struct {
	Owner string
	Repo  string
}

func CurrentRepo(ctx context.Context) (*RepoInfo, error) {
	out, err := run(ctx, "", "repo", "view", "--json", "owner,name", "-q", ".owner + \"/\" + .name")
	if err != nil {
		return nil, fmt.Errorf("detecting repo: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(out), "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected repo format: %s", out)
	}
	return &RepoInfo{Owner: parts[0], Repo: parts[1]}, nil
}

func FetchIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "issue", "view", strconv.Itoa(number),
		"--repo", nwo,
		"--json", "number,title,body,url,state")
	if err != nil {
		return nil, fmt.Errorf("fetching issue #%d: %w", number, err)
	}
	var issue Issue
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		return nil, fmt.Errorf("parsing issue JSON: %w", err)
	}
	return &issue, nil
}

func PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	nwo := owner + "/" + repo
	_, err := run(ctx, "", "issue", "comment", strconv.Itoa(number),
		"--repo", nwo,
		"--body", body)
	return err
}

func CreatePR(ctx context.Context, repoDir, title, body, base, head string) (*PR, error) {
	out, err := run(ctx, repoDir, "pr", "create",
		"--title", title,
		"--body", body,
		"--base", base,
		"--head", head,
		"--json", "number,url")
	if err != nil {
		return nil, fmt.Errorf("creating PR: %w", err)
	}
	var pr PR
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return nil, fmt.Errorf("parsing PR JSON: %w", err)
	}
	return &pr, nil
}

func PostReviewComment(ctx context.Context, owner, repo string, prNumber int, body string) error {
	nwo := owner + "/" + repo
	_, err := run(ctx, "", "pr", "review", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--comment",
		"--body", body)
	return err
}

func MergePR(ctx context.Context, owner, repo string, prNumber int) error {
	nwo := owner + "/" + repo
	_, err := run(ctx, "", "pr", "merge", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--merge",
		"--delete-branch=false")
	return err
}

func ClosePR(ctx context.Context, owner, repo string, prNumber int) error {
	nwo := owner + "/" + repo
	_, err := run(ctx, "", "pr", "close", strconv.Itoa(prNumber),
		"--repo", nwo)
	return err
}

func CloseIssue(ctx context.Context, owner, repo string, number int) error {
	nwo := owner + "/" + repo
	_, err := run(ctx, "", "issue", "close", strconv.Itoa(number),
		"--repo", nwo)
	return err
}

func GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	nwo := owner + "/" + repo
	return run(ctx, "", "pr", "diff", strconv.Itoa(prNumber), "--repo", nwo)
}

func DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "repo", "view", nwo, "--json", "defaultBranchRef", "-q", ".defaultBranchRef.name")
	if err != nil {
		return "", fmt.Errorf("detecting default branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %s: %w", strings.Join(args, " "), stderr.String(), err)
	}
	return stdout.String(), nil
}
