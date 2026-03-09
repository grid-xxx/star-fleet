package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/state"
	"github.com/nullne/star-fleet/internal/ui"
)

// ExitReason describes why the watch loop exited.
type ExitReason int

const (
	ExitMerged       ExitReason = iota // PR was merged
	ExitClosed                         // PR was closed without merging
	ExitTimeout                        // Total watch timeout exceeded
	ExitIdle                           // No new events for idle_timeout
	ExitMaxFix                         // Max fix rounds reached
	ExitReadyToMerge                   // CI passed, no pending tasks, auto-merge eligible
)

func (r ExitReason) String() string {
	switch r {
	case ExitMerged:
		return "merged"
	case ExitClosed:
		return "closed"
	case ExitTimeout:
		return "timeout"
	case ExitIdle:
		return "idle timeout"
	case ExitMaxFix:
		return "max fix rounds"
	case ExitReadyToMerge:
		return "ready to merge"
	default:
		return "unknown"
	}
}

// Result is returned when the watch loop exits.
type Result struct {
	Reason ExitReason
}

// Loop runs the PR watch loop until an exit condition is met.
func Loop(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*Result, error) {
	owner := codeAgent.Owner
	repo := codeAgent.Repo
	prNumber := s.PR.Number

	pollInterval := cfg.Watch.PollInterval.Duration
	timeout := cfg.Watch.Timeout.Duration
	idleTimeout := cfg.Watch.IdleTimeout.Duration
	maxFix := cfg.Watch.MaxFixRounds

	// Set watch start time if not already set
	if s.WatchStartedAt == nil {
		now := time.Now()
		s.WatchStartedAt = &now
		s.LastEventAt = &now
		_ = s.Save()
	}

	display.Info(fmt.Sprintf("Watching PR #%d (poll: %s, timeout: %s)", prNumber, pollInterval, timeout))

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Check timeout
		if time.Since(*s.WatchStartedAt) > timeout {
			msg := fmt.Sprintf("Watch loop timed out after %s.", timeout)
			_ = gh.PostComment(ctx, owner, repo, prNumber,
				"## ⚠️ Star Fleet — Watch Timeout\n\n"+msg)
			display.Warn("Watch timeout reached")
			return &Result{Reason: ExitTimeout}, nil
		}

		// Check idle timeout
		if s.LastEventAt != nil && time.Since(*s.LastEventAt) > idleTimeout {
			display.Warn("Idle timeout reached — no new events")
			return &Result{Reason: ExitIdle}, nil
		}

		// Check max fix rounds
		if s.FixCount >= maxFix {
			msg := fmt.Sprintf("Reached maximum fix rounds (%d). Manual review may be needed.", maxFix)
			_ = gh.PostComment(ctx, owner, repo, prNumber,
				"## ⚠️ Star Fleet — Max Fix Rounds\n\n"+msg)
			display.Warn(fmt.Sprintf("Max fix rounds reached (%d)", maxFix))
			return &Result{Reason: ExitMaxFix}, nil
		}

		// Poll for new events
		events, err := PollEvents(ctx, owner, repo, prNumber, s, cfg.CI.Enabled)
		if err != nil {
			display.Warn(fmt.Sprintf("Poll error: %v", err))
			time.Sleep(pollInterval)
			continue
		}

		// Process events
		hasActionable := false
		for _, event := range events {
			if event.Type == EventComment || event.Type == EventReview || event.Type == EventCIFail {
				hasActionable = true
				break
			}
		}

		for _, event := range events {
			// Terminal events
			if event.Type == EventMerged {
				display.Success("PR merged")
				return &Result{Reason: ExitMerged}, nil
			}
			if event.Type == EventClosed {
				display.Warn("PR closed without merge")
				return &Result{Reason: ExitClosed}, nil
			}

			// CI pass
			if event.Type == EventCIPass {
				display.Step("CI", "all checks passed")
				s.RecordEvent(event.ID)
				_ = s.Save()

				if cfg.Watch.AutoMerge && !hasActionable {
					display.Success("CI passed — ready to merge")
					return &Result{Reason: ExitReadyToMerge}, nil
				}

				_ = gh.PostComment(ctx, owner, repo, prNumber,
					"## ✅ Star Fleet — CI Passed\n\nAll CI checks have passed.")
				continue
			}

			// Actionable events — invoke agent
			display.Info(fmt.Sprintf("Processing: %s", event.Summary))

			result, err := HandleEvent(ctx, codeAgent, s, event, prNumber)
			if err != nil {
				display.Warn(fmt.Sprintf("Handler error: %v", err))
				s.RecordEvent(event.ID)
				_ = s.Save()
				continue
			}

			switch result.Action {
			case "fix":
				display.Step("Fix pushed", result.Message)
			case "reply":
				display.Step("Replied", result.Message)
			default:
				display.Step("Skipped", event.Summary)
			}
		}

		// Wait before next poll
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
