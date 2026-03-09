package watch

import (
	"testing"
)

func TestExitReasonString(t *testing.T) {
	tests := []struct {
		reason ExitReason
		want   string
	}{
		{ExitMerged, "merged"},
		{ExitClosed, "closed"},
		{ExitTimeout, "timeout"},
		{ExitIdle, "idle timeout"},
		{ExitMaxFix, "max fix rounds"},
		{ExitReadyToMerge, "ready to merge"},
		{ExitReason(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.reason.String(); got != tt.want {
			t.Errorf("ExitReason(%d).String() = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

func TestResultContainsReason(t *testing.T) {
	r := &Result{Reason: ExitMerged}
	if r.Reason != ExitMerged {
		t.Errorf("Result.Reason = %v, want ExitMerged", r.Reason)
	}
}
