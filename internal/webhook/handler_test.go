package webhook

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type stubRunner struct {
	mu        sync.Mutex
	calls     []runCall
	testCalls []testCall
	blockCh   chan struct{} // if non-nil, Run/Test blocks until closed
	doneCh    chan struct{} // closed after each Run/Test completes
	err       error
	testErr   error
}

type runCall struct {
	Owner  string
	Repo   string
	Number int
}

type testCall struct {
	Owner    string
	Repo     string
	PRNumber int
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

func (r *stubRunner) Test(owner, repo string, prNumber int) error {
	if r.blockCh != nil {
		<-r.blockCh
	}
	r.mu.Lock()
	r.testCalls = append(r.testCalls, testCall{owner, repo, prNumber})
	r.mu.Unlock()
	if r.doneCh != nil {
		r.doneCh <- struct{}{}
	}
	return r.testErr
}

func (r *stubRunner) getCalls() []runCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]runCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *stubRunner) getTestCalls() []testCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]testCall, len(r.testCalls))
	copy(out, r.testCalls)
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

func TestTryRun_RejectWhenBusy_SameRepo(t *testing.T) {
	t.Parallel()

	runner := newBlockingStubRunner()
	h := NewHandler("fleet", "", runner)

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

	// Second event for SAME repo: should be rejected because busy
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

func TestTryRun_AllowDifferentReposInParallel(t *testing.T) {
	t.Parallel()

	runner := newBlockingStubRunner()
	h := NewHandler("fleet", "", runner)

	// Trigger run for repo-a
	p1 := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 1},
		Sender: payloadUser{Login: "alice", Type: "User"},
		Repo:   payloadRepo{Name: "repo-a"},
	}
	p1.Repo.Owner.Login = "org"
	body1, _ := json.Marshal(p1)

	status1, err := h.HandleEvent("issues", body1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status1 != "triggered" {
		t.Errorf("repo-a status = %q, want %q", status1, "triggered")
	}

	time.Sleep(20 * time.Millisecond)

	// Trigger run for repo-b — should also be accepted (different repo)
	p2 := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 2},
		Sender: payloadUser{Login: "bob", Type: "User"},
		Repo:   payloadRepo{Name: "repo-b"},
	}
	p2.Repo.Owner.Login = "org"
	body2, _ := json.Marshal(p2)

	status2, err := h.HandleEvent("issues", body2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status2 != "triggered" {
		t.Errorf("repo-b status = %q, want %q (different repos should run in parallel)", status2, "triggered")
	}

	// Unblock both runs
	close(runner.blockCh)
	runner.waitDone(t)
	runner.waitDone(t)

	calls := runner.getCalls()
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (both repos should run)", len(calls))
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

	_, err = h.HandleEvent("pull_request", []byte(`not-json`))
	if err == nil {
		t.Fatal("expected error for malformed pull_request payload")
	}
}

// --- pull_request event tests ---

func TestHandlePullRequest_Opened(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := pullRequestPayload{
		Action:      "opened",
		Number:      42,
		PullRequest: payloadPR{Number: 42, Title: "feat: new thing"},
		Sender:      payloadUser{Login: "alice", Type: "User"},
		Repo:        payloadRepo{Name: "star-fleet"},
	}
	p.Repo.Owner.Login = "nullne"

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("pull_request", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}

	runner.waitDone(t)

	calls := runner.getTestCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d test calls, want 1", len(calls))
	}
	if calls[0].Owner != "nullne" || calls[0].Repo != "star-fleet" || calls[0].PRNumber != 42 {
		t.Errorf("call = %+v, want {nullne star-fleet 42}", calls[0])
	}
}

func TestHandlePullRequest_Synchronize(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := pullRequestPayload{
		Action:      "synchronize",
		Number:      15,
		PullRequest: payloadPR{Number: 15, Title: "fix: something"},
		Sender:      payloadUser{Login: "bob", Type: "User"},
		Repo:        payloadRepo{Name: "repo"},
	}
	p.Repo.Owner.Login = "org"

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("pull_request", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}

	runner.waitDone(t)

	calls := runner.getTestCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d test calls, want 1", len(calls))
	}
	if calls[0].PRNumber != 15 {
		t.Errorf("PRNumber = %d, want 15", calls[0].PRNumber)
	}
}

func TestHandlePullRequest_ClosedAction(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := pullRequestPayload{
		Action: "closed",
		Number: 5,
		Sender: payloadUser{Login: "alice", Type: "User"},
	}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("pull_request", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "ignored" {
		t.Errorf("status = %q, want %q", status, "ignored")
	}
}

func TestHandlePullRequest_BotSender(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "my-bot[bot]", runner)

	p := pullRequestPayload{
		Action:      "opened",
		Number:      10,
		PullRequest: payloadPR{Number: 10},
		Sender:      payloadUser{Login: "my-bot[bot]", Type: "Bot"},
	}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("pull_request", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandlePullRequest_BotType(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	p := pullRequestPayload{
		Action:      "synchronize",
		Number:      3,
		PullRequest: payloadPR{Number: 3},
		Sender:      payloadUser{Login: "dependabot[bot]", Type: "Bot"},
	}
	body, _ := json.Marshal(p)

	status, err := h.HandleEvent("pull_request", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "skipped" {
		t.Errorf("status = %q, want %q", status, "skipped")
	}
}

func TestHandlePullRequest_FallbackToPRNumberField(t *testing.T) {
	t.Parallel()

	runner := newStubRunner()
	h := NewHandler("fleet", "", runner)

	// Number at top-level is 0, but pull_request.number is set
	p := pullRequestPayload{
		Action:      "opened",
		Number:      0,
		PullRequest: payloadPR{Number: 99},
		Sender:      payloadUser{Login: "alice", Type: "User"},
		Repo:        payloadRepo{Name: "r"},
	}
	p.Repo.Owner.Login = "o"

	body, _ := json.Marshal(p)
	status, err := h.HandleEvent("pull_request", body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "triggered" {
		t.Errorf("status = %q, want %q", status, "triggered")
	}

	runner.waitDone(t)

	calls := runner.getTestCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d test calls, want 1", len(calls))
	}
	if calls[0].PRNumber != 99 {
		t.Errorf("PRNumber = %d, want 99", calls[0].PRNumber)
	}
}

// --- pull_request concurrency tests ---

func TestTryTest_RejectWhenBusy_SameRepo(t *testing.T) {
	t.Parallel()

	runner := newBlockingStubRunner()
	h := NewHandler("fleet", "", runner)

	p1 := pullRequestPayload{
		Action:      "opened",
		Number:      1,
		PullRequest: payloadPR{Number: 1},
		Sender:      payloadUser{Login: "alice", Type: "User"},
		Repo:        payloadRepo{Name: "r"},
	}
	p1.Repo.Owner.Login = "o"
	body1, _ := json.Marshal(p1)

	status1, err := h.HandleEvent("pull_request", body1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status1 != "triggered" {
		t.Errorf("first status = %q, want %q", status1, "triggered")
	}

	time.Sleep(20 * time.Millisecond)

	// Second PR for SAME repo: should be rejected because test is busy
	p2 := pullRequestPayload{
		Action:      "opened",
		Number:      2,
		PullRequest: payloadPR{Number: 2},
		Sender:      payloadUser{Login: "bob", Type: "User"},
		Repo:        payloadRepo{Name: "r"},
	}
	p2.Repo.Owner.Login = "o"
	body2, _ := json.Marshal(p2)

	status2, err := h.HandleEvent("pull_request", body2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status2 != "busy" {
		t.Errorf("second status = %q, want %q", status2, "busy")
	}

	close(runner.blockCh)
	runner.waitDone(t)

	calls := runner.getTestCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d test calls, want 1 (second should have been rejected)", len(calls))
	}
}

func TestTryTest_AllowDifferentReposInParallel(t *testing.T) {
	t.Parallel()

	runner := newBlockingStubRunner()
	h := NewHandler("fleet", "", runner)

	p1 := pullRequestPayload{
		Action:      "opened",
		Number:      1,
		PullRequest: payloadPR{Number: 1},
		Sender:      payloadUser{Login: "alice", Type: "User"},
		Repo:        payloadRepo{Name: "repo-a"},
	}
	p1.Repo.Owner.Login = "org"
	body1, _ := json.Marshal(p1)

	status1, err := h.HandleEvent("pull_request", body1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status1 != "triggered" {
		t.Errorf("repo-a status = %q, want %q", status1, "triggered")
	}

	time.Sleep(20 * time.Millisecond)

	p2 := pullRequestPayload{
		Action:      "opened",
		Number:      2,
		PullRequest: payloadPR{Number: 2},
		Sender:      payloadUser{Login: "bob", Type: "User"},
		Repo:        payloadRepo{Name: "repo-b"},
	}
	p2.Repo.Owner.Login = "org"
	body2, _ := json.Marshal(p2)

	status2, err := h.HandleEvent("pull_request", body2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status2 != "triggered" {
		t.Errorf("repo-b status = %q, want %q (different repos should run in parallel)", status2, "triggered")
	}

	close(runner.blockCh)
	runner.waitDone(t)
	runner.waitDone(t)

	calls := runner.getTestCalls()
	if len(calls) != 2 {
		t.Fatalf("got %d test calls, want 2 (both repos should run)", len(calls))
	}
}

func TestTryTest_IndependentFromTryRun(t *testing.T) {
	t.Parallel()

	runner := newBlockingStubRunner()
	h := NewHandler("fleet", "", runner)

	// Start a Run on a repo
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
		t.Errorf("run status = %q, want %q", status1, "triggered")
	}

	time.Sleep(20 * time.Millisecond)

	// Start a Test on the SAME repo — should succeed because Run and Test
	// have independent busy tracking
	p2 := pullRequestPayload{
		Action:      "opened",
		Number:      5,
		PullRequest: payloadPR{Number: 5},
		Sender:      payloadUser{Login: "bob", Type: "User"},
		Repo:        payloadRepo{Name: "r"},
	}
	p2.Repo.Owner.Login = "o"
	body2, _ := json.Marshal(p2)

	status2, err := h.HandleEvent("pull_request", body2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status2 != "triggered" {
		t.Errorf("test status = %q, want %q (run and test should be independent)", status2, "triggered")
	}

	close(runner.blockCh)
	runner.waitDone(t)
	runner.waitDone(t)

	runCalls := runner.getCalls()
	testCalls := runner.getTestCalls()
	if len(runCalls) != 1 {
		t.Errorf("got %d run calls, want 1", len(runCalls))
	}
	if len(testCalls) != 1 {
		t.Errorf("got %d test calls, want 1", len(testCalls))
	}
}
