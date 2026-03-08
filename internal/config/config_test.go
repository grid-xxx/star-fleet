package config

import (
	"os"
	"path/filepath"
	"testing"
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
	if cfg.Validate.MaxFixRounds != 3 {
		t.Errorf("validate.max_fix_rounds = %d, want 3", cfg.Validate.MaxFixRounds)
	}
	if cfg.Validate.MaxCycles != 2 {
		t.Errorf("validate.max_cycles = %d, want 2", cfg.Validate.MaxCycles)
	}

	// Watch defaults
	if cfg.Watch.PollInterval != "30s" {
		t.Errorf("watch.poll_interval = %q, want \"30s\"", cfg.Watch.PollInterval)
	}
	if cfg.Watch.Timeout != "2h" {
		t.Errorf("watch.timeout = %q, want \"2h\"", cfg.Watch.Timeout)
	}
	if cfg.Watch.IdleTimeout != "30m" {
		t.Errorf("watch.idle_timeout = %q, want \"30m\"", cfg.Watch.IdleTimeout)
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

	if cfg.Watch.PollInterval != "60s" {
		t.Errorf("poll_interval = %q, want \"60s\"", cfg.Watch.PollInterval)
	}
	if cfg.Watch.Timeout != "4h" {
		t.Errorf("timeout = %q, want \"4h\"", cfg.Watch.Timeout)
	}
	if cfg.Watch.IdleTimeout != "1h" {
		t.Errorf("idle_timeout = %q, want \"1h\"", cfg.Watch.IdleTimeout)
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

	if cfg.Watch.Timeout != "6h" {
		t.Errorf("timeout = %q, want \"6h\"", cfg.Watch.Timeout)
	}
	// Defaults for unspecified fields
	if cfg.Watch.PollInterval != "30s" {
		t.Errorf("poll_interval = %q, want default \"30s\"", cfg.Watch.PollInterval)
	}
	if cfg.Watch.IdleTimeout != "30m" {
		t.Errorf("idle_timeout = %q, want default \"30m\"", cfg.Watch.IdleTimeout)
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

[validate]
max_fix_rounds = 5
max_cycles = 3
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
	if cfg.Watch.PollInterval != "45s" {
		t.Errorf("watch.poll_interval = %q", cfg.Watch.PollInterval)
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
	if cfg.Validate.MaxFixRounds != 5 {
		t.Errorf("validate.max_fix_rounds = %d", cfg.Validate.MaxFixRounds)
	}
}
