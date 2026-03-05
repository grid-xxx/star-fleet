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

func defaults() Config {
	return Config{
		Agent: AgentConfig{Backend: "claude-code"},
		Validate: ValidateConfig{
			MaxFixRounds: 3,
			MaxCycles:    2,
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

	if cfg.Agent.Backend != "claude-code" && cfg.Agent.Backend != "cursor" {
		return nil, fmt.Errorf("unsupported agent backend %q (want \"claude-code\" or \"cursor\")", cfg.Agent.Backend)
	}

	return &cfg, nil
}
