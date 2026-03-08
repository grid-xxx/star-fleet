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
	PhaseNew      Phase = ""          // not started
	PhaseIntake   Phase = "intake"    // issue fetched + validated
	PhaseDispatch Phase = "dispatch"  // agents completed
	PhasePush     Phase = "push"      // branches pushed
	PhasePRs      Phase = "prs"       // PRs created
	PhaseReview   Phase = "review"    // review completed
	PhaseValidate Phase = "validate"  // cross-validation passed

	// Single-agent workflow phases (replaces Dispatch→Push→PRs→Review→Validate)
	PhaseImplement Phase = "implement" // single agent completed implementation + tests
	PhasePR        Phase = "pr"        // branch pushed + PR created
	PhaseWatch     Phase = "watch"     // watching PR for feedback

	PhaseDone Phase = "done" // delivery PR created, issue closed
)

var phaseOrder = map[Phase]int{
	PhaseNew:      0,
	PhaseIntake:   1,
	PhaseDispatch: 2,
	PhasePush:     3,
	PhasePRs:      4,
	PhaseReview:   5,
	PhaseValidate: 6,
	// Single-agent workflow ordering
	PhaseImplement: 10,
	PhasePR:        11,
	PhaseWatch:     12,
	PhaseDone:      99,
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
	DevBranch  string `json:"dev_branch"`
	TestBranch string `json:"test_branch"`

	DevAgentDone  bool `json:"dev_agent_done"`
	TestAgentDone bool `json:"test_agent_done"`

	DevPR  *PRInfo `json:"dev_pr,omitempty"`
	TestPR *PRInfo `json:"test_pr,omitempty"`

	ReviewDone bool `json:"review_done"`
	ValCycle   int  `json:"val_cycle"`
	ValRound   int  `json:"val_round"`

	DeliverPR *PRInfo `json:"deliver_pr,omitempty"`

	// Single-agent workflow fields
	Branch string  `json:"branch,omitempty"` // single branch (e.g. "fleet/42")
	PR     *PRInfo `json:"pr,omitempty"`     // single PR

	// Watch loop state
	ProcessedEvents []string  `json:"processed_events,omitempty"`
	LastEventAt     time.Time `json:"last_event_at,omitempty"`
	FixCount        int       `json:"fix_count,omitempty"`
	WatchStartedAt  time.Time `json:"watch_started_at,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`

	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`
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
	id := strconv.Itoa(number)
	return &RunState{
		Version:    1,
		Phase:      PhaseNew,
		Owner:      owner,
		Repo:       repo,
		Number:     number,
		DevBranch:  "fleet/dev/" + id,
		TestBranch: "fleet/test/" + id,
		path:       statePath(repoRoot, number),
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
