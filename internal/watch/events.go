package watch

import (
	"context"
	"time"
)

// EventType classifies PR events.
type EventType string

const (
	EventReviewComment EventType = "review_comment"
	EventPRComment     EventType = "pr_comment"
	EventCheckPass     EventType = "check_pass"
	EventCheckFail     EventType = "check_fail"
	EventMerged        EventType = "merged"
	EventClosed        EventType = "closed"
)

// Event represents a single PR event.
type Event struct {
	ID        string
	Type      EventType
	Body      string
	Author    string
	CreatedAt time.Time
}

// PRState represents the current state of a PR.
type PRState string

const (
	PROpen   PRState = "open"
	PRMerged PRState = "merged"
	PRClosed PRState = "closed"
)

// CheckRun represents a CI check run result.
type CheckRun struct {
	Name       string
	Status     string // "completed", "in_progress", "queued"
	Conclusion string // "success", "failure", "neutral", etc.
	LogURL     string
}

// EventSource abstracts fetching PR events from GitHub.
type EventSource interface {
	// FetchComments returns comments on the PR since the given time.
	FetchComments(ctx context.Context, owner, repo string, prNumber int, since time.Time) ([]Event, error)
	// FetchReviews returns review comments on the PR since the given time.
	FetchReviews(ctx context.Context, owner, repo string, prNumber int, since time.Time) ([]Event, error)
	// FetchCheckRuns returns CI check runs for the PR.
	FetchCheckRuns(ctx context.Context, owner, repo string, prNumber int) ([]CheckRun, error)
	// GetPRState returns the current state of the PR.
	GetPRState(ctx context.Context, owner, repo string, prNumber int) (PRState, error)
}
