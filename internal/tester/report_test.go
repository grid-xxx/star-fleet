package tester

import (
	"strings"
	"testing"
	"time"
)

func TestBuildReport_AllPassed(t *testing.T) {
	t.Parallel()
	results := []TestResult{
		{Module: Module{RelPath: "pkg/a"}, Passed: true, Duration: 1 * time.Second},
		{Module: Module{RelPath: "pkg/b"}, Passed: true, Duration: 2 * time.Second},
	}

	r := BuildReport(results, 3*time.Second)
	if r.TotalModules != 2 {
		t.Errorf("TotalModules = %d, want 2", r.TotalModules)
	}
	if r.PassedModules != 2 {
		t.Errorf("PassedModules = %d, want 2", r.PassedModules)
	}
	if r.FailedModules != 0 {
		t.Errorf("FailedModules = %d, want 0", r.FailedModules)
	}
	if r.ErrorModules != 0 {
		t.Errorf("ErrorModules = %d, want 0", r.ErrorModules)
	}
	if !r.AllPassed {
		t.Error("AllPassed = false, want true")
	}
}

func TestBuildReport_WithFailures(t *testing.T) {
	t.Parallel()
	results := []TestResult{
		{Module: Module{RelPath: "pkg/a"}, Passed: true, Duration: 1 * time.Second},
		{Module: Module{RelPath: "pkg/b"}, Passed: false, Duration: 2 * time.Second},
	}

	r := BuildReport(results, 3*time.Second)
	if r.PassedModules != 1 {
		t.Errorf("PassedModules = %d, want 1", r.PassedModules)
	}
	if r.FailedModules != 1 {
		t.Errorf("FailedModules = %d, want 1", r.FailedModules)
	}
	if r.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestBuildReport_WithErrors(t *testing.T) {
	t.Parallel()
	results := []TestResult{
		{Module: Module{RelPath: "pkg/a"}, Passed: true, Duration: 1 * time.Second},
		{Module: Module{RelPath: "pkg/b"}, Passed: false, Error: errTest("not found"), Duration: 0},
	}

	r := BuildReport(results, 1*time.Second)
	if r.ErrorModules != 1 {
		t.Errorf("ErrorModules = %d, want 1", r.ErrorModules)
	}
	if r.FailedModules != 0 {
		t.Errorf("FailedModules = %d, want 0 (errors are distinct from failures)", r.FailedModules)
	}
	if r.AllPassed {
		t.Error("AllPassed = true, want false")
	}
}

func TestBuildReport_Empty(t *testing.T) {
	t.Parallel()
	r := BuildReport(nil, 0)
	if r.TotalModules != 0 {
		t.Errorf("TotalModules = %d, want 0", r.TotalModules)
	}
	if !r.AllPassed {
		t.Error("AllPassed = false, want true (no failures = all passed)")
	}
}

func TestBuildReport_Duration(t *testing.T) {
	t.Parallel()
	dur := 5 * time.Second
	r := BuildReport(nil, dur)
	if r.TotalDuration != dur {
		t.Errorf("TotalDuration = %v, want %v", r.TotalDuration, dur)
	}
}

// --- FormatMarkdown tests ---

func TestFormatMarkdown_AllPassed(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/auth"}, Passed: true, Duration: 500 * time.Millisecond},
		},
		TotalModules:  1,
		PassedModules: 1,
		AllPassed:     true,
		TotalDuration: 500 * time.Millisecond,
	}

	md := r.FormatMarkdown()
	if !strings.Contains(md, "All Passed") {
		t.Error("FormatMarkdown() should contain 'All Passed'")
	}
	if !strings.Contains(md, "✅") {
		t.Error("FormatMarkdown() should contain success emoji")
	}
	if !strings.Contains(md, "pkg/auth") {
		t.Error("FormatMarkdown() should contain module path")
	}
	if !strings.Contains(md, "fleet test") {
		t.Error("FormatMarkdown() should contain footer")
	}
}

func TestFormatMarkdown_WithFailures(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/a"}, Passed: true, Duration: 1 * time.Second},
			{Module: Module{RelPath: "pkg/b"}, Passed: false, Output: "FAIL\n", Duration: 2 * time.Second},
		},
		TotalModules:  2,
		PassedModules: 1,
		FailedModules: 1,
		AllPassed:     false,
		TotalDuration: 3 * time.Second,
	}

	md := r.FormatMarkdown()
	if !strings.Contains(md, "Failures Detected") {
		t.Error("FormatMarkdown() should contain 'Failures Detected'")
	}
	if !strings.Contains(md, "❌") {
		t.Error("FormatMarkdown() should contain failure emoji")
	}
	if !strings.Contains(md, "FAIL") {
		t.Error("FormatMarkdown() should contain test output")
	}
}

func TestFormatMarkdown_WithError(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/a"}, Passed: false, Error: errTest("cmd not found"), Duration: 0},
		},
		TotalModules: 1,
		ErrorModules: 1,
		TotalDuration: 0,
	}

	md := r.FormatMarkdown()
	if !strings.Contains(md, "cmd not found") {
		t.Error("FormatMarkdown() should contain error message")
	}
	if !strings.Contains(md, "⚠️") {
		t.Error("FormatMarkdown() should contain warning emoji for errors")
	}
}

func TestFormatMarkdown_AgentGenerated(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/a"}, Passed: true, AgentGenerated: true, Duration: 1 * time.Second},
		},
		TotalModules:  1,
		PassedModules: 1,
		AllPassed:     true,
		TotalDuration: 1 * time.Second,
	}

	md := r.FormatMarkdown()
	if !strings.Contains(md, "🤖") {
		t.Error("FormatMarkdown() should contain robot emoji for agent-generated tests")
	}
}

func TestFormatMarkdown_Table(t *testing.T) {
	t.Parallel()
	r := &Report{
		TotalModules:  5,
		PassedModules: 3,
		FailedModules: 1,
		ErrorModules:  1,
		AllPassed:     false,
		TotalDuration: 10 * time.Second,
	}

	md := r.FormatMarkdown()
	if !strings.Contains(md, "| Modules tested | 5 |") {
		t.Error("FormatMarkdown() should contain summary table")
	}
	if !strings.Contains(md, "| Passed | 3 |") {
		t.Error("FormatMarkdown() should show passed count")
	}
	if !strings.Contains(md, "| Failed | 1 |") {
		t.Error("FormatMarkdown() should show failed count")
	}
}

// --- FormatTerminal tests ---

func TestFormatTerminal_AllPassed(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/auth"}, Passed: true, Duration: 500 * time.Millisecond},
			{Module: Module{RelPath: "pkg/user"}, Passed: true, Duration: 300 * time.Millisecond},
		},
		TotalModules:  2,
		PassedModules: 2,
		AllPassed:     true,
		TotalDuration: 800 * time.Millisecond,
	}

	out := r.FormatTerminal()
	if !strings.Contains(out, "✓") {
		t.Error("FormatTerminal() should contain success mark")
	}
	if !strings.Contains(out, "All 2 modules passed") {
		t.Error("FormatTerminal() should show all-passed message")
	}
}

func TestFormatTerminal_WithFailures(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/auth"}, Passed: true, Duration: 500 * time.Millisecond},
			{Module: Module{RelPath: "pkg/user"}, Passed: false, Duration: 300 * time.Millisecond},
		},
		TotalModules:  2,
		PassedModules: 1,
		FailedModules: 1,
		AllPassed:     false,
		TotalDuration: 800 * time.Millisecond,
	}

	out := r.FormatTerminal()
	if !strings.Contains(out, "✗") {
		t.Error("FormatTerminal() should contain failure mark")
	}
	if !strings.Contains(out, "1/2 passed") {
		t.Error("FormatTerminal() should show pass/fail counts")
	}
}

func TestFormatTerminal_WithErrors(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/a"}, Passed: false, Error: errTest("fail"), Duration: 0},
		},
		TotalModules: 1,
		ErrorModules: 1,
		TotalDuration: 0,
	}

	out := r.FormatTerminal()
	if !strings.Contains(out, "⚠") {
		t.Error("FormatTerminal() should contain warning mark for errors")
	}
}

func TestFormatTerminal_AgentGenerated(t *testing.T) {
	t.Parallel()
	r := &Report{
		Modules: []TestResult{
			{Module: Module{RelPath: "pkg/a"}, Passed: true, AgentGenerated: true, Duration: 1 * time.Second},
		},
		TotalModules:  1,
		PassedModules: 1,
		AllPassed:     true,
		TotalDuration: 1 * time.Second,
	}

	out := r.FormatTerminal()
	if !strings.Contains(out, "agent-generated") {
		t.Error("FormatTerminal() should indicate agent-generated tests")
	}
}

// helper error type
type testError string

func errTest(msg string) error {
	return testError(msg)
}

func (e testError) Error() string {
	return string(e)
}
