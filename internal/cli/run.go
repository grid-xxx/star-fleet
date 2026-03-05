package cli

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
	"github.com/nullne/star-fleet/internal/git"
	"github.com/nullne/star-fleet/internal/orchestrator"
	"github.com/nullne/star-fleet/internal/ui"
)

var runCmd = &cobra.Command{
	Use:   "run <issue>",
	Short: "Run the Star Fleet pipeline for a GitHub issue",
	Long: `Run the Star Fleet pipeline for a GitHub issue.

Accepts issue references in three formats:
  fleet run 42
  fleet run https://github.com/org/repo/issues/42
  fleet run org/repo#42`,
	Args: cobra.ExactArgs(1),
	RunE: runPipeline,
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

	display := ui.New()

	o := &orchestrator.Orchestrator{
		Owner:    ref.Owner,
		Repo:     ref.Repo,
		Number:   ref.Number,
		Config:   cfg,
		Display:  display,
		RepoRoot: repoRoot,
	}

	return o.Run(ctx)
}
