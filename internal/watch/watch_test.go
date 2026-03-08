package watch

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type mockEventSource struct {
	comments []Event
	reviews  []Event
	checks   []CheckRun
	prState  PRState
	fetchErr error
}

func (m *mockEventSource) FetchComments(_ context.Context, _, _ string, _ int, _ time.Time) ([]Event, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.comments, nil
}

func (m *mockEventSource) FetchReviews(_ context.Context, _, _ string, _ int, _ time.Time) ([]Event, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.reviews, nil
}

func (m *mockEventSource) FetchCheckRuns(_ context.Context, _, _ string, _ int) ([]CheckRun, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.checks, nil
}

func (m *mockEventSource) GetPRState(_ context.Context, _, _ string, _ int) (PRState, error) {
	return m.prState, nil
}

// dynamicMockEventSource generates new events on each call for testing loops.
type dynamicMockEventSource struct {
	prStateFn  func() PRState
	commentsFn func() []Event
	reviewsFn  func() []Event
	checksFn   func() []CheckRun
}

func (d *dynamicMockEventSource) FetchComments(_ context.Context, _, _ string, _ int, _ time.Time) ([]Event, error) {
	if d.commentsFn != nil {
		return d.commentsFn(), nil
	}
	return nil, nil
}

func (d *dynamicMockEventSource) FetchReviews(_ context.Context, _, _ string, _ int, _ time.Time) ([]Event, error) {
	if d.reviewsFn != nil {
		return d.reviewsFn(), nil
	}
	return nil, nil
}

func (d *dynamicMockEventSource) FetchCheckRuns(_ context.Context, _, _ string, _ int) ([]CheckRun, error) {
	if d.checksFn != nil {
		return d.checksFn(), nil
	}
	return nil, nil
}

func (d *dynamicMockEventSource) GetPRState(_ context.Context, _, _ string, _ int) (PRState, error) {
	if d.prStateFn != nil {
		return d.prStateFn(), nil
	}
	return PROpen, nil
}

type mockAgent struct {
	decideFn   func(event Event) (*Decision, error)
	fixCalls   []string
	replyCalls []string
	fixErr     error
	replyErr   error
}

func (m *mockAgent) Decide(_ context.Context, event Event, _ string, _ []Event) (*Decision, error) {
	if m.decideFn != nil {
		return m.decideFn(event)
	}
	return &Decision{Action: ActionNothing}, nil
}

func (m *mockAgent) Fix(_ context.Context, _, _, prompt string) error {
	m.fixCalls = append(m.fixCalls, prompt)
	return m.fixErr
}

func (m *mockAgent) Reply(_ context.Context, _, _ string, _ int, body string) error {
	m.replyCalls = append(m.replyCalls, body)
	return m.replyErr
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewWatcher(t *testing.T) {
	cfg := Config{
		PollInterval: 30 * time.Second,
		Timeout:      2 * time.Hour,
		IdleTimeout:  30 * time.Minute,
		MaxFixRounds: 5,
	}
	source := &mockEventSource{}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 42, "fleet/42", "/work", cfg, source, agent)

	if w.Owner != "owner" {
		t.Errorf("Owner = %q, want \"owner\"", w.Owner)
	}
	if w.Repo != "repo" {
		t.Errorf("Repo = %q, want \"repo\"", w.Repo)
	}
	if w.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", w.PRNumber)
	}
	if w.Branch != "fleet/42" {
		t.Errorf("Branch = %q, want \"fleet/42\"", w.Branch)
	}
	if w.Workdir != "/work" {
		t.Errorf("Workdir = %q, want \"/work\"", w.Workdir)
	}
	if w.Config.MaxFixRounds != 5 {
		t.Errorf("MaxFixRounds = %d, want 5", w.Config.MaxFixRounds)
	}
	if w.ProcessedEvents == nil {
		t.Error("ProcessedEvents should be initialized")
	}
	if len(w.ProcessedEvents) != 0 {
		t.Errorf("ProcessedEvents should be empty, got %d", len(w.ProcessedEvents))
	}
	if w.FixCount != 0 {
		t.Errorf("FixCount = %d, want 0", w.FixCount)
	}
	if w.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
	if w.LastEventAt.IsZero() {
		t.Error("LastEventAt should be set")
	}
}

// ---------------------------------------------------------------------------
// Event tracking
// ---------------------------------------------------------------------------

func TestIsProcessed(t *testing.T) {
	w := &Watcher{ProcessedEvents: make(map[string]bool)}

	if w.IsProcessed("event-1") {
		t.Error("event-1 should not be processed initially")
	}

	w.MarkProcessed("event-1")

	if !w.IsProcessed("event-1") {
		t.Error("event-1 should be processed after MarkProcessed")
	}
	if w.IsProcessed("event-2") {
		t.Error("event-2 should not be processed")
	}
}

func TestMarkProcessedIdempotent(t *testing.T) {
	w := &Watcher{ProcessedEvents: make(map[string]bool)}
	w.MarkProcessed("event-1")
	w.MarkProcessed("event-1") // second call should not panic or change state

	if !w.IsProcessed("event-1") {
		t.Error("event-1 should still be processed")
	}
	if len(w.ProcessedEvents) != 1 {
		t.Errorf("should have 1 processed event, got %d", len(w.ProcessedEvents))
	}
}

// ---------------------------------------------------------------------------
// Event filtering
// ---------------------------------------------------------------------------

func TestFilterNewEvents(t *testing.T) {
	w := &Watcher{ProcessedEvents: map[string]bool{
		"event-1": true,
		"event-3": true,
	}}

	events := []Event{
		{ID: "event-1", Type: EventPRComment, Body: "first"},
		{ID: "event-2", Type: EventPRComment, Body: "second"},
		{ID: "event-3", Type: EventReviewComment, Body: "third"},
		{ID: "event-4", Type: EventReviewComment, Body: "fourth"},
	}

	filtered := w.FilterNewEvents(events)

	if len(filtered) != 2 {
		t.Fatalf("got %d events, want 2", len(filtered))
	}
	if filtered[0].ID != "event-2" {
		t.Errorf("filtered[0].ID = %q, want \"event-2\"", filtered[0].ID)
	}
	if filtered[1].ID != "event-4" {
		t.Errorf("filtered[1].ID = %q, want \"event-4\"", filtered[1].ID)
	}
}

func TestFilterNewEventsNone(t *testing.T) {
	w := &Watcher{ProcessedEvents: map[string]bool{
		"event-1": true,
	}}

	events := []Event{
		{ID: "event-1", Type: EventPRComment},
	}

	filtered := w.FilterNewEvents(events)
	if len(filtered) != 0 {
		t.Errorf("expected 0 new events, got %d", len(filtered))
	}
}

func TestFilterNewEventsEmpty(t *testing.T) {
	w := &Watcher{ProcessedEvents: make(map[string]bool)}
	filtered := w.FilterNewEvents(nil)
	if filtered != nil {
		t.Errorf("filtering nil should return nil, got %v", filtered)
	}
}

func TestFilterNewEventsAllNew(t *testing.T) {
	w := &Watcher{ProcessedEvents: make(map[string]bool)}

	events := []Event{
		{ID: "event-1"},
		{ID: "event-2"},
	}

	filtered := w.FilterNewEvents(events)
	if len(filtered) != 2 {
		t.Errorf("expected 2 new events, got %d", len(filtered))
	}
}

// ---------------------------------------------------------------------------
// Exit conditions
// ---------------------------------------------------------------------------

func TestShouldExitTimeout(t *testing.T) {
	w := &Watcher{
		Config: Config{
			Timeout:      1 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 5,
		},
		StartedAt:   time.Now().Add(-2 * time.Hour),
		LastEventAt: time.Now(),
		FixCount:    0,
	}

	reason, shouldExit := w.ShouldExit()
	if !shouldExit {
		t.Error("should exit due to timeout")
	}
	if reason != ExitTimeout {
		t.Errorf("reason = %q, want %q", reason, ExitTimeout)
	}
}

func TestShouldExitIdleTimeout(t *testing.T) {
	w := &Watcher{
		Config: Config{
			Timeout:      2 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 5,
		},
		StartedAt:   time.Now().Add(-1 * time.Hour),
		LastEventAt: time.Now().Add(-45 * time.Minute),
		FixCount:    0,
	}

	reason, shouldExit := w.ShouldExit()
	if !shouldExit {
		t.Error("should exit due to idle timeout")
	}
	if reason != ExitIdle {
		t.Errorf("reason = %q, want %q", reason, ExitIdle)
	}
}

func TestShouldExitMaxFixes(t *testing.T) {
	w := &Watcher{
		Config: Config{
			Timeout:      2 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 5,
		},
		StartedAt:   time.Now(),
		LastEventAt: time.Now(),
		FixCount:    5,
	}

	reason, shouldExit := w.ShouldExit()
	if !shouldExit {
		t.Error("should exit due to max fix rounds")
	}
	if reason != ExitMaxFixes {
		t.Errorf("reason = %q, want %q", reason, ExitMaxFixes)
	}
}

func TestShouldNotExit(t *testing.T) {
	w := &Watcher{
		Config: Config{
			Timeout:      2 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 5,
		},
		StartedAt:   time.Now(),
		LastEventAt: time.Now(),
		FixCount:    2,
	}

	_, shouldExit := w.ShouldExit()
	if shouldExit {
		t.Error("should not exit yet")
	}
}

func TestShouldExitTimeoutPriority(t *testing.T) {
	// When both timeout and idle timeout are exceeded, timeout takes priority
	w := &Watcher{
		Config: Config{
			Timeout:      1 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 5,
		},
		StartedAt:   time.Now().Add(-2 * time.Hour),
		LastEventAt: time.Now().Add(-1 * time.Hour),
		FixCount:    0,
	}

	reason, shouldExit := w.ShouldExit()
	if !shouldExit {
		t.Error("should exit")
	}
	if reason != ExitTimeout {
		t.Errorf("reason = %q, want %q (timeout should have priority)", reason, ExitTimeout)
	}
}

func TestShouldExitMaxFixesExact(t *testing.T) {
	// When fix count exactly equals max, should exit
	w := &Watcher{
		Config: Config{
			Timeout:      2 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 3,
		},
		StartedAt:   time.Now(),
		LastEventAt: time.Now(),
		FixCount:    3,
	}

	reason, shouldExit := w.ShouldExit()
	if !shouldExit {
		t.Error("should exit when fix count equals max")
	}
	if reason != ExitMaxFixes {
		t.Errorf("reason = %q, want %q", reason, ExitMaxFixes)
	}
}

func TestShouldNotExitBelowMax(t *testing.T) {
	w := &Watcher{
		Config: Config{
			Timeout:      2 * time.Hour,
			IdleTimeout:  30 * time.Minute,
			MaxFixRounds: 3,
		},
		StartedAt:   time.Now(),
		LastEventAt: time.Now(),
		FixCount:    2,
	}

	_, shouldExit := w.ShouldExit()
	if shouldExit {
		t.Error("should not exit when fix count is below max")
	}
}

// ---------------------------------------------------------------------------
// Watch loop exit conditions (integration-level tests)
// These test the full Run() loop behavior.
// ---------------------------------------------------------------------------

func TestWatcherExitOnMerge(t *testing.T) {
	source := &mockEventSource{prState: PRMerged}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      1 * time.Hour,
		IdleTimeout:  30 * time.Minute,
		MaxFixRounds: 5,
	}, source, agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ExitMerged {
		t.Errorf("reason = %q, want %q", result.Reason, ExitMerged)
	}
}

func TestWatcherExitOnClose(t *testing.T) {
	source := &mockEventSource{prState: PRClosed}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      1 * time.Hour,
		IdleTimeout:  30 * time.Minute,
		MaxFixRounds: 5,
	}, source, agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ExitClosed {
		t.Errorf("reason = %q, want %q", result.Reason, ExitClosed)
	}
}

func TestWatcherExitOnTimeout(t *testing.T) {
	source := &mockEventSource{prState: PROpen}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      50 * time.Millisecond,
		IdleTimeout:  1 * time.Hour,
		MaxFixRounds: 5,
	}, source, agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ExitTimeout {
		t.Errorf("reason = %q, want %q", result.Reason, ExitTimeout)
	}
}

func TestWatcherExitOnIdle(t *testing.T) {
	source := &mockEventSource{prState: PROpen}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      1 * time.Hour,
		IdleTimeout:  50 * time.Millisecond,
		MaxFixRounds: 5,
	}, source, agent)
	// Set LastEventAt to the past so idle timeout fires immediately
	w.LastEventAt = time.Now().Add(-1 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ExitIdle {
		t.Errorf("reason = %q, want %q", result.Reason, ExitIdle)
	}
}

func TestWatcherExitOnContextCancel(t *testing.T) {
	source := &mockEventSource{prState: PROpen}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      1 * time.Hour,
		IdleTimeout:  1 * time.Hour,
		MaxFixRounds: 5,
	}, source, agent)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	_, err := w.Run(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestWatcherProcessesNewComments(t *testing.T) {
	source := &mockEventSource{
		prState: PROpen,
		comments: []Event{
			{ID: "comment-1", Type: EventPRComment, Body: "Please fix the typo", Author: "reviewer"},
		},
	}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      200 * time.Millisecond,
		IdleTimeout:  1 * time.Hour,
		MaxFixRounds: 5,
	}, source, agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = w.Run(ctx)

	if !w.IsProcessed("comment-1") {
		t.Error("comment-1 should be marked as processed")
	}
}

func TestWatcherSkipsProcessedEvents(t *testing.T) {
	source := &mockEventSource{
		prState: PROpen,
		comments: []Event{
			{ID: "comment-1", Type: EventPRComment, Body: "Already seen"},
		},
	}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      200 * time.Millisecond,
		IdleTimeout:  1 * time.Hour,
		MaxFixRounds: 5,
	}, source, agent)
	w.MarkProcessed("comment-1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = w.Run(ctx)

	// Agent Fix should not be called for already-processed events
	if len(agent.fixCalls) > 0 {
		t.Errorf("agent.Fix should not be called for processed events, got %d calls", len(agent.fixCalls))
	}
}

func TestWatcherMaxFixRoundsExit(t *testing.T) {
	callCount := 0
	source := &dynamicMockEventSource{
		prStateFn: func() PRState { return PROpen },
		reviewsFn: func() []Event {
			callCount++
			return []Event{
				{ID: fmt.Sprintf("review-%d", callCount), Type: EventReviewComment, Body: "Fix this"},
			}
		},
	}
	agent := &mockAgent{
		decideFn: func(_ Event) (*Decision, error) {
			return &Decision{Action: ActionFix, Message: "fixing"}, nil
		},
	}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      10 * time.Second,
		IdleTimeout:  10 * time.Second,
		MaxFixRounds: 3,
	}, source, agent)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := w.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != ExitMaxFixes {
		t.Errorf("reason = %q, want %q", result.Reason, ExitMaxFixes)
	}
	if w.FixCount > 3 {
		t.Errorf("fix count = %d, should not exceed max_fix_rounds (3)", w.FixCount)
	}
}

func TestWatcherUpdatesLastEventTime(t *testing.T) {
	before := time.Now()

	source := &mockEventSource{
		prState: PROpen,
		comments: []Event{
			{ID: "comment-1", Type: EventPRComment, Body: "New comment", CreatedAt: time.Now()},
		},
	}
	agent := &mockAgent{}

	w := NewWatcher("owner", "repo", 1, "fleet/1", "/work", Config{
		PollInterval: 10 * time.Millisecond,
		Timeout:      200 * time.Millisecond,
		IdleTimeout:  1 * time.Hour,
		MaxFixRounds: 5,
	}, source, agent)
	// Set LastEventAt to a known past time
	w.LastEventAt = before.Add(-1 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _ = w.Run(ctx)

	// LastEventAt should have been updated when the comment was processed
	if w.LastEventAt.Before(before) {
		t.Error("LastEventAt should have been updated when processing new events")
	}
}

// ---------------------------------------------------------------------------
// ExitReason constants
// ---------------------------------------------------------------------------

func TestExitReasonConstants(t *testing.T) {
	tests := []struct {
		reason ExitReason
		str    string
	}{
		{ExitMerged, "merged"},
		{ExitClosed, "closed"},
		{ExitTimeout, "timeout"},
		{ExitIdle, "idle"},
		{ExitMaxFixes, "max_fixes"},
	}
	for _, tt := range tests {
		if string(tt.reason) != tt.str {
			t.Errorf("ExitReason = %q, want %q", tt.reason, tt.str)
		}
	}
}
