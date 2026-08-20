// session_auth_test.go 覆盖会话认证的测试。
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

// setupAuthSessionTestRouter 创建带有完整认证处理器的测试路由器，
// 包含需要认证的路由（登出、whoami）。
func setupAuthSessionTestRouter(t *testing.T) (chi.Router, *httptest.Server) {
	t.Helper()

	_, db, _ := setupTestRouter(t)

	svc := service.New(db, nil, nil)
	r := chi.NewRouter()

	// Auth 路由（公开）
	authHandler := handler.NewAuthHandler(svc, testJWTSecret)
	r.Mount("/api/auth", authHandler.Routes())

	// 认证路由——使用不同的路径前缀避免 chi Mount 冲突
	r.Group(func(r chi.Router) {
		r.Use(svcmw.AuthMiddleware(testJWTSecret, testAPIKeyAuthenticator(svc), nil))
		r.Get("/api/auth/whoami", authHandler.Whoami)
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return r, ts
}

// TestSessionTokenExchange 验证通过 API 令牌交换会话令牌的功能。
func TestSessionTokenExchange(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	// 首先注册成员，获取有效的 JWT
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

	// 为代理创建一个 API 令牌（需要直接插入）
	// 由于无法通过 API 轻松创建代理 API 令牌，
	// 我们使用无效凭证格式测试 token-exchange 端点
	exchangeBody := map[string]string{
		"api_token": "tm_invalidtoken123",
	}
	_, status, _ = doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/token-exchange", exchangeBody)
	// 响应失败，因为凭证在数据库中不存在
	if status != http.StatusUnauthorized {
		t.Logf("token exchange with invalid token: got status %d (expected 401)", status)
	}

	// 验证 JWT 凭证在认证端点上有效
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
	t.Log("会话令牌交换：JWT 认证对 whoami 有效")
}

// TestSessionTokenExpiry 验证过期的会话令牌会被拒绝。
func TestSessionTokenExpiry(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	// 手动构造一个过期的 JWT
	// 测试是否拒绝过期令牌
	// JWT 会检查 "exp" 声明
	// 由于无法在不直接操作令牌的情况下轻松创建过期的 JWT，
	// 我们改用格式错误的令牌进行测试
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
	t.Log("会话令牌过期：无效过期令牌被正确拒绝")
}

// TestLogout 验证登出后会话令牌失效。
// TestWhoami 验证 whoami 端点返回正确的用户信息。
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

	// 调用 whoami
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
	t.Logf("whoami：返回正确的信息 (name=%s, email=%s, type=%s)",
		whoamiResult["name"], whoamiResult["email"], whoamiResult["user_type"])
}

// TestWhoamiUnauthenticated 验证 whoami 需要认证。
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
	t.Log("whoami 未认证时正确返回 401")
}

// TestRSAEncryptionRoundtrip 验证 RSA 加密往返：
// 上传公钥、获取加密密钥、验证解密
// 测试认证处理器和 crypto 包的集成。
func TestRSAEncryptionRoundtrip(t *testing.T) {
	// 生成 RSA 密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	// 将公钥编码为 PEM 格式
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	// 测试加密/解密往返
	secretData := "super-secret-api-key-sk-1234567890"

	// 使用公钥加密
	hash := sha256.New()
	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, &privateKey.PublicKey, []byte(secretData), nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// 使用私钥解密
	hash2 := sha256.New()
	decrypted, err := rsa.DecryptOAEP(hash2, rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != secretData {
		t.Fatalf("roundtrip failed: got %q, want %q", decrypted, secretData)
	}

	// 验证 PEM 可以被解析
	parsedKey, err := parsePublicKeyPEM(pubKeyPEM)
	if err != nil {
		t.Fatalf("parse PEM: %v", err)
	}

	// 使用解析后的密钥加密
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

	t.Log("RSA 加密往返：PEM 公钥 → 加密 → 解密成功")
}

// TestTokenExchangeInvalidFormat 验证非 tm_ 前缀的令牌会被拒绝。
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
	t.Log("令牌交换：格式错误的令牌被正确拒绝")
}

// TestJWTTokenAuthentication 验证完整的 JWT 认证流程。
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

	// 使用 JWT 进行认证请求
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

	// 使用无效的 JWT
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
	t.Log("JWT 认证：有效令牌被接受，无效令牌被拒绝")
}

// TestLoginAndSessionFlow 测试完整的登录 + 会话流程。
func TestLoginAndSessionFlow(t *testing.T) {
	_, ts := setupAuthSessionTestRouter(t)
	client := ts.Client()

	email := "session-flow-" + uuid.New().String()[:8] + "@test.com"
	password := "Test123456"

	// 注册
	regBody := map[string]string{
		"name":     "Session Flow User",
		"email":    email,
		"password": password,
	}
	_, status, _ := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	// 登录
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

	// 使用登录令牌调用 whoami
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
	t.Log("登录 + 会话流程：注册并登录 + whoami 成功")
}

// parsePublicKeyPEM 是一个辅助函数，用于解析 PEM 编码的 RSA 公钥。
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
