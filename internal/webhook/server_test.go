package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sign(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	payload := []byte(`{"action":"labeled"}`)

	tests := []struct {
		name string
		sig  string
		want bool
	}{
		{"valid", sign(payload, secret), true},
		{"wrong secret", sign(payload, "wrong"), false},
		{"empty signature", "", false},
		{"missing prefix", hex.EncodeToString([]byte("nope")), false},
		{"invalid hex", "sha256=zzzz", false},
		{"empty secret", sign(payload, ""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := secret
			if tt.name == "empty secret" {
				s = ""
			}
			got := verifySignature(payload, tt.sig, s)
			if got != tt.want {
				t.Errorf("verifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	handler := NewHandler("fleet", "", &stubRunner{})
	srv := NewServer(0, "secret", handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"ok"`) {
		t.Errorf("health body = %q, want to contain 'ok'", body)
	}
}

func TestWebhookEndpoint_InvalidSignature(t *testing.T) {
	t.Parallel()

	handler := NewHandler("fleet", "", &stubRunner{})
	srv := NewServer(0, "secret", handler)

	payload := `{"action":"labeled"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	req.Header.Set("X-GitHub-Event", "issues")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestWebhookEndpoint_MissingEventHeader(t *testing.T) {
	t.Parallel()

	handler := NewHandler("fleet", "", &stubRunner{})
	srv := NewServer(0, "my-secret", handler)

	payload := []byte(`{"action":"labeled"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", sign(payload, "my-secret"))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWebhookEndpoint_ValidEvent(t *testing.T) {
	t.Parallel()

	runner := &stubRunner{}
	handler := NewHandler("fleet", "", runner)
	srv := NewServer(0, "my-secret", handler)

	payload := issuesPayload{
		Action: "labeled",
		Label:  payloadLabel{Name: "fleet"},
		Issue:  payloadIssue{Number: 42, Title: "test"},
		Sender: payloadUser{Login: "human", Type: "User"},
		Repo:   payloadRepo{Name: "my-repo"},
	}
	payload.Repo.Owner.Login = "my-org"

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sign(body, "my-secret"))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "test-delivery-123")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	respBody, _ := io.ReadAll(w.Body)
	var resp map[string]string
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "triggered" {
		t.Errorf("status = %q, want %q", resp["status"], "triggered")
	}
}

func TestWebhookEndpoint_IgnoredEvent(t *testing.T) {
	t.Parallel()

	handler := NewHandler("fleet", "", &stubRunner{})
	srv := NewServer(0, "secret", handler)

	payload := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", sign(payload, "secret"))
	req.Header.Set("X-GitHub-Event", "push")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
