package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Phase represents a completed pipeline checkpoint.
// The pipeline resumes from the phase AFTER the last saved one.
type Phase string

const (
	PhaseNew       Phase = ""          // not started
	PhaseIntake    Phase = "intake"    // issue fetched + validated
	PhaseImplement Phase = "implement" // agent completed implementation + tests
	PhasePR        Phase = "pr"       // branch pushed, PR created
	PhaseWatch     Phase = "watch"    // watching PR for feedback
	PhaseDone      Phase = "done"     // PR merged/closed, pipeline complete
)

var phaseOrder = map[Phase]int{
	PhaseNew:       0,
	PhaseIntake:    1,
	PhaseImplement: 2,
	PhasePR:        3,
	PhaseWatch:     4,
	PhaseDone:      5,
}

func (p Phase) String() string {
	if p == PhaseNew {
		return "new"
	}
	return string(p)
}

// AtLeast returns true if p is at or past the given phase.
func (p Phase) AtLeast(other Phase) bool {
	return phaseOrder[p] >= phaseOrder[other]
}

type PRInfo struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// RunState tracks pipeline progress for a single issue.
// Saved as JSON to .fleet/runs/{number}.json in the repo root.
type RunState struct {
	Version int   `json:"version"`
	Phase   Phase `json:"phase"`

	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`

	BaseBranch string `json:"base_branch,omitempty"`
	IssueTitle string `json:"issue_title,omitempty"`
	Branch     string `json:"branch"`

	AgentDone bool `json:"agent_done"`

	PR *PRInfo `json:"pr,omitempty"`

	// Watch loop state
	ProcessedEvents []string   `json:"processed_events,omitempty"`
	FixCount        int        `json:"fix_count"`
	WatchStartedAt  *time.Time `json:"watch_started_at,omitempty"`
	LastEventAt     *time.Time `json:"last_event_at,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`

	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`
}

// HasProcessedEvent returns true if the given event ID has already been handled.
func (s *RunState) HasProcessedEvent(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.ProcessedEvents {
		if e == id {
			return true
		}
	}
	return false
}

// RecordEvent marks an event as processed and updates LastEventAt.
func (s *RunState) RecordEvent(id string) {
	s.mu.Lock()
	s.ProcessedEvents = append(s.ProcessedEvents, id)
	now := time.Now()
	s.LastEventAt = &now
	s.mu.Unlock()
}

func statePath(repoRoot string, number int) string {
	return filepath.Join(repoRoot, ".fleet", "runs", strconv.Itoa(number)+".json")
}

// Load reads existing run state from disk. Returns nil, nil if no state file exists.
func Load(repoRoot string, number int) (*RunState, error) {
	p := statePath(repoRoot, number)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading state: %w", err)
	}
	var s RunState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}
	s.path = p
	return &s, nil
}

// New creates a fresh run state for the given issue.
func New(repoRoot, owner, repo string, number int) *RunState {
	return &RunState{
		Version: 2,
		Phase:   PhaseNew,
		Owner:   owner,
		Repo:    repo,
		Number:  number,
		Branch:  fmt.Sprintf("fleet/%d", number),
		path:    statePath(repoRoot, number),
	}
}

// Advance sets the phase and persists state. Safe for concurrent use.
func (s *RunState) Advance(p Phase) error {
	s.mu.Lock()
	s.Phase = p
	s.mu.Unlock()
	return s.Save()
}

// Save persists the current state to disk. Safe for concurrent use.
func (s *RunState) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	// Auto-gitignore state files
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); os.IsNotExist(err) {
		_ = os.WriteFile(gi, []byte("*\n!.gitignore\n"), 0o644)
	}

	return os.WriteFile(s.path, data, 0o644)
}

// Remove deletes the state file from disk.
func (s *RunState) Remove() error {
	return os.Remove(s.path)
}
