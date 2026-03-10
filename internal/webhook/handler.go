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
}

// Handler routes GitHub webhook events to the fleet pipeline.
type Handler struct {
	label   string
	botUser string
	runner  Runner

	mu   sync.Mutex
	busy bool
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

func (h *Handler) isBot(user payloadUser) bool {
	if user.Type == "Bot" {
		return true
	}
	if h.botUser != "" && strings.EqualFold(user.Login, h.botUser) {
		return true
	}
	return false
}

func (h *Handler) tryRun(owner, repo string, number int) (string, error) {
	h.mu.Lock()
	if h.busy {
		h.mu.Unlock()
		log.Printf("handler: already running, rejecting issue #%d", number)
		return "busy", nil
	}
	h.busy = true
	h.mu.Unlock()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("handler: panic in fleet run for %s/%s#%d: %v", owner, repo, number, r)
			}
			h.mu.Lock()
			h.busy = false
			h.mu.Unlock()
		}()

		log.Printf("handler: starting fleet run for %s/%s#%d", owner, repo, number)
		if err := h.runner.Run(owner, repo, number); err != nil {
			log.Printf("handler: fleet run failed for %s/%s#%d: %v", owner, repo, number, err)
			return
		}
		log.Printf("handler: fleet run completed for %s/%s#%d", owner, repo, number)
	}()

	return "triggered", nil
}
