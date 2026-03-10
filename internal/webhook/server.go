package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Server is an HTTP server that receives GitHub webhook events.
type Server struct {
	handler       *Handler
	webhookSecret string
	mux           *http.ServeMux
	httpServer    *http.Server
}

// NewServer creates a webhook server with the given configuration.
func NewServer(port int, webhookSecret string, handler *Handler) *Server {
	s := &Server{
		handler:       handler,
		webhookSecret: webhookSecret,
		mux:           http.NewServeMux(),
	}
	s.mux.HandleFunc("POST /webhook", s.handleWebhook)
	s.mux.HandleFunc("GET /health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// ListenAndServe starts the HTTP server. It blocks until the server is shut down.
func (s *Server) ListenAndServe() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"ok"}`)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		log.Printf("webhook: error reading body: %v", err)
		http.Error(w, "error reading body", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if !verifySignature(body, sig, s.webhookSecret) {
		log.Printf("webhook: invalid signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		log.Printf("webhook: missing X-GitHub-Event header")
		http.Error(w, "missing event type", http.StatusBadRequest)
		return
	}

	delivery := r.Header.Get("X-GitHub-Delivery")
	log.Printf("webhook: received event=%s delivery=%s", eventType, delivery)

	result, err := s.handler.HandleEvent(eventType, body)
	if err != nil {
		log.Printf("webhook: error handling event=%s delivery=%s: %v", eventType, delivery, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]string{"status": result}
	_ = json.NewEncoder(w).Encode(resp)
}

// verifySignature checks the HMAC-SHA256 signature from GitHub.
// The header format is "sha256=<hex>".
func verifySignature(payload []byte, signature, secret string) bool {
	if secret == "" {
		return false
	}
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	sigBytes, err := hex.DecodeString(signature[len("sha256="):])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sigBytes, expected)
}
