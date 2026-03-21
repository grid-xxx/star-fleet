package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/ghapp"
	"github.com/nullne/star-fleet/internal/orchestrator"
	"github.com/nullne/star-fleet/internal/repocache"
	"github.com/nullne/star-fleet/internal/tester"
	"github.com/nullne/star-fleet/internal/ui"
	"github.com/nullne/star-fleet/internal/webhook"
)

var (
	servePort          int
	serveWebhookSecret string
	serveWorkdir       string
	serveAppID         string
	serveAppPrivateKey string
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
via the --webhook-secret flag or the FLEET_WEBHOOK_SECRET environment variable.

GitHub App credentials (--app-id, --app-private-key) are required for authenticating
git operations and API calls. They can also be set via FLEET_APP_ID and
FLEET_APP_PRIVATE_KEY_PATH environment variables.`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8888, "HTTP listen port")
	serveCmd.Flags().StringVar(&serveWebhookSecret, "webhook-secret", "", "GitHub webhook secret for signature verification (or FLEET_WEBHOOK_SECRET env)")
	serveCmd.Flags().StringVar(&serveWorkdir, "workdir", "/data/fleet-workdirs", "base directory for repo clones")
	serveCmd.Flags().StringVar(&serveAppID, "app-id", "", "GitHub App ID (or FLEET_APP_ID env)")
	serveCmd.Flags().StringVar(&serveAppPrivateKey, "app-private-key", "", "path to GitHub App private key PEM (or FLEET_APP_PRIVATE_KEY_PATH env)")
	serveCmd.Flags().StringVar(&serveLabel, "label", "fleet", "issue label that triggers a fleet run")
	serveCmd.Flags().StringVar(&serveBotUser, "bot-user", "", "GitHub login of the bot to ignore (anti-loop)")
}

func resolveEnvFlag(flagValue, envKey string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(envKey)
}

type serveConfig struct {
	secret  string
	appID   int64
	pemPath string
}

func resolveServeConfig(webhookSecret, appIDFlag, privateKeyFlag string) (*serveConfig, error) {
	secret := resolveEnvFlag(webhookSecret, "FLEET_WEBHOOK_SECRET")
	if secret == "" {
		return nil, fmt.Errorf("webhook secret is required: use --webhook-secret or set FLEET_WEBHOOK_SECRET")
	}

	appIDStr := resolveEnvFlag(appIDFlag, "FLEET_APP_ID")
	if appIDStr == "" {
		return nil, fmt.Errorf("app ID is required: use --app-id or set FLEET_APP_ID")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid app ID %q: %w", appIDStr, err)
	}

	pemPath := resolveEnvFlag(privateKeyFlag, "FLEET_APP_PRIVATE_KEY_PATH")
	if pemPath == "" {
		return nil, fmt.Errorf("app private key is required: use --app-private-key or set FLEET_APP_PRIVATE_KEY_PATH")
	}

	return &serveConfig{secret: secret, appID: appID, pemPath: pemPath}, nil
}

func runServe(cmd *cobra.Command, args []string) error {
	cfg, err := resolveServeConfig(serveWebhookSecret, serveAppID, serveAppPrivateKey)
	if err != nil {
		return err
	}

	appClient, err := ghapp.NewClient(cfg.appID, cfg.pemPath)
	if err != nil {
		return fmt.Errorf("initializing GitHub App client: %w", err)
	}

	workdir := serveWorkdir
	cache := repocache.New(workdir, appClient.InstallationToken)

	runner := &pipelineRunner{
		cache: cache,
	}

	handler := webhook.NewHandler(serveLabel, serveBotUser, runner)
	srv := webhook.NewServer(servePort, cfg.secret, handler)

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("serve: shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("serve: shutdown error: %v", err)
		}
	}()

	log.Printf("serve: listening on :%d", servePort)
	log.Printf("serve: trigger label=%q, bot-user=%q, workdir=%q", serveLabel, serveBotUser, workdir)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// pipelineRunner implements webhook.Runner by cloning/fetching the repo and
// running an Orchestrator against the local clone.
type pipelineRunner struct {
	cache *repocache.Cache
}

func (r *pipelineRunner) Run(owner, repo string, number int) error {
	ctx := context.Background()

	repoRoot, err := r.cache.Ensure(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("ensuring repo clone: %w", err)
	}

	cfg, err := config.Load(repoRoot)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	display := ui.New()
	o := &orchestrator.Orchestrator{
		Owner:    owner,
		Repo:     repo,
		Number:   number,
		Config:   cfg,
		Display:  display,
		RepoRoot: repoRoot,
	}
	return o.Run(ctx)
}

func (r *pipelineRunner) Test(owner, repo string, prNumber int) error {
	ctx := context.Background()

	repoRoot, err := r.cache.Ensure(ctx, owner, repo)
	if err == nil {
		// Fetch and checkout the PR branch specifically for testing
		prRef := fmt.Sprintf("pull/%d/head", prNumber)
		fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", prRef)
		fetchCmd.Dir = repoRoot
		fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		_ = fetchCmd.Run()

		checkoutCmd := exec.CommandContext(ctx, "git", "checkout", "FETCH_HEAD")
		checkoutCmd.Dir = repoRoot
		_ = checkoutCmd.Run()
	}
	if err != nil {
		return fmt.Errorf("ensuring repo clone: %w", err)
	}

	cfg, err := config.Load(repoRoot)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	testerCfg := &tester.Config{
		RepoRoot:    repoRoot,
		TestCommand: cfg.Test.Command,
		Owner:       owner,
		Repo:        repo,
		PRNumber:    prNumber,
		GH:          ghCommenter{},
		Log:         cliLogger{},
	}

	report, err := tester.Run(ctx, testerCfg)
	if err != nil {
		return fmt.Errorf("running tests: %w", err)
	}

	if !report.AllPassed {
		return fmt.Errorf("test failures: %d/%d modules failed",
			report.FailedModules+report.ErrorModules, report.TotalModules)
	}

	return nil
}
