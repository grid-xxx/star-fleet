package watch

import (
	"context"
	"fmt"
	"time"
)

// ExitReason describes why the watch loop exited.
type ExitReason string

const (
	ExitMerged   ExitReason = "merged"
	ExitClosed   ExitReason = "closed"
	ExitTimeout  ExitReason = "timeout"
	ExitIdle     ExitReason = "idle"
	ExitMaxFixes ExitReason = "max_fixes"
)

// Result is returned when the watch loop exits.
type Result struct {
	Reason ExitReason
}

// Config holds watch loop configuration.
type Config struct {
	PollInterval time.Duration
	Timeout      time.Duration
	IdleTimeout  time.Duration
	MaxFixRounds int
}

// Watcher monitors a PR for events and responds to them.
type Watcher struct {
	Owner    string
	Repo     string
	PRNumber int
	Branch   string
	Workdir  string
	Config   Config
	Source   EventSource
	Agent    Agent

	// State tracking
	ProcessedEvents map[string]bool
	FixCount        int
	LastEventAt     time.Time
	StartedAt       time.Time
}

// NewWatcher creates a new Watcher with the given configuration.
func NewWatcher(owner, repo string, prNumber int, branch, workdir string, cfg Config, source EventSource, agent Agent) *Watcher {
	now := time.Now()
	return &Watcher{
		Owner:           owner,
		Repo:            repo,
		PRNumber:        prNumber,
		Branch:          branch,
		Workdir:         workdir,
		Config:          cfg,
		Source:          source,
		Agent:           agent,
		ProcessedEvents: make(map[string]bool),
		StartedAt:       now,
		LastEventAt:     now,
	}
}

// Run starts the watch loop. It polls for new events and processes them
// until the PR is merged, closed, or a timeout is reached.
func (w *Watcher) Run(ctx context.Context) (*Result, error) {
	return nil, fmt.Errorf("not implemented")
}

// ProcessEvent handles a single event and returns the action taken.
func (w *Watcher) ProcessEvent(ctx context.Context, event Event) (*Decision, error) {
	return nil, fmt.Errorf("not implemented")
}

// IsProcessed returns true if the event has already been processed.
func (w *Watcher) IsProcessed(eventID string) bool {
	return w.ProcessedEvents[eventID]
}

// MarkProcessed marks an event as processed.
func (w *Watcher) MarkProcessed(eventID string) {
	w.ProcessedEvents[eventID] = true
}

// ShouldExit checks if the watch loop should exit based on timeouts or fix limits.
func (w *Watcher) ShouldExit() (ExitReason, bool) {
	if time.Since(w.StartedAt) > w.Config.Timeout {
		return ExitTimeout, true
	}
	if time.Since(w.LastEventAt) > w.Config.IdleTimeout {
		return ExitIdle, true
	}
	if w.FixCount >= w.Config.MaxFixRounds {
		return ExitMaxFixes, true
	}
	return "", false
}

// FilterNewEvents returns only events that haven't been processed yet.
func (w *Watcher) FilterNewEvents(events []Event) []Event {
	var filtered []Event
	for _, e := range events {
		if !w.IsProcessed(e.ID) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
