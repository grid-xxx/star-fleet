package tester

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Module represents a discovered module directory that contains test
// specifications (tests.md) and skill documentation (skills/usage.md).
type Module struct {
	// Path is the absolute path to the module directory.
	Path string

	// RelPath is the path relative to the repo root.
	RelPath string

	// TestsMD is the absolute path to the tests.md file.
	TestsMD string

	// UsageMD is the absolute path to the skills/usage.md file.
	UsageMD string

	// TestFiles lists existing *_test.go (or similar) files already present.
	TestFiles []string
}

// ScanModules recursively walks the repo root looking for directories that
// contain both a "tests.md" and a "skills/usage.md" file. Each such directory
// is considered a testable module.
func ScanModules(repoRoot string) ([]Module, error) {
	var modules []Module

	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}

		// Skip hidden directories and common noise
		name := info.Name()
		if info.IsDir() && path != repoRoot && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "worktrees") {
			return filepath.SkipDir
		}

		// We're looking for tests.md files
		if info.IsDir() || name != "tests.md" {
			return nil
		}

		dir := filepath.Dir(path)
		usagePath := filepath.Join(dir, "skills", "usage.md")
		if _, err := os.Stat(usagePath); os.IsNotExist(err) {
			return nil // no skills/usage.md — not a testable module
		}

		relPath, err := filepath.Rel(repoRoot, dir)
		if err != nil {
			relPath = dir
		}

		testFiles := findTestFiles(dir)

		modules = append(modules, Module{
			Path:      dir,
			RelPath:   relPath,
			TestsMD:   path,
			UsageMD:   usagePath,
			TestFiles: testFiles,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(modules, func(i, j int) bool {
		return modules[i].RelPath < modules[j].RelPath
	})

	return modules, nil
}

// findTestFiles returns the paths of test files in dir (and its subdirectories).
// It looks for common test file patterns: *_test.go, *_test.py, *.test.js,
// *.test.ts, *.spec.js, *.spec.ts.
func findTestFiles(dir string) []string {
	var files []string

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if isTestFile(name) {
			files = append(files, path)
		}
		return nil
	})

	sort.Strings(files)
	return files
}

// isTestFile returns true if the filename matches common test file patterns.
func isTestFile(name string) bool {
	switch {
	case strings.HasSuffix(name, "_test.go"):
		return true
	case strings.HasSuffix(name, "_test.py"):
		return true
	case strings.HasSuffix(name, ".test.js"):
		return true
	case strings.HasSuffix(name, ".test.ts"):
		return true
	case strings.HasSuffix(name, ".test.tsx"):
		return true
	case strings.HasSuffix(name, ".spec.js"):
		return true
	case strings.HasSuffix(name, ".spec.ts"):
		return true
	case strings.HasSuffix(name, ".spec.tsx"):
		return true
	default:
		return false
	}
}
