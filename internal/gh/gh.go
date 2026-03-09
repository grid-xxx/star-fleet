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

// PRStatus holds the current state of a pull request.
type PRStatus struct {
	State string `json:"state"` // "OPEN", "CLOSED", "MERGED"
}

// PRComment represents a comment on a PR.
type PRComment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
	URL       string `json:"url"`
}

// PRReview represents a submitted review on a PR.
type PRReview struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	State     string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED
	Author    string `json:"author"`
	CreatedAt string `json:"submittedAt"`
}

// CheckRun represents a CI check run on a PR.
type CheckRun struct {
	ID         int    `json:"databaseId"`
	Name       string `json:"name"`
	Status     string `json:"status"`     // "COMPLETED", "IN_PROGRESS", "QUEUED"
	Conclusion string `json:"conclusion"` // "SUCCESS", "FAILURE", "NEUTRAL", etc.
	DetailsURL string `json:"detailsUrl"`
}

func CurrentRepo(ctx context.Context) (*RepoInfo, error) {
	out, err := runFn(ctx, "", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
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

func CreatePR(ctx context.Context, owner, repo, workdir, title, body, base, head string) (*PR, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, workdir, "pr", "create",
		"--repo", nwo,
		"--title", title,
		"--body", body,
		"--base", base,
		"--head", head)
	if err != nil {
		return nil, fmt.Errorf("creating PR: %w", err)
	}
	prURL := strings.TrimSpace(out)
	parts := strings.Split(prURL, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected PR URL: %s", prURL)
	}
	num, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return nil, fmt.Errorf("parsing PR number from URL %s: %w", prURL, err)
	}
	return &PR{Number: num, URL: prURL}, nil
}

// FindPR returns an existing open PR for the given head branch, or nil if none exists.
func FindPR(ctx context.Context, owner, repo, head string) (*PR, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "pr", "list",
		"--repo", nwo,
		"--head", head,
		"--state", "open",
		"--json", "number,url",
		"--limit", "1")
	if err != nil {
		return nil, fmt.Errorf("listing PRs for %s: %w", head, err)
	}
	var prs []PR
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("parsing PR list: %w", err)
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
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

// GetPRStatus returns the current state of a PR (OPEN, CLOSED, or MERGED).
func GetPRStatus(ctx context.Context, owner, repo string, prNumber int) (*PRStatus, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "pr", "view", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--json", "state")
	if err != nil {
		return nil, fmt.Errorf("getting PR status: %w", err)
	}
	var status PRStatus
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		return nil, fmt.Errorf("parsing PR status: %w", err)
	}
	return &status, nil
}

// ListPRComments returns comments on a PR (issue comments).
func ListPRComments(ctx context.Context, owner, repo string, prNumber int) ([]PRComment, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "pr", "view", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--json", "comments",
		"--jq", ".comments[] | {id: .id, body: .body, author: .author.login, createdAt: .createdAt, url: .url}")
	if err != nil {
		return nil, fmt.Errorf("listing PR comments: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	// gh --jq outputs one JSON object per line (NDJSON)
	var comments []PRComment
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c PRComment
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// ListPRReviews returns submitted reviews on a PR.
func ListPRReviews(ctx context.Context, owner, repo string, prNumber int) ([]PRReview, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "pr", "view", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--json", "reviews",
		"--jq", ".reviews[] | {id: .id, body: .body, state: .state, author: .author.login, submittedAt: .submittedAt}")
	if err != nil {
		return nil, fmt.Errorf("listing PR reviews: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var reviews []PRReview
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r PRReview
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}

// ListCheckRuns returns CI check runs for a PR.
func ListCheckRuns(ctx context.Context, owner, repo string, prNumber int) ([]CheckRun, error) {
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "pr", "checks", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--json", "name,state,conclusion,detailsUrl")
	if err != nil {
		// pr checks can fail if there are no checks configured
		return nil, nil
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var checks []CheckRun
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		return nil, nil
	}
	return checks, nil
}

// GetCheckRunLogs fetches a description of a failed CI check run.
func GetCheckRunLogs(ctx context.Context, owner, repo string, checkRun CheckRun) string {
	if checkRun.ID == 0 {
		return fmt.Sprintf("Check %q failed (conclusion: %s). Details: %s",
			checkRun.Name, checkRun.Conclusion, checkRun.DetailsURL)
	}
	nwo := owner + "/" + repo
	out, err := run(ctx, "", "api",
		fmt.Sprintf("repos/%s/check-runs/%d/annotations", nwo, checkRun.ID),
		"--jq", ".[].message")
	if err != nil || strings.TrimSpace(out) == "" {
		return fmt.Sprintf("Check %q failed (conclusion: %s). Details: %s",
			checkRun.Name, checkRun.Conclusion, checkRun.DetailsURL)
	}
	return fmt.Sprintf("Check %q failed:\n%s", checkRun.Name, strings.TrimSpace(out))
}

// runFn is the function used to execute gh commands.
// It can be overridden in tests to avoid shelling out.
var runFn = run

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
