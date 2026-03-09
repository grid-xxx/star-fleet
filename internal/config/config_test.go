package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Existing defaults
	if cfg.Agent.Backend != "claude-code" {
		t.Errorf("agent.backend = %q, want \"claude-code\"", cfg.Agent.Backend)
	}

	// Watch defaults
	if cfg.Watch.PollInterval.Duration != 30*time.Second {
		t.Errorf("watch.poll_interval = %v, want 30s", cfg.Watch.PollInterval.Duration)
	}
	if cfg.Watch.Timeout.Duration != 2*time.Hour {
		t.Errorf("watch.timeout = %v, want 2h", cfg.Watch.Timeout.Duration)
	}
	if cfg.Watch.IdleTimeout.Duration != 30*time.Minute {
		t.Errorf("watch.idle_timeout = %v, want 30m", cfg.Watch.IdleTimeout.Duration)
	}
	if cfg.Watch.MaxFixRounds != 5 {
		t.Errorf("watch.max_fix_rounds = %d, want 5", cfg.Watch.MaxFixRounds)
	}

	// CI defaults
	if !cfg.CI.Enabled {
		t.Error("ci.enabled should default to true")
	}
	if cfg.CI.RequiredChecks == nil {
		t.Error("ci.required_checks should default to empty slice, not nil")
	}
	if len(cfg.CI.RequiredChecks) != 0 {
		t.Errorf("ci.required_checks should be empty, got %v", cfg.CI.RequiredChecks)
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Should return defaults when no config file exists
	if cfg.Agent.Backend != "claude-code" {
		t.Errorf("expected default backend, got %q", cfg.Agent.Backend)
	}
}

func TestLoadWatchConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[watch]
poll_interval = "60s"
timeout = "4h"
idle_timeout = "1h"
max_fix_rounds = 10
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Watch.PollInterval.Duration != 60*time.Second {
		t.Errorf("poll_interval = %v, want 60s", cfg.Watch.PollInterval.Duration)
	}
	if cfg.Watch.Timeout.Duration != 4*time.Hour {
		t.Errorf("timeout = %v, want 4h", cfg.Watch.Timeout.Duration)
	}
	if cfg.Watch.IdleTimeout.Duration != 1*time.Hour {
		t.Errorf("idle_timeout = %v, want 1h", cfg.Watch.IdleTimeout.Duration)
	}
	if cfg.Watch.MaxFixRounds != 10 {
		t.Errorf("max_fix_rounds = %d, want 10", cfg.Watch.MaxFixRounds)
	}
}

func TestLoadCIConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[ci]
enabled = false
required_checks = ["build", "test", "lint"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CI.Enabled {
		t.Error("ci.enabled should be false")
	}
	if len(cfg.CI.RequiredChecks) != 3 {
		t.Fatalf("ci.required_checks len = %d, want 3", len(cfg.CI.RequiredChecks))
	}
	expected := []string{"build", "test", "lint"}
	for i, want := range expected {
		if cfg.CI.RequiredChecks[i] != want {
			t.Errorf("ci.required_checks[%d] = %q, want %q", i, cfg.CI.RequiredChecks[i], want)
		}
	}
}

func TestReviewDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Review.Enabled {
		t.Error("review.enabled should default to true")
	}
	if cfg.Review.MaxRounds != 3 {
		t.Errorf("review.max_rounds = %d, want 3", cfg.Review.MaxRounds)
	}
	if cfg.Review.Backend != "" {
		t.Errorf("review.backend should default to empty, got %q", cfg.Review.Backend)
	}
	if cfg.Review.PromptFile != "" {
		t.Errorf("review.prompt_file should default to empty, got %q", cfg.Review.PromptFile)
	}
	if cfg.Review.Name != "Code Review" {
		t.Errorf("review.name should default to %q, got %q", "Code Review", cfg.Review.Name)
	}
}

func TestLoadReviewConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[review]
enabled = false
max_rounds = 5
backend = "cursor"
prompt_file = "review.md"
name = "Fleet Bot"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Review.Enabled {
		t.Error("review.enabled should be false")
	}
	if cfg.Review.MaxRounds != 5 {
		t.Errorf("review.max_rounds = %d, want 5", cfg.Review.MaxRounds)
	}
	if cfg.Review.Backend != "cursor" {
		t.Errorf("review.backend = %q, want %q", cfg.Review.Backend, "cursor")
	}
	if cfg.Review.PromptFile != "review.md" {
		t.Errorf("review.prompt_file = %q, want %q", cfg.Review.PromptFile, "review.md")
	}
	if cfg.Review.Name != "Fleet Bot" {
		t.Errorf("review.name = %q, want %q", cfg.Review.Name, "Fleet Bot")
	}
}

func TestLoadReviewInvalidBackend(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[review]
backend = "gpt-4"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for unsupported review backend")
	}
}

func TestAutoMergeDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Watch.AutoMerge {
		t.Error("watch.auto_merge should default to false")
	}
}

func TestLoadAutoMergeConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[watch]
auto_merge = true
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.Watch.AutoMerge {
		t.Error("watch.auto_merge should be true")
	}
}

func TestLoadAutoMergeFalse(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[watch]
auto_merge = false
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Watch.AutoMerge {
		t.Error("watch.auto_merge should be false")
	}
}

func TestLoadPartialWatchConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Only specify timeout; other watch fields should use defaults
	data := `[agent]
backend = "claude-code"

[watch]
timeout = "6h"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Watch.Timeout.Duration != 6*time.Hour {
		t.Errorf("timeout = %v, want 6h", cfg.Watch.Timeout.Duration)
	}
	// Defaults for unspecified fields
	if cfg.Watch.PollInterval.Duration != 30*time.Second {
		t.Errorf("poll_interval = %v, want default 30s", cfg.Watch.PollInterval.Duration)
	}
	if cfg.Watch.IdleTimeout.Duration != 30*time.Minute {
		t.Errorf("idle_timeout = %v, want default 30m", cfg.Watch.IdleTimeout.Duration)
	}
	if cfg.Watch.MaxFixRounds != 5 {
		t.Errorf("max_fix_rounds = %d, want default 5", cfg.Watch.MaxFixRounds)
	}
}

func TestLoadTestCommand(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[test]
command = "make test"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Test.Command != "make test" {
		t.Errorf("test.command = %q, want \"make test\"", cfg.Test.Command)
	}
}

func TestTelegramDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.BotToken != "" {
		t.Errorf("telegram.bot_token should default to empty, got %q", cfg.Telegram.BotToken)
	}
	if cfg.Telegram.ChatID != "" {
		t.Errorf("telegram.chat_id should default to empty, got %q", cfg.Telegram.ChatID)
	}
}

func TestLoadTelegramConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[telegram]
bot_token = "123:ABC"
chat_id = "-100999"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.BotToken != "123:ABC" {
		t.Errorf("telegram.bot_token = %q, want %q", cfg.Telegram.BotToken, "123:ABC")
	}
	if cfg.Telegram.ChatID != "-100999" {
		t.Errorf("telegram.chat_id = %q, want %q", cfg.Telegram.ChatID, "-100999")
	}
}

func TestTelegramEnvVarOverride(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "claude-code"

[telegram]
bot_token = "file-token"
chat_id = "file-chat"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("FLEET_TELEGRAM_BOT_TOKEN", "env-token")
	t.Setenv("FLEET_TELEGRAM_CHAT_ID", "env-chat")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.BotToken != "env-token" {
		t.Errorf("telegram.bot_token = %q, want env override %q", cfg.Telegram.BotToken, "env-token")
	}
	if cfg.Telegram.ChatID != "env-chat" {
		t.Errorf("telegram.chat_id = %q, want env override %q", cfg.Telegram.ChatID, "env-chat")
	}
}

func TestTelegramEnvVarWithoutFile(t *testing.T) {
	dir := t.TempDir()

	t.Setenv("FLEET_TELEGRAM_BOT_TOKEN", "env-only-token")
	t.Setenv("FLEET_TELEGRAM_CHAT_ID", "env-only-chat")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.BotToken != "env-only-token" {
		t.Errorf("telegram.bot_token = %q, want %q", cfg.Telegram.BotToken, "env-only-token")
	}
	if cfg.Telegram.ChatID != "env-only-chat" {
		t.Errorf("telegram.chat_id = %q, want %q", cfg.Telegram.ChatID, "env-only-chat")
	}
}

func TestInvalidBackend(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "gpt-4"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
}

func TestLoadFullConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".fleet")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	data := `[agent]
backend = "cursor"

[test]
command = "go test ./..."

[watch]
poll_interval = "45s"
timeout = "3h"
idle_timeout = "20m"
max_fix_rounds = 8

[ci]
enabled = true
required_checks = ["ci/build"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Agent.Backend != "cursor" {
		t.Errorf("backend = %q", cfg.Agent.Backend)
	}
	if cfg.Test.Command != "go test ./..." {
		t.Errorf("test.command = %q", cfg.Test.Command)
	}
	if cfg.Watch.PollInterval.Duration != 45*time.Second {
		t.Errorf("watch.poll_interval = %v", cfg.Watch.PollInterval.Duration)
	}
	if cfg.Watch.MaxFixRounds != 8 {
		t.Errorf("watch.max_fix_rounds = %d", cfg.Watch.MaxFixRounds)
	}
	if !cfg.CI.Enabled {
		t.Error("ci.enabled should be true")
	}
	if len(cfg.CI.RequiredChecks) != 1 || cfg.CI.RequiredChecks[0] != "ci/build" {
		t.Errorf("ci.required_checks = %v", cfg.CI.RequiredChecks)
	}
}
