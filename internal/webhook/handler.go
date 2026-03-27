package webhook

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Runner abstracts the fleet pipeline execution, allowing injection for testing.
type Runner interface {
	Run(owner, repo string, number int) error
	Test(owner, repo, headSHA string, prNumber int) error
}

// IssueLinter abstracts the issue linting process, allowing injection for testing.
type IssueLinter interface {
	// LintIssue runs the lint check. Returns the configured dedup window
	// (zero means use handler default) and any error.
	LintIssue(owner, repo string, issueNumber int) (dedupWindow time.Duration, err error)
}

// Handler routes GitHub webhook events to the fleet pipeline.
type Handler struct {
	label       string
	botUser     string
	runner      Runner
	linter      IssueLinter
	dedupWindow time.Duration // default dedup window for edited events (5m)

	mu              sync.Mutex
	running         map[string]bool          // per-repo busy tracking: "owner/repo" -> true
	testing         map[string]bool          // per-repo busy tracking for test runs
	linting         map[string]bool          // per-repo busy tracking for issue lint
	lintDedup       map[string]time.Time     // "owner/repo#number" -> last lint time
	repoDedupWindow map[string]time.Duration // "owner/repo" -> per-repo dedup window
}

// NewHandler creates an event handler.
//   - label: the issue label that triggers a run (e.g. "fleet")
//   - botUser: the GitHub login of the bot, used to prevent loops (e.g. "my-app[bot]")
//   - runner: executes the fleet pipeline
func NewHandler(label, botUser string, runner Runner) *Handler {
	return &Handler{
		label:           label,
		botUser:         botUser,
		runner:          runner,
		dedupWindow:     5 * time.Minute,
		running:         make(map[string]bool),
		testing:         make(map[string]bool),
		linting:         make(map[string]bool),
		lintDedup:       make(map[string]time.Time),
		repoDedupWindow: make(map[string]time.Duration),
	}
}

// SetLinter configures an optional issue linter for opened/edited events.
func (h *Handler) SetLinter(linter IssueLinter) {
	h.linter = linter
}

// SetDedupWindow configures the dedup window for issue lint events.
func (h *Handler) SetDedupWindow(d time.Duration) {
	h.dedupWindow = d
}

// HandleEvent parses a webhook payload and dispatches it. It returns a short
// status string describing what happened ("triggered", "skipped", "ignored").
func (h *Handler) HandleEvent(eventType string, payload []byte) (string, error) {
	switch eventType {
	case "issues":
		return h.handleIssues(payload)
	case "issue_comment":
		return h.handleIssueComment(payload)
	case "pull_request":
		return h.handlePullRequest(payload)
	default:
		log.Printf("handler: ignoring event type %q", eventType)
		return "ignored", nil
	}
}

// --- payload types (minimal subsets of GitHub's webhook payloads) ---

type issuesPayload struct {
	Action string       `json:"action"`
	Label  payloadLabel `json:"label"`
	Issue  payloadIssue `json:"issue"`
	Sender payloadUser  `json:"sender"`
	Repo   payloadRepo  `json:"repository"`
}

type issueCommentPayload struct {
	Action  string       `json:"action"`
	Issue   payloadIssue `json:"issue"`
	Comment struct {
		Body string      `json:"body"`
		User payloadUser `json:"user"`
	} `json:"comment"`
	Repo payloadRepo `json:"repository"`
}

type pullRequestPayload struct {
	Action      string      `json:"action"`
	Number      int         `json:"number"`
	PullRequest payloadPR   `json:"pull_request"`
	Sender      payloadUser `json:"sender"`
	Repo        payloadRepo `json:"repository"`
}

type payloadPR struct {
	Number int          `json:"number"`
	Title  string       `json:"title"`
	Head   payloadPRRef `json:"head"`
}

type payloadPRRef struct {
	SHA string `json:"sha"`
}

type payloadLabel struct {
	Name string `json:"name"`
}

type payloadIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type payloadUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type payloadRepo struct {
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

func (h *Handler) handleIssues(payload []byte) (string, error) {
	var p issuesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", fmt.Errorf("parsing issues payload: %w", err)
	}

	switch p.Action {
	case "labeled":
		if !strings.EqualFold(p.Label.Name, h.label) {
			log.Printf("handler: label %q does not match %q, skipping", p.Label.Name, h.label)
			return "skipped", nil
		}

		if h.isBot(p.Sender) {
			log.Printf("handler: ignoring event from bot user %q", p.Sender.Login)
			return "skipped", nil
		}

		log.Printf("handler: issue #%d labeled with %q — triggering run", p.Issue.Number, h.label)
		return h.tryRun(p.Repo.Owner.Login, p.Repo.Name, p.Issue.Number)

	case "opened", "edited":
		if h.isBot(p.Sender) {
			log.Printf("handler: ignoring %s event from bot user %q", p.Action, p.Sender.Login)
			return "skipped", nil
		}

		if h.linter == nil {
			log.Printf("handler: issues event action=%q, no linter configured, ignoring", p.Action)
			return "ignored", nil
		}

		log.Printf("handler: issue #%d %s — triggering lint", p.Issue.Number, p.Action)
		return h.tryLint(p.Repo.Owner.Login, p.Repo.Name, p.Issue.Number)

	default:
		log.Printf("handler: issues event action=%q, ignoring", p.Action)
		return "ignored", nil
	}
}

func (h *Handler) handleIssueComment(payload []byte) (string, error) {
	var p issueCommentPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", fmt.Errorf("parsing issue_comment payload: %w", err)
	}

	if p.Action != "created" {
		log.Printf("handler: issue_comment action=%q, ignoring (want created)", p.Action)
		return "ignored", nil
	}

	body := strings.TrimSpace(p.Comment.Body)
	if body != "/fleet run" && !strings.HasPrefix(body, "/fleet run ") && !strings.HasPrefix(body, "/fleet run\n") {
		log.Printf("handler: comment does not match /fleet run command, skipping")
		return "skipped", nil
	}

	if h.isBot(p.Comment.User) {
		log.Printf("handler: ignoring comment from bot user %q", p.Comment.User.Login)
		return "skipped", nil
	}

	log.Printf("handler: /fleet run comment on issue #%d — triggering run", p.Issue.Number)
	return h.tryRun(p.Repo.Owner.Login, p.Repo.Name, p.Issue.Number)
}

func (h *Handler) handlePullRequest(payload []byte) (string, error) {
	var p pullRequestPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", fmt.Errorf("parsing pull_request payload: %w", err)
	}

	if p.Action != "opened" && p.Action != "synchronize" {
		log.Printf("handler: pull_request action=%q, ignoring (want opened/synchronize)", p.Action)
		return "ignored", nil
	}

	if h.isBot(p.Sender) {
		log.Printf("handler: ignoring pull_request from bot user %q", p.Sender.Login)
		return "skipped", nil
	}

	prNumber := p.Number
	if prNumber == 0 {
		prNumber = p.PullRequest.Number
	}

	headSHA := p.PullRequest.Head.SHA

	log.Printf("handler: pull_request #%d action=%q sha=%s — triggering test", prNumber, p.Action, headSHA)
	return h.tryTest(p.Repo.Owner.Login, p.Repo.Name, headSHA, prNumber)
}

func (h *Handler) tryTest(owner, repo, headSHA string, prNumber int) (string, error) {
	key := repoKey(owner, repo)

	h.mu.Lock()
	if h.testing[key] {
		h.mu.Unlock()
		log.Printf("handler: %s test already running, rejecting PR #%d", key, prNumber)
		return "busy", nil
	}
	h.testing[key] = true
	h.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("handler: panic in fleet test for %s#%d: %v", key, prNumber, r)
			}
			h.mu.Lock()
			delete(h.testing, key)
			h.mu.Unlock()
		}()

		log.Printf("handler: starting fleet test for %s#%d", key, prNumber)
		if err := h.runner.Test(owner, repo, headSHA, prNumber); err != nil {
			log.Printf("handler: fleet test failed for %s#%d: %v", key, prNumber, err)
			return
		}
		log.Printf("handler: fleet test completed for %s#%d", key, prNumber)
	}()

	return "triggered", nil
}

func (h *Handler) tryLint(owner, repo string, number int) (string, error) {
	key := repoKey(owner, repo)
	dedupKey := fmt.Sprintf("%s#%d", key, number)

	h.mu.Lock()
	window := h.dedupWindow
	if w, ok := h.repoDedupWindow[key]; ok {
		window = w
	}
	if last, ok := h.lintDedup[dedupKey]; ok && time.Since(last) < window {
		h.mu.Unlock()
		log.Printf("handler: %s issue #%d linted %v ago, dedup skipping", key, number, time.Since(last).Round(time.Second))
		return "skipped", nil
	}
	if h.linting[key] {
		h.mu.Unlock()
		log.Printf("handler: %s lint already running, rejecting issue #%d", key, number)
		return "busy", nil
	}
	h.linting[key] = true
	h.lintDedup[dedupKey] = time.Now()
	h.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("handler: panic in issue lint for %s#%d: %v", key, number, r)
			}
			h.mu.Lock()
			delete(h.linting, key)
			h.mu.Unlock()
		}()

		log.Printf("handler: starting issue lint for %s#%d", key, number)
		cfgWindow, err := h.linter.LintIssue(owner, repo, number)
		if err != nil {
			log.Printf("handler: issue lint failed for %s#%d: %v", key, number, err)
			// Clear dedup on failure so the next event can retry
			h.mu.Lock()
			delete(h.lintDedup, dedupKey)
			h.mu.Unlock()
			return
		}
		// Update per-repo dedup window from config if provided
		if cfgWindow > 0 {
			h.mu.Lock()
			h.repoDedupWindow[key] = cfgWindow
			h.mu.Unlock()
		}
		log.Printf("handler: issue lint completed for %s#%d", key, number)
	}()

	return "triggered", nil
}

func (h *Handler) isBot(user payloadUser) bool {
	if user.Type == "Bot" {
		return true
	}
	if h.botUser != "" && strings.EqualFold(user.Login, h.botUser) {
		return true
	}
	return false
}

func repoKey(owner, repo string) string {
	return owner + "/" + repo
}

func (h *Handler) tryRun(owner, repo string, number int) (string, error) {
	key := repoKey(owner, repo)

	h.mu.Lock()
	if h.running[key] {
		h.mu.Unlock()
		log.Printf("handler: %s already running, rejecting issue #%d", key, number)
		return "busy", nil
	}
	h.running[key] = true
	h.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("handler: panic in fleet run for %s#%d: %v", key, number, r)
			}
			h.mu.Lock()
			delete(h.running, key)
			h.mu.Unlock()
		}()

		log.Printf("handler: starting fleet run for %s#%d", key, number)
		if err := h.runner.Run(owner, repo, number); err != nil {
			log.Printf("handler: fleet run failed for %s#%d: %v", key, number, err)
			return
		}
		log.Printf("handler: fleet run completed for %s#%d", key, number)
	}()

	return "triggered", nil
}
