package starfleet_test

import (
	"os"
	"strings"
	"testing"
)

func TestReadmeBadges(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		t.Fatal("README.md has fewer than 3 lines")
	}

	if strings.TrimSpace(lines[0]) != "# Star Fleet" {
		t.Errorf("first line = %q, want \"# Star Fleet\"", strings.TrimSpace(lines[0]))
	}

	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("second line should be blank, got %q", lines[1])
	}

	badgeLine := lines[2]

	ciBadge := "[![test](https://github.com/grid-xxx/star-fleet/actions/workflows/test.yml/badge.svg)](https://github.com/grid-xxx/star-fleet/actions/workflows/test.yml)"
	if !strings.Contains(badgeLine, ciBadge) {
		t.Errorf("badge line missing CI badge.\ngot:  %s\nwant substring: %s", badgeLine, ciBadge)
	}

	goBadge := "[![Go](https://img.shields.io/github/go-mod/go-version/grid-xxx/star-fleet)](https://go.dev/)"
	if !strings.Contains(badgeLine, goBadge) {
		t.Errorf("badge line missing Go version badge.\ngot:  %s\nwant substring: %s", badgeLine, goBadge)
	}

	ciIdx := strings.Index(badgeLine, ciBadge)
	goIdx := strings.Index(badgeLine, goBadge)
	if ciIdx > goIdx {
		t.Error("CI badge should appear before Go version badge")
	}

	sep := badgeLine[ciIdx+len(ciBadge) : goIdx]
	if sep != " " {
		t.Errorf("badges should be separated by a single space, got %q", sep)
	}
}

func TestReadmeSingleAgentArchitecture(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	t.Run("describes single agent pipeline", func(t *testing.T) {
		required := []string{
			"single code agent",
			"watch loop",
			"auto-merge",
		}
		for _, phrase := range required {
			if !strings.Contains(strings.ToLower(content), strings.ToLower(phrase)) {
				t.Errorf("README should mention %q", phrase)
			}
		}
	})

	t.Run("no references to old dual-agent architecture", func(t *testing.T) {
		forbidden := []string{
			"Two agents run in parallel",
			"Dev writes implementation, Test writes tests",
			"cross-validat",
			"internal/review/",
			"internal/validate/",
			"worktrees/dev",
			"worktrees/test",
			"fleet/dev/",
			"fleet/test/",
			"Dev Agent",
			"Test Agent",
			"max_cycles",
		}
		for _, phrase := range forbidden {
			if strings.Contains(content, phrase) {
				t.Errorf("README should not contain old architecture reference %q", phrase)
			}
		}
	})
}

func TestReadmeFlowDiagram(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	steps := []string{
		"fleet run <issue>",
		"Fetch & validate Issue spec",
		"Create worktree",
		"Code Agent implements feature + writes tests",
		"Push branch",
		"Create PR",
		"Watch loop",
	}
	for _, step := range steps {
		if !strings.Contains(content, step) {
			t.Errorf("flow diagram should contain step %q", step)
		}
	}
}

func TestReadmeDirectoryStructure(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	dirs := []string{
		"agent/",
		"cli/",
		"config/",
		"gh/",
		"git/",
		"notify/",
		"orchestrator/",
		"retry/",
		"state/",
		"ui/",
		"watch/",
	}
	for _, dir := range dirs {
		if !strings.Contains(content, dir) {
			t.Errorf("architecture listing should contain %q", dir)
		}
	}

	if !strings.Contains(content, "internal/") {
		t.Error("architecture listing should contain the internal/ parent directory")
	}
}

func TestReadmeConfigOptions(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	sections := []string{
		"[agent]",
		"[watch]",
		"[ci]",
		"[test]",
		"[telegram]",
	}
	for _, section := range sections {
		if !strings.Contains(content, section) {
			t.Errorf("config documentation should contain section %q", section)
		}
	}

	keys := []string{
		"backend",
		"auto_merge",
		"poll_interval",
		"idle_timeout",
		"timeout",
		"max_fix_rounds",
		"bot_token",
		"chat_id",
	}
	for _, key := range keys {
		if !strings.Contains(content, key) {
			t.Errorf("config documentation should document key %q", key)
		}
	}
}

func TestReadmeCLIUsage(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	flags := []string{
		"--auto-merge",
		"--restart",
		"--no-watch",
	}
	for _, flag := range flags {
		if !strings.Contains(content, flag) {
			t.Errorf("CLI usage should document flag %q", flag)
		}
	}
}

func TestReadmeGitHubTrail(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	stages := []string{
		"Intake",
		"Spec gap",
		"PR created",
		"Watch",
		"Done",
	}
	for _, stage := range stages {
		if !strings.Contains(content, stage) {
			t.Errorf("GitHub Trail table should contain stage %q", stage)
		}
	}
}

func TestReadmeRequiredSections(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	content := string(data)

	sections := []string{
		"## Prerequisites",
		"## Install",
		"## Usage",
		"## How It Works",
		"## Configuration",
		"## GitHub Trail",
		"## Architecture",
		"## License",
	}
	for _, section := range sections {
		if !strings.Contains(content, section) {
			t.Errorf("README should contain section %q", section)
		}
	}
}
