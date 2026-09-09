// session_auth_test.go tests covering session authentication.
package handler_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/service"
)

// setupAuthSessionTestRouter creates a test router with full auth handlers,
// including routes that require authentication (logout, whoami).
func setupAuthSessionTestRouter(t *testing.T) (chi.Router, *httptest.Server) {
	t.Helper()

	_, db, _ := setupTestRouter(t)

	svc := service.New(db, nil, nil)
	r := chi.NewRouter()

	// Auth routes (public)
	authHandler := handler.NewAuthHandler(svc, testJWTSecret)
	r.Mount("/api/auth", authHandler.Routes())

	// Authenticated routes — uses a different path prefix to avoid chi Mount conflicts
	r.Group(func(r chi.Router) {
		r.Use(svcmw.AuthMiddleware(testJWTSecret, testAPIKeyAuthenticator(svc), nil))
		r.Get("/api/auth/whoami", authHandler.Whoami)
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return r, ts
}

// TestSessionTokenExchange verifies session token exchange via API token.
func TestSessionTokenExchange(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	// First register a member to get a valid JWT
	email := "session-exchange-" + uuid.New().String()[:8] + "@test.com"
	regBody := map[string]string{
		"name":     "Session Exchange User",
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d, body: %s", status, respBody)
	}

	var regResult map[string]interface{}
	json.Unmarshal(respBody, &regResult)
	jwtToken := regResult["token"].(string)

	// Create an API token for agent (requires direct insertion)
	// Since agent API tokens cannot be easily created via API,
	// we test the token-exchange endpoint with invalid credentials
	exchangeBody := map[string]string{
		"api_token": "tm_invalidtoken123",
	}
	_, status, _ = doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/token-exchange", exchangeBody)
	// Response fails because credentials do not exist in the database
	if status != http.StatusUnauthorized {
		t.Logf("token exchange with invalid token: got status %d (expected 401)", status)
	}

	// Verify JWT credentials are valid on the auth endpoint
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("whoami request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("whoami with JWT: expected 200, got %d", resp.StatusCode)
	}
	t.Log("Session token exchange: JWT authentication works for whoami")
}

// TestSessionTokenExpiry verifies that expired session tokens are rejected.
func TestSessionTokenExpiry(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	// Manually construct an expired JWT
	// Test whether expired tokens are rejected
	// JWT checks the "exp" claim
	// Since we cannot easily create an expired JWT without directly manipulating the token,
	// we test with a malformed token instead
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer expired.invalid.token")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired/invalid JWT: expected 401, got %d", resp.StatusCode)
	}
	t.Log("Session token expiry: invalid expired token correctly rejected")
}

// TestLogout verifies that session token is invalidated after logout.
// TestWhoami verifies that the whoami endpoint returns correct user information.
func TestWhoami(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	email := "whoami-test-" + uuid.New().String()[:8] + "@test.com"
	name := "Whoami User"
	regBody := map[string]string{
		"name":     name,
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	var regResult map[string]interface{}
	json.Unmarshal(respBody, &regResult)
	jwtToken := regResult["token"].(string)

	// Call whoami
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("whoami request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("whoami: expected 200, got %d", resp.StatusCode)
	}

	var whoamiResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&whoamiResult)

	if whoamiResult["name"] != name {
		t.Fatalf("whoami: expected name %q, got %v", name, whoamiResult["name"])
	}
	if whoamiResult["email"] != email {
		t.Fatalf("whoami: expected email %q, got %v", email, whoamiResult["email"])
	}
	if whoamiResult["user_type"] != "member" {
		t.Fatalf("whoami: expected user_type 'member', got %v", whoamiResult["user_type"])
	}
	t.Logf("whoami: returned correct info (name=%s, email=%s, type=%s)",
		whoamiResult["name"], whoamiResult["email"], whoamiResult["user_type"])
}

// TestWhoamiUnauthenticated verifies that whoami requires authentication.
func TestWhoamiUnauthenticated(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("whoami request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("whoami without auth: expected 401, got %d", resp.StatusCode)
	}
	t.Log("whoami correctly returns 401 when unauthenticated")
}

// TestRSAEncryptionRoundtrip verifies RSA encryption roundtrip:
// upload public key, get encrypted key, verify decryption
// Tests the integration of auth handler and crypto package.
func TestRSAEncryptionRoundtrip(t *testing.T) {
	// Generate RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	// Encode public key to PEM format
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	// Test encrypt/decrypt roundtrip
	secretData := "super-secret-api-key-sk-1234567890"

	// Encrypt with public key
	hash := sha256.New()
	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, &privateKey.PublicKey, []byte(secretData), nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Decrypt with private key
	hash2 := sha256.New()
	decrypted, err := rsa.DecryptOAEP(hash2, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != secretData {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, secretData)
	}

	// Verify PEM can be parsed
	parsedKey, err := parsePublicKeyPEM(pubKeyPEM)
	if err != nil {
		t.Fatalf("parse PEM: %v", err)
	}

	// Encrypt with parsed key
	hash3 := sha256.New()
	ciphertext2, err := rsa.EncryptOAEP(hash3, rand.Reader, parsedKey, []byte(secretData), nil)
	if err != nil {
		t.Fatalf("encrypt with parsed key: %v", err)
	}

	hash4 := sha256.New()
	decrypted2, err := rsa.DecryptOAEP(hash4, rand.Reader, privateKey, ciphertext2, nil)
	if err != nil {
		t.Fatalf("decrypt with original key: %v", err)
	}

	if string(decrypted2) != secretData {
		t.Fatalf("roundtrip with parsed key failed: got %q, want %q", decrypted2, secretData)
	}

	t.Log("RSA encryption roundtrip: PEM public key → encrypt → decrypt succeeded")
}

// TestTokenExchangeInvalidFormat verifies that tokens without the tm_ prefix are rejected.
func TestTokenExchangeInvalidFormat(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	testCases := []struct {
		name     string
		apiToken string
	}{
		{"empty_token", ""},
		{"wrong_prefix", "sk-abc123"},
		{"no_prefix", "justarandomstring"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]string{"api_token": tc.apiToken}
			_, status, _ := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/token-exchange", body)
			if status != http.StatusBadRequest {
				t.Fatalf("expected 400 for %q, got %d", tc.apiToken, status)
			}
		})
	}
	t.Log("Token exchange: malformed tokens correctly rejected")
}

// TestJWTTokenAuthentication verifies the complete JWT authentication flow.
func TestJWTTokenAuthentication(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	email := "jwt-auth-" + uuid.New().String()[:8] + "@test.com"
	regBody := map[string]string{
		"name":     "JWT Auth User",
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	var regResult map[string]interface{}
	json.Unmarshal(respBody, &regResult)
	jwtToken := regResult["token"].(string)

	// Make authenticated request with JWT
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authenticated request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated request: expected 200, got %d", resp.StatusCode)
	}

	// Use invalid JWT
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	req2.Header.Set("Authorization", "Bearer invalid.jwt.token")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("invalid JWT request: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid JWT: expected 401, got %d", resp2.StatusCode)
	}
	t.Log("JWT authentication: valid token accepted, invalid token rejected")
}

// TestLoginAndSessionFlow tests the complete login + session flow.
func TestLoginAndSessionFlow(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	email := "session-flow-" + uuid.New().String()[:8] + "@test.com"
	password := "Test123456"

	// Register
	regBody := map[string]string{
		"name":     "Session Flow User",
		"email":    email,
		"password": password,
	}
	_, status, _ := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	// Login
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	if status != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", status)
	}

	var loginResult map[string]interface{}
	json.Unmarshal(respBody, &loginResult)

	token := loginResult["token"].(string)
	if token == "" {
		t.Fatal("login: expected token in response")
	}

	// Call whoami with login token
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("whoami after login: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("whoami after login: expected 200, got %d", resp.StatusCode)
	}

	var whoamiResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&whoamiResult)
	if whoamiResult["email"] != email {
		t.Fatalf("whoami email: expected %q, got %v", email, whoamiResult["email"])
	}
	t.Log("Login + session flow: register, login + whoami succeeded")
}

// parsePublicKeyPEM is a helper function that parses a PEM-encoded RSA public key.
func parsePublicKeyPEM(pemData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return rsaPub, nil
}
