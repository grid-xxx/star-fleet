package ghapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func generateTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test key: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, pemData
}

func TestNewClient_ValidPEM(t *testing.T) {
	t.Parallel()
	_, pemData := generateTestKey(t)

	c, err := NewClientFromPEM(12345, pemData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.appID != 12345 {
		t.Errorf("appID = %d, want 12345", c.appID)
	}
}

func TestNewClient_InvalidPEM(t *testing.T) {
	t.Parallel()
	_, err := NewClientFromPEM(1, []byte("not-a-pem"))
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestNewClient_FromFile(t *testing.T) {
	t.Parallel()
	_, pemData := generateTestKey(t)

	dir := t.TempDir()
	pemPath := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(pemPath, pemData, 0o600); err != nil {
		t.Fatalf("writing PEM file: %v", err)
	}

	c, err := NewClient(12345, pemPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.appID != 12345 {
		t.Errorf("appID = %d, want 12345", c.appID)
	}
}

func TestNewClient_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := NewClient(1, "/nonexistent/path.pem")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGenerateJWT(t *testing.T) {
	t.Parallel()
	key, pemData := generateTestKey(t)

	c, err := NewClientFromPEM(42, pemData)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}

	tokenStr, err := c.GenerateJWT()
	if err != nil {
		t.Fatalf("generating JWT: %v", err)
	}

	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parsing JWT: %v", err)
	}
	if !token.Valid {
		t.Fatal("JWT is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("unexpected claims type")
	}
	iss, _ := claims.GetIssuer()
	if iss != "42" {
		t.Errorf("issuer = %q, want %q", iss, "42")
	}
}

func TestInstallationToken(t *testing.T) {
	t.Parallel()
	_, pemData := generateTestKey(t)

	expiresAt := time.Now().Add(1 * time.Hour)
	mux := http.NewServeMux()
	mux.HandleFunc("/users/test-owner/installation", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(installationResponse{ID: 999})
	})
	mux.HandleFunc("/app/installations/999/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{
			Token:     "ghs_test_token_abc",
			ExpiresAt: expiresAt,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClientFromPEM(42, pemData)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	c.baseURL = srv.URL

	token, err := c.InstallationToken("test-owner")
	if err != nil {
		t.Fatalf("getting token: %v", err)
	}
	if token != "ghs_test_token_abc" {
		t.Errorf("token = %q, want %q", token, "ghs_test_token_abc")
	}
}

func TestInstallationToken_Cached(t *testing.T) {
	t.Parallel()
	_, pemData := generateTestKey(t)

	tokenCallCount := 0
	installCallCount := 0
	expiresAt := time.Now().Add(1 * time.Hour)
	mux := http.NewServeMux()
	mux.HandleFunc("/users/test-owner/installation", func(w http.ResponseWriter, r *http.Request) {
		installCallCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(installationResponse{ID: 100})
	})
	mux.HandleFunc("/app/installations/100/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		tokenCallCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{
			Token:     "ghs_cached_token",
			ExpiresAt: expiresAt,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClientFromPEM(42, pemData)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	c.baseURL = srv.URL

	// First call fetches both installation ID and token
	token1, err := c.InstallationToken("test-owner")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call should use cached installation ID and cached token
	token2, err := c.InstallationToken("test-owner")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if token1 != token2 {
		t.Errorf("tokens differ: %q vs %q", token1, token2)
	}
	if tokenCallCount != 1 {
		t.Errorf("token endpoint called %d times, want 1 (should be cached)", tokenCallCount)
	}
	if installCallCount != 1 {
		t.Errorf("installation lookup called %d times, want 1 (should be cached)", installCallCount)
	}
}

func TestInstallationToken_ExpiredCacheRefreshes(t *testing.T) {
	t.Parallel()
	_, pemData := generateTestKey(t)

	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/users/owner/installation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(installationResponse{ID: 200})
	})
	mux.HandleFunc("/app/installations/200/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(installationTokenResponse{
			Token:     fmt.Sprintf("ghs_token_%d", callCount),
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClientFromPEM(42, pemData)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	c.baseURL = srv.URL

	// Seed cache with an expired token
	c.mu.Lock()
	c.tokenCache[200] = &cachedToken{
		Token:     "ghs_old",
		ExpiresAt: time.Now().Add(2 * time.Minute), // within the 5-min buffer
	}
	c.mu.Unlock()

	token, err := c.InstallationToken("owner")
	if err != nil {
		t.Fatalf("getting token: %v", err)
	}
	if token != "ghs_token_1" {
		t.Errorf("token = %q, want refreshed token", token)
	}
}

func TestInstallationToken_InstallationNotFound(t *testing.T) {
	t.Parallel()
	_, pemData := generateTestKey(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/users/unknown-owner/installation", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClientFromPEM(42, pemData)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	c.baseURL = srv.URL

	_, err = c.InstallationToken("unknown-owner")
	if err == nil {
		t.Fatal("expected error for unknown owner")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want to contain 404", err)
	}
}
