package watch

import (
	"context"
	"fmt"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
	"github.com/nullne/star-fleet/internal/state"
)

// HandleResult describes what the handler did in response to an event.
type HandleResult struct {
	Action  string // "fix", "reply", "none"
	Message string // summary of what happened
}

// HandleEvent processes a single PR event by invoking the agent and taking
// appropriate action (push code fixes, post replies).
func HandleEvent(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, event Event, prNumber int) (*HandleResult, error) {
	// Build context for the agent
	eventContext := buildEventContext(event)

	// Get the current commit before running the agent
	beforeRef, _ := currentRef(ctx, codeAgent.Workdir)

	// Run agent to handle the event
	response, err := codeAgent.HandleEvent(ctx, eventContext)
	if err != nil {
		return nil, fmt.Errorf("agent handling event: %w", err)
	}

	// Check if agent made code changes
	afterRef, _ := currentRef(ctx, codeAgent.Workdir)
	hasCodeChanges := beforeRef != "" && afterRef != "" && beforeRef != afterRef

	// Also check for uncommitted changes and commit them
	hasUncommitted, _ := git.HasChanges(ctx, codeAgent.Workdir)
	if hasUncommitted {
		if err := git.CommitAll(ctx, codeAgent.Workdir, "fix: address PR feedback"); err == nil {
			hasCodeChanges = true
		}
	}

	// Push if there are code changes
	if hasCodeChanges {
		if err := git.ForcePush(ctx, codeAgent.Workdir, "origin", codeAgent.Branch); err != nil {
			return nil, fmt.Errorf("pushing fixes: %w", err)
		}
		s.FixCount++
		_ = s.Save()
	}

	// Determine action and post reply
	response = strings.TrimSpace(response)
	if response == "" || strings.HasPrefix(strings.ToUpper(response), "NO_ACTION") {
		s.RecordEvent(event.ID)
		_ = s.Save()
		return &HandleResult{Action: "none", Message: "no action needed"}, nil
	}

	// Post the response as a PR comment
	action := "reply"
	if hasCodeChanges {
		action = "fix"
	}

	reply := response
	if hasCodeChanges {
		reply = fmt.Sprintf("%s\n\n_Fixed in latest push._", response)
	}

	_ = gh.PostComment(ctx, codeAgent.Owner, codeAgent.Repo, prNumber, reply)

	s.RecordEvent(event.ID)
	_ = s.Save()

	return &HandleResult{Action: action, Message: truncate(response, 80)}, nil
}

func buildEventContext(event Event) string {
	var b strings.Builder

	switch event.Type {
	case EventComment:
		fmt.Fprintf(&b, "### PR Comment from @%s\n\n%s", event.Author, event.Detail)
	case EventReview:
		fmt.Fprintf(&b, "### PR Review from @%s\n\n%s", event.Author, event.Detail)
	case EventCIFail:
		fmt.Fprintf(&b, "### CI Failure\n\n%s", event.Detail)
	case EventCIPass:
		fmt.Fprintf(&b, "### CI Status\n\nAll CI checks have passed.")
	default:
		fmt.Fprintf(&b, "### Event\n\n%s", event.Summary)
	}

	return b.String()
}

func currentRef(ctx context.Context, dir string) (string, error) {
	return git.CurrentHead(ctx, dir)
}

func truncate(s string, max int) string {
	// Truncate to first line and max chars
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
