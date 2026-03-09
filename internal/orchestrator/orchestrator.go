package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
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
}

// GitClient abstracts git operations for testing.
type GitClient interface {
	CreateWorktree(ctx context.Context, repoRoot, name, branch string) (string, error)
	RemoveWorktree(ctx context.Context, repoRoot, name string) error
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

type defaultGit struct{}

func (defaultGit) CreateWorktree(ctx context.Context, repoRoot, name, branch string) (string, error) {
	return git.CreateWorktree(ctx, repoRoot, name, branch)
}
func (defaultGit) RemoveWorktree(ctx context.Context, repoRoot, name string) error {
	return git.RemoveWorktree(ctx, repoRoot, name)
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

type defaultBackendFactory struct{}

func (defaultBackendFactory) NewBackend(name string) (agent.Backend, error) {
	return agent.NewBackend(name)
}

// Orchestrator is the core pipeline controller.
type Orchestrator struct {
	Owner    string
	Repo     string
	Number   int
	Config   *config.Config
	Display  *ui.Display
	RepoRoot string
	Restart  bool // when true, discard existing state and start fresh
	NoWatch  bool // when true, skip the watch loop after creating PR

	GH      GHClient
	Git     GitClient
	State   StateManager
	Watch   WatchRunner
	Backend BackendFactory
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
}

func (o *Orchestrator) Run(ctx context.Context) error {
	o.init()

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
		return o.State.New(o.RepoRoot, o.Owner, o.Repo, o.Number), nil
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

	result, err := o.Watch.Loop(ctx, codeAgent, s, o.Config, o.Display)
	if err != nil {
		return fmt.Errorf("watch loop: %w", err)
	}

	switch result.Reason {
	case watch.ExitMerged:
		o.Display.Success(fmt.Sprintf("PR #%d merged — done!", pr.Number))
	case watch.ExitClosed:
		o.Display.Warn(fmt.Sprintf("PR #%d closed without merge", pr.Number))
	case watch.ExitTimeout:
		o.Display.Warn("Watch loop timed out")
	case watch.ExitIdle:
		o.Display.Warn("Watch loop exited due to inactivity")
	case watch.ExitMaxFix:
		o.Display.Warn("Watch loop exited — max fix rounds reached")
	}

	return s.Advance(state.PhaseDone)
}

func (o *Orchestrator) cleanup(ctx context.Context) {
	_ = o.Git.RemoveWorktree(ctx, o.RepoRoot, "impl")
}
