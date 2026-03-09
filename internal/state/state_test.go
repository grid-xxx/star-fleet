package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPhaseOrdering(t *testing.T) {
	phases := []Phase{PhaseNew, PhaseIntake, PhaseImplement, PhasePR, PhaseReview, PhaseWatch, PhaseDone}
	for i := 1; i < len(phases); i++ {
		if !phases[i].AtLeast(phases[i-1]) {
			t.Errorf("%s should be AtLeast %s", phases[i], phases[i-1])
		}
		if phases[i-1].AtLeast(phases[i]) {
			t.Errorf("%s should not be AtLeast %s", phases[i-1], phases[i])
		}
	}
	for _, p := range phases {
		if !p.AtLeast(p) {
			t.Errorf("%s should be AtLeast itself", p)
		}
	}
}

func TestNewAndSaveLoad(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "owner", "repo", 42)

	if s.Phase != PhaseNew {
		t.Errorf("phase = %s, want new", s.Phase)
	}
	if s.Branch != "fleet/42" {
		t.Errorf("branch = %s", s.Branch)
	}

	s.BaseBranch = "main"
	s.IssueTitle = "test issue"
	if err := s.Advance(PhaseIntake); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, 42)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Phase != PhaseIntake {
		t.Errorf("loaded phase = %s, want intake", loaded.Phase)
	}
	if loaded.BaseBranch != "main" {
		t.Errorf("loaded base branch = %s", loaded.BaseBranch)
	}
	if loaded.IssueTitle != "test issue" {
		t.Errorf("loaded issue title = %s", loaded.IssueTitle)
	}
}

func TestLoadMissing(t *testing.T) {
	s, err := Load(t.TempDir(), 99)
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Error("Load should return nil for missing state")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "o", "r", 1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	if err := s.Remove(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Error("state should be gone after Remove")
	}
}

func TestGitignoreCreated(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "o", "r", 1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	gi := filepath.Join(dir, ".fleet", "runs", ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("gitignore not created: %v", err)
	}
	if string(data) != "*\n!.gitignore\n" {
		t.Errorf("gitignore content = %q", data)
	}
}

func TestAdvancePersists(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "o", "r", 7)

	s.AgentDone = true
	if err := s.Advance(PhaseImplement); err != nil {
		t.Fatal(err)
	}

	loaded, _ := Load(dir, 7)
	if loaded.Phase != PhaseImplement {
		t.Errorf("phase = %s", loaded.Phase)
	}
	if !loaded.AgentDone {
		t.Error("AgentDone should be true")
	}
}
