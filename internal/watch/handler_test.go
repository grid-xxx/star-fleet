package watch

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Action and Decision constants
// ---------------------------------------------------------------------------

func TestActionConstants(t *testing.T) {
	tests := []struct {
		action   Action
		expected string
	}{
		{ActionReply, "reply"},
		{ActionFix, "fix"},
		{ActionFixReply, "fix_reply"},
		{ActionNothing, "nothing"},
	}
	for _, tt := range tests {
		if string(tt.action) != tt.expected {
			t.Errorf("Action = %q, want %q", tt.action, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Agent interface compliance
// ---------------------------------------------------------------------------

func TestMockAgentImplementsInterface(t *testing.T) {
	var _ Agent = &mockAgent{}
}

// ---------------------------------------------------------------------------
// ProcessEvent dispatch tests
// These verify that ProcessEvent correctly dispatches the agent's decision.
// ---------------------------------------------------------------------------

func TestProcessEventFixAction(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionFix, Message: "Fixed the typo in variable name"}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "review-1",
		Type: EventReviewComment,
		Body: "There's a typo in the variable name on line 15",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionFix {
		t.Errorf("action = %q, want %q", decision.Action, ActionFix)
	}
	if len(agent.fixCalls) != 1 {
		t.Errorf("expected 1 fix call, got %d", len(agent.fixCalls))
	}
}

func TestProcessEventReplyAction(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionReply, Message: "The current implementation handles this case via the fallback in line 42"}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "review-2",
		Type: EventReviewComment,
		Body: "Why did you choose this approach over using a map?",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionReply {
		t.Errorf("action = %q, want %q", decision.Action, ActionReply)
	}
	if len(agent.replyCalls) != 1 {
		t.Errorf("expected 1 reply call, got %d", len(agent.replyCalls))
	}
	// Fix should NOT be called for reply-only actions
	if len(agent.fixCalls) != 0 {
		t.Errorf("fix should not be called for reply-only, got %d calls", len(agent.fixCalls))
	}
}

func TestProcessEventFixReplyAction(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionFixReply, Message: "Fixed in abc123"}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "review-3",
		Type: EventReviewComment,
		Body: "This function should validate its input",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionFixReply {
		t.Errorf("action = %q, want %q", decision.Action, ActionFixReply)
	}
	// Both fix and reply should be called
	if len(agent.fixCalls) != 1 {
		t.Errorf("expected 1 fix call, got %d", len(agent.fixCalls))
	}
	if len(agent.replyCalls) != 1 {
		t.Errorf("expected 1 reply call, got %d", len(agent.replyCalls))
	}
}

func TestProcessEventNothingAction(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "comment-1",
		Type: EventPRComment,
		Body: "LGTM",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionNothing {
		t.Errorf("action = %q, want %q", decision.Action, ActionNothing)
	}
	if len(agent.fixCalls) != 0 {
		t.Errorf("fix should not be called, got %d calls", len(agent.fixCalls))
	}
	if len(agent.replyCalls) != 0 {
		t.Errorf("reply should not be called, got %d calls", len(agent.replyCalls))
	}
}

// ---------------------------------------------------------------------------
// ProcessEvent state management
// ---------------------------------------------------------------------------

func TestProcessEventIncrementsFixCount(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionFix, Message: "fixing"}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
		FixCount:        0,
	}

	event := Event{ID: "review-1", Type: EventReviewComment, Body: "Fix this"}
	_, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}

	if w.FixCount != 1 {
		t.Errorf("FixCount = %d, want 1", w.FixCount)
	}
}

func TestProcessEventReplyDoesNotIncrementFixCount(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionReply, Message: "explanation"}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
		FixCount:        1,
	}

	event := Event{ID: "review-1", Type: EventReviewComment, Body: "Question"}
	_, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}

	if w.FixCount != 1 {
		t.Errorf("FixCount = %d, want 1 (reply should not increment)", w.FixCount)
	}
}

func TestProcessEventMarksProcessed(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{ID: "comment-42", Type: EventPRComment, Body: "test"}
	_, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}

	if !w.IsProcessed("comment-42") {
		t.Error("event should be marked as processed after ProcessEvent")
	}
}

func TestProcessEventUpdatesLastEventTime(t *testing.T) {
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionNothing}, nil
		},
	}

	past := time.Now().Add(-1 * time.Hour)
	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
		LastEventAt:     past,
	}

	event := Event{ID: "comment-1", Type: EventPRComment, Body: "test", CreatedAt: time.Now()}
	_, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}

	if !w.LastEventAt.After(past) {
		t.Error("LastEventAt should be updated after processing an event")
	}
}

// ---------------------------------------------------------------------------
// Event type handling scenarios (verifying "think before acting" principle)
// ---------------------------------------------------------------------------

func TestHandleReviewComment_ActionableSuggestion(t *testing.T) {
	// "Reasonable suggestion → Fix code, reply 'Fixed in <commit>'"
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			if e.Type == EventReviewComment {
				return &Decision{Action: ActionFixReply, Message: "Fixed in abc1234"}, nil
			}
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "review-1",
		Type: EventReviewComment,
		Body: "This variable should be renamed to follow Go conventions",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionFixReply {
		t.Errorf("actionable suggestion should result in fix_reply, got %q", decision.Action)
	}
}

func TestHandleReviewComment_DiscussionQuestion(t *testing.T) {
	// "Discussion/question → Reply with explanation, don't change code"
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			if e.Type == EventReviewComment {
				return &Decision{Action: ActionReply, Message: "Good question. I used a slice here because..."}, nil
			}
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "review-2",
		Type: EventReviewComment,
		Body: "Why did you choose a slice instead of a map here?",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionReply {
		t.Errorf("discussion should result in reply only, got %q", decision.Action)
	}
	if len(agent.fixCalls) != 0 {
		t.Error("discussion should not trigger a code fix")
	}
}

func TestHandleCIFailure_OwnCode(t *testing.T) {
	// "CI failure from own code → Fix"
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			if e.Type == EventCheckFail {
				return &Decision{Action: ActionFix, Message: "Fix CI failure: missing import"}, nil
			}
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "check-1",
		Type: EventCheckFail,
		Body: "Build failed: undefined reference to 'NewWatcher'",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionFix {
		t.Errorf("CI failure from own code should result in fix, got %q", decision.Action)
	}
	if len(agent.fixCalls) != 1 {
		t.Errorf("expected 1 fix call, got %d", len(agent.fixCalls))
	}
}

func TestHandleCIFailure_FlakyTest(t *testing.T) {
	// "CI failure from flaky test / environment → Explain, don't change"
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			if e.Type == EventCheckFail {
				return &Decision{Action: ActionReply, Message: "This appears to be a flaky test unrelated to my changes"}, nil
			}
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "check-2",
		Type: EventCheckFail,
		Body: "Test timeout: network connection refused (flaky)",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionReply {
		t.Errorf("flaky test should result in reply (explain), got %q", decision.Action)
	}
	if len(agent.fixCalls) != 0 {
		t.Error("flaky test should not trigger a code fix")
	}
}

func TestHandleCIPass_Summary(t *testing.T) {
	// "CI pass → Comment summary"
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			if e.Type == EventCheckPass {
				return &Decision{Action: ActionReply, Message: "All CI checks passing"}, nil
			}
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "check-3",
		Type: EventCheckPass,
		Body: "All checks passed",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionReply {
		t.Errorf("CI pass should result in reply (summary), got %q", decision.Action)
	}
}

func TestHandleCIFailure_Unrelated(t *testing.T) {
	// "CI failure unrelated → Ignore"
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:   "check-4",
		Type: EventCheckFail,
		Body: "Deploy to staging failed: infrastructure issue",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionNothing {
		t.Errorf("unrelated CI failure should be ignored, got %q", decision.Action)
	}
	if len(agent.fixCalls) != 0 {
		t.Error("unrelated CI failure should not trigger a fix")
	}
	if len(agent.replyCalls) != 0 {
		t.Error("unrelated CI failure should not trigger a reply")
	}
}

func TestHandlePRComment(t *testing.T) {
	// PR comments (not review comments) should also be processed
	agent := &mockAgent{
		decideFn: func(e Event) (*Decision, error) {
			if e.Type == EventPRComment {
				return &Decision{Action: ActionReply, Message: "Thanks for the feedback!"}, nil
			}
			return &Decision{Action: ActionNothing}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
	}

	event := Event{
		ID:     "comment-1",
		Type:   EventPRComment,
		Body:   "Can you also add a test for the edge case?",
		Author: "maintainer",
	}

	decision, err := w.ProcessEvent(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != ActionReply {
		t.Errorf("PR comment should be handled, got action %q", decision.Action)
	}
}

// ---------------------------------------------------------------------------
// Multiple fixes tracking
// ---------------------------------------------------------------------------

func TestMultipleFixesIncrementCount(t *testing.T) {
	fixCount := 0
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionFix, Message: "fixing"}, nil
		},
	}

	w := &Watcher{
		Owner:           "owner",
		Repo:            "repo",
		PRNumber:        42,
		Branch:          "fleet/42",
		Workdir:         "/work",
		Source:          &mockEventSource{prState: PROpen},
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
		FixCount:        fixCount,
	}

	events := []Event{
		{ID: "r-1", Type: EventReviewComment, Body: "Fix 1"},
		{ID: "r-2", Type: EventReviewComment, Body: "Fix 2"},
		{ID: "r-3", Type: EventReviewComment, Body: "Fix 3"},
	}

	for _, e := range events {
		_, err := w.ProcessEvent(context.Background(), e)
		if err != nil {
			t.Fatal(err)
		}
	}

	if w.FixCount != 3 {
		t.Errorf("FixCount = %d, want 3 after 3 fixes", w.FixCount)
	}
}
