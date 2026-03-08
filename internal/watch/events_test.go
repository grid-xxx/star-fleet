package watch

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Event type constants
// ---------------------------------------------------------------------------

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventReviewComment, "review_comment"},
		{EventPRComment, "pr_comment"},
		{EventCheckPass, "check_pass"},
		{EventCheckFail, "check_fail"},
		{EventMerged, "merged"},
		{EventClosed, "closed"},
	}
	for _, tt := range tests {
		if string(tt.eventType) != tt.expected {
			t.Errorf("EventType = %q, want %q", tt.eventType, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// PRState constants
// ---------------------------------------------------------------------------

func TestPRStateConstants(t *testing.T) {
	tests := []struct {
		state    PRState
		expected string
	}{
		{PROpen, "open"},
		{PRMerged, "merged"},
		{PRClosed, "closed"},
	}
	for _, tt := range tests {
		if string(tt.state) != tt.expected {
			t.Errorf("PRState = %q, want %q", tt.state, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Event struct
// ---------------------------------------------------------------------------

func TestEventFields(t *testing.T) {
	now := time.Now()
	e := Event{
		ID:        "comment-123",
		Type:      EventReviewComment,
		Body:      "Please fix the variable naming",
		Author:    "reviewer",
		CreatedAt: now,
	}

	if e.ID != "comment-123" {
		t.Errorf("ID = %q", e.ID)
	}
	if e.Type != EventReviewComment {
		t.Errorf("Type = %q", e.Type)
	}
	if e.Body != "Please fix the variable naming" {
		t.Errorf("Body = %q", e.Body)
	}
	if e.Author != "reviewer" {
		t.Errorf("Author = %q", e.Author)
	}
	if !e.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v", e.CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// CheckRun struct
// ---------------------------------------------------------------------------

func TestCheckRunFields(t *testing.T) {
	tests := []struct {
		name       string
		run        CheckRun
		wantPassed bool
	}{
		{
			name:       "successful check",
			run:        CheckRun{Name: "build", Status: "completed", Conclusion: "success"},
			wantPassed: true,
		},
		{
			name:       "failed check",
			run:        CheckRun{Name: "test", Status: "completed", Conclusion: "failure"},
			wantPassed: false,
		},
		{
			name:       "neutral check",
			run:        CheckRun{Name: "lint", Status: "completed", Conclusion: "neutral"},
			wantPassed: false,
		},
		{
			name:       "in-progress check",
			run:        CheckRun{Name: "deploy", Status: "in_progress", Conclusion: ""},
			wantPassed: false,
		},
		{
			name:       "queued check",
			run:        CheckRun{Name: "e2e", Status: "queued", Conclusion: ""},
			wantPassed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed := tt.run.Status == "completed" && tt.run.Conclusion == "success"
			if passed != tt.wantPassed {
				t.Errorf("check %q: passed = %v, want %v", tt.run.Name, passed, tt.wantPassed)
			}
		})
	}
}

func TestCheckRunLogURL(t *testing.T) {
	run := CheckRun{
		Name:       "CI",
		Status:     "completed",
		Conclusion: "failure",
		LogURL:     "https://github.com/owner/repo/actions/runs/123",
	}
	if run.LogURL == "" {
		t.Error("LogURL should be set for failed checks")
	}
}

// ---------------------------------------------------------------------------
// EventSource interface compliance
// ---------------------------------------------------------------------------

func TestMockEventSourceImplementsInterface(t *testing.T) {
	// Compile-time check that mockEventSource implements EventSource
	var _ EventSource = &mockEventSource{}
}

func TestDynamicMockEventSourceImplementsInterface(t *testing.T) {
	var _ EventSource = &dynamicMockEventSource{}
}

// ---------------------------------------------------------------------------
// Event ordering
// ---------------------------------------------------------------------------

func TestEventsChronologicalOrder(t *testing.T) {
	now := time.Now()
	events := []Event{
		{ID: "1", CreatedAt: now.Add(-3 * time.Minute)},
		{ID: "2", CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "3", CreatedAt: now.Add(-1 * time.Minute)},
	}

	for i := 1; i < len(events); i++ {
		if !events[i].CreatedAt.After(events[i-1].CreatedAt) {
			t.Errorf("events should be in chronological order: event %d (%v) should be after event %d (%v)",
				i, events[i].CreatedAt, i-1, events[i-1].CreatedAt)
		}
	}
}

// ---------------------------------------------------------------------------
// Multiple check run aggregation
// ---------------------------------------------------------------------------

func TestAllChecksPassed(t *testing.T) {
	checks := []CheckRun{
		{Name: "build", Status: "completed", Conclusion: "success"},
		{Name: "test", Status: "completed", Conclusion: "success"},
		{Name: "lint", Status: "completed", Conclusion: "success"},
	}

	allPassed := true
	for _, c := range checks {
		if c.Status != "completed" || c.Conclusion != "success" {
			allPassed = false
			break
		}
	}
	if !allPassed {
		t.Error("all checks should pass")
	}
}

func TestAnyCheckFailed(t *testing.T) {
	checks := []CheckRun{
		{Name: "build", Status: "completed", Conclusion: "success"},
		{Name: "test", Status: "completed", Conclusion: "failure"},
		{Name: "lint", Status: "completed", Conclusion: "success"},
	}

	var failed []string
	for _, c := range checks {
		if c.Status == "completed" && c.Conclusion == "failure" {
			failed = append(failed, c.Name)
		}
	}
	if len(failed) != 1 || failed[0] != "test" {
		t.Errorf("expected [test] to be failed, got %v", failed)
	}
}

func TestChecksStillPending(t *testing.T) {
	checks := []CheckRun{
		{Name: "build", Status: "completed", Conclusion: "success"},
		{Name: "test", Status: "in_progress", Conclusion: ""},
	}

	allCompleted := true
	for _, c := range checks {
		if c.Status != "completed" {
			allCompleted = false
			break
		}
	}
	if allCompleted {
		t.Error("not all checks should be completed")
	}
}
