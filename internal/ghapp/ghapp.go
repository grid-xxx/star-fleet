package ghapp

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Client authenticates as a GitHub App and obtains installation access tokens.
type Client struct {
	appID      int64
	privateKey *rsa.PrivateKey

	mu             sync.Mutex
	tokenCache     map[int64]*cachedToken
	installIDCache map[string]int64
	httpClient     *http.Client
	baseURL        string
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// NewClient creates a GitHub App client from an App ID and PEM-encoded private key file.
func NewClient(appID int64, pemPath string) (*Client, error) {
	pemData, err := os.ReadFile(pemPath)
	if err != nil {
		return nil, fmt.Errorf("reading private key %s: %w", pemPath, err)
	}
	return NewClientFromPEM(appID, pemData)
}

// NewClientFromPEM creates a GitHub App client from an App ID and raw PEM data.
func NewClientFromPEM(appID int64, pemData []byte) (*Client, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM(pemData)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	return &Client{
		appID:          appID,
		privateKey:     key,
		tokenCache:     make(map[int64]*cachedToken),
		installIDCache: make(map[string]int64),
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		baseURL:        "https://api.github.com",
	}, nil
}

// GenerateJWT creates a short-lived JWT for authenticating as the GitHub App.
func (c *Client) GenerateJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", c.appID),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.privateKey)
}

type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type installationResponse struct {
	ID int64 `json:"id"`
}

// InstallationToken returns an installation access token for the given owner.
// Both the owner→installationID mapping and the token itself are cached.
func (c *Client) InstallationToken(owner string) (string, error) {
	c.mu.Lock()
	cachedID, ok := c.installIDCache[owner]
	c.mu.Unlock()

	if ok {
		return c.getInstallationToken(cachedID)
	}

	installID, err := c.getInstallationID(owner)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.installIDCache[owner] = installID
	c.mu.Unlock()

	return c.getInstallationToken(installID)
}

func (c *Client) getInstallationID(owner string) (int64, error) {
	appJWT, err := c.GenerateJWT()
	if err != nil {
		return 0, fmt.Errorf("generating JWT: %w", err)
	}

	reqURL := fmt.Sprintf("%s/users/%s/installation", c.baseURL, url.PathEscape(owner))
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("looking up installation for %s: %w", owner, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("installation lookup for %s: HTTP %d: %s", owner, resp.StatusCode, body)
	}

	var inst installationResponse
	if err := json.NewDecoder(resp.Body).Decode(&inst); err != nil {
		return 0, fmt.Errorf("decoding installation response: %w", err)
	}
	return inst.ID, nil
}

func (c *Client) getInstallationToken(installationID int64) (string, error) {
	c.mu.Lock()
	if cached, ok := c.tokenCache[installationID]; ok {
		if time.Now().Add(5 * time.Minute).Before(cached.ExpiresAt) {
			c.mu.Unlock()
			return cached.Token, nil
		}
	}
	c.mu.Unlock()

	appJWT, err := c.GenerateJWT()
	if err != nil {
		return "", fmt.Errorf("generating JWT: %w", err)
	}

	reqURL := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.baseURL, installationID)
	req, err := http.NewRequest("POST", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("creating installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("installation token: HTTP %d: %s", resp.StatusCode, body)
	}

	var tokenResp installationTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	c.mu.Lock()
	c.tokenCache[installationID] = &cachedToken{
		Token:     tokenResp.Token,
		ExpiresAt: tokenResp.ExpiresAt,
	}
	c.mu.Unlock()

	return tokenResp.Token, nil
}
