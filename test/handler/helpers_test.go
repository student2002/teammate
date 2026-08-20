// helpers_test.go 覆盖 handler 层辅助函数的测试。
package handler_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/crypto"
	dbgen "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/server"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// TestMain 确保测试数据库存在后再运行测试。
func TestMain(m *testing.M) {
	// 确保测试数据库存在
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}

	// 初始化 AES 加密密钥（env_vars 加密所需）
	if err := crypto.SetEncryptionKey([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set encryption key: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}

const testJWTSecret = "test-jwt-secret-for-handler-tests"

func testAPIKeyAuthenticator(svc *service.Service) svcmw.APIKeyAuthenticator {
	authSvc := service.NewAuthService(svc, testJWTSecret)
	return func(ctx context.Context, apiKey string) (svcmw.AuthClaims, error) {
		result, err := authSvc.AuthenticateAPIKey(ctx, apiKey)
		if err != nil {
			return svcmw.AuthClaims{}, err
		}
		return svcmw.AuthClaims{
			UserID:   result.UserID,
			UserType: result.UserType,
		}, nil
	}
}

// getTestDSN 获取测试数据库 DSN 字符串。
func getTestDSN() string {
	return testdb.GetTestDSN()
}

func configureWorkspaceAuthForTest(svc *service.Service) (svcmw.WorkspaceAccessCheckerFunc, svcmw.ProjectAccessCheckerFunc, svcmw.TaskWorkspaceCheckerFunc, svcmw.NodeWorkspaceCheckerFunc) {
	workspaceAccessChecker := func(ctx context.Context, userID uuid.UUID, userType string, workspaceID uuid.UUID) (string, error) {
		switch userType {
		case "member":
			wm, err := svc.Store.GetWorkspaceMember(ctx, types.GetWorkspaceMemberParams{WorkspaceID: workspaceID.String(), MemberID: userID.String()})
			if err != nil {
				return "", err
			}
			return wm.Role, nil
		case "agent":
			agent, err := svc.Store.GetAgent(ctx, userID)
			if err != nil {
				return "", err
			}
			if agent.WorkspaceID != workspaceID.String() {
				return "", fmt.Errorf("agent workspace mismatch")
			}
			return "agent", nil
		default:
			return "", fmt.Errorf("unknown user type")
		}
	}
	// checker 通过返回值传递给调用方，不再设置全局

	projSvc := service.NewProjectService(svc)
	projectAccessChecker := func(ctx context.Context, userID uuid.UUID, userType string, projectID uuid.UUID) (svcmw.WorkspaceContext, error) {
		project, err := svc.Store.GetProject(ctx, projectID)
		if err != nil {
			return svcmw.WorkspaceContext{}, err
		}
		role, err := workspaceAccessChecker(ctx, userID, userType, uuid.MustParse(project.WorkspaceID))
		if err != nil {
			return svcmw.WorkspaceContext{}, err
		}
		if userType == "agent" {
			if err := projSvc.CheckAgentProjectAccess(ctx, userID, projectID); err != nil {
				return svcmw.WorkspaceContext{}, err
			}
		} else if err := projSvc.CheckMemberProjectAccess(ctx, userID, projectID, role); err != nil {
			return svcmw.WorkspaceContext{}, err
		}
		return svcmw.WorkspaceContext{WorkspaceID: uuid.MustParse(project.WorkspaceID), Role: role}, nil
	}

	taskWorkspaceChecker := func(ctx context.Context, taskID int32) (interface{}, uuid.UUID, error) {
		taskSvc := service.NewTaskService(svc)
		task, err := taskSvc.Get(ctx, taskID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		project, err := svc.Store.GetProject(ctx, uuid.MustParse(task.ProjectID))
		if err != nil {
			return nil, uuid.Nil, err
		}
		return task, uuid.MustParse(project.WorkspaceID), nil
	}

	nodeWorkspaceChecker := func(ctx context.Context, nodeID uuid.UUID) (interface{}, uuid.UUID, error) {
		node, err := svc.Store.GetTaskNode(ctx, nodeID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		taskSvc := service.NewTaskService(svc)
		task, err := taskSvc.Get(ctx, node.TaskID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		project, err := svc.Store.GetProject(ctx, uuid.MustParse(task.ProjectID))
		if err != nil {
			return nil, uuid.Nil, err
		}
		return node, uuid.MustParse(project.WorkspaceID), nil
	}

	return workspaceAccessChecker, projectAccessChecker, taskWorkspaceChecker, nodeWorkspaceChecker
}

// setupTestRouter 创建带有所有处理器路由和认证中间件的测试路由器。// 使用生产路由器配置（server.NewRouter），确保测试路由与生产路由一致。
func setupTestRouter(t *testing.T) (chi.Router, *sql.DB, *dbgen.Queries) {
	t.Helper()

	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping db: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	q := dbgen.New(db)
	router := server.NewRouter(server.RouterDeps{
		Config: server.Config{
			JWTSecret:      testJWTSecret,
			AllowedOrigins: "*",
		},
		DB:      db,
		Redis:   nil,
		Hub:     ws.NewHub(nil),
		Gateway: ws.NewGateway(nil),
	})

	return router, db, q
}

// setupTestRouterWithClock 创建使用自定义时钟的测试路由器（用于测试时间相关逻辑）。// 使用生产路由器配置（server.NewRouter），确保测试路由与生产路由一致。
func setupTestRouterWithClock(t *testing.T, c clock.Clock) (chi.Router, *sql.DB) {
	t.Helper()

	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping db: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	router := server.NewRouter(server.RouterDeps{
		Config: server.Config{
			JWTSecret:      testJWTSecret,
			AllowedOrigins: "*",
		},
		DB:      db,
		Redis:   nil,
		Hub:     ws.NewHub(nil),
		Gateway: ws.NewGateway(nil),
		Clock:   c,
	})

	return router, db
}

// registerTestUser 通过注册 API 创建测试用户并返回 JWT Token 和工作区 ID。
func registerTestUser(t *testing.T, client *http.Client, baseURL string) (string, string) {
	t.Helper()

	email := "test-" + uuid.New().String()[:8] + "@test.com"
	body := map[string]string{
		"name":     "Test User",
		"email":    email,
		"password": "Test123456",
	}
	_, status, respBody := doRequest(t, client, http.MethodPost, baseURL+"/api/auth/register", body)
	if status != http.StatusCreated {
		t.Fatalf("registerTestUser: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	token, ok := result["token"].(string)
	if !ok || token == "" {
		t.Fatalf("registerTestUser: expected token in response, got %v", result)
	}

	wsID, ok := result["workspace_id"].(string)
	if !ok || wsID == "" {
		t.Fatalf("registerTestUser: expected workspace_id in register response, got %v", result)
	}

	// 为注册创建的工作区和成员注册清理
	memberID := extractMemberIDFromJWT(t, token)
	t.Cleanup(func() {
		if db, err := sql.Open("pgx", testdb.GetTestDSN()); err == nil {
			defer db.Close()
			_ = testdb.DeleteWorkspace(db, wsID)
			_ = testdb.DeleteMember(db, memberID)
		}
	})

	return token, wsID
}

// extractMemberIDFromJWT 解码 JWT Token 并提取 sub（成员 ID）声明。
func extractMemberIDFromJWT(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT token format")
	}
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("base64 decode JWT payload: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	sub, ok := claims["user_id"].(string)
	if !ok {
		t.Fatalf("user_id not found in JWT claims: %v", claims)
	}
	return sub
}

func doRequest(t *testing.T, client *http.Client, method, url string, body interface{}) (*http.Response, int, []byte) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return resp, resp.StatusCode, respBody
}

// doRequestWithToken 使用 JWT Bearer Token 执行 HTTP 请求。
func doRequestWithToken(t *testing.T, client *http.Client, method, url, token string, body interface{}) (*http.Response, int, []byte) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return resp, resp.StatusCode, respBody
}

// doRequestWithAPIKey 使用 API 密钥（X-API-Key 头部）执行 HTTP 请求。// 用于代理认证场景，代理直接使用其 API Token。
func doRequestWithAPIKey(t *testing.T, client *http.Client, method, url, apiKey string, body interface{}) (*http.Response, int, []byte) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return resp, resp.StatusCode, respBody
}

// ---------- Helpers ----------

// createWorkspace 通过 API 创建工作区并返回其 ID。
func createWorkspace(t *testing.T, client *http.Client, baseURL, token string) string {
	t.Helper()

	uid := uuid.New().String()[:8]
	body := map[string]string{
		"name":         "ws-" + uid,
		"description":  "test workspace",
		"issue_prefix": "T" + uid[:4],
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, baseURL+"/api/workspaces", token, body)
	if status != http.StatusCreated {
		t.Fatalf("createWorkspace: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	wsID := result["id"].(string)

	// 为通过 API 创建工作区注册清理
	t.Cleanup(func() {
		if db, err := sql.Open("pgx", testdb.GetTestDSN()); err == nil {
			defer db.Close()
			_ = testdb.DeleteWorkspace(db, wsID)
		}
	})

	return wsID
}

// createProject 通过 API 在指定工作区下创建项目并返回项目 ID。
func createProject(t *testing.T, client *http.Client, baseURL, wsID, token string) string {
	t.Helper()

	body := map[string]interface{}{
		"name":              "proj-" + uuid.New().String()[:8],
		"description":       "test project",
		"status":            "active",
		"repo_url":          "https://github.com/test/repo.git",
		"max_review_cycles": 3,
	}
	url := fmt.Sprintf("%s/api/workspaces/%s/projects", baseURL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, url, token, body)
	if status != http.StatusCreated {
		t.Fatalf("createProject: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	return result["id"].(string)
}

// createWorkflowTemplate3Nodes 创建包含 3 个节点（编码→审查→部署）的工作流模板。
func createWorkflowTemplate3Nodes(t *testing.T, client *http.Client, baseURL, wsID, token string) string {
	t.Helper()

	body := map[string]interface{}{
		"name":        "flow-" + uuid.New().String()[:8],
		"description": "3-node workflow: code -> review -> deploy",
		"nodes": []map[string]interface{}{
			{
				"name":            "code",
				"description":     "write code",
				"sort_order":      1,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
			{
				"name":            "review",
				"description":     "review code",
				"sort_order":      2,
				"node_type":       "review",
				"assignee_type":   "any_agent",
				"timeout_minutes": 30,
			},
			{
				"name":            "deploy",
				"description":     "deploy code",
				"sort_order":      3,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
		},
	}
	url := fmt.Sprintf("%s/api/workspaces/%s/workflows", baseURL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, url, token, body)
	if status != http.StatusCreated {
		t.Fatalf("createWorkflowTemplate3Nodes: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode workflow template: %v", err)
	}
	tplMap := result["template"].(map[string]interface{})
	return tplMap["id"].(string)
}

// createWorkflowTemplate2Nodes 创建包含 2 个节点（编码+审查）的工作流模板。
func createWorkflowTemplate2Nodes(t *testing.T, client *http.Client, baseURL, wsID, token string) string {
	t.Helper()

	body := map[string]interface{}{
		"name":        "flow-" + uuid.New().String()[:8],
		"description": "2-node workflow: code + review",
		"nodes": []map[string]interface{}{
			{
				"name":            "code",
				"description":     "write code",
				"sort_order":      1,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
			{
				"name":            "review",
				"description":     "review code",
				"sort_order":      2,
				"node_type":       "review",
				"assignee_type":   "any_agent",
				"timeout_minutes": 30,
			},
		},
	}
	url := fmt.Sprintf("%s/api/workspaces/%s/workflows", baseURL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, url, token, body)
	if status != http.StatusCreated {
		t.Fatalf("createWorkflowTemplate2Nodes: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode workflow template: %v", err)
	}
	tplMap := result["template"].(map[string]interface{})
	return tplMap["id"].(string)
}

func createAgent(t *testing.T, client *http.Client, baseURL, wsID, token string) (string, string) {
	t.Helper()

	body := map[string]interface{}{
		"name":         "agent-" + uuid.New().String()[:8],
		"provider":     "claude",
		"instructions": "test agent",
		"model":        "claude-3.5-sonnet",
		"extra_args":   []string{},
		"git_name":     "Test Agent",
		"git_email":    "agent@test.com",
	}
	url := fmt.Sprintf("%s/api/workspaces/%s/agents", baseURL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, url, token, body)
	if status != http.StatusCreated {
		t.Fatalf("createAgent: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	agentID := result["id"].(string)
	apiToken, _ := result["api_token"].(string)
	return agentID, apiToken
}

// grantAgentPermission 直接通过 service 层向代理授予权限（原 HTTP 端点 /permissions 已移除）。
func grantAgentPermission(t *testing.T, client *http.Client, baseURL, wsID, agentID, permission, token string) {
	t.Helper()

	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("grantAgentPermission: connect db: %v", err)
	}
	defer db.Close()

	svc := service.New(db, nil, nil)
	permSvc := service.NewAgentPermissionService(svc)
	agentUUID, err := uuid.Parse(agentID)
	if err != nil {
		t.Fatalf("grantAgentPermission: invalid agent id %q: %v", agentID, err)
	}
	// granted_by 使用 token 对应的真实 member ID（避免外键约束失败）
	grantedBy := uuid.MustParse(extractMemberIDFromJWT(t, token))
	if _, err := permSvc.Grant(context.Background(), agentUUID, permission, "*", nil, grantedBy); err != nil {
		t.Fatalf("grantAgentPermission(%s): %v", permission, err)
	}
}

// grantAgentAllTaskPermissions 向代理授予所有任务相关权限。
func grantAgentAllTaskPermissions(t *testing.T, client *http.Client, baseURL, wsID, agentID, token string) {
	t.Helper()
	for _, perm := range []string{"task:claim", "task:execute", "task:approve", "task:reject", "task:comment"} {
		grantAgentPermission(t, client, baseURL, wsID, agentID, perm, token)
	}
}

func setProjectDefaultWorkflow(t *testing.T, client *http.Client, baseURL, wsID, projID, tplID, token string) {
	t.Helper()

	body := map[string]interface{}{
		"name":                "test-project",
		"description":         "test project",
		"status":              "active",
		"default_workflow_id": tplID,
		"max_review_cycles":   3,
	}
	_, _, _ = doRequestWithToken(t, client, http.MethodPut,
		fmt.Sprintf("%s/api/workspaces/%s/projects/%s", baseURL, wsID, projID), token, body)
}

func createTask(t *testing.T, client *http.Client, baseURL, projID, tplID, token string) (int32, []map[string]interface{}) {
	t.Helper()

	taskBaseURL := fmt.Sprintf("%s/api/projects/%s/tasks", baseURL, projID)
	authorID := uuid.New().String()
	body := map[string]interface{}{
		"title":                "Test task",
		"description":          "Integration test task",
		"type":                 "task",
		"priority":             "medium",
		"author_type":          "agent",
		"author_id":            authorID,
		"workflow_template_id": tplID,
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, taskBaseURL, token, body)
	if status != http.StatusCreated {
		t.Fatalf("createTask: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	taskMap := result["task"].(map[string]interface{})
	taskID := int32(taskMap["id"].(float64))

	nodesRaw := result["nodes"].([]interface{})
	nodes := make([]map[string]interface{}, 0, len(nodesRaw))
	for _, n := range nodesRaw {
		nodes = append(nodes, n.(map[string]interface{}))
	}
	return taskID, nodes
}

func claimNode(t *testing.T, client *http.Client, baseURL string, taskID int32, nodeID, agentID, agentAPIToken string) map[string]interface{} {
	t.Helper()

	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", baseURL, taskID)
	body := map[string]interface{}{
		"agent_id": agentID,
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/claim", agentAPIToken, body)
	if status != http.StatusOK {
		t.Fatalf("claimNode: expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode claimed node: %v", err)
	}
	return result
}

func approveNode(t *testing.T, client *http.Client, baseURL string, taskID int32, nodeID, operatorID, agentAPIToken string) map[string]interface{} {
	t.Helper()

	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", baseURL, taskID)
	body := map[string]interface{}{
		"operator_id":   operatorID,
		"operator_type": "agent",
		"comment":       "approved",
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/approve", agentAPIToken, body)
	if status != http.StatusOK {
		t.Fatalf("approveNode: expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode approved node: %v", err)
	}
	return result
}

func rejectNode(t *testing.T, client *http.Client, baseURL string, taskID int32, nodeID, operatorID, agentAPIToken string, targetNodeID *string) (int, map[string]interface{}) {
	t.Helper()

	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", baseURL, taskID)
	body := map[string]interface{}{
		"operator_id":   operatorID,
		"operator_type": "agent",
		"comment":       "rejected",
	}
	if targetNodeID != nil {
		body["target_node_id"] = *targetNodeID
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/reject", agentAPIToken, body)

	var result map[string]interface{}
	if status == http.StatusOK {
		if err := json.Unmarshal(respBody, &result); err != nil {
			t.Fatalf("decode rejected node: %v", err)
		}
	}
	return status, result
}

func listTaskNodes(t *testing.T, client *http.Client, baseURL string, taskID int32, token string) []map[string]interface{} {
	t.Helper()

	nodesURL := fmt.Sprintf("%s/api/tasks/%d/nodes", baseURL, taskID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, nodesURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("listTaskNodes: expected 200, got %d, body: %s", status, respBody)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode task nodes: %v", err)
	}
	return result
}

func deleteTask(t *testing.T, client *http.Client, baseURL, projID string, taskID int32, token string) {
	t.Helper()

	taskBaseURL := fmt.Sprintf("%s/api/projects/%s/tasks", baseURL, projID)
	_, status, _ := doRequestWithToken(t, client, http.MethodDelete, fmt.Sprintf("%s/%d", taskBaseURL, taskID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("deleteTask: expected 204, got %d", status)
	}
}

func deleteWorkspace(t *testing.T, client *http.Client, baseURL, wsID, token string) {
	t.Helper()

	doRequestWithToken(t, client, http.MethodDelete, baseURL+"/api/workspaces/"+wsID, token, nil)
}

func addAgentToProject(t *testing.T, q *dbgen.Queries, projectID, agentID string) {
	t.Helper()
	_, err := q.CreateProjectMember(context.Background(), dbgen.CreateProjectMemberParams{
		ProjectID:  uuid.MustParse(projectID),
		MemberType: "agent",
		AgentID:    uuid.NullUUID{UUID: uuid.MustParse(agentID), Valid: true},
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("addAgentToProject: %v", err)
	}
}

func registerRuntime(t *testing.T, client *http.Client, baseURL, workspaceID, agentID, token string) string {
	t.Helper()

	body := map[string]interface{}{
		"agent_id": agentID,
		"provider": "claude",
		"version":  "1.0.0",
		"status":   "online",
	}
	url := baseURL + "/api/workspaces/" + workspaceID + "/runtimes"
	// 先尝试使用成员 Token；如果被拒绝（非管理员），调用方应
	// 改用 registerRuntimeWithAgentToken。
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, url, token, body)
	if status != http.StatusCreated {
		t.Fatalf("registerRuntime: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	return result["id"].(string)
}

func registerRuntimeWithAgentToken(t *testing.T, client *http.Client, baseURL, workspaceID, agentID, agentAPIToken string) string {
	t.Helper()

	body := map[string]interface{}{
		"agent_id": agentID,
		"provider": "claude",
		"version":  "1.0.0",
		"status":   "online",
	}
	url := baseURL + "/api/workspaces/" + workspaceID + "/runtimes"
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, url, agentAPIToken, body)
	if status != http.StatusCreated {
		t.Fatalf("registerRuntimeWithAgentToken: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	return result["id"].(string)
}

// syncRuntime 直接通过 service 层同步运行时（原 HTTP 端点 /runtimes/{id}/sync 已移除）。
func syncRuntime(t *testing.T, client *http.Client, baseURL, workspaceID, runtimeID, token string) map[string]interface{} {
	t.Helper()

	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("syncRuntime: connect db: %v", err)
	}
	defer db.Close()

	svc := service.New(db, nil, nil)
	rtSvc := service.NewRuntimeService(svc)
	runtimeUUID, err := uuid.Parse(runtimeID)
	if err != nil {
		t.Fatalf("syncRuntime: invalid runtime id %q: %v", runtimeID, err)
	}
	result, err := rtSvc.Sync(context.Background(), runtimeUUID)
	if err != nil {
		t.Fatalf("syncRuntime: %v", err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("syncRuntime: marshal result: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("syncRuntime: decode result: %v", err)
	}
	return m
}

func dbQueries(t *testing.T, db *sql.DB) *dbgen.Queries {
	t.Helper()
	return dbgen.New(db)
}
