package watch

import (
	"testing"
)

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		eventType EventType
		wantVal   EventType
	}{
		{EventComment, EventComment},
		{EventReview, EventReview},
		{EventCIFail, EventCIFail},
		{EventCIPass, EventCIPass},
		{EventMerged, EventMerged},
		{EventClosed, EventClosed},
	}
	for _, tt := range tests {
		if tt.eventType != tt.wantVal {
			t.Errorf("EventType constant mismatch: got %d, want %d", tt.eventType, tt.wantVal)
		}
	}
}

func TestEventTypeOrdering(t *testing.T) {
	if EventComment != 0 {
		t.Errorf("EventComment = %d, want 0 (first iota)", EventComment)
	}
	if EventClosed <= EventComment {
		t.Errorf("EventClosed (%d) should be greater than EventComment (%d)", EventClosed, EventComment)
	}
}

func TestEventFields(t *testing.T) {
	e := Event{
		Type:    EventComment,
		ID:      "comment-123",
		Summary: "Comment by @reviewer",
		Detail:  "Please fix the variable naming",
		Author:  "reviewer",
	}

	if e.ID != "comment-123" {
		t.Errorf("ID = %q", e.ID)
	}
	if e.Type != EventComment {
		t.Errorf("Type = %d, want EventComment", e.Type)
	}
	if e.Summary != "Comment by @reviewer" {
		t.Errorf("Summary = %q", e.Summary)
	}
	if e.Detail != "Please fix the variable naming" {
		t.Errorf("Detail = %q", e.Detail)
	}
	if e.Author != "reviewer" {
		t.Errorf("Author = %q", e.Author)
	}
}

func TestIsOwnComment(t *testing.T) {
	tests := []struct {
		body string
		want bool
	}{
		{"## 🚀 Star Fleet — PR Ready\n\nSome body", true},
		{"## ⚠️ Star Fleet — Watch Timeout\n\nTimed out", true},
		{"## 🔍 Star Fleet — Review\n\nDetails", true},
		{"## ✅ Star Fleet — CI Passed\n\nAll checks passed.", true},
		{"LGTM", false},
		{"Looks good to me!", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isOwnComment(tt.body); got != tt.want {
			t.Errorf("isOwnComment(%q) = %v, want %v", tt.body[:min(len(tt.body), 30)], got, tt.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
