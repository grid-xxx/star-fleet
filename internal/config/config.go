package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Agent    AgentConfig    `toml:"agent"`
	Test     TestConfig     `toml:"test"`
	Validate ValidateConfig `toml:"validate"`
	Watch    WatchConfig    `toml:"watch"`
	CI       CIConfig       `toml:"ci"`
}

type AgentConfig struct {
	Backend string `toml:"backend"`
}

type TestConfig struct {
	Command string `toml:"command"`
}

type ValidateConfig struct {
	MaxFixRounds int `toml:"max_fix_rounds"`
	MaxCycles    int `toml:"max_cycles"`
}

type WatchConfig struct {
	PollInterval string `toml:"poll_interval"`
	Timeout      string `toml:"timeout"`
	IdleTimeout  string `toml:"idle_timeout"`
	MaxFixRounds int    `toml:"max_fix_rounds"`
}

type CIConfig struct {
	Enabled        bool     `toml:"enabled"`
	RequiredChecks []string `toml:"required_checks"`
}

func defaults() Config {
	return Config{
		Agent: AgentConfig{Backend: "claude-code"},
		Validate: ValidateConfig{
			MaxFixRounds: 3,
			MaxCycles:    2,
		},
		Watch: WatchConfig{
			PollInterval: "30s",
			Timeout:      "2h",
			IdleTimeout:  "30m",
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
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	switch cfg.Agent.Backend {
	case "claude-code", "cursor", "mock":
	default:
		return nil, fmt.Errorf("unsupported agent backend %q (want \"claude-code\", \"cursor\", or \"mock\")", cfg.Agent.Backend)
	}

	return &cfg, nil
}
