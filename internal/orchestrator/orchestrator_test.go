package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/state"
	"github.com/nullne/star-fleet/internal/ui"
	"github.com/nullne/star-fleet/internal/watch"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockGH struct {
	fetchIssue    func(ctx context.Context, owner, repo string, number int) (*gh.Issue, error)
	postComment   func(ctx context.Context, owner, repo string, number int, body string) error
	defaultBranch func(ctx context.Context, owner, repo string) (string, error)
	findPR        func(ctx context.Context, owner, repo, head string) (*gh.PR, error)
	createPR      func(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error)
}

func (m *mockGH) FetchIssue(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
	if m.fetchIssue != nil {
		return m.fetchIssue(ctx, owner, repo, number)
	}
	return &gh.Issue{Number: number, Title: "Test issue", Body: strings.Repeat("x", 100)}, nil
}
func (m *mockGH) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	if m.postComment != nil {
		return m.postComment(ctx, owner, repo, number, body)
	}
	return nil
}
func (m *mockGH) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	if m.defaultBranch != nil {
		return m.defaultBranch(ctx, owner, repo)
	}
	return "main", nil
}
func (m *mockGH) FindPR(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
	if m.findPR != nil {
		return m.findPR(ctx, owner, repo, head)
	}
	return nil, nil
}
func (m *mockGH) CreatePR(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error) {
	if m.createPR != nil {
		return m.createPR(ctx, workdir, title, body, base, head)
	}
	return &gh.PR{Number: 42, URL: "https://github.com/test/repo/pull/42"}, nil
}

type mockGit struct {
	createWorktree func(ctx context.Context, repoRoot, name, branch string) (string, error)
	removeWorktree func(ctx context.Context, repoRoot, name string) error
	push           func(ctx context.Context, dir, remote, branch string) error
}

func (m *mockGit) CreateWorktree(ctx context.Context, repoRoot, name, branch string) (string, error) {
	if m.createWorktree != nil {
		return m.createWorktree(ctx, repoRoot, name, branch)
	}
	return "/tmp/worktree", nil
}
func (m *mockGit) RemoveWorktree(ctx context.Context, repoRoot, name string) error {
	if m.removeWorktree != nil {
		return m.removeWorktree(ctx, repoRoot, name)
	}
	return nil
}
func (m *mockGit) Push(ctx context.Context, dir, remote, branch string) error {
	if m.push != nil {
		return m.push(ctx, dir, remote, branch)
	}
	return nil
}

type mockState struct {
	load func(repoRoot string, number int) (*state.RunState, error)
	new_ func(repoRoot, owner, repo string, number int) *state.RunState
}

func (m *mockState) Load(repoRoot string, number int) (*state.RunState, error) {
	if m.load != nil {
		return m.load(repoRoot, number)
	}
	return nil, nil
}
func (m *mockState) New(repoRoot, owner, repo string, number int) *state.RunState {
	if m.new_ != nil {
		return m.new_(repoRoot, owner, repo, number)
	}
	return state.New(repoRoot, owner, repo, number)
}

type mockWatch struct {
	loop func(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error)
}

func (m *mockWatch) Loop(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error) {
	if m.loop != nil {
		return m.loop(ctx, codeAgent, s, cfg, display)
	}
	return &watch.Result{Reason: watch.ExitMerged}, nil
}

type mockBackendFactory struct {
	newBackend func(name string) (agent.Backend, error)
}

func (m *mockBackendFactory) NewBackend(name string) (agent.Backend, error) {
	if m.newBackend != nil {
		return m.newBackend(name)
	}
	return &noopBackend{}, nil
}

type noopBackend struct{}

func (noopBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testDisplay() *ui.Display {
	return ui.New()
}

func testConfig() *config.Config {
	return &config.Config{
		Agent: config.AgentConfig{Backend: "mock"},
		Watch: config.WatchConfig{MaxFixRounds: 5},
	}
}

func baseOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	return &Orchestrator{
		Owner:    "owner",
		Repo:     "repo",
		Number:   1,
		Config:   testConfig(),
		Display:  testDisplay(),
		RepoRoot: t.TempDir(),
		GH:       &mockGH{},
		Git:      &mockGit{},
		State:    &mockState{},
		Watch:    &mockWatch{},
		Backend:  &mockBackendFactory{},
	}
}

// ---------------------------------------------------------------------------
// loadState tests
// ---------------------------------------------------------------------------

func TestLoadState_Restart(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.Restart = true
	o.init()

	s, err := o.loadState()
	if err != nil {
		t.Fatalf("loadState() error = %v", err)
	}
	if s.Phase != state.PhaseNew {
		t.Errorf("loadState() phase = %v, want %v", s.Phase, state.PhaseNew)
	}
}

func TestLoadState_ExistingState(t *testing.T) {
	t.Parallel()
	existing := state.New(t.TempDir(), "owner", "repo", 1)
	existing.Phase = state.PhaseIntake

	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			return existing, nil
		},
	}
	o.init()

	s, err := o.loadState()
	if err != nil {
		t.Fatalf("loadState() error = %v", err)
	}
	if s.Phase != state.PhaseIntake {
		t.Errorf("loadState() phase = %v, want %v", s.Phase, state.PhaseIntake)
	}
}

func TestLoadState_NoStateFile(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			return nil, nil
		},
	}
	o.init()

	s, err := o.loadState()
	if err != nil {
		t.Fatalf("loadState() error = %v", err)
	}
	if s.Phase != state.PhaseNew {
		t.Errorf("loadState() phase = %v, want %v", s.Phase, state.PhaseNew)
	}
}

func TestLoadState_LoadError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			return nil, errors.New("disk error")
		},
	}
	o.init()

	_, err := o.loadState()
	if err == nil {
		t.Fatal("loadState() expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// validateSpec tests
// ---------------------------------------------------------------------------

func TestValidateSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "empty body",
			body:      "",
			wantErr:   true,
			errSubstr: "empty body",
		},
		{
			name:      "whitespace only",
			body:      "   \n\t  ",
			wantErr:   true,
			errSubstr: "empty body",
		},
		{
			name:      "too short",
			body:      "fix the bug",
			wantErr:   true,
			errSubstr: "too brief",
		},
		{
			name:    "49 chars (just under limit)",
			body:    strings.Repeat("x", 49),
			wantErr: true,
		},
		{
			name:    "50 chars (at limit)",
			body:    strings.Repeat("x", 50),
			wantErr: false,
		},
		{
			name:    "long body",
			body:    strings.Repeat("This is a detailed issue description. ", 10),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var commentPosted bool
			o := baseOrchestrator(t)
			o.GH = &mockGH{
				postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
					commentPosted = true
					return nil
				},
			}

			issue := &gh.Issue{Number: 1, Title: "Test", Body: tt.body}
			err := o.validateSpec(context.Background(), issue)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateSpec() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !commentPosted {
				t.Error("validateSpec() should post a comment on failure")
			}
			if tt.errSubstr != "" && err != nil && !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("validateSpec() error = %q, want substring %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

func TestValidateSpec_PostCommentError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			return errors.New("github error")
		},
	}

	issue := &gh.Issue{Number: 1, Title: "Test", Body: ""}
	err := o.validateSpec(context.Background(), issue)
	if err == nil {
		t.Fatal("validateSpec() should still return error even when comment fails")
	}
}

// ---------------------------------------------------------------------------
// phaseIntake tests
// ---------------------------------------------------------------------------

func TestPhaseIntake_NewIssue(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	var commentBodies []string
	o.GH = &mockGH{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			commentBodies = append(commentBodies, body)
			return nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	issue, err := o.phaseIntake(context.Background(), s)
	if err != nil {
		t.Fatalf("phaseIntake() error = %v", err)
	}
	if issue == nil {
		t.Fatal("phaseIntake() returned nil issue")
	}
	if s.Phase != state.PhaseIntake {
		t.Errorf("state phase = %v, want %v", s.Phase, state.PhaseIntake)
	}

	var pickedUp bool
	for _, body := range commentBodies {
		if strings.Contains(body, "Picked Up") {
			pickedUp = true
		}
	}
	if !pickedUp {
		t.Error("phaseIntake() should post 'Picked Up' comment")
	}
}

func TestPhaseIntake_AlreadyPastIntake(t *testing.T) {
	t.Parallel()
	var commentPosted bool
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			commentPosted = true
			return nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseImplement

	issue, err := o.phaseIntake(context.Background(), s)
	if err != nil {
		t.Fatalf("phaseIntake() error = %v", err)
	}
	if issue == nil {
		t.Fatal("phaseIntake() returned nil issue")
	}
	if commentPosted {
		t.Error("phaseIntake() should not post comment when resuming past intake")
	}
}

func TestPhaseIntake_FetchError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		fetchIssue: func(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
			return nil, errors.New("network error")
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	_, err := o.phaseIntake(context.Background(), s)
	if err == nil {
		t.Fatal("phaseIntake() expected error, got nil")
	}
}

func TestPhaseIntake_ValidationFails(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		fetchIssue: func(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
			return &gh.Issue{Number: number, Title: "Test", Body: ""}, nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	_, err := o.phaseIntake(context.Background(), s)
	if err == nil {
		t.Fatal("phaseIntake() expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "empty body") {
		t.Errorf("expected 'empty body' error, got %v", err)
	}
}

func TestPhaseIntake_PostPickedUpError(t *testing.T) {
	t.Parallel()
	callCount := 0
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		postComment: func(ctx context.Context, owner, repo string, number int, body string) error {
			callCount++
			if strings.Contains(body, "Picked Up") {
				return errors.New("github error")
			}
			return nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	_, err := o.phaseIntake(context.Background(), s)
	if err == nil {
		t.Fatal("phaseIntake() expected error from postPickedUp, got nil")
	}
}

// ---------------------------------------------------------------------------
// phaseImplement tests
// ---------------------------------------------------------------------------

func testIssue() *gh.Issue {
	return &gh.Issue{Number: 1, Title: "Test issue", Body: strings.Repeat("x", 100)}
}

func testCodeAgent() *agent.CodeAgent {
	return &agent.CodeAgent{
		Backend: &noopBackend{},
		Issue:   testIssue(),
		Workdir: "/tmp/worktree",
		Branch:  "fleet/1",
	}
}

func TestPhaseImplement_AlreadyPast(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhasePR

	err := o.phaseImplement(context.Background(), s, testCodeAgent())
	if err != nil {
		t.Fatalf("phaseImplement() error = %v", err)
	}
}

func TestPhaseImplement_AgentDone(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseIntake
	s.AgentDone = true

	err := o.phaseImplement(context.Background(), s, testCodeAgent())
	if err != nil {
		t.Fatalf("phaseImplement() error = %v", err)
	}
	if s.Phase != state.PhaseImplement {
		t.Errorf("state phase = %v, want %v", s.Phase, state.PhaseImplement)
	}
}

func TestPhaseImplement_Success(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseIntake

	err := o.phaseImplement(context.Background(), s, testCodeAgent())
	if err != nil {
		t.Fatalf("phaseImplement() error = %v", err)
	}
	if !s.AgentDone {
		t.Error("phaseImplement() should set AgentDone=true")
	}
	if s.Phase != state.PhaseImplement {
		t.Errorf("state phase = %v, want %v", s.Phase, state.PhaseImplement)
	}
}

func TestPhaseImplement_AgentFails(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseIntake

	ca := testCodeAgent()
	ca.Backend = &failBackend{err: errors.New("agent crash")}
	err := o.phaseImplement(context.Background(), s, ca)
	if err == nil {
		t.Fatal("phaseImplement() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "code agent") {
		t.Errorf("expected 'code agent' in error, got %v", err)
	}
}

type failBackend struct {
	err error
}

func (f *failBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	return f.err
}

// ---------------------------------------------------------------------------
// phasePR tests
// ---------------------------------------------------------------------------

func TestPhasePR_AlreadyPast(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseWatch
	s.PR = &state.PRInfo{Number: 99, URL: "https://github.com/test/repo/pull/99"}

	pr, err := o.phasePR(context.Background(), s, testCodeAgent(), testIssue())
	if err != nil {
		t.Fatalf("phasePR() error = %v", err)
	}
	if pr.Number != 99 {
		t.Errorf("phasePR() PR number = %d, want 99", pr.Number)
	}
}

func TestPhasePR_PushFails(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.Git = &mockGit{
		push: func(ctx context.Context, dir, remote, branch string) error {
			return errors.New("push failed")
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseImplement

	_, err := o.phasePR(context.Background(), s, testCodeAgent(), testIssue())
	if err == nil {
		t.Fatal("phasePR() expected push error, got nil")
	}
	if !strings.Contains(err.Error(), "pushing branch") {
		t.Errorf("expected 'pushing branch' in error, got %v", err)
	}
}

func TestPhasePR_PRAlreadyExists(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		findPR: func(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
			return &gh.PR{Number: 77, URL: "https://github.com/test/repo/pull/77"}, nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseImplement

	pr, err := o.phasePR(context.Background(), s, testCodeAgent(), testIssue())
	if err != nil {
		t.Fatalf("phasePR() error = %v", err)
	}
	if pr.Number != 77 {
		t.Errorf("phasePR() reused PR number = %d, want 77", pr.Number)
	}
	if s.PR.Number != 77 {
		t.Errorf("state PR number = %d, want 77", s.PR.Number)
	}
	if s.Phase != state.PhasePR {
		t.Errorf("state phase = %v, want %v", s.Phase, state.PhasePR)
	}
}

func TestPhasePR_CreatesFreshPR(t *testing.T) {
	t.Parallel()
	var capturedTitle, capturedBody string
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		findPR: func(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
			return nil, nil
		},
		createPR: func(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error) {
			capturedTitle = title
			capturedBody = body
			return &gh.PR{Number: 42, URL: "https://github.com/test/repo/pull/42"}, nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseImplement
	s.BaseBranch = "main"

	issue := &gh.Issue{Number: 1, Title: "Add feature", Body: strings.Repeat("x", 100)}
	pr, err := o.phasePR(context.Background(), s, testCodeAgent(), issue)
	if err != nil {
		t.Fatalf("phasePR() error = %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("phasePR() PR number = %d, want 42", pr.Number)
	}
	if !strings.Contains(capturedTitle, "Add feature") {
		t.Errorf("PR title = %q, want to contain 'Add feature'", capturedTitle)
	}
	if !strings.Contains(capturedBody, "Closes #1") {
		t.Errorf("PR body should contain 'Closes #1', got %q", capturedBody)
	}
}

func TestPhasePR_CreatePRFails(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		findPR: func(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
			return nil, nil
		},
		createPR: func(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error) {
			return nil, errors.New("pr creation failed")
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseImplement

	_, err := o.phasePR(context.Background(), s, testCodeAgent(), testIssue())
	if err == nil {
		t.Fatal("phasePR() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "creating PR") {
		t.Errorf("expected 'creating PR' in error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// findOrCreatePR tests
// ---------------------------------------------------------------------------

func TestFindOrCreatePR_ExistingPR(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		findPR: func(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
			return &gh.PR{Number: 10, URL: "https://github.com/test/repo/pull/10"}, nil
		},
		createPR: func(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error) {
			t.Error("CreatePR should not be called when PR already exists")
			return nil, nil
		},
	}

	pr, err := o.findOrCreatePR(context.Background(), "/tmp", "title", "body", "main", "fleet/1")
	if err != nil {
		t.Fatalf("findOrCreatePR() error = %v", err)
	}
	if pr.Number != 10 {
		t.Errorf("findOrCreatePR() PR number = %d, want 10", pr.Number)
	}
}

func TestFindOrCreatePR_NoExistingPR(t *testing.T) {
	t.Parallel()
	var created bool
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		findPR: func(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
			return nil, nil
		},
		createPR: func(ctx context.Context, workdir, title, body, base, head string) (*gh.PR, error) {
			created = true
			return &gh.PR{Number: 50, URL: "https://github.com/test/repo/pull/50"}, nil
		},
	}

	pr, err := o.findOrCreatePR(context.Background(), "/tmp", "title", "body", "main", "fleet/1")
	if err != nil {
		t.Fatalf("findOrCreatePR() error = %v", err)
	}
	if !created {
		t.Error("findOrCreatePR() should call CreatePR when no existing PR found")
	}
	if pr.Number != 50 {
		t.Errorf("findOrCreatePR() PR number = %d, want 50", pr.Number)
	}
}

func TestFindOrCreatePR_FindError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		findPR: func(ctx context.Context, owner, repo, head string) (*gh.PR, error) {
			return nil, errors.New("api error")
		},
	}

	_, err := o.findOrCreatePR(context.Background(), "/tmp", "title", "body", "main", "fleet/1")
	if err == nil {
		t.Fatal("findOrCreatePR() expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// phaseWatch tests
// ---------------------------------------------------------------------------

func TestPhaseWatch_AlreadyDone(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.Watch = &mockWatch{
		loop: func(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error) {
			t.Error("Loop should not be called when already done")
			return nil, nil
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhaseDone

	pr := &gh.PR{Number: 42, URL: "https://github.com/test/repo/pull/42"}

	err := o.phaseWatch(context.Background(), s, testCodeAgent(), pr)
	if err != nil {
		t.Fatalf("phaseWatch() error = %v", err)
	}
}

func TestPhaseWatch_ExitReasons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason watch.ExitReason
	}{
		{"merged", watch.ExitMerged},
		{"closed", watch.ExitClosed},
		{"timeout", watch.ExitTimeout},
		{"idle", watch.ExitIdle},
		{"max-fix", watch.ExitMaxFix},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := baseOrchestrator(t)
			o.Watch = &mockWatch{
				loop: func(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error) {
					return &watch.Result{Reason: tt.reason}, nil
				},
			}

			s := state.New(t.TempDir(), "owner", "repo", 1)
			s.Phase = state.PhasePR

			pr := &gh.PR{Number: 42, URL: "https://github.com/test/repo/pull/42"}

			err := o.phaseWatch(context.Background(), s, testCodeAgent(), pr)
			if err != nil {
				t.Fatalf("phaseWatch() error = %v", err)
			}
			if s.Phase != state.PhaseDone {
				t.Errorf("state phase = %v, want %v", s.Phase, state.PhaseDone)
			}
		})
	}
}

func TestPhaseWatch_LoopError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.Watch = &mockWatch{
		loop: func(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error) {
			return nil, errors.New("watch error")
		},
	}

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhasePR

	pr := &gh.PR{Number: 42, URL: "https://github.com/test/repo/pull/42"}

	err := o.phaseWatch(context.Background(), s, testCodeAgent(), pr)
	if err == nil {
		t.Fatal("phaseWatch() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "watch loop") {
		t.Errorf("expected 'watch loop' in error, got %v", err)
	}
}

func TestPhaseWatch_AdvancesToWatch(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	s := state.New(t.TempDir(), "owner", "repo", 1)
	s.Phase = state.PhasePR

	pr := &gh.PR{Number: 42, URL: "https://github.com/test/repo/pull/42"}

	err := o.phaseWatch(context.Background(), s, testCodeAgent(), pr)
	if err != nil {
		t.Fatalf("phaseWatch() error = %v", err)
	}
	if s.Phase != state.PhaseDone {
		t.Errorf("state phase = %v, want %v", s.Phase, state.PhaseDone)
	}
}

// ---------------------------------------------------------------------------
// Run (full pipeline) tests
// ---------------------------------------------------------------------------

func TestRun_FullPipeline(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_AlreadyDone(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			s := state.New(t.TempDir(), "owner", "repo", 1)
			s.Phase = state.PhaseDone
			return s, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_ResumeFromIntake(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			s := state.New(t.TempDir(), "owner", "repo", 1)
			s.Phase = state.PhaseIntake
			s.BaseBranch = "main"
			return s, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_ResumeFromImplement(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			s := state.New(t.TempDir(), "owner", "repo", 1)
			s.Phase = state.PhaseImplement
			s.BaseBranch = "main"
			return s, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_ResumeFromPR(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			s := state.New(t.TempDir(), "owner", "repo", 1)
			s.Phase = state.PhasePR
			s.BaseBranch = "main"
			s.PR = &state.PRInfo{Number: 42, URL: "https://github.com/test/repo/pull/42"}
			return s, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_ResumeFromWatch(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			s := state.New(t.TempDir(), "owner", "repo", 1)
			s.Phase = state.PhaseWatch
			s.BaseBranch = "main"
			s.PR = &state.PRInfo{Number: 42, URL: "https://github.com/test/repo/pull/42"}
			return s, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRun_RestartFlag(t *testing.T) {
	t.Parallel()
	loadCalled := false
	o := baseOrchestrator(t)
	o.Restart = true
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			loadCalled = true
			return nil, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if loadCalled {
		t.Error("Run(restart=true) should not call State.Load")
	}
}

func TestRun_NoWatchFlag(t *testing.T) {
	t.Parallel()
	watchCalled := false
	o := baseOrchestrator(t)
	o.NoWatch = true
	o.Watch = &mockWatch{
		loop: func(ctx context.Context, codeAgent *agent.CodeAgent, s *state.RunState, cfg *config.Config, display *ui.Display) (*watch.Result, error) {
			watchCalled = true
			return &watch.Result{Reason: watch.ExitMerged}, nil
		},
	}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if watchCalled {
		t.Error("Run(noWatch=true) should not call Watch.Loop")
	}
}

func TestRun_ContextCancelled_DuringIntake(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	o := baseOrchestrator(t)
	o.GH = &mockGH{
		fetchIssue: func(ctx context.Context, owner, repo string, number int) (*gh.Issue, error) {
			return nil, ctx.Err()
		},
	}

	err := o.Run(ctx)
	if err == nil {
		t.Fatal("Run() expected context cancellation error, got nil")
	}
}

func TestRun_ContextCancelled_DuringImplement(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	o := baseOrchestrator(t)
	o.Backend = &mockBackendFactory{
		newBackend: func(name string) (agent.Backend, error) {
			return &failBackend{err: fmt.Errorf("backend: %w", context.Canceled)}, nil
		},
	}

	go func() {
		cancel()
	}()

	err := o.Run(ctx)
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
}

func TestRun_DefaultBranchError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.GH = &mockGH{
		defaultBranch: func(ctx context.Context, owner, repo string) (string, error) {
			return "", errors.New("api error")
		},
	}

	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "detecting default branch") {
		t.Errorf("expected 'detecting default branch' error, got %v", err)
	}
}

func TestRun_WorktreeError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.Git = &mockGit{
		createWorktree: func(ctx context.Context, repoRoot, name, branch string) (string, error) {
			return "", errors.New("worktree error")
		},
	}

	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("Run() expected worktree error, got nil")
	}
	if !strings.Contains(err.Error(), "creating worktree") {
		t.Errorf("expected 'creating worktree' error, got %v", err)
	}
}

func TestRun_BackendFactoryError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.Backend = &mockBackendFactory{
		newBackend: func(name string) (agent.Backend, error) {
			return nil, errors.New("unknown backend")
		},
	}

	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("Run() expected backend error, got nil")
	}
}

func TestRun_StateLoadError(t *testing.T) {
	t.Parallel()
	o := baseOrchestrator(t)
	o.State = &mockState{
		load: func(repoRoot string, number int) (*state.RunState, error) {
			return nil, errors.New("state corruption")
		},
	}

	err := o.Run(context.Background())
	if err == nil {
		t.Fatal("Run() expected state error, got nil")
	}
}

// ---------------------------------------------------------------------------
// init() defaults tests
// ---------------------------------------------------------------------------

func TestInit_SetsDefaults(t *testing.T) {
	t.Parallel()
	o := &Orchestrator{}
	o.init()

	if o.GH == nil {
		t.Error("init() should set GH default")
	}
	if o.Git == nil {
		t.Error("init() should set Git default")
	}
	if o.State == nil {
		t.Error("init() should set State default")
	}
	if o.Watch == nil {
		t.Error("init() should set Watch default")
	}
	if o.Backend == nil {
		t.Error("init() should set Backend default")
	}
}

func TestInit_PreservesOverrides(t *testing.T) {
	t.Parallel()
	customGH := &mockGH{}
	o := &Orchestrator{GH: customGH}
	o.init()

	if o.GH != customGH {
		t.Error("init() should not overwrite existing GH")
	}
}

// ---------------------------------------------------------------------------
// cleanup tests
// ---------------------------------------------------------------------------

func TestCleanup(t *testing.T) {
	t.Parallel()
	var removed bool
	o := baseOrchestrator(t)
	o.Git = &mockGit{
		removeWorktree: func(ctx context.Context, repoRoot, name string) error {
			removed = true
			if name != "impl" {
				t.Errorf("cleanup() name = %q, want 'impl'", name)
			}
			return nil
		},
	}

	o.cleanup(context.Background())
	if !removed {
		t.Error("cleanup() should call RemoveWorktree")
	}
}
