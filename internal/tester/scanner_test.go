package tester

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanModules_NoModules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0", len(modules))
	}
}

func TestScanModules_SingleModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create module with tests.md + skills/usage.md
	modDir := filepath.Join(dir, "pkg", "auth")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Auth Tests\n- test login\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Auth Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("ScanModules() found %d modules, want 1", len(modules))
	}

	m := modules[0]
	if m.RelPath != filepath.Join("pkg", "auth") {
		t.Errorf("RelPath = %q, want %q", m.RelPath, filepath.Join("pkg", "auth"))
	}
	if m.TestsMD != filepath.Join(modDir, "tests.md") {
		t.Errorf("TestsMD = %q, want %q", m.TestsMD, filepath.Join(modDir, "tests.md"))
	}
	if m.UsageMD != filepath.Join(modDir, "skills", "usage.md") {
		t.Errorf("UsageMD = %q, want %q", m.UsageMD, filepath.Join(modDir, "skills", "usage.md"))
	}
}

func TestScanModules_MultipleModules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		modDir := filepath.Join(dir, name)
		mustMkdir(t, filepath.Join(modDir, "skills"))
		mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
		mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")
	}

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 3 {
		t.Fatalf("ScanModules() found %d modules, want 3", len(modules))
	}

	// Should be sorted alphabetically
	if modules[0].RelPath != "alpha" {
		t.Errorf("modules[0].RelPath = %q, want %q", modules[0].RelPath, "alpha")
	}
	if modules[1].RelPath != "beta" {
		t.Errorf("modules[1].RelPath = %q, want %q", modules[1].RelPath, "beta")
	}
	if modules[2].RelPath != "gamma" {
		t.Errorf("modules[2].RelPath = %q, want %q", modules[2].RelPath, "gamma")
	}
}

func TestScanModules_TestsMDOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// tests.md present but no skills/usage.md — should NOT be found
	modDir := filepath.Join(dir, "incomplete")
	mustMkdir(t, modDir)
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0 (missing skills/usage.md)", len(modules))
	}
}

func TestScanModules_UsageMDOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// skills/usage.md present but no tests.md — should NOT be found
	modDir := filepath.Join(dir, "incomplete")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0 (missing tests.md)", len(modules))
	}
}

func TestScanModules_SkipsHiddenDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Module inside .hidden directory — should be skipped
	modDir := filepath.Join(dir, ".hidden", "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0 (hidden dir)", len(modules))
	}
}

func TestScanModules_SkipsVendor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "vendor", "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0 (vendor dir)", len(modules))
	}
}

func TestScanModules_SkipsNodeModules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "node_modules", "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0 (node_modules dir)", len(modules))
	}
}

func TestScanModules_SkipsWorktrees(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "worktrees", "impl", "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0 (worktrees dir)", len(modules))
	}
}

func TestScanModules_NestedModules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Parent module
	parentDir := filepath.Join(dir, "services")
	mustMkdir(t, filepath.Join(parentDir, "skills"))
	mustWrite(t, filepath.Join(parentDir, "tests.md"), "# Parent Tests\n")
	mustWrite(t, filepath.Join(parentDir, "skills", "usage.md"), "# Parent Usage\n")

	// Child module
	childDir := filepath.Join(parentDir, "auth")
	mustMkdir(t, filepath.Join(childDir, "skills"))
	mustWrite(t, filepath.Join(childDir, "tests.md"), "# Child Tests\n")
	mustWrite(t, filepath.Join(childDir, "skills", "usage.md"), "# Child Usage\n")

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("ScanModules() found %d modules, want 2", len(modules))
	}
}

func TestScanModules_DetectsTestFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "pkg")
	mustMkdir(t, filepath.Join(modDir, "skills"))
	mustWrite(t, filepath.Join(modDir, "tests.md"), "# Tests\n")
	mustWrite(t, filepath.Join(modDir, "skills", "usage.md"), "# Usage\n")
	mustWrite(t, filepath.Join(modDir, "foo_test.go"), "package pkg\n")
	mustWrite(t, filepath.Join(modDir, "bar_test.go"), "package pkg\n")
	mustWrite(t, filepath.Join(modDir, "foo.go"), "package pkg\n") // not a test file

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("ScanModules() found %d modules, want 1", len(modules))
	}

	m := modules[0]
	if len(m.TestFiles) != 2 {
		t.Errorf("TestFiles count = %d, want 2", len(m.TestFiles))
	}
}

func TestIsTestFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"foo_test.go", true},
		{"bar_test.py", true},
		{"app.test.js", true},
		{"app.test.ts", true},
		{"app.test.tsx", true},
		{"app.spec.js", true},
		{"app.spec.ts", true},
		{"app.spec.tsx", true},
		{"foo.go", false},
		{"test.go", false},
		{"tests.md", false},
		{"foo_test.txt", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isTestFile(tt.name); got != tt.want {
				t.Errorf("isTestFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestScanModules_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	modules, err := ScanModules(dir)
	if err != nil {
		t.Fatalf("ScanModules() error = %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("ScanModules() found %d modules, want 0", len(modules))
	}
}

// helpers

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
