package watch

import (
	"testing"
)

func TestBuildEventContext_Comment(t *testing.T) {
	e := Event{Type: EventComment, Author: "alice", Detail: "Please fix the typo"}
	ctx := buildEventContext(e)
	if ctx == "" {
		t.Error("buildEventContext should return non-empty string")
	}
}

func TestBuildEventContext_Review(t *testing.T) {
	e := Event{Type: EventReview, Author: "bob", Detail: "Looks good but rename x"}
	ctx := buildEventContext(e)
	if ctx == "" {
		t.Error("buildEventContext should return non-empty string")
	}
}

func TestBuildEventContext_CIFail(t *testing.T) {
	e := Event{Type: EventCIFail, Detail: "build failed: missing import"}
	ctx := buildEventContext(e)
	if ctx == "" {
		t.Error("buildEventContext should return non-empty string")
	}
}

func TestBuildEventContext_CIPass(t *testing.T) {
	e := Event{Type: EventCIPass}
	ctx := buildEventContext(e)
	if ctx == "" {
		t.Error("buildEventContext should return non-empty string for CI pass")
	}
}

func TestBuildEventContext_Default(t *testing.T) {
	e := Event{Type: EventMerged, Summary: "PR merged"}
	ctx := buildEventContext(e)
	if ctx == "" {
		t.Error("buildEventContext should return non-empty string for default event type")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 80, "short"},
		{"line1\nline2", 80, "line1"},
		{"a very long string that exceeds the limit", 20, "a very long strin..."},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestHandleResultFields(t *testing.T) {
	r := HandleResult{Action: "fix", Message: "Fixed missing import"}
	if r.Action != "fix" {
		t.Errorf("Action = %q, want %q", r.Action, "fix")
	}
	if r.Message != "Fixed missing import" {
		t.Errorf("Message = %q", r.Message)
	}
}
