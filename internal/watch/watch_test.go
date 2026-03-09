package watch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/state"
	"github.com/nullne/star-fleet/internal/ui"
)

func TestExitReasonString(t *testing.T) {
	tests := []struct {
		reason ExitReason
		want   string
	}{
		{ExitMerged, "merged"},
		{ExitClosed, "closed"},
		{ExitTimeout, "timeout"},
		{ExitIdle, "idle timeout"},
		{ExitMaxFix, "max fix rounds"},
		{ExitReadyToMerge, "ready to merge"},
		{ExitReason(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.reason.String(); got != tt.want {
			t.Errorf("ExitReason(%d).String() = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

func TestResultContainsReason(t *testing.T) {
	r := &Result{Reason: ExitMerged}
	if r.Reason != ExitMerged {
		t.Errorf("Result.Reason = %v, want ExitMerged", r.Reason)
	}
}

// ghMock routes gh CLI calls to test-controlled responses.
type ghMock struct {
	prState     string // JSON for pr view --json state
	comments    string // NDJSON for pr view --json comments
	reviews     string // NDJSON for pr view --json reviews
	checks      string // JSON array for pr checks --json
	postComment string // capture posted comment body
}

func (m *ghMock) run(_ context.Context, _ string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "--json state"):
		return m.prState, nil
	case strings.Contains(joined, "--json comments"):
		return m.comments, nil
	case strings.Contains(joined, "--json reviews"):
		return m.reviews, nil
	case strings.Contains(joined, "checks") && strings.Contains(joined, "--json"):
		return m.checks, nil
	case strings.Contains(joined, "comment"):
		// Capture posted comment (for timeout/maxfix messages)
		for i, a := range args {
			if a == "--body" && i+1 < len(args) {
				m.postComment = args[i+1]
			}
		}
		return "", nil
	}
	return "", nil
}

func newTestState(t *testing.T) *state.RunState {
	t.Helper()
	dir := t.TempDir()
	s := state.New(dir, "owner", "repo", 1)
	s.PR = &state.PRInfo{Number: 1, URL: "https://github.com/owner/repo/pull/1"}
	return s
}

func newTestConfig(autoMerge bool) *config.Config {
	return &config.Config{
		Watch: config.WatchConfig{
			PollInterval: config.Duration{Duration: 10 * time.Millisecond},
			Timeout:      config.Duration{Duration: 1 * time.Hour},
			IdleTimeout:  config.Duration{Duration: 30 * time.Minute},
			MaxFixRounds: 5,
			AutoMerge:    autoMerge,
		},
		CI: config.CIConfig{
			Enabled: true,
		},
	}
}

func newTestAgent(t *testing.T) *agent.CodeAgent {
	t.Helper()
	return &agent.CodeAgent{
		Owner:   "owner",
		Repo:    "repo",
		Issue:   &gh.Issue{Number: 1, Title: "test", Body: "test body"},
		Backend: &agent.MockBackend{},
		Workdir: t.TempDir(),
	}
}

func TestLoop_InitialCheckCIAlreadyGreen(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"OPEN"}`,
		comments: "",
		reviews:  "",
		checks:   `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"},{"name":"lint","state":"COMPLETED","conclusion":"SUCCESS"}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	cfg := newTestConfig(true)
	a := newTestAgent(t)
	d := ui.New()
	ctx := context.Background()

	result, err := Loop(ctx, a, s, cfg, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != ExitReadyToMerge {
		t.Errorf("Reason = %v, want ExitReadyToMerge", result.Reason)
	}
}

func TestLoop_InitialCheckSkippedWhenAutoMergeDisabled(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"OPEN"}`,
		comments: "",
		reviews:  "",
		checks:   `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	cfg := newTestConfig(false)
	a := newTestAgent(t)
	d := ui.New()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := Loop(ctx, a, s, cfg, d)
	if err == nil && result != nil && result.Reason == ExitReadyToMerge {
		t.Error("should not return ExitReadyToMerge when AutoMerge is disabled")
	}
}

func TestLoop_InitialCheckSkippedWhenCIDisabled(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"OPEN"}`,
		comments: "",
		reviews:  "",
		checks:   `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	cfg := newTestConfig(true)
	cfg.CI.Enabled = false
	a := newTestAgent(t)
	d := ui.New()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := Loop(ctx, a, s, cfg, d)
	if err == nil && result != nil && result.Reason == ExitReadyToMerge {
		t.Error("should not return ExitReadyToMerge when CI is disabled")
	}
}

func TestLoop_InitialCheckBlockedByActionableComment(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"OPEN"}`,
		comments: `{"id":"c1","body":"Please fix the typo","author":"reviewer","createdAt":"2025-01-01T00:00:00Z","url":"https://example.com"}`,
		reviews:  "",
		checks:   `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	cfg := newTestConfig(true)
	a := newTestAgent(t)
	d := ui.New()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := Loop(ctx, a, s, cfg, d)
	if err == nil && result != nil && result.Reason == ExitReadyToMerge {
		t.Error("should not return ExitReadyToMerge when there are actionable comments")
	}
}

func TestLoop_InitialCheckBlockedByCIInProgress(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"OPEN"}`,
		comments: "",
		reviews:  "",
		checks:   `[{"name":"build","state":"IN_PROGRESS","conclusion":""}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	cfg := newTestConfig(true)
	a := newTestAgent(t)
	d := ui.New()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := Loop(ctx, a, s, cfg, d)
	if err == nil && result != nil && result.Reason == ExitReadyToMerge {
		t.Error("should not return ExitReadyToMerge when CI is still in progress")
	}
}

func TestLoop_InitialCheckWithAlreadyProcessedComment(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"OPEN"}`,
		comments: `{"id":"c1","body":"Please fix the typo","author":"reviewer","createdAt":"2025-01-01T00:00:00Z","url":"https://example.com"}`,
		reviews:  "",
		checks:   `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	s.RecordEvent("comment-c1")
	cfg := newTestConfig(true)
	a := newTestAgent(t)
	d := ui.New()

	result, err := Loop(context.Background(), a, s, cfg, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reason != ExitReadyToMerge {
		t.Errorf("Reason = %v, want ExitReadyToMerge (comment already processed)", result.Reason)
	}
}

func TestLoop_InitialCheckPRAlreadyMerged(t *testing.T) {
	mock := &ghMock{
		prState:  `{"state":"MERGED"}`,
		comments: "",
		reviews:  "",
		checks:   `[{"name":"build","state":"COMPLETED","conclusion":"SUCCESS"}]`,
	}
	restore := gh.SetRunFn(mock.run)
	t.Cleanup(restore)

	s := newTestState(t)
	cfg := newTestConfig(true)
	a := newTestAgent(t)
	d := ui.New()

	result, err := Loop(context.Background(), a, s, cfg, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
