// auth_test.go 覆盖认证接口（登录/注册/登出等）的测试。
package handler_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/service"
)

// setupAuthTestRouter 创建包含认证处理器和角色保护路由的测试路由器。
func setupAuthTestRouter(t *testing.T) (chi.Router, *httptest.Server) {
	t.Helper()

	_, db, _ := setupTestRouter(t)

	svc := service.New(db, nil, nil)
	r := chi.NewRouter()

	// 认证路由（公开）
	authHandler := handler.NewAuthHandler(svc, testJWTSecret)
	r.Mount("/api/auth", authHandler.Routes())

	// 已认证路由
	r.Route("/api", func(r chi.Router) {
		r.Use(svcmw.AuthMiddleware(testJWTSecret, testAPIKeyAuthenticator(svc), nil))

		// 工作区管理 - 仅管理员
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireRole("admin"))
			r.Delete("/workspaces/{id}", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		})

		// 任务操作 - 成员及以上
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireRole("member"))
			r.Post("/tasks", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		})

		// 只读 - 观察者及以上
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireRole("viewer"))
			r.Get("/data", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		})
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return r, ts
}

func TestRegisterWithPassword(t *testing.T) {
	_, ts := setupAuthTestRouter(t)
	client := ts.Client()

	email := "auth-test-" + uuid.New().String()[:8] + "@test.com"

	// 使用密码注册
	body := map[string]string{
		"name":     "Auth Test User",
		"email":    email,
		"password": "Test123456",
	}
	resp, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", body)
	_ = resp

	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result["token"] == nil || result["token"] == "" {
		t.Fatal("expected token in response")
	}

	member := result["member"].(map[string]interface{})
	if member["email"] != email {
		t.Fatalf("expected email %s, got %v", email, member["email"])
	}
}

func TestRegisterWithoutPassword(t *testing.T) {
	_, ts := setupAuthTestRouter(t)
	client := ts.Client()

	body := map[string]string{
		"name":  "No Password User",
		"email": "nopass@test.com",
	}
	_, status, _ := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", body)

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing password, got %d", status)
	}
}

func TestLoginWithPassword(t *testing.T) {
	_, ts := setupAuthTestRouter(t)
	client := ts.Client()

	email := "login-test-" + uuid.New().String()[:8] + "@test.com"
	password := "Test123456"

	// 先注册
	regBody := map[string]string{
		"name":     "Login Test User",
		"email":    email,
		"password": password,
	}
	_, status, _ := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	// 使用正确密码登录
	loginBody := map[string]string{
		"email":    email,
		"password": password,
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	if status != http.StatusOK {
		t.Fatalf("login: expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result["token"] == nil || result["token"] == "" {
		t.Fatal("expected token in login response")
	}
}

func TestLoginWithWrongPassword(t *testing.T) {
	_, ts := setupAuthTestRouter(t)
	client := ts.Client()

	email := "wrong-pass-" + uuid.New().String()[:8] + "@test.com"

	// 注册
	regBody := map[string]string{
		"name":     "Wrong Pass User",
		"email":    email,
		"password": "Correct123",
	}
	_, status, _ := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	// 使用错误密码登录
	loginBody := map[string]string{
		"email":    email,
		"password": "wrong123",
	}
	_, status, _ = doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong password, got %d", status)
	}
}

func TestJWTDoesNotContainWorkspaceOrRole(t *testing.T) {
	_, ts := setupAuthTestRouter(t)
	client := ts.Client()

	email := "claims-test-" + uuid.New().String()[:8] + "@test.com"
	body := map[string]string{
		"name":     "Claims Test User",
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", body)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	var regResult map[string]interface{}
	if err := json.Unmarshal(respBody, &regResult); err != nil {
		t.Fatalf("decode: %v", err)
	}
	token := regResult["token"].(string)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode token payload: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if _, ok := claims["workspace_id"]; ok {
		t.Fatalf("JWT must not contain workspace_id: %v", claims)
	}
	if _, ok := claims["role"]; ok {
		t.Fatalf("JWT must not contain role: %v", claims)
	}
}
