package state

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// New phases
// ---------------------------------------------------------------------------

func TestNewPhaseConstants(t *testing.T) {
	tests := []struct {
		phase Phase
		str   string
	}{
		{PhaseImplement, "implement"},
		{PhasePR, "pr"},
		{PhaseWatch, "watch"},
	}
	for _, tt := range tests {
		if string(tt.phase) != tt.str {
			t.Errorf("Phase(%q) string = %q, want %q", tt.phase, string(tt.phase), tt.str)
		}
	}
}

func TestNewPhaseOrdering(t *testing.T) {
	// The new single-agent pipeline: New → Intake → Implement → PR → Watch → Done
	phases := []Phase{PhaseNew, PhaseIntake, PhaseImplement, PhasePR, PhaseWatch, PhaseDone}

	for i := 1; i < len(phases); i++ {
		if !phases[i].AtLeast(phases[i-1]) {
			t.Errorf("%s should be AtLeast %s", phases[i], phases[i-1])
		}
		if phases[i-1].AtLeast(phases[i]) {
			t.Errorf("%s should not be AtLeast %s", phases[i-1], phases[i])
		}
	}
	for _, p := range phases {
		if !p.AtLeast(p) {
			t.Errorf("%s should be AtLeast itself", p)
		}
	}
}

func TestImplementBeforePR(t *testing.T) {
	if !PhasePR.AtLeast(PhaseImplement) {
		t.Error("PR phase should come after Implement")
	}
	if PhaseImplement.AtLeast(PhasePR) {
		t.Error("Implement should come before PR")
	}
}

func TestWatchAfterPR(t *testing.T) {
	if !PhaseWatch.AtLeast(PhasePR) {
		t.Error("Watch phase should come after PR")
	}
	if PhasePR.AtLeast(PhaseWatch) {
		t.Error("PR should come before Watch")
	}
}

func TestDoneAfterWatch(t *testing.T) {
	if !PhaseDone.AtLeast(PhaseWatch) {
		t.Error("Done should come after Watch")
	}
	if PhaseWatch.AtLeast(PhaseDone) {
		t.Error("Watch should come before Done")
	}
}

// ---------------------------------------------------------------------------
// Single branch field (no more dual worktrees)
// ---------------------------------------------------------------------------

func TestSingleBranchField(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 42)

	s.Branch = "fleet/42"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, 42)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Branch != "fleet/42" {
		t.Errorf("Branch = %q, want \"fleet/42\"", loaded.Branch)
	}
}

func TestSinglePRField(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 42)

	s.PR = &PRInfo{Number: 100, URL: "https://github.com/owner/repo/pull/100"}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, 42)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PR == nil {
		t.Fatal("PR should not be nil")
	}
	if loaded.PR.Number != 100 {
		t.Errorf("PR.Number = %d, want 100", loaded.PR.Number)
	}
	if loaded.PR.URL != "https://github.com/owner/repo/pull/100" {
		t.Errorf("PR.URL = %q", loaded.PR.URL)
	}
}

// ---------------------------------------------------------------------------
// Watch state fields
// ---------------------------------------------------------------------------

func TestWatchStateFields(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 42)

	now := time.Now().Truncate(time.Second)
	s.Branch = "fleet/42"
	s.PR = &PRInfo{Number: 100, URL: "https://github.com/owner/repo/pull/100"}
	s.ProcessedEvents = []string{"comment-123", "review-456"}
	lastEvent := now
	s.LastEventAt = &lastEvent
	s.FixCount = 2
	watchStart := now.Add(-1 * time.Hour)
	s.WatchStartedAt = &watchStart

	if err := s.Advance(PhaseWatch); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, 42)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Phase != PhaseWatch {
		t.Errorf("Phase = %q, want \"watch\"", loaded.Phase)
	}
	if loaded.Branch != "fleet/42" {
		t.Errorf("Branch = %q", loaded.Branch)
	}
	if len(loaded.ProcessedEvents) != 2 {
		t.Fatalf("ProcessedEvents len = %d, want 2", len(loaded.ProcessedEvents))
	}
	if loaded.ProcessedEvents[0] != "comment-123" {
		t.Errorf("ProcessedEvents[0] = %q", loaded.ProcessedEvents[0])
	}
	if loaded.ProcessedEvents[1] != "review-456" {
		t.Errorf("ProcessedEvents[1] = %q", loaded.ProcessedEvents[1])
	}
	if loaded.FixCount != 2 {
		t.Errorf("FixCount = %d, want 2", loaded.FixCount)
	}
	if loaded.LastEventAt == nil || !loaded.LastEventAt.Equal(now) {
		t.Errorf("LastEventAt = %v, want %v", loaded.LastEventAt, now)
	}
	if loaded.WatchStartedAt == nil || !loaded.WatchStartedAt.Equal(now.Add(-1 * time.Hour)) {
		t.Errorf("WatchStartedAt = %v", loaded.WatchStartedAt)
	}
}

func TestWatchStateResumability(t *testing.T) {
	dir := t.TempDir()

	// Simulate initial run: Intake → Implement → PR → Watch (in progress)
	s := New(dir, "owner", "repo", 10)
	s.Branch = "fleet/10"
	s.PR = &PRInfo{Number: 50, URL: "https://github.com/owner/repo/pull/50"}
	s.ProcessedEvents = []string{"comment-1", "comment-2"}
	s.FixCount = 1
	watchStart2 := time.Now().Add(-30 * time.Minute).Truncate(time.Second)
	s.WatchStartedAt = &watchStart2
	lastEvent2 := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	s.LastEventAt = &lastEvent2
	if err := s.Advance(PhaseWatch); err != nil {
		t.Fatal(err)
	}

	// Simulate resume: Load state and verify all watch fields are preserved
	loaded, err := Load(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("state should exist")
	}
	if loaded.Phase != PhaseWatch {
		t.Errorf("resumed phase = %q, want \"watch\"", loaded.Phase)
	}
	if loaded.Branch != "fleet/10" {
		t.Errorf("resumed branch = %q", loaded.Branch)
	}
	if loaded.PR == nil || loaded.PR.Number != 50 {
		t.Error("resumed PR should be preserved")
	}
	if len(loaded.ProcessedEvents) != 2 {
		t.Errorf("resumed ProcessedEvents len = %d, want 2", len(loaded.ProcessedEvents))
	}
	if loaded.FixCount != 1 {
		t.Errorf("resumed FixCount = %d, want 1", loaded.FixCount)
	}

	// Resume should be able to advance to Done
	loaded.FixCount = 3
	loaded.ProcessedEvents = append(loaded.ProcessedEvents, "comment-3")
	if err := loaded.Advance(PhaseDone); err != nil {
		t.Fatal(err)
	}

	final, err := Load(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if final.Phase != PhaseDone {
		t.Errorf("final phase = %q, want \"done\"", final.Phase)
	}
	if final.FixCount != 3 {
		t.Errorf("final FixCount = %d", final.FixCount)
	}
	if len(final.ProcessedEvents) != 3 {
		t.Errorf("final ProcessedEvents len = %d", len(final.ProcessedEvents))
	}
}

func TestStateJSONFormat(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 42)
	s.Branch = "fleet/42"
	s.PR = &PRInfo{Number: 100, URL: "https://github.com/owner/repo/pull/100"}
	s.ProcessedEvents = []string{"comment-123", "review-456"}
	s.FixCount = 2
	watchStart3 := time.Date(2026, 3, 8, 13, 0, 0, 0, time.UTC)
	s.WatchStartedAt = &watchStart3
	lastEvent3 := time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC)
	s.LastEventAt = &lastEvent3

	if err := s.Advance(PhaseWatch); err != nil {
		t.Fatal(err)
	}

	// Read the raw JSON file and verify expected fields are present
	data, err := os.ReadFile(statePath(dir, 42))
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if raw["phase"] != "watch" {
		t.Errorf("JSON phase = %v", raw["phase"])
	}
	if raw["branch"] != "fleet/42" {
		t.Errorf("JSON branch = %v", raw["branch"])
	}

	pr, ok := raw["pr"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON pr should be an object")
	}
	if pr["number"] != float64(100) {
		t.Errorf("JSON pr.number = %v", pr["number"])
	}

	events, ok := raw["processed_events"].([]interface{})
	if !ok {
		t.Fatal("JSON processed_events should be an array")
	}
	if len(events) != 2 {
		t.Errorf("JSON processed_events len = %d", len(events))
	}

	if raw["fix_count"] != float64(2) {
		t.Errorf("JSON fix_count = %v", raw["fix_count"])
	}
}

func TestEmptyProcessedEvents(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 1)
	s.Branch = "fleet/1"

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ProcessedEvents) != 0 {
		t.Errorf("ProcessedEvents should be empty, got %v", loaded.ProcessedEvents)
	}
	if loaded.FixCount != 0 {
		t.Errorf("FixCount should be 0, got %d", loaded.FixCount)
	}
}

func TestAdvanceFromImplementToPR(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 5)
	s.Branch = "fleet/5"

	if err := s.Advance(PhaseIntake); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(PhaseImplement); err != nil {
		t.Fatal(err)
	}

	loaded, _ := Load(dir, 5)
	if loaded.Phase != PhaseImplement {
		t.Errorf("phase = %q, want \"implement\"", loaded.Phase)
	}

	loaded.PR = &PRInfo{Number: 42, URL: "https://github.com/owner/repo/pull/42"}
	if err := loaded.Advance(PhasePR); err != nil {
		t.Fatal(err)
	}

	final, _ := Load(dir, 5)
	if final.Phase != PhasePR {
		t.Errorf("phase = %q, want \"pr\"", final.Phase)
	}
	if final.PR == nil || final.PR.Number != 42 {
		t.Error("PR should be set after advancing to PR phase")
	}
}
