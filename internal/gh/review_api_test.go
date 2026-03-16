package gh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func staticToken(tok string) TokenFunc {
	return func(owner string) (string, error) { return tok, nil }
}

// ---------------------------------------------------------------------------
// SubmitReview tests
// ---------------------------------------------------------------------------

func TestAPIReviewClient_SubmitReview_Approve(t *testing.T) {
	t.Parallel()

	var captured reviewRequest
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("ghs_test"), BaseURL: srv.URL}
	err := c.SubmitReview(context.Background(), "owner", "repo", 42, "APPROVE", "Looks great!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedAuth != "Bearer ghs_test" {
		t.Errorf("auth = %q, want Bearer ghs_test", capturedAuth)
	}
	if captured.Event != "APPROVE" {
		t.Errorf("event = %q, want APPROVE", captured.Event)
	}
	if captured.Body != "Looks great!" {
		t.Errorf("body = %q, want 'Looks great!'", captured.Body)
	}
}

func TestAPIReviewClient_SubmitReview_RequestChanges(t *testing.T) {
	t.Parallel()

	var captured reviewRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":2}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	err := c.SubmitReview(context.Background(), "owner", "repo", 1, "REQUEST_CHANGES", "fix this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.Event != "REQUEST_CHANGES" {
		t.Errorf("event = %q, want REQUEST_CHANGES", captured.Event)
	}
}

func TestAPIReviewClient_SubmitReviewWithComments(t *testing.T) {
	t.Parallel()

	var captured reviewRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":3}`))
	}))
	defer srv.Close()

	comments := []ReviewComment{
		{Path: "main.go", Line: 10, Body: "missing nil check"},
		{Path: "util.go", Line: 25, Body: "unused import"},
	}
	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	err := c.SubmitReviewWithComments(context.Background(), "owner", "repo", 5, "REQUEST_CHANGES", "Issues found", comments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(captured.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(captured.Comments))
	}
	if captured.Comments[0].Path != "main.go" || captured.Comments[0].Line != 10 {
		t.Errorf("comment[0] = %+v, want path=main.go line=10", captured.Comments[0])
	}
}

func TestAPIReviewClient_SubmitReview_OwnPRFallback(t *testing.T) {
	t.Parallel()

	var callCount int32
	var events []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req reviewRequest
		json.NewDecoder(r.Body).Decode(&req)
		events = append(events, req.Event)
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 && req.Event == "REQUEST_CHANGES" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"message":"Can not request changes on your own pull request"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	err := c.SubmitReview(context.Background(), "owner", "repo", 1, "REQUEST_CHANGES", "needs work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 calls (REQUEST_CHANGES then COMMENT), got %d", len(events))
	}
	if events[0] != "REQUEST_CHANGES" {
		t.Errorf("first call event = %q, want REQUEST_CHANGES", events[0])
	}
	if events[1] != "COMMENT" {
		t.Errorf("second call event = %q, want COMMENT", events[1])
	}
}

func TestAPIReviewClient_SubmitReview_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	err := c.SubmitReview(context.Background(), "owner", "repo", 1, "APPROVE", "ok")
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want to contain 500", err)
	}
}

func TestAPIReviewClient_SubmitReview_TokenError(t *testing.T) {
	t.Parallel()

	tokenFn := func(owner string) (string, error) {
		return "", fmt.Errorf("no token")
	}
	c := &APIReviewClient{Token: tokenFn, BaseURL: "http://unused"}
	err := c.SubmitReview(context.Background(), "owner", "repo", 1, "APPROVE", "ok")
	if err == nil {
		t.Fatal("expected error when token func fails")
	}
	if !strings.Contains(err.Error(), "installation token") {
		t.Errorf("error = %v, want to mention installation token", err)
	}
}

// ---------------------------------------------------------------------------
// GetPRBranches tests
// ---------------------------------------------------------------------------

func TestAPIReviewClient_GetPRBranches(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/pulls/7") {
			t.Errorf("path = %s, want to contain /pulls/7", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"base": map[string]string{"ref": "main"},
			"head": map[string]string{"ref": "fleet/7"},
		})
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	branches, err := c.GetPRBranches(context.Background(), "owner", "repo", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branches.Base != "main" {
		t.Errorf("Base = %q, want main", branches.Base)
	}
	if branches.Head != "fleet/7" {
		t.Errorf("Head = %q, want fleet/7", branches.Head)
	}
}

func TestAPIReviewClient_GetPRBranches_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	_, err := c.GetPRBranches(context.Background(), "owner", "repo", 99)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want to contain 404", err)
	}
}

// ---------------------------------------------------------------------------
// PostComment tests
// ---------------------------------------------------------------------------

func TestAPIReviewClient_PostComment(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/issues/5/comments") {
			t.Errorf("path = %s, want to contain /issues/5/comments", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	err := c.PostComment(context.Background(), "owner", "repo", 5, "Hello!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["body"] != "Hello!" {
		t.Errorf("body = %q, want 'Hello!'", capturedBody["body"])
	}
}

func TestAPIReviewClient_PostComment_Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	err := c.PostComment(context.Background(), "owner", "repo", 5, "Hello!")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

// ---------------------------------------------------------------------------
// isOwnPRAPIError tests
// ---------------------------------------------------------------------------

func TestIsOwnPRAPIError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		msg  string
		want bool
	}{
		{`{"message":"Can not request changes on your own pull request"}`, true},
		{`{"message":"cannot request changes on your own pull request"}`, true},
		{`{"message":"Not Found"}`, false},
		{"", false},
	}
	for _, tt := range tests {
		got := isOwnPRAPIError(tt.msg)
		if got != tt.want {
			t.Errorf("isOwnPRAPIError(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// URL construction test
// ---------------------------------------------------------------------------

func TestAPIReviewClient_URLConstruction(t *testing.T) {
	t.Parallel()

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	c := &APIReviewClient{Token: staticToken("tok"), BaseURL: srv.URL}
	_ = c.SubmitReview(context.Background(), "my-org", "my-repo", 123, "APPROVE", "ok")

	want := "/repos/my-org/my-repo/pulls/123/reviews"
	if capturedPath != want {
		t.Errorf("path = %q, want %q", capturedPath, want)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// Verify APIReviewClient satisfies the review.GHReview interface at compile time.
// (We import the interface indirectly; this checks the three required methods.)
var _ interface {
	GetPRBranches(ctx context.Context, owner, repo string, prNumber int) (*PRBranches, error)
	SubmitReview(ctx context.Context, owner, repo string, prNumber int, event, body string) error
	PostComment(ctx context.Context, owner, repo string, number int, body string) error
} = (*APIReviewClient)(nil)
