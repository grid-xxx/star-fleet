package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/nullne/star-fleet/internal/retry"
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
	Status     string `json:"state"`      // "COMPLETED", "IN_PROGRESS", "QUEUED"
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
	out, err := runFn(ctx, "", "issue", "view", strconv.Itoa(number),
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
	_, err := runFn(ctx, "", "issue", "comment", strconv.Itoa(number),
		"--repo", nwo,
		"--body", body)
	return err
}

func CreatePR(ctx context.Context, owner, repo, workdir, title, body, base, head string) (*PR, error) {
	nwo := owner + "/" + repo
	var pr *PR
	err := retry.Do(ctx, func() error {
		out, err := runFn(ctx, workdir, "pr", "create",
			"--repo", nwo,
			"--title", title,
			"--body", body,
			"--base", base,
			"--head", head)
		if err != nil {
			return fmt.Errorf("creating PR: %w", err)
		}
		prURL := strings.TrimSpace(out)
		parts := strings.Split(prURL, "/")
		if len(parts) < 2 {
			return fmt.Errorf("unexpected PR URL: %s", prURL)
		}
		num, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return fmt.Errorf("parsing PR number from URL %s: %w", prURL, err)
		}
		pr = &PR{Number: num, URL: prURL}
		return nil
	})
	return pr, err
}

// FindPR returns an existing open PR for the given head branch, or nil if none exists.
func FindPR(ctx context.Context, owner, repo, head string) (*PR, error) {
	nwo := owner + "/" + repo
	var result *PR
	err := retry.Do(ctx, func() error {
		out, err := runFn(ctx, "", "pr", "list",
			"--repo", nwo,
			"--head", head,
			"--state", "open",
			"--json", "number,url",
			"--limit", "1")
		if err != nil {
			return fmt.Errorf("listing PRs for %s: %w", head, err)
		}
		var prs []PR
		if err := json.Unmarshal([]byte(out), &prs); err != nil {
			return fmt.Errorf("parsing PR list: %w", err)
		}
		if len(prs) == 0 {
			result = nil
		} else {
			result = &prs[0]
		}
		return nil
	})
	return result, err
}

func PostReviewComment(ctx context.Context, owner, repo string, prNumber int, body string) error {
	nwo := owner + "/" + repo
	_, err := runFn(ctx, "", "pr", "review", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--comment",
		"--body", body)
	return err
}

func MergePR(ctx context.Context, owner, repo string, prNumber int) error {
	nwo := owner + "/" + repo
	_, err := runFn(ctx, "", "pr", "merge", strconv.Itoa(prNumber),
		"--repo", nwo,
		"--squash",
		"--delete-branch")
	return err
}

func ClosePR(ctx context.Context, owner, repo string, prNumber int) error {
	nwo := owner + "/" + repo
	_, err := runFn(ctx, "", "pr", "close", strconv.Itoa(prNumber),
		"--repo", nwo)
	return err
}

func CloseIssue(ctx context.Context, owner, repo string, number int) error {
	nwo := owner + "/" + repo
	_, err := runFn(ctx, "", "issue", "close", strconv.Itoa(number),
		"--repo", nwo)
	return err
}

func GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	nwo := owner + "/" + repo
	return runFn(ctx, "", "pr", "diff", strconv.Itoa(prNumber), "--repo", nwo)
}

func DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	nwo := owner + "/" + repo
	out, err := runFn(ctx, "", "repo", "view", nwo, "--json", "defaultBranchRef", "-q", ".defaultBranchRef.name")
	if err != nil {
		return "", fmt.Errorf("detecting default branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// GetPRStatus returns the current state of a PR (OPEN, CLOSED, or MERGED).
func GetPRStatus(ctx context.Context, owner, repo string, prNumber int) (*PRStatus, error) {
	nwo := owner + "/" + repo
	out, err := runFn(ctx, "", "pr", "view", strconv.Itoa(prNumber),
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
	out, err := runFn(ctx, "", "pr", "view", strconv.Itoa(prNumber),
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
	out, err := runFn(ctx, "", "pr", "view", strconv.Itoa(prNumber),
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
	out, err := runFn(ctx, "", "pr", "checks", strconv.Itoa(prNumber),
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

// CIStatus summarizes the overall CI state for a PR.
type CIStatus struct {
	AllGreen bool       // true if all checks completed successfully
	Total    int        // total number of check runs
	Checks   []CheckRun // individual check runs
}

// CheckCIStatus fetches CI check runs for a PR and returns whether all are green.
func CheckCIStatus(ctx context.Context, owner, repo string, prNumber int) (*CIStatus, error) {
	checks, err := ListCheckRuns(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("checking CI status: %w", err)
	}
	if len(checks) == 0 {
		return &CIStatus{AllGreen: false, Total: 0, Checks: nil}, nil
	}

	allGreen := true
	for _, c := range checks {
		if c.Status != "COMPLETED" {
			allGreen = false
			break
		}
		if c.Conclusion != "SUCCESS" && c.Conclusion != "NEUTRAL" && c.Conclusion != "SKIPPED" {
			allGreen = false
			break
		}
	}
	return &CIStatus{AllGreen: allGreen, Total: len(checks), Checks: checks}, nil
}

// GetCheckRunLogs fetches a description of a failed CI check run.
func GetCheckRunLogs(ctx context.Context, owner, repo string, checkRun CheckRun) string {
	if checkRun.ID == 0 {
		return fmt.Sprintf("Check %q failed (conclusion: %s). Details: %s",
			checkRun.Name, checkRun.Conclusion, checkRun.DetailsURL)
	}
	nwo := owner + "/" + repo
	out, err := runFn(ctx, "", "api",
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

// SetRunFn replaces the gh command executor and returns a function to restore
// the original. Intended for use in tests from other packages.
func SetRunFn(fn func(ctx context.Context, dir string, args ...string) (string, error)) func() {
	orig := runFn
	runFn = fn
	return func() { runFn = orig }
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
