package watch

import (
	"context"
	"fmt"
	"strings"

	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/state"
)

// EventType classifies what kind of PR event occurred.
type EventType int

const (
	EventComment  EventType = iota // PR comment (issue comment)
	EventReview                    // PR review (with body)
	EventCIFail                    // CI check failed
	EventCIPass                    // All CI checks passed
	EventMerged                    // PR was merged
	EventClosed                    // PR was closed (not merged)
)

// Event represents a single PR event to process.
type Event struct {
	Type    EventType
	ID      string // unique identifier for deduplication
	Summary string // human-readable summary
	Detail  string // full content (comment body, CI log, etc.)
	Author  string // who triggered it
}

// PollEvents fetches new PR events that haven't been processed yet.
// Returns events in chronological order.
func PollEvents(ctx context.Context, owner, repo string, prNumber int, s *state.RunState, ciEnabled bool) ([]Event, error) {
	// Check PR status first
	status, err := gh.GetPRStatus(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("checking PR status: %w", err)
	}

	if status.State == "MERGED" {
		return []Event{{Type: EventMerged, ID: "merged", Summary: "PR merged"}}, nil
	}
	if status.State == "CLOSED" {
		return []Event{{Type: EventClosed, ID: "closed", Summary: "PR closed"}}, nil
	}

	var events []Event

	// Fetch new comments
	comments, err := gh.ListPRComments(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("listing comments: %w", err)
	}
	for _, c := range comments {
		id := fmt.Sprintf("comment-%s", c.ID)
		if s.HasProcessedEvent(id) {
			continue
		}
		// Skip our own comments (Star Fleet bot)
		if isOwnComment(c.Body) {
			s.RecordEvent(id)
			continue
		}
		events = append(events, Event{
			Type:    EventComment,
			ID:      id,
			Summary: fmt.Sprintf("Comment by @%s", c.Author),
			Detail:  c.Body,
			Author:  c.Author,
		})
	}

	// Fetch new reviews
	reviews, err := gh.ListPRReviews(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("listing reviews: %w", err)
	}
	for _, r := range reviews {
		id := fmt.Sprintf("review-%s", r.ID)
		if s.HasProcessedEvent(id) {
			continue
		}
		if r.Body == "" && r.State == "COMMENTED" {
			// Empty review with just inline comments — skip the review itself
			s.RecordEvent(id)
			continue
		}
		events = append(events, Event{
			Type:    EventReview,
			ID:      id,
			Summary: fmt.Sprintf("Review by @%s (%s)", r.Author, r.State),
			Detail:  fmt.Sprintf("Review state: %s\n\n%s", r.State, r.Body),
			Author:  r.Author,
		})
	}

	// Fetch CI check status
	if ciEnabled {
		checks, err := gh.ListCheckRuns(ctx, owner, repo, prNumber)
		if err == nil && len(checks) > 0 {
			allDone := true
			allPassed := true
			var failedChecks []gh.CheckRun

			for _, c := range checks {
				if c.Status != "COMPLETED" {
					allDone = false
					continue
				}
				if c.Conclusion != "SUCCESS" && c.Conclusion != "NEUTRAL" && c.Conclusion != "SKIPPED" {
					allPassed = false
					failedChecks = append(failedChecks, c)
				}
			}

			if allDone {
				if allPassed {
					id := fmt.Sprintf("ci-pass-%d", len(checks))
					if !s.HasProcessedEvent(id) {
						events = append(events, Event{
							Type:    EventCIPass,
							ID:      id,
							Summary: fmt.Sprintf("All %d CI checks passed", len(checks)),
						})
					}
				} else {
					for _, fc := range failedChecks {
						id := fmt.Sprintf("ci-fail-%s-%s", fc.Name, fc.Conclusion)
						if s.HasProcessedEvent(id) {
							continue
						}
						logs := gh.GetCheckRunLogs(ctx, owner, repo, fc)
						events = append(events, Event{
							Type:    EventCIFail,
							ID:      id,
							Summary: fmt.Sprintf("CI check %q failed", fc.Name),
							Detail:  logs,
						})
					}
				}
			}
		}
	}

	return events, nil
}

// isOwnComment detects if a comment was posted by Star Fleet.
func isOwnComment(body string) bool {
	markers := []string{
		"## 🚀 Star Fleet",
		"## ⚠️ Star Fleet",
		"## 🔍 Star Fleet",
		"## ✅ Star Fleet",
	}
	for _, m := range markers {
		if strings.HasPrefix(body, m) {
			return true
		}
	}
	return false
}
