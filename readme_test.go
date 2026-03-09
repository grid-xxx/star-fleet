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
