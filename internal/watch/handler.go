package watch

import (
	"context"
)

// Action represents what the agent should do in response to an event.
type Action string

const (
	ActionReply    Action = "reply"      // Reply to comment only
	ActionFix      Action = "fix"        // Fix code and push
	ActionFixReply Action = "fix_reply"  // Fix code, push, and reply
	ActionNothing  Action = "nothing"    // No action needed
)

// Decision is the handler's decision for how to respond to an event.
type Decision struct {
	Action  Action
	Message string // Reply message or commit message
}

// Agent abstracts the code agent for analyzing, fixing, and replying.
type Agent interface {
	// Decide analyzes the event context and returns a decision on what to do.
	Decide(ctx context.Context, event Event, prDiff string, history []Event) (*Decision, error)
	// Fix applies a code fix based on the prompt and pushes the changes.
	Fix(ctx context.Context, workdir, branch, prompt string) error
	// Reply posts a comment on the PR.
	Reply(ctx context.Context, owner, repo string, prNumber int, body string) error
}
