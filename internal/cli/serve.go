package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/orchestrator"
	"github.com/nullne/star-fleet/internal/ui"
	"github.com/nullne/star-fleet/internal/webhook"
)

var (
	servePort          int
	serveWebhookSecret string
	serveRepo          string
	serveLabel         string
	serveBotUser       string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a webhook server for GitHub App integration",
	Long: `Start an HTTP server that receives GitHub webhook events and triggers
fleet runs automatically.

The server handles:
  - issues events: triggers a run when an issue is labeled with the configured label
  - issue_comment events: triggers a run when a comment starts with /fleet run

The webhook secret is required for signature verification and can be provided
via the --webhook-secret flag or the FLEET_WEBHOOK_SECRET environment variable.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8888, "HTTP listen port")
	serveCmd.Flags().StringVar(&serveWebhookSecret, "webhook-secret", "", "GitHub webhook secret for signature verification (or FLEET_WEBHOOK_SECRET env)")
	serveCmd.Flags().StringVar(&serveRepo, "repo", ".", "path to the git repo to operate on")
	serveCmd.Flags().StringVar(&serveLabel, "label", "fleet", "issue label that triggers a fleet run")
	serveCmd.Flags().StringVar(&serveBotUser, "bot-user", "", "GitHub login of the bot to ignore (anti-loop)")
}

func resolveWebhookSecret(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("FLEET_WEBHOOK_SECRET")
}

func runServe(cmd *cobra.Command, args []string) error {
	secret := resolveWebhookSecret(serveWebhookSecret)
	if secret == "" {
		return fmt.Errorf("webhook secret is required: use --webhook-secret or set FLEET_WEBHOOK_SECRET")
	}

	repoRoot := serveRepo

	cfg, err := config.Load(repoRoot)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	runner := &pipelineRunner{
		repoRoot: repoRoot,
		cfg:      cfg,
	}

	handler := webhook.NewHandler(serveLabel, serveBotUser, runner)
	srv := webhook.NewServer(servePort, secret, handler)

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("serve: shutting down...")
		shutdownCtx := context.Background()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("serve: shutdown error: %v", err)
		}
	}()

	log.Printf("serve: listening on :%d", servePort)
	log.Printf("serve: trigger label=%q, bot-user=%q, repo=%q", serveLabel, serveBotUser, repoRoot)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// pipelineRunner implements webhook.Runner by creating and running an Orchestrator.
type pipelineRunner struct {
	repoRoot string
	cfg      *config.Config
}

func (r *pipelineRunner) Run(owner, repo string, number int) error {
	display := ui.New()
	o := &orchestrator.Orchestrator{
		Owner:    owner,
		Repo:     repo,
		Number:   number,
		Config:   r.cfg,
		Display:  display,
		RepoRoot: r.repoRoot,
	}
	return o.Run(context.Background())
}
