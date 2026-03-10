package webhook

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type stubRunner struct {
	mu      sync.Mutex
	calls   []runCall
	blockCh chan struct{} // if non-nil, Run blocks until closed
	doneCh  chan struct{} // closed after each Run completes
	err     error
}

type runCall struct {
	Owner  string
	Repo   string
	Number int
}

func newStubRunner() *stubRunner {
	return &stubRunner{
		doneCh: make(chan struct{}, 10),
	}
}

func newBlockingStubRunner() *stubRunner {
	return &stubRunner{
		blockCh: make(chan struct{}),
		doneCh:  make(chan struct{}, 10),
	}
}

func (r *stubRunner) Run(owner, repo string, number int) error {
	if r.blockCh != nil {
		<-r.blockCh
	}
	r.mu.Lock()
	r.calls = append(r.calls, runCall{owner, repo, number})
	r.mu.Unlock()
	if r.doneCh != nil {
		r.doneCh <- struct{}{}
	}
	return r.err
}

func (r *stubRunner) getCalls() []runCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]runCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *stubRunner) waitDone(t *testing.T) {
	t.Helper()
	select {
	case <-r.doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for run to complete")
	}
}

// --- issues event tests ---

func TestHandleIssues_Labeled(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 7, Title: "test issue"},
		Sender: payloadUser{Login: "alice", Type: "User"},
		Repo:   payloadRepo{Name: "star-fleet"},
	}
	p.Repo.Owner.Login = "nullne"

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}

	runner.waitDone(t)

	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Owner != "nullne" || calls[0].Repo != "star-fleet" || calls[0].Number != 7 {
		t.Errorf("call = %+v, want {nullne star-fleet 7}", calls[0])
	}
}

func TestHandleIssues_WrongLabel(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "bug"},
		Issue:  payloadIssue{Number: 1},
		Sender: payloadUser{Login: "alice", Type: "User"},
	}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandleIssues_WrongAction(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issuesPayload{Action: "opened"}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "ignored" {
		t.Errorf("status = %q, want %q", status, "ignored")
	}
}

func TestHandleIssues_BotSender(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "my-bot[bot]", runner)

	p := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 1},
		Sender: payloadUser{Login: "my-bot[bot]", Type: "Bot"},
	}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandleIssues_BotType(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 1},
		Sender: payloadUser{Login: "some-app[bot]", Type: "Bot"},
	}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandleIssues_LabelCaseInsensitive(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "Fleet"},
		Issue:  payloadIssue{Number: 5},
		Sender: payloadUser{Login: "alice", Type: "User"},
		Repo:   payloadRepo{Name: "r"},
	}
	p.Repo.Owner.Login = "o"
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}
}

// --- issue_comment event tests ---

func TestHandleIssueComment_FleetRun(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issueCommentPayload{
		Action: "created",
		Issue:  payloadIssue{Number: 10},
		Repo:   payloadRepo{Name: "star-fleet"},
	}
	p.Comment.Body = "/fleet run"
	p.Comment.User = payloadUser{Login: "alice", Type: "User"}
	p.Repo.Owner.Login = "nullne"

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issue_comment", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}

	runner.waitDone(t)
	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Number != 10 {
		t.Errorf("number = %d, want 10", calls[0].Number)
	}
}

func TestHandleIssueComment_FleetRunWithArgs(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issueCommentPayload{
		Action: "created",
		Issue:  payloadIssue{Number: 3},
		Repo:   payloadRepo{Name: "r"},
	}
	p.Comment.Body = "/fleet run --restart"
	p.Comment.User = payloadUser{Login: "alice", Type: "User"}
	p.Repo.Owner.Login = "o"

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issue_comment", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}
}

func TestHandleIssueComment_FleetRunSubstring(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	// "/fleet running" should NOT trigger
	p := issueCommentPayload{Action: "created"}
	p.Comment.Body = "/fleet running tests"
	p.Comment.User = payloadUser{Login: "alice", Type: "User"}

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issue_comment", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandleIssueComment_NoCommand(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issueCommentPayload{Action: "created"}
	p.Comment.Body = "this is a regular comment"
	p.Comment.User = payloadUser{Login: "alice", Type: "User"}

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issue_comment", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandleIssueComment_EditedAction(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issueCommentPayload{Action: "edited"}
	p.Comment.Body = "/fleet run"
	p.Comment.User = payloadUser{Login: "alice", Type: "User"}

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issue_comment", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "ignored" {
		t.Errorf("status = %q, want %q", status, "ignored")
	}
}

func TestHandleIssueComment_BotComment(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "fleet-bot[bot]", runner)

	p := issueCommentPayload{
		Action: "created",
		Issue:  payloadIssue{Number: 1},
	}
	p.Comment.Body = "/fleet run"
	p.Comment.User = payloadUser{Login: "fleet-bot[bot]", Type: "Bot"}

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("issue_comment", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

// --- concurrency tests ---

func TestTryRun_RejectWhenBusy(t *testing.T) {
	t.Parallel()

	runner := newBlockingStubRunner()
	h := NewHandler("fleet", "", runner)

	// First event: triggers a run that blocks
	p1 := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 1},
		Sender: payloadUser{Login: "alice", Type: "User"},
		Repo:   payloadRepo{Name: "r"},
	}
	p1.Repo.Owner.Login = "o"
	body1, _ := json.Marshal(p1)

	status1, err := h.HandleEvent("issues", body1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status1 != "triggered" {
		t.Errorf("first status = %q, want %q", status1, "triggered")
	}

	// Give goroutine time to start and acquire the busy flag
	time.Sleep(20 * time.Millisecond)

	// Second event: should be rejected because busy
	p2 := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 2},
		Sender: payloadUser{Login: "bob", Type: "User"},
		Repo:   payloadRepo{Name: "r"},
	}
	p2.Repo.Owner.Login = "o"
	body2, _ := json.Marshal(p2)

	status2, err := h.HandleEvent("issues", body2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status2 != "busy" {
		t.Errorf("second status = %q, want %q", status2, "busy")
	}

	// Unblock the first run
	close(runner.blockCh)
	runner.waitDone(t)

	calls := runner.getCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1 (second should have been rejected)", len(calls))
	}
}

func TestTryRun_AllowsAfterCompletion(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 1},
		Sender: payloadUser{Login: "alice", Type: "User"},
		Repo:   payloadRepo{Name: "r"},
	}
	p.Repo.Owner.Login = "o"
	body, _ := json.Marshal(p)

	status1, err := h.HandleEvent("issues", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status1 != "triggered" {
		t.Errorf("status = %q, want %q", status1, "triggered")
	}

	// Wait for first run to complete
	runner.waitDone(t)

	// Second run should also be accepted
	p.Issue.Number = 2
	body2, _ := json.Marshal(p)
	status2, err := h.HandleEvent("issues", body2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status2 != "triggered" {
		t.Errorf("second status = %q, want %q", status2, "triggered")
	}
}

// --- unknown event type ---

func TestHandleEvent_UnknownType(t *testing.T) {
	t.Parallel()

	h := NewHandler("fleet", "", newStubRunner())
	status, err := h.HandleEvent("push", []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "ignored" {
		t.Errorf("status = %q, want %q", status, "ignored")
	}
}

// --- malformed payload ---

func TestHandleEvent_MalformedPayload(t *testing.T) {
	t.Parallel()

	h := NewHandler("fleet", "", newStubRunner())

	_, err := h.HandleEvent("issues", []byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}

	_, err = h.HandleEvent("issue_comment", []byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for malformed payload")
	}
}
