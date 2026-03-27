package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Agent     AgentConfig     `toml:"agent"`
	Review    ReviewConfig    `toml:"review"`
	Watch     WatchConfig     `toml:"watch"`
	CI        CIConfig        `toml:"ci"`
	Test      TestConfig      `toml:"test"`
	Telegram  TelegramConfig  `toml:"telegram"`
	IssueLint IssueLintConfig `toml:"issue_lint"`
}

type AgentConfig struct {
	Backend string `toml:"backend"`
}

type WatchConfig struct {
	PollInterval Duration `toml:"poll_interval"`
	Timeout      Duration `toml:"timeout"`
	IdleTimeout  Duration `toml:"idle_timeout"`
	MaxFixRounds int      `toml:"max_fix_rounds"`
	AutoMerge    bool     `toml:"auto_merge"`
}

type CIConfig struct {
	Enabled        bool     `toml:"enabled"`
	RequiredChecks []string `toml:"required_checks"`
}

type TestConfig struct {
	Command string `toml:"command"`
}

type ReviewConfig struct {
	Enabled    bool   `toml:"enabled"`
	MaxRounds  int    `toml:"max_rounds"`
	Backend    string `toml:"backend"`
	PromptFile string `toml:"prompt_file"`
	Name       string `toml:"name"` // display name in logs

	// GitHub App credentials for submitting reviews as a bot identity.
	// If both are set, reviews are posted via the GitHub API using an
	// installation token; otherwise we fall back to the gh CLI.
	AppID      int64  `toml:"app_id"`
	AppKeyFile string `toml:"app_key_file"`
}

type TelegramConfig struct {
	BotToken string `toml:"bot_token"`
	ChatID   string `toml:"chat_id"`
}

type IssueLintConfig struct {
	Enabled       bool   `toml:"enabled"`
	GuidelineFile string `toml:"guideline_file"`
	APIKey        string `toml:"api_key"`
	Model         string `toml:"model"`
}

// Duration wraps time.Duration to support TOML string parsing (e.g. "30s", "2h").
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
}

func defaults() Config {
	return Config{
		Agent: AgentConfig{Backend: "claude-code"},
		Review: ReviewConfig{
			Enabled:   true,
			MaxRounds: 3,
			Name:      "Code Review",
		},
		Watch: WatchConfig{
			PollInterval: Duration{30 * time.Second},
			Timeout:      Duration{2 * time.Hour},
			IdleTimeout:  Duration{30 * time.Minute},
			MaxFixRounds: 5,
		},
		CI: CIConfig{
			Enabled:        true,
			RequiredChecks: []string{},
		},
	}
}

func Load(repoRoot string) (*Config, error) {
	cfg := defaults()

	path := filepath.Join(repoRoot, ".fleet", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	} else {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	}

	if v := os.Getenv("FLEET_TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("FLEET_TELEGRAM_CHAT_ID"); v != "" {
		cfg.Telegram.ChatID = v
	}

	if v := os.Getenv("FLEET_REVIEW_APP_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing FLEET_REVIEW_APP_ID: %w", err)
		}
		cfg.Review.AppID = id
	}
	if v := os.Getenv("FLEET_REVIEW_APP_KEY"); v != "" {
		cfg.Review.AppKeyFile = v
	}

	if v := os.Getenv("FLEET_ISSUE_LINT_API_KEY"); v != "" {
		cfg.IssueLint.APIKey = v
	}

	switch cfg.Agent.Backend {
	case "claude-code", "cursor", "mock":
	default:
		return nil, fmt.Errorf("unsupported agent backend %q (want \"claude-code\", \"cursor\", or \"mock\")", cfg.Agent.Backend)
	}

	if cfg.Review.Backend != "" {
		switch cfg.Review.Backend {
		case "claude-code", "cursor", "mock":
		default:
			return nil, fmt.Errorf("unsupported review backend %q (want \"claude-code\", \"cursor\", or \"mock\")", cfg.Review.Backend)
		}
	}

	if cfg.Review.MaxRounds < 1 {
		cfg.Review.MaxRounds = 3
	}

	if cfg.IssueLint.Model == "" {
		cfg.IssueLint.Model = "claude-sonnet-4-20250514"
	}
	if cfg.IssueLint.GuidelineFile == "" {
		cfg.IssueLint.GuidelineFile = "docs/ISSUE-SPEC.md"
	}

	return &cfg, nil
}

// Summary returns a human-readable config summary for display at startup.
func (c *Config) Summary() string {
	lines := []string{
		fmt.Sprintf("agent.backend      = %s", c.Agent.Backend),
		fmt.Sprintf("review.enabled     = %v", c.Review.Enabled),
		fmt.Sprintf("review.max_rounds  = %d", c.Review.MaxRounds),
		fmt.Sprintf("watch.timeout      = %s", c.Watch.Timeout.String()),
		fmt.Sprintf("watch.auto_merge   = %v", c.Watch.AutoMerge),
	}
	return strings.Join(lines, "\n")
}
