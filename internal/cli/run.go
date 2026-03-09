package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
	"github.com/nullne/star-fleet/internal/orchestrator"
	"github.com/nullne/star-fleet/internal/state"
	"github.com/nullne/star-fleet/internal/ui"
)

var (
	restartFlag   bool
	noWatchFlag   bool
	autoMergeFlag bool
)

var runCmd = &cobra.Command{
	Use:   "run <issue>",
	Short: "Run the Star Fleet pipeline for a GitHub issue",
	Long: `Run the Star Fleet pipeline for a GitHub issue.

Accepts issue references in three formats:
  fleet run 42
  fleet run https://github.com/org/repo/issues/42
  fleet run org/repo#42

The pipeline automatically saves progress. If a run is interrupted, re-running
the same command resumes from the last completed phase. Use --restart to discard
saved state and start fresh.

After creating a PR, the agent enters a watch loop that responds to review
comments and CI results. Use --no-watch to skip the watch loop.`,
	Args: cobra.ExactArgs(1),
	RunE: runPipeline,
}

func init() {
	runCmd.Flags().BoolVar(&restartFlag, "restart", false, "discard saved state and start the pipeline from scratch")
	runCmd.Flags().BoolVar(&noWatchFlag, "no-watch", false, "skip the watch loop after creating the PR")
	runCmd.Flags().BoolVar(&autoMergeFlag, "auto-merge", false, "automatically squash-merge the PR when CI passes")
}

type issueRef struct {
	Owner  string
	Repo   string
	Number int
}

var (
	nwoPattern = regexp.MustCompile(`^([^/]+)/([^#]+)#(\d+)$`)
	urlPattern = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/issues/(\d+)`)
)

func parseIssueRef(ctx context.Context, ref string) (*issueRef, error) {
	// Try plain number
	if n, err := strconv.Atoi(ref); err == nil {
		repo, err := gh.CurrentRepo(ctx)
		if err != nil {
			return nil, fmt.Errorf("issue number %d given but cannot detect repo: %w", n, err)
		}
		return &issueRef{Owner: repo.Owner, Repo: repo.Repo, Number: n}, nil
	}

	// Try org/repo#42
	if m := nwoPattern.FindStringSubmatch(ref); m != nil {
		n, _ := strconv.Atoi(m[3])
		return &issueRef{Owner: m[1], Repo: m[2], Number: n}, nil
	}

	// Try full URL
	if strings.Contains(ref, "github.com") {
		if u, err := url.Parse(ref); err == nil {
			if m := urlPattern.FindStringSubmatch(u.String()); m != nil {
				n, _ := strconv.Atoi(m[3])
				return &issueRef{Owner: m[1], Repo: m[2], Number: n}, nil
			}
		}
	}

	return nil, fmt.Errorf("cannot parse issue reference %q\n\nExpected formats:\n  fleet run 42\n  fleet run org/repo#42\n  fleet run https://github.com/org/repo/issues/42", ref)
}

func runPipeline(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	ref, err := parseIssueRef(ctx, args[0])
	if err != nil {
		return err
	}

	repoRoot, err := git.RepoRoot(ctx)
	if err != nil {
		return fmt.Errorf("must be run inside a git repository: %w", err)
	}

	cfg, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	restart := restartFlag
	if !restart {
		restart, err = promptIfStateExists(repoRoot, ref.Number)
		if err != nil {
			return err
		}
	}

	autoMerge := cfg.Watch.AutoMerge
	if cmd.Flags().Changed("auto-merge") {
		autoMerge = autoMergeFlag
	}

	display := ui.New()

	o := &orchestrator.Orchestrator{
		Owner:     ref.Owner,
		Repo:      ref.Repo,
		Number:    ref.Number,
		Config:    cfg,
		Display:   display,
		RepoRoot:  repoRoot,
		Restart:   restart,
		NoWatch:   noWatchFlag,
		AutoMerge: autoMerge,
	}

	return o.Run(ctx)
}

// promptIfStateExists checks for saved pipeline state and asks the user
// whether to resume or restart. Returns true if the user chooses to restart.
// In non-interactive environments (piped stdin), defaults to resume (false).
func promptIfStateExists(repoRoot string, number int) (restart bool, _ error) {
	s, err := state.Load(repoRoot, number)
	if err != nil {
		return false, err
	}
	if s == nil || s.Phase == state.PhaseNew || s.Phase == state.PhaseDone {
		return false, nil
	}

	if !isInteractive() {
		return false, nil
	}

	fmt.Fprintf(os.Stderr, "\n  Previous run found for #%d (stopped at: %s)\n", number, s.Phase)
	fmt.Fprintf(os.Stderr, "  Resume? [Y/n]: ")

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "n" || answer == "no" {
		return true, nil
	}
	return false, nil
}

func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
