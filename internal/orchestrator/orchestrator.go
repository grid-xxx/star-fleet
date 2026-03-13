package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
	"github.com/nullne/star-fleet/internal/notify"
	"github.com/nullne/star-fleet/internal/review"
	"github.com/nullne/star-fleet/internal/state"
	"github.com/nullne/star-fleet/internal/ui"
	"github.com/nullne/star-fleet/internal/watch"
)

// GHClient abstracts GitHub operations for testing.
type GHClient interface {
	FetchIssue(ctx context.Context, owner, repo string, number int) (*gh.Issue, error)
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
	DefaultBranch(ctx context.Context, owner, repo string) (string, error)
	FindPR(ctx context.Context, owner, repo, head string) (*gh.PR, error)
	CreatePR(ctx context.Context, owner, repo, workdir, title, body, base, head string) (*gh.PR, error)
	MergePR(ctx context.Context, owner, repo string, number int) error
	ClosePR(ctx context.Context, owner, repo string, number int) error
	GetPRBranches(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error)
	SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error
}

// ReviewRunner abstracts the review process for testing.
type ReviewRunner interface {
	// Review runs the review agent and returns (feedback, issuesCount, error)
	Review(ctx context.Context, owner, repo string, prNumber int, cfg *config.ReviewConfig) (string, int, error)
}

// GitClient abstracts git operations for testing.
type GitClient interface {
	CreateWorktree(ctx context.Context, repoRoot, name, branch string) (string, error)
	RemoveWorktree(ctx context.Context, repoRoot, name string) error
	PruneWorktrees(ctx context.Context, repoRoot string) error
	RemoteBranchExists(ctx context.Context, repoRoot, remote, branch string) (bool, error)
	DeleteRemoteBranch(ctx context.Context, repoRoot, remote, branch string) error
	Push(ctx context.Context, dir, remote, branch string) error
}

// StateManager abstracts state persistence for testing.
type StateManager interface {
	Load(repoRoot string, number int) (*state.RunState, error)
	New(repoRoot, owner, repo string, number int) *state.RunState
}

// WatchRunner abstracts the watch loop for testing.
type WatchRunner interface {
	Loop(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error)
}

// BackendFactory abstracts agent backend creation for testing.
type BackendFactory interface {
	NewBackend(name string) (agent.Backend, error)
}

// --- Default implementations that delegate to the real packages ---

type defaultGH struct{}

func (defaultGH) FetchIssue(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
	return gh.FetchIssue(ctx, owner, repo, number)
}
func (defaultGH) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	return gh.PostComment(ctx, owner, repo, number, body)
}
func (defaultGH) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	return gh.DefaultBranch(ctx, owner, repo)
}
func (defaultGH) FindPR(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
	return gh.FindPR(ctx, owner, repo, head)
}
func (defaultGH) CreatePR(ctx context.Context, owner, repo, workdir, title, body, base, head string) (*gh.PR, error) {
	return gh.CreatePR(ctx, owner, repo, workdir, title, body, base, head)
}
func (defaultGH) MergePR(ctx context.Context, owner, repo string, number int) error {
	return gh.MergePR(ctx, owner, repo, number)
}
func (defaultGH) ClosePR(ctx context.Context, owner, repo string, number int) error {
	return gh.ClosePR(ctx, owner, repo, number)
}
func (defaultGH) GetPRBranches(ctx context.Context, owner, repo string, prNumber int) (*gh.PRBranches, error) {
	return gh.GetPRBranches(ctx, owner, repo, prNumber)
}
func (defaultGH) SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
	return gh.SubmitReview(ctx, owner, repo, prNumber, event, body)
}

type defaultGit struct{}

func (defaultGit) CreateWorktree(ctx context.Context, repoRoot, name, branch string) (string, error) {
	return git.CreateWorktree(ctx, repoRoot, name, branch)
}
func (defaultGit) RemoveWorktree(ctx context.Context, repoRoot, name string) error {
	return git.RemoveWorktree(ctx, repoRoot, name)
}
func (defaultGit) PruneWorktrees(ctx context.Context, repoRoot string) error {
	return git.PruneWorktrees(ctx, repoRoot)
}
func (defaultGit) RemoteBranchExists(ctx context.Context, repoRoot, remote, branch string) (bool, error) {
	return git.RemoteBranchExists(ctx, repoRoot, remote, branch)
}
func (defaultGit) DeleteRemoteBranch(ctx context.Context, repoRoot, remote, branch string) error {
	return git.DeleteRemoteBranch(ctx, repoRoot, remote, branch)
}
func (defaultGit) Push(ctx context.Context, dir, remote, branch string) error {
	return git.Push(ctx, dir, remote, branch)
}

type defaultState struct{}

func (defaultState) Load(repoRoot string, number int) (*state.RunState, error) {
	return state.Load(repoRoot, number)
}
func (defaultState) New(repoRoot, owner, repo string, number int) *state.RunState {
	return state.New(repoRoot, owner, repo, number)
}

type defaultWatch struct{}

func (defaultWatch) Loop(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error) {
	return watch.Loop(ctx, codeAgent, s, cfg, display)
}

type defaultReview struct {
	gh      GHClient
	backend BackendFactory
}

func (d defaultReview) Review(ctx context.Context, owner, repo string, prNumber int, cfg *config.ReviewConfig) (string, int, error) {
	backendName := cfg.Backend
	if backendName == "" {
		backendName = "claude-code"
	}
	b, err := d.backend.NewBackend(backendName)
	if err != nil {
		return "", 0, err
	}
	r := &review.Reviewer{Agent: b, GH: d.gh}
	return r.Review(ctx, owner, repo, prNumber, cfg)
}

type defaultBackendFactory struct{}

func (defaultBackendFactory) NewBackend(name string) (agent.Backend, error) {
	return agent.NewBackend(name)
}

// Orchestrator is the core pipeline controller.
type Orchestrator struct {
	Owner      string
	Repo       string
	Number     int
	Config     *config.Config
	Display    *ui.Display
	RepoRoot   string
	Restart    bool // when true, discard existing state and start fresh
	NoWatch    bool // when true, skip the watch loop after creating PR
	AutoMerge  bool // when true, auto-merge PR when CI passes
	NoReview   bool // when true, skip the review phase
	ReviewOnly bool // when true, only run review on existing PR

	GH       GHClient
	Git      GitClient
	State    StateManager
	Watch    WatchRunner
	Backend  BackendFactory
	Reviewer ReviewRunner
	Notify   notify.Notifier
}

func (o *Orchestrator) init() {
	if o.GH == nil {
		o.GH = defaultGH{}
	}
	if o.Git == nil {
		o.Git = defaultGit{}
	}
	if o.State == nil {
		o.State = defaultState{}
	}
	if o.Watch == nil {
		o.Watch = defaultWatch{}
	}
	if o.Backend == nil {
		o.Backend = defaultBackendFactory{}
	}
	if o.Reviewer == nil {
		o.Reviewer = defaultReview{gh: o.GH, backend: o.Backend}
	}
	if o.Notify == nil {
		if o.Config != nil {
			o.Notify = notify.New(o.Config.Telegram.BotToken, o.Config.Telegram.ChatID)
		} else {
			o.Notify = notify.Nop{}
		}
	}
}

func (o *Orchestrator) Run(ctx context.Context) (runErr error) {
	o.init()

	defer func() {
		if runErr != nil {
			o.Notify.RunFailed(o.Number, runErr.Error())
		}
	}()

	s, err := o.loadState()
	if err != nil {
		return err
	}

	if s.Phase == state.PhaseDone {
		o.Display.Success("Pipeline already completed for this issue")
		return nil
	}

	resuming := s.Phase != state.PhaseNew

	if s.BaseBranch == "" {
		baseBranch, err := o.GH.DefaultBranch(ctx, o.Owner, o.Repo)
		if err != nil {
			return fmt.Errorf("detecting default branch: %w", err)
		}
		s.BaseBranch = baseBranch
	}

	o.Display.Title(o.Owner, o.Repo, o.Number)

	// Display resolved config
	for _, line := range strings.Split(o.Config.Summary(), "\n") {
		o.Display.Info("  " + line)
	}

	if resuming {
		o.Display.Info(fmt.Sprintf("⟳ Resuming from %s phase", s.Phase))
	}

	// === INTAKE ===
	issue, err := o.phaseIntake(ctx, s)
	if err != nil {
		return err
	}

	// === WORKTREE (single branch) ===
	workdir, err := o.Git.CreateWorktree(ctx, o.RepoRoot, "impl", s.Branch)
	if err != nil {
		o.Display.StepFail("Creating worktree...", err.Error())
		return fmt.Errorf("creating worktree: %w", err)
	}
	o.Display.Info(fmt.Sprintf("Branch: %s → worktrees/impl", s.Branch))
	defer o.cleanup(ctx)

	backend, err := o.Backend.NewBackend(o.Config.Agent.Backend)
	if err != nil {
		return err
	}

	codeAgent := &agent.CodeAgent{
		Backend:    backend,
		Owner:      o.Owner,
		Repo:       o.Repo,
		Issue:      issue,
		Workdir:    workdir,
		Branch:     s.Branch,
		BaseBranch: s.BaseBranch,
	}

	if o.ReviewOnly {
		// --review-only: skip implement, find existing PR, run review only
		pr, err := o.GH.FindPR(ctx, o.Owner, o.Repo, s.Branch)
		if err != nil {
			return fmt.Errorf("finding PR for review-only: %w", err)
		}
		if pr == nil {
			return fmt.Errorf("no open PR found for branch %s", s.Branch)
		}
		s.PR = &state.PRInfo{Number: pr.Number, URL: pr.URL}
		o.Display.Step(fmt.Sprintf("PR #%d", pr.Number), pr.URL)

		if err := o.phaseReview(ctx, s, codeAgent, pr); err != nil {
			return err
		}

		o.Display.Result(pr.URL)
		return s.Advance(state.PhaseDone)
	}

	// === IMPLEMENT (single agent writes code + tests) ===
	o.Display.Blank()
	if err := o.phaseImplement(ctx, s, codeAgent); err != nil {
		return err
	}

	// === PUSH + CREATE PR ===
	pr, err := o.phasePR(ctx, s, codeAgent, issue)
	if err != nil {
		return err
	}

	// === REVIEW (automated code review) ===
	if err := o.phaseReview(ctx, s, codeAgent, pr); err != nil {
		return err
	}

	// === WATCH (respond to feedback loop) ===
	if o.NoWatch {
		o.Display.Info("--no-watch: skipping watch loop")
		o.Display.Result(pr.URL)
		return s.Advance(state.PhaseDone)
	}

	o.Display.Blank()
	return o.phaseWatch(ctx, s, codeAgent, pr)
}

// ---------------------------------------------------------------------------
// State management
// ---------------------------------------------------------------------------

func (o *Orchestrator) loadState() (*state.RunState, error) {
	if o.Restart {
		o.restartCleanup()
		s := o.State.New(o.RepoRoot, o.Owner, o.Repo, o.Number)
		branch, err := o.nextVersionedBranch(context.Background(), s.Branch)
		if err != nil {
			return nil, fmt.Errorf("determining versioned branch: %w", err)
		}
		s.Branch = branch
		return s, nil
	}
	s, err := o.State.Load(o.RepoRoot, o.Number)
	if err != nil {
		return nil, err
	}
	if s != nil {
		return s, nil
	}
	return o.State.New(o.RepoRoot, o.Owner, o.Repo, o.Number), nil
}

// nextVersionedBranch returns the next available branch name.
// If fleet/<N> doesn't exist on the remote, it returns fleet/<N> unchanged.
// If fleet/<N> exists, it probes fleet/<N>-v2, fleet/<N>-v3, ... and returns
// the first name that doesn't exist, preserving previous branches for reference.
func (o *Orchestrator) nextVersionedBranch(ctx context.Context, baseBranch string) (string, error) {
	exists, err := o.Git.RemoteBranchExists(ctx, o.RepoRoot, "origin", baseBranch)
	if err != nil {
		return baseBranch, nil
	}
	if !exists {
		return baseBranch, nil
	}

	for v := 2; ; v++ {
		candidate := fmt.Sprintf("%s-v%d", baseBranch, v)
		exists, err := o.Git.RemoteBranchExists(ctx, o.RepoRoot, "origin", candidate)
		if err != nil {
			return candidate, nil
		}
		if !exists {
			return candidate, nil
		}
	}
}

func (o *Orchestrator) restartCleanup() {
	ctx := context.Background()

	old, err := o.State.Load(o.RepoRoot, o.Number)
	if err != nil {
		o.Display.Warn(fmt.Sprintf("restart: could not load old state: %v", err))
	}

	if err := o.Git.RemoveWorktree(ctx, o.RepoRoot, "impl"); err != nil {
		o.Display.Warn(fmt.Sprintf("restart: removing worktree: %v", err))
	}
	if err := o.Git.PruneWorktrees(ctx, o.RepoRoot); err != nil {
		o.Display.Warn(fmt.Sprintf("restart: pruning worktrees: %v", err))
	}

	if old != nil && old.PR != nil {
		if err := o.GH.ClosePR(ctx, o.Owner, o.Repo, old.PR.Number); err != nil {
			o.Display.Warn(fmt.Sprintf("restart: closing PR #%d: %v", old.PR.Number, err))
		}
	}
}

// ---------------------------------------------------------------------------
// Phase: Intake
// ---------------------------------------------------------------------------

func (o *Orchestrator) phaseIntake(ctx context.Context, s *state.RunState) (*gh.Issue, error) {
	if s.Phase.AtLeast(state.PhaseIntake) {
		issue, err := o.GH.FetchIssue(ctx, o.Owner, o.Repo, o.Number)
		if err != nil {
			return nil, err
		}
		o.Display.Step("Fetching issue...", issue.Title)
		o.Display.Step("Validating spec...", "")
		return issue, nil
	}

	issue, err := o.intake(ctx)
	if err != nil {
		return nil, err
	}
	if err := o.postPickedUp(ctx); err != nil {
		return nil, err
	}

	s.IssueTitle = issue.Title
	if err := s.Advance(state.PhaseIntake); err != nil {
		return nil, err
	}
	return issue, nil
}

func (o *Orchestrator) intake(ctx context.Context) (*gh.Issue, error) {
	sp := o.Display.TreeLeaf("Fetching issue...", "")
	issue, err := o.GH.FetchIssue(ctx, o.Owner, o.Repo, o.Number)
	if err != nil {
		sp.Stop("fail", err.Error())
		return nil, err
	}
	sp.Stop("success", issue.Title)

	if err := o.validateSpec(ctx, issue); err != nil {
		return nil, err
	}
	o.Display.Step("Validating spec...", "")
	return issue, nil
}

func (o *Orchestrator) validateSpec(ctx context.Context, issue *gh.Issue) error {
	if strings.TrimSpace(issue.Body) == "" {
		gaps := "The issue body is empty. Please add a description of the desired behavior."
		comment := "## 🔍 Star Fleet — Spec Gap\n\n" + gaps + "\n\nPipeline paused. Please update the issue and re-run."
		_ = o.GH.PostComment(ctx, o.Owner, o.Repo, o.Number, comment)
		o.Display.StepFail("Validating spec...", "empty issue body")
		return fmt.Errorf("issue #%d has an empty body", o.Number)
	}
	if len(issue.Body) < 50 {
		gaps := "The issue description seems too brief. Please provide more detail about expected behavior, acceptance criteria, or edge cases."
		comment := "## 🔍 Star Fleet — Spec Gap\n\n" + gaps + "\n\nPipeline paused. Please update the issue and re-run."
		_ = o.GH.PostComment(ctx, o.Owner, o.Repo, o.Number, comment)
		o.Display.StepWarn("Validating spec...", "issue body is very short")
		return fmt.Errorf("issue #%d body is too brief (%d chars)", o.Number, len(issue.Body))
	}
	return nil
}

func (o *Orchestrator) postPickedUp(ctx context.Context) error {
	return o.GH.PostComment(ctx, o.Owner, o.Repo, o.Number,
		"## 🚀 Star Fleet — Picked Up\n\nThis issue has been picked up by Star Fleet. An agent is implementing the feature and writing tests.")
}

// ---------------------------------------------------------------------------
// Phase: Implement (single agent writes implementation + tests)
// ---------------------------------------------------------------------------

func (o *Orchestrator) phaseImplement(ctx context.Context, s *state.RunState, codeAgent *agent.CodeAgent) error {
	if s.Phase.AtLeast(state.PhaseImplement) {
		o.Display.Step("Code Agent", "done (cached)")
		return nil
	}

	lv := o.Display.StartLiveView([]ui.AgentConfig{
		{Label: "Code Agent", Tree: "└", Message: "Implementing feature + writing tests..."},
	})
	defer lv.Stop()

	panel := lv.Panel(0)

	if s.AgentDone {
		panel.Finish("success", "done (cached)")
	} else {
		err := codeAgent.Run(ctx, panel)
		if err != nil {
			panel.Finish("fail", err.Error())
			return fmt.Errorf("code agent: %w", err)
		}
		panel.Finish("success", "done")
		s.AgentDone = true
		_ = s.Save()
	}

	return s.Advance(state.PhaseImplement)
}

// ---------------------------------------------------------------------------
// Phase: PR (push branch + create PR)
// ---------------------------------------------------------------------------

func (o *Orchestrator) phasePR(ctx context.Context, s *state.RunState, codeAgent *agent.CodeAgent, issue *gh.Issue) (*gh.PR, error) {
	if s.Phase.AtLeast(state.PhasePR) {
		pr := &gh.PR{Number: s.PR.Number, URL: s.PR.URL}
		o.Display.Step(fmt.Sprintf("PR #%d", pr.Number), pr.URL)
		return pr, nil
	}

	// Push
	if err := o.Git.Push(ctx, codeAgent.Workdir, "origin", codeAgent.Branch); err != nil {
		o.Display.StepFail("Pushing branch...", err.Error())
		return nil, fmt.Errorf("pushing branch: %w", err)
	}
	o.Display.Step("Pushing branch...", "")

	// Create PR
	title := fmt.Sprintf("#%d %s", o.Number, issue.Title)
	body := fmt.Sprintf("## Star Fleet\n\nCloses #%d\n\n"+
		"This PR was generated by Star Fleet. "+
		"Implementation and tests were written by a single code agent.\n\n"+
		"The agent is watching this PR and will respond to review comments and CI results.",
		o.Number)

	pr, err := o.findOrCreatePR(ctx, codeAgent.Workdir, title, body, s.BaseBranch, s.Branch)
	if err != nil {
		o.Display.StepFail("Creating PR...", err.Error())
		return nil, fmt.Errorf("creating PR: %w", err)
	}
	o.Display.Step(fmt.Sprintf("PR #%d", pr.Number), pr.URL)

	o.Notify.PRCreated(pr.Number, o.Number, issue.Title)

	s.PR = &state.PRInfo{Number: pr.Number, URL: pr.URL}
	if err := s.Advance(state.PhasePR); err != nil {
		return nil, err
	}

	o.Display.Result(pr.URL)
	return pr, nil
}

func (o *Orchestrator) findOrCreatePR(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error) {
	existing, err := o.GH.FindPR(ctx, o.Owner, o.Repo, head)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return o.GH.CreatePR(ctx, o.Owner, o.Repo, workdir, title, body, base, head)
}

// ---------------------------------------------------------------------------
// Phase: Review (automated code review)
// ---------------------------------------------------------------------------

func (o *Orchestrator) phaseReview(ctx context.Context, s *state.RunState, codeAgent *agent.CodeAgent, pr *gh.PR) error {
	if s.Phase.AtLeast(state.PhaseReview) {
		o.Display.Step("Code Review", "done (cached)")
		return nil
	}

	if o.NoReview || !o.Config.Review.Enabled {
		o.Display.Step("Code Review", "skipped")
		return s.Advance(state.PhaseReview)
	}

	maxRounds := o.Config.Review.MaxRounds
	if maxRounds < 1 {
		maxRounds = 3
	}

	_ = o.GH.PostComment(ctx, o.Owner, o.Repo, pr.Number,
		"🔍 Star Fleet — Reviewing PR...")

	for round := s.ReviewRound + 1; round <= maxRounds; round++ {
		o.Display.Step("Code Review", fmt.Sprintf("round %d/%d...", round, maxRounds))

		feedback, issues, err := o.Reviewer.Review(ctx, o.Owner, o.Repo, pr.Number, &o.Config.Review)
		if err != nil {
			o.Display.StepFail("Code Review", err.Error())
			return fmt.Errorf("review round %d: %w", round, err)
		}

		s.ReviewRound = round
		_ = s.Save()

		if issues == 0 {
			o.Display.Step("Code Review", "approved")
			return s.Advance(state.PhaseReview)
		}

		o.Display.Step("Code Review", fmt.Sprintf("round %d: %d issue(s) found", round, issues))

		if round == maxRounds {
			o.Display.Warn(fmt.Sprintf("Code Review: max rounds (%d) reached, proceeding", maxRounds))
			break
		}

		_ = o.GH.PostComment(ctx, o.Owner, o.Repo, pr.Number,
			fmt.Sprintf("🔧 Addressing review feedback (round %d/%d)...", round, maxRounds))

		fixPrompt := fmt.Sprintf("Address the code review feedback from round %d.\n\nReview feedback:\n%s\n\nFix the issues, commit, and push.", round, feedback)
		
		if err := codeAgent.Fix(ctx, fixPrompt); err != nil {
			return fmt.Errorf("fixing review issues (round %d): %w", round, err)
		}

		if err := o.Git.Push(ctx, codeAgent.Workdir, "origin", codeAgent.Branch); err != nil {
			return fmt.Errorf("pushing review fixes (round %d): %w", round, err)
		}
	}

	return s.Advance(state.PhaseReview)
}

// ---------------------------------------------------------------------------
// Phase: Watch (respond to PR feedback)
// ---------------------------------------------------------------------------

func (o *Orchestrator) phaseWatch(ctx context.Context, s *state.RunState, codeAgent *agent.CodeAgent, pr *gh.PR) error {
	if s.Phase.AtLeast(state.PhaseDone) {
		return nil
	}

	if !s.Phase.AtLeast(state.PhaseWatch) {
		if err := s.Advance(state.PhaseWatch); err != nil {
			return err
		}
	}

	o.Display.Info(fmt.Sprintf("Entering watch loop for PR #%d", pr.Number))

	o.Config.Watch.AutoMerge = o.AutoMerge
	result, err := o.Watch.Loop(ctx, codeAgent, s, o.Config, o.Display)
	if err != nil {
		return fmt.Errorf("watch loop: %w", err)
	}

	switch result.Reason {
	case watch.ExitMerged:
		o.Display.Success(fmt.Sprintf("PR #%d merged — done!", pr.Number))
		o.Notify.PRMerged(pr.Number, o.Number)
	case watch.ExitClosed:
		o.Display.Warn(fmt.Sprintf("PR #%d closed without merge", pr.Number))
	case watch.ExitTimeout:
		o.Display.Warn("Watch loop timed out")
	case watch.ExitIdle:
		o.Display.Warn("Watch loop exited due to inactivity")
	case watch.ExitMaxFix:
		o.Display.Warn("Watch loop exited — max fix rounds reached")
	case watch.ExitReadyToMerge:
		o.Display.Info(fmt.Sprintf("Auto-merging PR #%d...", pr.Number))
		if err := o.GH.MergePR(ctx, o.Owner, o.Repo, pr.Number); err != nil {
			o.Display.Warn(fmt.Sprintf("Auto-merge failed: %v", err))
			return fmt.Errorf("auto-merge PR #%d: %w", pr.Number, err)
		}
		o.Display.Success(fmt.Sprintf("PR #%d squash-merged!", pr.Number))
		o.Notify.PRMerged(pr.Number, o.Number)
	}

	return s.Advance(state.PhaseDone)
}

func (o *Orchestrator) cleanup(ctx context.Context) {
	_ = o.Git.RemoveWorktree(ctx, o.RepoRoot, "impl")
}
