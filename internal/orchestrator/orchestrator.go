package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/nullne/star-fleet/internal/agent"
	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
	"github.com/nullne/star-fleet/internal/review"
	"github.com/nullne/star-fleet/internal/ui"
	"github.com/nullne/star-fleet/internal/validate"
)

type Orchestrator struct {
	Owner    string
	Repo     string
	Number   int
	Config   *config.Config
	Display  *ui.Display
	RepoRoot string
}

func (o *Orchestrator) Run(ctx context.Context) error {
	baseBranch, err := gh.DefaultBranch(ctx, o.Owner, o.Repo)
	if err != nil {
		return fmt.Errorf("detecting default branch: %w", err)
	}

	o.Display.Title(o.Owner, o.Repo, o.Number)

	// --- Intake ---
	issue, err := o.intake(ctx)
	if err != nil {
		return err
	}

	if err := o.postPickedUp(ctx); err != nil {
		return err
	}

	// --- Create worktrees ---
	issueID := strconv.Itoa(o.Number)
	devBranch := "fleet/dev/" + issueID
	testBranch := "fleet/test/" + issueID

	devDir, err := git.CreateWorktree(ctx, o.RepoRoot, "dev", devBranch)
	if err != nil {
		o.Display.StepFail("Creating worktrees...", err.Error())
		return fmt.Errorf("creating dev worktree: %w", err)
	}

	testDir, err := git.CreateWorktree(ctx, o.RepoRoot, "test", testBranch)
	if err != nil {
		o.Display.StepFail("Creating worktrees...", err.Error())
		return fmt.Errorf("creating test worktree: %w", err)
	}

	defer o.cleanup(ctx)

	// --- Create backend ---
	backend, err := agent.NewBackend(o.Config.Agent.Backend)
	if err != nil {
		return err
	}

	devAgent := &agent.DevAgent{
		Backend:    backend,
		Owner:      o.Owner,
		Repo:       o.Repo,
		Issue:      issue,
		Workdir:    devDir,
		Branch:     devBranch,
		BaseBranch: baseBranch,
	}

	testAgent := &agent.TestAgent{
		Backend:    backend,
		Owner:      o.Owner,
		Repo:       o.Repo,
		Issue:      issue,
		Workdir:    testDir,
		Branch:     testBranch,
		BaseBranch: baseBranch,
	}

	// --- Dispatch agents in parallel ---
	o.Display.Blank()
	devPR, testPR, err := o.dispatch(ctx, devAgent, testAgent, baseBranch)
	if err != nil {
		return err
	}

	// --- Review loop ---
	o.Display.Blank()
	if err := o.reviewLoop(ctx, backend, devAgent, testAgent, devPR, testPR); err != nil {
		return err
	}

	// --- Cross-validation ---
	maxCycles := o.Config.Validate.MaxCycles
	maxRounds := o.Config.Validate.MaxFixRounds

	for cycle := 1; cycle <= maxCycles; cycle++ {
		for round := 1; round <= maxRounds; round++ {
			label := fmt.Sprintf("Cross-validation (round %d)", round)

			result, err := validate.CrossValidate(ctx, o.RepoRoot, devBranch, testBranch, baseBranch, o.Config.Test.Command)
			if err != nil {
				o.Display.StepFail(label, err.Error())
				return err
			}

			if result.Passed {
				passed := result.TotalCount
				if passed == 0 {
					passed = result.FailCount
				}
				o.Display.Step(label, fmt.Sprintf("%d/%d passed", passed, passed))

				finalPR, err := o.createFinalPR(ctx, devBranch, testBranch, baseBranch, issue)
				if err != nil {
					return err
				}
				o.Display.Result(finalPR.URL)
				_ = gh.CloseIssue(ctx, o.Owner, o.Repo, o.Number)
				return nil
			}

			attr := result.Attribution
			if len(attr) > 0 {
				attr = strings.ToUpper(attr[:1]) + attr[1:]
			}
			o.Display.StepFail(label, fmt.Sprintf(
				"%d failed → %s Agent fixing",
				result.FailCount, attr))

			if err := o.fixFromValidation(ctx, devAgent, testAgent, result); err != nil {
				return err
			}

			validate.Cleanup(ctx, o.RepoRoot)
		}
	}

	// Exhausted all retries
	summary := fmt.Sprintf(
		"Star Fleet was unable to deliver a passing implementation after %d cycles × %d rounds.\n\nThe pipeline has been halted. Manual intervention is required.",
		maxCycles, maxRounds)
	_ = gh.PostComment(ctx, o.Owner, o.Repo, o.Number, "## ⚠️ Star Fleet — Pipeline Exhausted\n\n"+summary)

	o.Display.FailResult("Pipeline exhausted after max retries. Failure summary posted to issue.")
	return fmt.Errorf("pipeline exhausted")
}

func (o *Orchestrator) intake(ctx context.Context) (*gh.Issue, error) {
	sp := o.Display.TreeLeaf("Fetching issue...", "")
	issue, err := gh.FetchIssue(ctx, o.Owner, o.Repo, o.Number)
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
		_ = gh.PostComment(ctx, o.Owner, o.Repo, o.Number, comment)
		o.Display.StepFail("Validating spec...", "empty issue body")
		return fmt.Errorf("issue #%d has an empty body", o.Number)
	}
	if len(issue.Body) < 50 {
		gaps := "The issue description seems too brief. Please provide more detail about expected behavior, acceptance criteria, or edge cases."
		comment := "## 🔍 Star Fleet — Spec Gap\n\n" + gaps + "\n\nPipeline paused. Please update the issue and re-run."
		_ = gh.PostComment(ctx, o.Owner, o.Repo, o.Number, comment)
		o.Display.StepWarn("Validating spec...", "issue body is very short")
		return fmt.Errorf("issue #%d body is too brief (%d chars)", o.Number, len(issue.Body))
	}
	return nil
}

func (o *Orchestrator) postPickedUp(ctx context.Context) error {
	return gh.PostComment(ctx, o.Owner, o.Repo, o.Number,
		"## 🚀 Star Fleet — Picked Up\n\nThis issue has been picked up by Star Fleet. Dev and Test agents are being dispatched.")
}

func (o *Orchestrator) dispatch(ctx context.Context, devAgent *agent.DevAgent, testAgent *agent.TestAgent, baseBranch string) (*gh.PR, *gh.PR, error) {
	devSpinner := o.Display.TreeBranch("Dev Agent", "Writing implementation...")
	testSpinner := o.Display.TreeLeaf("Test Agent", "Writing tests...")

	type agentResult struct {
		role string
		err  error
	}

	results := make(chan agentResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		err := devAgent.Run(ctx)
		results <- agentResult{role: "dev", err: err}
	}()

	go func() {
		defer wg.Done()
		err := testAgent.Run(ctx)
		results <- agentResult{role: "test", err: err}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var devErr, testErr error
	for r := range results {
		switch r.role {
		case "dev":
			if r.err != nil {
				devErr = r.err
				devSpinner.Stop("fail", r.err.Error())
			}
		case "test":
			if r.err != nil {
				testErr = r.err
				testSpinner.Stop("fail", r.err.Error())
			}
		}
	}

	if devErr != nil {
		return nil, nil, fmt.Errorf("dev agent: %w", devErr)
	}
	if testErr != nil {
		return nil, nil, fmt.Errorf("test agent: %w", testErr)
	}

	// Push branches and create PRs
	if err := git.Push(ctx, devAgent.Workdir, "origin", devAgent.Branch); err != nil {
		devSpinner.Stop("fail", "push failed")
		return nil, nil, fmt.Errorf("pushing dev branch: %w", err)
	}
	if err := git.Push(ctx, testAgent.Workdir, "origin", testAgent.Branch); err != nil {
		testSpinner.Stop("fail", "push failed")
		return nil, nil, fmt.Errorf("pushing test branch: %w", err)
	}

	nwo := devAgent.Owner + "/" + devAgent.Repo
	devPR, err := gh.CreatePR(ctx, devAgent.Workdir,
		fmt.Sprintf("[fleet/dev] #%d %s", devAgent.Issue.Number, devAgent.Issue.Title),
		fmt.Sprintf("Implementation for #%d by Star Fleet Dev Agent.", devAgent.Issue.Number),
		baseBranch, devAgent.Branch)
	if err != nil {
		devSpinner.Stop("fail", "PR creation failed")
		return nil, nil, fmt.Errorf("creating dev PR: %w (repo: %s)", err, nwo)
	}
	devSpinner.Stop("success", fmt.Sprintf("PR #%d → %s", devPR.Number, devPR.URL))

	testPR, err := gh.CreatePR(ctx, testAgent.Workdir,
		fmt.Sprintf("[fleet/test] #%d %s", testAgent.Issue.Number, testAgent.Issue.Title),
		fmt.Sprintf("Tests for #%d by Star Fleet Test Agent.", testAgent.Issue.Number),
		baseBranch, testAgent.Branch)
	if err != nil {
		testSpinner.Stop("fail", "PR creation failed")
		return nil, nil, fmt.Errorf("creating test PR: %w", err)
	}
	testSpinner.Stop("success", fmt.Sprintf("PR #%d → %s", testPR.Number, testPR.URL))

	return devPR, testPR, nil
}

func (o *Orchestrator) reviewLoop(ctx context.Context, backend agent.Backend, devAgent *agent.DevAgent, testAgent *agent.TestAgent, devPR, testPR *gh.PR) error {
	o.Display.Info("Reviewing PRs...")

	devResult, err := review.ReviewPR(ctx, backend, devAgent.Workdir, o.Owner, o.Repo, devPR.Number, "dev")
	if err != nil {
		return fmt.Errorf("reviewing dev PR: %w", err)
	}

	if devResult.Clean {
		o.Display.Step(fmt.Sprintf("PR #%d", devPR.Number), "")
	} else {
		count := review.CountIssues(devResult.Feedback)
		o.Display.StepWarn(fmt.Sprintf("PR #%d", devPR.Number), fmt.Sprintf("%d comments posted", count))

		if err := devAgent.Fix(ctx, devResult.Feedback); err != nil {
			return fmt.Errorf("dev fix: %w", err)
		}
		if err := git.Push(ctx, devAgent.Workdir, "origin", devAgent.Branch); err != nil {
			return fmt.Errorf("pushing dev fixes: %w", err)
		}
	}

	testResult, err := review.ReviewPR(ctx, backend, testAgent.Workdir, o.Owner, o.Repo, testPR.Number, "test")
	if err != nil {
		return fmt.Errorf("reviewing test PR: %w", err)
	}

	if testResult.Clean {
		o.Display.Step(fmt.Sprintf("PR #%d", testPR.Number), "")
	} else {
		count := review.CountIssues(testResult.Feedback)
		o.Display.StepWarn(fmt.Sprintf("PR #%d", testPR.Number), fmt.Sprintf("%d comments posted", count))

		if err := testAgent.Fix(ctx, testResult.Feedback); err != nil {
			return fmt.Errorf("test fix: %w", err)
		}
		if err := git.Push(ctx, testAgent.Workdir, "origin", testAgent.Branch); err != nil {
			return fmt.Errorf("pushing test fixes: %w", err)
		}
	}

	return nil
}

func (o *Orchestrator) fixFromValidation(ctx context.Context, devAgent *agent.DevAgent, testAgent *agent.TestAgent, result *validate.Result) error {
	feedback := fmt.Sprintf("Cross-validation failed. Test output:\n\n```\n%s\n```\n\nFix the issues so all tests pass.", result.Output)

	switch result.Attribution {
	case "dev":
		if err := devAgent.Fix(ctx, feedback); err != nil {
			return fmt.Errorf("dev fix: %w", err)
		}
		if err := git.Push(ctx, devAgent.Workdir, "origin", devAgent.Branch); err != nil {
			return fmt.Errorf("pushing dev fixes: %w", err)
		}
	case "test":
		if err := testAgent.Fix(ctx, feedback); err != nil {
			return fmt.Errorf("test fix: %w", err)
		}
		if err := git.Push(ctx, testAgent.Workdir, "origin", testAgent.Branch); err != nil {
			return fmt.Errorf("pushing test fixes: %w", err)
		}
	}
	return nil
}

func (o *Orchestrator) createFinalPR(ctx context.Context, devBranch, testBranch, baseBranch string, issue *gh.Issue) (*gh.PR, error) {
	finalBranch := fmt.Sprintf("fleet/deliver/%d", o.Number)

	finalDir, err := git.CreateWorktree(ctx, o.RepoRoot, "deliver", finalBranch)
	if err != nil {
		return nil, fmt.Errorf("creating delivery worktree: %w", err)
	}
	defer func() {
		_ = git.RemoveWorktree(ctx, o.RepoRoot, "deliver")
	}()

	if err := git.Merge(ctx, finalDir, devBranch); err != nil {
		return nil, fmt.Errorf("merging dev into delivery: %w", err)
	}
	if err := git.Merge(ctx, finalDir, testBranch); err != nil {
		return nil, fmt.Errorf("merging test into delivery: %w", err)
	}

	if err := git.Push(ctx, finalDir, "origin", finalBranch); err != nil {
		return nil, fmt.Errorf("pushing delivery branch: %w", err)
	}

	pr, err := gh.CreatePR(ctx, finalDir,
		fmt.Sprintf("#%d %s", o.Number, issue.Title),
		fmt.Sprintf("## Star Fleet Delivery\n\nCloses #%d\n\nThis PR was generated by Star Fleet. Implementation and tests were written by independent agents and cross-validated.", o.Number),
		baseBranch, finalBranch)
	if err != nil {
		return nil, fmt.Errorf("creating final PR: %w", err)
	}

	return pr, nil
}

func (o *Orchestrator) cleanup(ctx context.Context) {
	_ = git.RemoveWorktree(ctx, o.RepoRoot, "dev")
	_ = git.RemoveWorktree(ctx, o.RepoRoot, "test")
	_ = git.RemoveWorktree(ctx, o.RepoRoot, "validate")
}
