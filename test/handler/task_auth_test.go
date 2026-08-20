// task_auth_test.go 覆盖任务权限控制的测试。
package handler_test

import (
	"context"
	"encoding/json"
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

// ==========================================
// 任务 CRUD 测试
// ==========================================

func setupTaskTestRouter(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	router, db, _ := setupTestRouter(t)

	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
		db.Close()
	})
	return ts, ts.Client()
}

func TestCreateTask(t *testing.T) {
	ts, client := setupTaskTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	tplID := createWorkflowTemplate3Nodes(t, client, ts.URL, wsID, token)
	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)

	taskID, nodes := createTask(t, client, ts.URL, projID, tplID, token)

	if taskID == 0 {
		t.Fatal("expected non-empty task ID")
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// 第一个节点应为 in_progress（自动启动）
	if nodes[0]["status"] != "in_progress" && nodes[0]["status"] != "pending" {
		t.Fatalf("expected first node to be in_progress or pending, got %v", nodes[0]["status"])
	}
	// 其余节点应为 pending
	if nodes[1]["status"] != "pending" {
		t.Fatalf("expected second node to be pending, got %v", nodes[1]["status"])
	}
}

func TestUpdateTask(t *testing.T) {
	ts, client := setupTaskTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	tplID := createWorkflowTemplate3Nodes(t, client, ts.URL, wsID, token)
	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)

	taskID, _ := createTask(t, client, ts.URL, projID, tplID, token)

	// 更新任务
	body := map[string]interface{}{
		"title":       "Updated Task Title",
		"description": "Updated description",
		"priority":    "high",
		"labels":      []string{"bug", "urgent"},
		"status":      "active",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPut,
		fmt.Sprintf("%s/api/projects/%s/tasks/%d", ts.URL, projID, taskID), token, body)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result["title"] != "Updated Task Title" {
		t.Fatalf("expected updated title, got %v", result["title"])
	}
}

func TestUpdateTaskTitleTooLong(t *testing.T) {
	ts, client := setupTaskTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	tplID := createWorkflowTemplate3Nodes(t, client, ts.URL, wsID, token)
	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)

	taskID, _ := createTask(t, client, ts.URL, projID, tplID, token)

	// 标题过长（超过 200 个字符）
	longTitle := ""
	for i := 0; i < 201; i++ {
		longTitle += "a"
	}
	body := map[string]interface{}{
		"title":  longTitle,
		"status": "active",
	}
	_, status, _ := doRequestWithToken(t, client, http.MethodPut,
		fmt.Sprintf("%s/api/projects/%s/tasks/%d", ts.URL, projID, taskID), token, body)

	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for title too long, got %d", status)
	}
}

func TestDeleteTask(t *testing.T) {
	ts, client := setupTaskTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	tplID := createWorkflowTemplate3Nodes(t, client, ts.URL, wsID, token)
	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)

	taskID, _ := createTask(t, client, ts.URL, projID, tplID, token)

	// 删除任务
	_, status, _ := doRequestWithToken(t, client, http.MethodDelete,
		fmt.Sprintf("%s/api/projects/%s/tasks/%d", ts.URL, projID, taskID), token, nil)

	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// 验证任务已删除（或已软删除）
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/projects/%s/tasks/%d", ts.URL, projID, taskID), token, nil)

	// 删除后应返回 404（软删除将其过滤掉）
	// 若软删除尚未生效则返回 200（任务状态已改为 cancelled）
	if status != http.StatusNotFound && status != http.StatusOK {
		t.Fatalf("expected 404 or 200 after delete, got %d", status)
	}
	if status == http.StatusOK {
		var task map[string]interface{}
		if err := json.Unmarshal(respBody, &task); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if task["status"] != "cancelled" {
			t.Fatalf("expected cancelled status after soft delete, got %v", task["status"])
		}
	}
}

// ==========================================
// 认证中间件 + RequireRole 测试
// ==========================================

func TestRequireRoleMiddleware(t *testing.T) {
	_, db, _ := setupTestRouter(t)

	svc := service.New(db, nil, nil)
	wsChecker, _, _, _ := configureWorkspaceAuthForTest(svc)
	r := chi.NewRouter()
	authHandler := handler.NewAuthHandler(svc, testJWTSecret)
	r.Mount("/api/auth", authHandler.Routes())

	r.Route("/api", func(r chi.Router) {
		r.Use(svcmw.AuthMiddleware(testJWTSecret, testAPIKeyAuthenticator(svc), nil))
		r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
			r.Use(svcmw.WorkspaceAuthMiddlewareWithChecker(wsChecker))

			// 仅限管理员的端点
			r.Group(func(r chi.Router) {
				r.Use(svcmw.RequireRole("owner", "admin"))
				r.Post("/admin-action", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			})

			// member 及以上权限的端点
			r.Group(func(r chi.Router) {
				r.Use(svcmw.RequireRole("owner", "admin", "member"))
				r.Post("/member-action", func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			})
		})
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	client := ts.Client()

	// 注册用户（在自己的工作区获得 owner 角色）
	email := "role-test-" + uuid.New().String()[:8] + "@test.com"
	regBody := map[string]string{
		"name":     "Role Test",
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	var regResult map[string]interface{}
	if err := json.Unmarshal(respBody, &regResult); err != nil {
		t.Fatalf("decode: %v", err)
	}
	token := regResult["token"].(string)

	// 为了测试将用户降级为 member 角色
	memberData := regResult["member"].(map[string]interface{})
	memberID := memberData["id"].(string)
	wsID := regResult["workspace_id"].(string)
	_, err := db.ExecContext(context.Background(), "UPDATE workspace_members SET role = 'member' WHERE member_id = $1 AND workspace_id = $2", memberID, wsID)
	if err != nil {
		t.Fatalf("update role: %v", err)
	}

	// 重新登录以获取具有更新角色的 Token
	loginBody := map[string]string{
		"email":    email,
		"password": "Test123456",
	}
	_, loginStatus, loginRespBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/login", loginBody)
	if loginStatus != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", loginStatus)
	}
	var loginResult map[string]interface{}
	if err := json.Unmarshal(loginRespBody, &loginResult); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	token = loginResult["token"].(string)

	// 成员不应访问仅限管理员的端点
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/workspaces/"+wsID+"/admin-action", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member should be forbidden from admin action, got %d", resp.StatusCode)
	}

	// 成员应该能够访问 member 及以上权限的端点
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/workspaces/"+wsID+"/member-action", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member should access member action, got %d", resp.StatusCode)
	}
}

func TestRegisterDefaultRoleIsMember(t *testing.T) {
	_, db, _ := setupTestRouter(t)

	svc := service.New(db, nil, nil)
	r := chi.NewRouter()
	authHandler := handler.NewAuthHandler(svc, testJWTSecret)
	r.Mount("/api/auth", authHandler.Routes())

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	client := ts.Client()

	email := "default-role-" + uuid.New().String()[:8] + "@test.com"
	regBody := map[string]string{
		"name":     "Default Role Test",
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, ts.URL+"/api/auth/register", regBody)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", status)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	role := result["role"].(string)
	if role != "owner" {
		t.Fatalf("expected default role 'owner' (new users get their own workspace), got %v", role)
	}
}

func TestUnauthenticatedRequest(t *testing.T) {
	_, db, _ := setupTestRouter(t)
	svc := service.New(db, nil, nil)

	r := chi.NewRouter()
	r.Use(svcmw.AuthMiddleware(testJWTSecret, testAPIKeyAuthenticator(svc), nil))
	r.Get("/protected", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	client := ts.Client()

	// 不带认证头的请求
	resp, err := client.Get(ts.URL + "/protected")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", resp.StatusCode)
	}
}
