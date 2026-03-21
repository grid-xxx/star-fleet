package webhook

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// Runner abstracts the fleet pipeline execution, allowing injection for testing.
type Runner interface {
	Run(owner, repo string, number int) error
	Test(owner, repo string, prNumber int) error
}

// Handler routes GitHub webhook events to the fleet pipeline.
type Handler struct {
	label   string
	botUser string
	runner  Runner

	mu      sync.Mutex
	running map[string]bool // per-repo busy tracking: "owner/repo" -> true
	testing map[string]bool // per-repo busy tracking for test runs
}

// NewHandler creates an event handler.
//   - label: the issue label that triggers a run (e.g. "fleet")
//   - botUser: the GitHub login of the bot, used to prevent loops (e.g. "my-app[bot]")
//   - runner: executes the fleet pipeline
func NewHandler(label, botUser string, runner Runner) *Handler {
	return &Handler{
		label:   label,
		botUser: botUser,
		runner:  runner,
		running: make(map[string]bool),
		testing: make(map[string]bool),
	}
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
	Number int    `json:"number"`
	Title  string `json:"title"`
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

	if p.Action != "labeled" {
		log.Printf("handler: issues event action=%q, ignoring (want labeled)", p.Action)
		return "ignored", nil
	}

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

	log.Printf("handler: pull_request #%d action=%q — triggering test", prNumber, p.Action)
	return h.tryTest(p.Repo.Owner.Login, p.Repo.Name, prNumber)
}

func (h *Handler) tryTest(owner, repo string, prNumber int) (string, error) {
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
		if err := h.runner.Test(owner, repo, prNumber); err != nil {
			log.Printf("handler: fleet test failed for %s#%d: %v", key, prNumber, err)
			return
		}
		log.Printf("handler: fleet test completed for %s#%d", key, prNumber)
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
