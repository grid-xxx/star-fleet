package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Agent AgentConfig `toml:"agent"`
	Watch WatchConfig `toml:"watch"`
	CI    CIConfig    `toml:"ci"`
	Test  TestConfig  `toml:"test"`
}

type AgentConfig struct {
	Backend string `toml:"backend"`
}

type WatchConfig struct {
	PollInterval Duration `toml:"poll_interval"`
	Timeout      Duration `toml:"timeout"`
	IdleTimeout  Duration `toml:"idle_timeout"`
	MaxFixRounds int      `toml:"max_fix_rounds"`
}

type CIConfig struct {
	Enabled        bool     `toml:"enabled"`
	RequiredChecks []string `toml:"required_checks"`
}

type TestConfig struct {
	Command string `toml:"command"`
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
		Watch: WatchConfig{
			PollInterval: Duration{30 * time.Second},
			Timeout:      Duration{2 * time.Hour},
			IdleTimeout:  Duration{30 * time.Minute},
			MaxFixRounds: 5,
		},
		CI: CIConfig{
			Enabled: true,
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
