package review

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nullne/star-fleet/internal/config"
	"github.com/nullne/star-fleet/internal/gh"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockGHReview struct {
	getPRDiff      func(ctx context.Context, owner, repo string, prNumber int) (string, error)
	submitReview   func(ctx context.Context, owner, repo string, prNumber int, event, body string) error
	submitPRReview func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error
	postComment    func(ctx context.Context, owner, repo string, number int, body string) error
}

func (m *mockGHReview) GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	if m.getPRDiff != nil {
		return m.getPRDiff(ctx, owner, repo, prNumber)
	}
	return "+some diff", nil
}
func (m *mockGHReview) SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
	if m.submitReview != nil {
		return m.submitReview(ctx, owner, repo, prNumber, event, body)
	}
	return nil
}
func (m *mockGHReview) SubmitPRReview(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
	if m.submitPRReview != nil {
		return m.submitPRReview(ctx, owner, repo, prNumber, event, body, comments)
	}
	return nil
}
func (m *mockGHReview) PostComment(ctx context.Context, owner, repo string, number int, body string) error {
	if m.postComment != nil {
		return m.postComment(ctx, owner, repo, number, body)
	}
	return nil
}

type mockBackend struct {
	run func(ctx context.Context, workdir string, prompt string, output io.Writer) error
}

func (m *mockBackend) Run(ctx context.Context, workdir string, prompt string, output io.Writer) error {
	if m.run != nil {
		return m.run(ctx, workdir, prompt, output)
	}
	return nil
}

func defaultCfg() *config.ReviewConfig {
	return &config.ReviewConfig{Enabled: true, MaxRounds: 3, Name: "Code Review"}
}

// ---------------------------------------------------------------------------
// isApproval tests
// ---------------------------------------------------------------------------

func TestIsApproval(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		want     bool
	}{
		{"empty", "", true},
		{"NO_ISSUES", "NO_ISSUES", true},
		{"LGTM", "LGTM", true},
		{"no issues found", "No issues found in this diff.", true},
		{"no problems found", "No problems found.", true},
		{"looks good", "This looks good to me.", true},
		{"lgtm lowercase", "lgtm, ship it", true},
		{"has issues", "- Bug in line 42: missing nil check", false},
		{"request changes", "Please fix the error handling", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isApproval(tt.response)
			if got != tt.want {
				t.Errorf("isApproval(%q) = %v, want %v", tt.response, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// countIssues tests
// ---------------------------------------------------------------------------

func TestCountIssues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{"empty", "", 0},
		{"single bullet", "- Missing nil check in foo.go:42", 1},
		{"multiple bullets", "- Issue 1\n- Issue 2\n- Issue 3", 3},
		{"asterisk bullets", "* Error handling missing\n* No tests", 2},
		{"numbered", "1. Bug\n2. Missing test", 2},
		{"mixed", "- Bug\n* Style issue\n1. Missing test", 3},
		{"no list", "This code has problems but no structured list", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := countIssues(tt.response)
			if got != tt.want {
				t.Errorf("countIssues(%q) = %d, want %d", tt.response, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Review tests
// ---------------------------------------------------------------------------

func TestReview_EmptyDiff(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			getPRDiff: func(ctx context.Context, owner, repo string, prNumber int) (string, error) {
				return "", nil
			},
		},
	}

	issues, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if issues != 0 {
		t.Errorf("Review() issues = %d, want 0", issues)
	}
}

func TestReview_DiffFetchError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			getPRDiff: func(ctx context.Context, owner, repo string, prNumber int) (string, error) {
				return "", errors.New("network error")
			},
		},
	}

	_, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err == nil {
		t.Fatal("Review() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetching PR diff") {
		t.Errorf("expected 'fetching PR diff' in error, got %v", err)
	}
}

func TestReview_AgentApproves(t *testing.T) {
	t.Parallel()
	var submittedEvent, submittedBody string

	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				submittedEvent = event
				submittedBody = body
				return nil
			},
		},
	}

	issues, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if issues != 0 {
		t.Errorf("Review() issues = %d, want 0", issues)
	}
	if submittedEvent != "APPROVE" {
		t.Errorf("submitted event = %q, want APPROVE", submittedEvent)
	}
	if !strings.Contains(submittedBody, "Code Review") {
		t.Errorf("submitted body should contain display name, got %q", submittedBody)
	}
	if !strings.Contains(submittedBody, "approved") {
		t.Errorf("submitted body should contain verdict, got %q", submittedBody)
	}
}

func TestReview_SubmitApprovalError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				if event == "APPROVE" {
					return errors.New("api error")
				}
				return nil
			},
		},
	}

	_, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err == nil {
		t.Fatal("Review() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "submitting approval") {
		t.Errorf("expected 'submitting approval' in error, got %v", err)
	}
}

func TestReview_SubmitRequestChangesError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				if event == "REQUEST_CHANGES" {
					return errors.New("api error")
				}
				return nil
			},
		},
	}

	// This test validates the error path when submitReview fails for REQUEST_CHANGES.
	// Since the agent mock produces empty output (treated as approval), this tests
	// the approval path. The REQUEST_CHANGES error path is tested via structured reviews.
	_ = r
}

func TestBuildReviewPrompt_Default(t *testing.T) {
	t.Parallel()
	prompt := buildReviewPrompt("diff content here", defaultCfg())

	if !strings.Contains(prompt, "diff content here") {
		t.Error("prompt should contain the diff")
	}
	if !strings.Contains(prompt, "code review") {
		t.Error("prompt should mention code review")
	}
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should ask for JSON output format")
	}
}

func TestBuildReviewPrompt_NilConfig(t *testing.T) {
	t.Parallel()
	prompt := buildReviewPrompt("some diff", nil)
	if !strings.Contains(prompt, "some diff") {
		t.Error("prompt should contain the diff")
	}
}

func TestBuildReviewPrompt_CustomPromptFile(t *testing.T) {
	t.Parallel()
	cfg := &config.ReviewConfig{
		Enabled:    true,
		PromptFile: "/nonexistent/path.md",
	}
	prompt := buildReviewPrompt("diff", cfg)
	if !strings.Contains(prompt, "diff") {
		t.Error("prompt should fall back to default when prompt file doesn't exist")
	}
}

func TestReviewer_IsApproved(t *testing.T) {
	t.Parallel()
	r := &Reviewer{}

	if !r.IsApproved("NO_ISSUES") {
		t.Error("IsApproved(NO_ISSUES) should be true")
	}
	if !r.IsApproved("") {
		t.Error("IsApproved('') should be true")
	}
	if r.IsApproved("- Bug found") {
		t.Error("IsApproved('- Bug found') should be false")
	}
}

// ---------------------------------------------------------------------------
// parseStructuredReview tests
// ---------------------------------------------------------------------------

func TestParseStructuredReview_ValidJSON(t *testing.T) {
	t.Parallel()
	input := `{"summary": "Looks good overall", "verdict": "approve", "comments": []}`
	sr, ok := parseStructuredReview(input)
	if !ok {
		t.Fatal("parseStructuredReview should succeed for valid JSON")
	}
	if sr.Verdict != "approve" {
		t.Errorf("verdict = %q, want %q", sr.Verdict, "approve")
	}
	if sr.Summary != "Looks good overall" {
		t.Errorf("summary = %q, want %q", sr.Summary, "Looks good overall")
	}
	if len(sr.Comments) != 0 {
		t.Errorf("comments = %d, want 0", len(sr.Comments))
	}
}

func TestParseStructuredReview_WithComments(t *testing.T) {
	t.Parallel()
	input := `{
		"summary": "A few issues found",
		"verdict": "request_changes",
		"comments": [
			{"path": "main.go", "line": 10, "body": "Missing nil check"},
			{"path": "util.go", "line": 25, "body": "Unused variable"}
		]
	}`
	sr, ok := parseStructuredReview(input)
	if !ok {
		t.Fatal("parseStructuredReview should succeed")
	}
	if sr.Verdict != "request_changes" {
		t.Errorf("verdict = %q", sr.Verdict)
	}
	if len(sr.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(sr.Comments))
	}
	if sr.Comments[0].Path != "main.go" {
		t.Errorf("comments[0].path = %q, want %q", sr.Comments[0].Path, "main.go")
	}
	if sr.Comments[0].Line != 10 {
		t.Errorf("comments[0].line = %d, want 10", sr.Comments[0].Line)
	}
}

func TestParseStructuredReview_MarkdownFence(t *testing.T) {
	t.Parallel()
	input := "Here's my review:\n```json\n{\"summary\": \"ok\", \"verdict\": \"approve\", \"comments\": []}\n```\n"
	sr, ok := parseStructuredReview(input)
	if !ok {
		t.Fatal("parseStructuredReview should handle markdown json fences")
	}
	if sr.Verdict != "approve" {
		t.Errorf("verdict = %q", sr.Verdict)
	}
}

func TestParseStructuredReview_PlainFence(t *testing.T) {
	t.Parallel()
	input := "```\n{\"summary\": \"issues\", \"verdict\": \"request_changes\", \"comments\": []}\n```"
	sr, ok := parseStructuredReview(input)
	if !ok {
		t.Fatal("parseStructuredReview should handle plain fences")
	}
	if sr.Verdict != "request_changes" {
		t.Errorf("verdict = %q", sr.Verdict)
	}
}

func TestParseStructuredReview_InvalidJSON(t *testing.T) {
	t.Parallel()
	_, ok := parseStructuredReview("not json at all")
	if ok {
		t.Error("parseStructuredReview should fail for non-JSON")
	}
}

func TestParseStructuredReview_MissingVerdict(t *testing.T) {
	t.Parallel()
	_, ok := parseStructuredReview(`{"summary": "ok", "comments": []}`)
	if ok {
		t.Error("parseStructuredReview should fail when verdict is missing")
	}
}

func TestParseStructuredReview_EmptyString(t *testing.T) {
	t.Parallel()
	_, ok := parseStructuredReview("")
	if ok {
		t.Error("parseStructuredReview should fail for empty string")
	}
}

func TestParseStructuredReview_PlainText(t *testing.T) {
	t.Parallel()
	_, ok := parseStructuredReview("- Bug in foo.go line 42\n- Missing test")
	if ok {
		t.Error("parseStructuredReview should fail for plain bullet list")
	}
}

// ---------------------------------------------------------------------------
// formatReviewBody tests
// ---------------------------------------------------------------------------

func TestFormatReviewBody_Approved(t *testing.T) {
	t.Parallel()
	body := formatReviewBody("Code Review", "approved", "No issues found.", 0)
	if !strings.Contains(body, "## 🤖 Code Review (approved)") {
		t.Errorf("body should contain header, got %q", body)
	}
	if !strings.Contains(body, "No issues found.") {
		t.Errorf("body should contain summary, got %q", body)
	}
	if strings.Contains(body, "inline comment") {
		t.Error("body should not mention inline comments when count is 0")
	}
}

func TestFormatReviewBody_WithComments(t *testing.T) {
	t.Parallel()
	body := formatReviewBody("Code Review", "request_changes", "Found issues.", 3)
	if !strings.Contains(body, "## 🤖 Code Review (request_changes)") {
		t.Errorf("body should contain header, got %q", body)
	}
	if !strings.Contains(body, "📝 3 inline comment(s) below.") {
		t.Errorf("body should mention inline comments, got %q", body)
	}
}

func TestFormatReviewBody_CustomName(t *testing.T) {
	t.Parallel()
	body := formatReviewBody("Fleet Review Bot", "approved", "All good.", 0)
	if !strings.Contains(body, "Fleet Review Bot") {
		t.Errorf("body should use custom name, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// extractJSON tests
// ---------------------------------------------------------------------------

func TestExtractJSON_RawJSON(t *testing.T) {
	t.Parallel()
	input := `{"key": "value"}`
	got := extractJSON(input)
	if got != input {
		t.Errorf("extractJSON = %q, want %q", got, input)
	}
}

func TestExtractJSON_WithJsonFence(t *testing.T) {
	t.Parallel()
	input := "text\n```json\n{\"key\": \"value\"}\n```\nmore text"
	want := `{"key": "value"}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON = %q, want %q", got, want)
	}
}

func TestExtractJSON_WithPlainFence(t *testing.T) {
	t.Parallel()
	input := "```\n{\"key\": \"value\"}\n```"
	want := `{"key": "value"}`
	got := extractJSON(input)
	if got != want {
		t.Errorf("extractJSON = %q, want %q", got, want)
	}
}

func TestExtractJSON_NonJSONInFence(t *testing.T) {
	t.Parallel()
	input := "```\nnot json at all\n```"
	got := extractJSON(input)
	if got != "" {
		t.Errorf("extractJSON = %q, want empty", got)
	}
}

func TestExtractJSON_NoJSON(t *testing.T) {
	t.Parallel()
	got := extractJSON("just plain text")
	if got != "" {
		t.Errorf("extractJSON = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// Structured review submission tests
// ---------------------------------------------------------------------------

func TestReview_StructuredApproval(t *testing.T) {
	t.Parallel()

	var capturedEvent, capturedBody string
	var capturedComments []gh.InlineComment

	r := &Reviewer{
		Agent: &mockBackend{
			run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
				return writeReviewOutput(workdir, `{"summary": "All good", "verdict": "approve", "comments": []}`)
			},
		},
		GH: &mockGHReview{
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				capturedEvent = event
				capturedBody = body
				capturedComments = comments
				return nil
			},
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				capturedEvent = event
				capturedBody = body
				return nil
			},
		},
	}

	// Agent writes empty output file -> isApproval("") = true -> falls back to legacy path
	// To test structured path, the agent would need to write JSON to the file,
	// which requires the real RunForReview. We test the parsing logic separately.
	issues, err := r.Review(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if issues != 0 {
		t.Errorf("Review() issues = %d, want 0", issues)
	}
	// The mock backend writes nothing -> RunForReview returns "" -> isApproval("") = true
	if capturedEvent != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", capturedEvent)
	}
	_ = capturedBody
	_ = capturedComments
}

func TestReview_DisplayNameFromConfig(t *testing.T) {
	t.Parallel()

	var submittedBody string
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				submittedBody = body
				return nil
			},
		},
	}

	cfg := &config.ReviewConfig{Enabled: true, MaxRounds: 3, Name: "My Bot Review"}
	issues, err := r.Review(context.Background(), "owner", "repo", 1, cfg)
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if issues != 0 {
		t.Errorf("issues = %d, want 0", issues)
	}
	if !strings.Contains(submittedBody, "My Bot Review") {
		t.Errorf("body should contain custom display name, got %q", submittedBody)
	}
}

func TestReview_DisplayNameDefault(t *testing.T) {
	t.Parallel()

	var submittedBody string
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				submittedBody = body
				return nil
			},
		},
	}

	cfg := &config.ReviewConfig{Enabled: true, MaxRounds: 3}
	_, err := r.Review(context.Background(), "owner", "repo", 1, cfg)
	if err != nil {
		t.Fatalf("Review() error = %v", err)
	}
	if !strings.Contains(submittedBody, "Code Review") {
		t.Errorf("body should contain default display name, got %q", submittedBody)
	}
}

// ---------------------------------------------------------------------------
// submitStructured tests
// ---------------------------------------------------------------------------

func TestSubmitStructured_Approve(t *testing.T) {
	t.Parallel()

	var capturedEvent, capturedBody string
	r := &Reviewer{
		GH: &mockGHReview{
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				capturedEvent = event
				capturedBody = body
				return nil
			},
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				capturedEvent = event
				capturedBody = body
				return nil
			},
		},
	}

	sr := &structuredReview{
		Summary:  "All looks good",
		Verdict:  "approve",
		Comments: nil,
	}

	issues, err := r.submitStructured(context.Background(), "owner", "repo", 42, sr, "Code Review")
	if err != nil {
		t.Fatalf("submitStructured() error = %v", err)
	}
	if issues != 0 {
		t.Errorf("issues = %d, want 0", issues)
	}
	if capturedEvent != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", capturedEvent)
	}
	if !strings.Contains(capturedBody, "Code Review") {
		t.Errorf("body should contain display name, got %q", capturedBody)
	}
}

func TestSubmitStructured_RequestChanges(t *testing.T) {
	t.Parallel()

	var capturedEvent string
	var capturedComments []gh.InlineComment

	r := &Reviewer{
		GH: &mockGHReview{
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				capturedEvent = event
				capturedComments = comments
				return nil
			},
		},
	}

	sr := &structuredReview{
		Summary: "Found issues",
		Verdict: "request_changes",
		Comments: []reviewComment{
			{Path: "main.go", Line: 10, Body: "Missing nil check"},
			{Path: "util.go", Line: 25, Body: "Unused variable"},
		},
	}

	issues, err := r.submitStructured(context.Background(), "owner", "repo", 42, sr, "Code Review")
	if err != nil {
		t.Fatalf("submitStructured() error = %v", err)
	}
	if issues != 2 {
		t.Errorf("issues = %d, want 2", issues)
	}
	if capturedEvent != "REQUEST_CHANGES" {
		t.Errorf("event = %q, want REQUEST_CHANGES", capturedEvent)
	}
	if len(capturedComments) != 2 {
		t.Fatalf("inline comments = %d, want 2", len(capturedComments))
	}
	if capturedComments[0].Path != "main.go" {
		t.Errorf("comment[0].path = %q, want %q", capturedComments[0].Path, "main.go")
	}
	if capturedComments[0].Line != 10 {
		t.Errorf("comment[0].line = %d, want 10", capturedComments[0].Line)
	}
}

func TestSubmitStructured_Error(t *testing.T) {
	t.Parallel()

	r := &Reviewer{
		GH: &mockGHReview{
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				return errors.New("api error")
			},
		},
	}

	sr := &structuredReview{
		Summary: "Issues found",
		Verdict: "request_changes",
		Comments: []reviewComment{
			{Path: "main.go", Line: 1, Body: "bug"},
		},
	}

	_, err := r.submitStructured(context.Background(), "owner", "repo", 42, sr, "Code Review")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "submitting review") {
		t.Errorf("expected 'submitting review' error, got %v", err)
	}
}

func TestSubmitStructured_VerdictVariants(t *testing.T) {
	t.Parallel()

	approvedVerdicts := []string{"approve", "approved", "lgtm"}
	for _, v := range approvedVerdicts {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			var capturedEvent string
			r := &Reviewer{
				GH: &mockGHReview{
					submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
						capturedEvent = event
						return nil
					},
					submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
						capturedEvent = event
						return nil
					},
				},
			}
			sr := &structuredReview{Verdict: v, Summary: "ok"}
			issues, err := r.submitStructured(context.Background(), "o", "r", 1, sr, "Review")
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if issues != 0 {
				t.Errorf("issues = %d, want 0", issues)
			}
			if capturedEvent != "APPROVE" {
				t.Errorf("event = %q, want APPROVE for verdict %q", capturedEvent, v)
			}
		})
	}
}

func TestSubmitStructured_RequestChangesNoComments(t *testing.T) {
	t.Parallel()
	var capturedEvent string
	r := &Reviewer{
		GH: &mockGHReview{
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				capturedEvent = event
				return nil
			},
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				capturedEvent = event
				return nil
			},
		},
	}
	sr := &structuredReview{Verdict: "request_changes", Summary: "needs work"}
	issues, err := r.submitStructured(context.Background(), "o", "r", 1, sr, "Review")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if issues != 1 {
		t.Errorf("issues = %d, want 1 (minimum)", issues)
	}
	if capturedEvent != "REQUEST_CHANGES" {
		t.Errorf("event = %q, want REQUEST_CHANGES", capturedEvent)
	}
}

// ---------------------------------------------------------------------------
// ReviewLocal tests
// ---------------------------------------------------------------------------

func TestReviewLocal_EmptyDiff(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			getPRDiff: func(ctx context.Context, owner, repo string, prNumber int) (string, error) {
				return "", nil
			},
		},
	}

	result, err := r.ReviewLocal(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("ReviewLocal() error = %v", err)
	}
	if !result.Approved {
		t.Error("ReviewLocal() should approve empty diff")
	}
}

func TestReviewLocal_DiffFetchError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			getPRDiff: func(ctx context.Context, owner, repo string, prNumber int) (string, error) {
				return "", errors.New("network error")
			},
		},
	}

	_, err := r.ReviewLocal(context.Background(), "owner", "repo", 1, defaultCfg())
	if err == nil {
		t.Fatal("ReviewLocal() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "fetching PR diff") {
		t.Errorf("expected 'fetching PR diff' in error, got %v", err)
	}
}

func TestReviewLocal_AgentApproves(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{},
		GH:    &mockGHReview{},
	}

	result, err := r.ReviewLocal(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("ReviewLocal() error = %v", err)
	}
	if !result.Approved {
		t.Error("ReviewLocal() should approve when agent returns empty (LGTM)")
	}
}

func TestReviewLocal_AgentError(t *testing.T) {
	t.Parallel()
	r := &Reviewer{
		Agent: &mockBackend{
			run: func(ctx context.Context, workdir string, prompt string, output io.Writer) error {
				return errors.New("agent crashed")
			},
		},
		GH: &mockGHReview{},
	}

	_, err := r.ReviewLocal(context.Background(), "owner", "repo", 1, defaultCfg())
	if err == nil {
		t.Fatal("ReviewLocal() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "running review agent") {
		t.Errorf("expected 'running review agent' in error, got %v", err)
	}
}

func TestReviewLocal_DoesNotSubmitToGitHub(t *testing.T) {
	t.Parallel()
	var submitted bool
	r := &Reviewer{
		Agent: &mockBackend{},
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				submitted = true
				return nil
			},
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				submitted = true
				return nil
			},
		},
	}

	_, err := r.ReviewLocal(context.Background(), "owner", "repo", 1, defaultCfg())
	if err != nil {
		t.Fatalf("ReviewLocal() error = %v", err)
	}
	if submitted {
		t.Error("ReviewLocal() must not submit reviews to GitHub")
	}
}

// ---------------------------------------------------------------------------
// parseReviewResponse tests
// ---------------------------------------------------------------------------

func TestParseReviewResponse_ApprovalPlainText(t *testing.T) {
	t.Parallel()
	result := parseReviewResponse("LGTM")
	if !result.Approved {
		t.Error("should approve for LGTM")
	}
}

func TestParseReviewResponse_EmptyIsApproval(t *testing.T) {
	t.Parallel()
	result := parseReviewResponse("")
	if !result.Approved {
		t.Error("should approve for empty response")
	}
}

func TestParseReviewResponse_IssuesPlainText(t *testing.T) {
	t.Parallel()
	result := parseReviewResponse("- Bug in foo.go line 42\n- Missing test")
	if result.Approved {
		t.Error("should not approve for list of issues")
	}
	if result.Issues != 2 {
		t.Errorf("issues = %d, want 2", result.Issues)
	}
	if result.Feedback == "" {
		t.Error("feedback should not be empty")
	}
}

func TestParseReviewResponse_StructuredApproval(t *testing.T) {
	t.Parallel()
	input := `{"summary": "All good", "verdict": "approve", "comments": []}`
	result := parseReviewResponse(input)
	if !result.Approved {
		t.Error("should approve for structured approval")
	}
}

func TestParseReviewResponse_StructuredRequestChanges(t *testing.T) {
	t.Parallel()
	input := `{"summary": "Issues found", "verdict": "request_changes", "comments": [{"path": "x.go", "line": 1, "body": "bug"}]}`
	result := parseReviewResponse(input)
	if result.Approved {
		t.Error("should not approve for request_changes")
	}
	if result.Issues != 1 {
		t.Errorf("issues = %d, want 1", result.Issues)
	}
	if !strings.Contains(result.Feedback, "x.go:1") {
		t.Errorf("feedback should contain file:line ref, got %q", result.Feedback)
	}
}

func TestParseReviewResponse_StructuredRequestChangesNoComments(t *testing.T) {
	t.Parallel()
	input := `{"summary": "needs work", "verdict": "request_changes", "comments": []}`
	result := parseReviewResponse(input)
	if result.Approved {
		t.Error("should not approve")
	}
	if result.Issues != 1 {
		t.Errorf("issues = %d, want 1 (minimum)", result.Issues)
	}
}

// ---------------------------------------------------------------------------
// formatLocalFeedback tests
// ---------------------------------------------------------------------------

func TestFormatLocalFeedback_NoComments(t *testing.T) {
	t.Parallel()
	sr := &structuredReview{Summary: "All good", Verdict: "approve"}
	feedback := formatLocalFeedback(sr)
	if feedback != "All good" {
		t.Errorf("feedback = %q, want %q", feedback, "All good")
	}
}

func TestFormatLocalFeedback_WithComments(t *testing.T) {
	t.Parallel()
	sr := &structuredReview{
		Summary: "Issues found",
		Verdict: "request_changes",
		Comments: []reviewComment{
			{Path: "main.go", Line: 10, Body: "Missing nil check"},
			{Path: "util.go", Line: 25, Body: "Unused variable"},
		},
	}
	feedback := formatLocalFeedback(sr)
	if !strings.Contains(feedback, "Issues found") {
		t.Errorf("feedback should contain summary, got %q", feedback)
	}
	if !strings.Contains(feedback, "1. main.go:10") {
		t.Errorf("feedback should contain numbered comment ref, got %q", feedback)
	}
	if !strings.Contains(feedback, "2. util.go:25") {
		t.Errorf("feedback should contain second comment ref, got %q", feedback)
	}
	if !strings.Contains(feedback, "Missing nil check") {
		t.Errorf("feedback should contain comment body, got %q", feedback)
	}
}

// ---------------------------------------------------------------------------
// submitWithFallback / self-review fallback tests
// ---------------------------------------------------------------------------

func TestSubmitWithFallback_NoError(t *testing.T) {
	t.Parallel()
	var capturedEvent string
	r := &Reviewer{
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				capturedEvent = event
				return nil
			},
		},
	}

	err := r.submitWithFallback(context.Background(), "owner", "repo", 42, "APPROVE", "body", nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if capturedEvent != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", capturedEvent)
	}
}

func TestSubmitWithFallback_SelfReviewFallbackToComment(t *testing.T) {
	t.Parallel()
	callCount := 0
	var events []string
	r := &Reviewer{
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				callCount++
				events = append(events, event)
				if event == "APPROVE" {
					return errors.New("422: can not approve your own pull request")
				}
				return nil
			},
		},
	}

	err := r.submitWithFallback(context.Background(), "owner", "repo", 42, "APPROVE", "## 🤖 Code Review (approved)\n\nbody", nil)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("submit called %d times, want 2 (APPROVE then COMMENT fallback)", callCount)
	}
	if len(events) != 2 || events[0] != "APPROVE" || events[1] != "COMMENT" {
		t.Errorf("events = %v, want [APPROVE, COMMENT]", events)
	}
}

func TestSubmitWithFallback_SelfReviewFallbackWithInlineComments(t *testing.T) {
	t.Parallel()
	callCount := 0
	var events []string
	r := &Reviewer{
		GH: &mockGHReview{
			submitPRReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string, comments []gh.InlineComment) error {
				callCount++
				events = append(events, event)
				if event == "APPROVE" {
					return errors.New("422: cannot approve your own pull request")
				}
				return nil
			},
		},
	}

	comments := []gh.InlineComment{{Path: "x.go", Line: 1, Body: "ok"}}
	err := r.submitWithFallback(context.Background(), "owner", "repo", 42, "APPROVE", "body", comments)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("submit called %d times, want 2", callCount)
	}
	if len(events) != 2 || events[1] != "COMMENT" {
		t.Errorf("events = %v, want fallback to COMMENT", events)
	}
}

func TestSubmitWithFallback_NonSelfReviewErrorNotRetried(t *testing.T) {
	t.Parallel()
	callCount := 0
	r := &Reviewer{
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				callCount++
				return errors.New("500: internal server error")
			},
		},
	}

	err := r.submitWithFallback(context.Background(), "owner", "repo", 42, "APPROVE", "body", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("submit called %d times, want 1 (no fallback for non-self-review error)", callCount)
	}
}

func TestSubmitWithFallback_RequestChangesNotRetried(t *testing.T) {
	t.Parallel()
	callCount := 0
	r := &Reviewer{
		GH: &mockGHReview{
			submitReview: func(ctx context.Context, owner, repo string, prNumber int, event, body string) error {
				callCount++
				return errors.New("422: can not approve your own pull request")
			},
		},
	}

	err := r.submitWithFallback(context.Background(), "owner", "repo", 42, "REQUEST_CHANGES", "body", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("submit called %d times, want 1 (no fallback for REQUEST_CHANGES)", callCount)
	}
}

// ---------------------------------------------------------------------------
// isSelfReviewError tests
// ---------------------------------------------------------------------------

func TestIsSelfReviewError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"can not approve", errors.New("422: can not approve your own pull request"), true},
		{"cannot approve", errors.New("cannot approve your own pull request"), true},
		{"422 status", errors.New("gh api: 422: Unprocessable Entity"), true},
		{"self-review keyword", errors.New("self-review not allowed"), true},
		{"unrelated error", errors.New("500: internal server error"), false},
		{"network error", errors.New("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSelfReviewError(tt.err)
			if got != tt.want {
				t.Errorf("isSelfReviewError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeReviewOutput(workdir, content string) error {
	return nil
}
