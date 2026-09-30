// helpers_test.go provides test helpers for the handler layer.
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

// TestMain ensures the test database exists before running tests.
func TestMain(m *testing.M) {
	// Ensure test database exists
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}

	// Initialize AES encryption key (required for env_vars encryption)
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

// getTestDSN returns the test database DSN string.
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
	// checker is passed to the caller via return value, no longer set globally

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

// setupTestRouter creates a test router with all handler routes and auth middleware. // Uses production router configuration (server.NewRouter) to ensure test routes match production.
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

// setupTestRouterWithClock creates a test router with a custom clock (for testing time-related logic). // Uses production router configuration (server.NewRouter) to ensure test routes match production.
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

// registerTestUser creates a test user via the register API and returns a JWT Token and workspace ID.
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

	// Register cleanup for workspace and member created during registration
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

// extractMemberIDFromJWT decodes the JWT Token and extracts the sub (member ID) claim.
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

// doRequestWithToken executes an HTTP request using a JWT Bearer Token.
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

// doRequestWithAPIKey executes an HTTP request using an API key (X-API-Key header). // Used for agent authentication scenarios where the agent uses its API Token directly.
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

// createWorkspace creates a workspace via the API and returns its ID.
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

	// Register cleanup for workspace created via API
	t.Cleanup(func() {
		if db, err := sql.Open("pgx", testdb.GetTestDSN()); err == nil {
			defer db.Close()
			_ = testdb.DeleteWorkspace(db, wsID)
		}
	})

	return wsID
}

// createProject creates a project in the specified workspace via the API and returns the project ID.
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

// createWorkflowTemplate3Nodes creates a workflow template with 3 nodes (code → review → deploy).
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

// createWorkflowTemplate2Nodes creates a workflow template with 2 nodes (code + review).
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

// grantAgentPermission grants a permission to an agent directly via the service layer (the original HTTP endpoint /permissions has been removed).
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
	// granted_by uses the real member ID corresponding to the token (to avoid foreign key constraint failure)
	grantedBy := uuid.MustParse(extractMemberIDFromJWT(t, token))
	if _, err := permSvc.Grant(context.Background(), agentUUID, permission, "*", nil, grantedBy); err != nil {
		t.Fatalf("grantAgentPermission(%s): %v", permission, err)
	}
}

// grantAgentAllTaskPermissions grants all task-related permissions to an agent.
func grantAgentAllTaskPermissions(t *testing.T, client *http.Client, baseURL, wsID, agentID, token string) {
	t.Helper()
	for _, perm := range []string{"task:claim", "task:execute", "task:approve", "task:reject", "task:comment"} {
		grantAgentPermission(t, client, baseURL, wsID, agentID, perm, token)
	}
}

// testEnv carries the HTTP fixtures shared by handler-test helpers.
// Bundling them keeps helper signatures stable when the fixture set changes.
type testEnv struct {
	Client  *http.Client
	BaseURL string
	Token   string
}

func setProjectDefaultWorkflow(t *testing.T, env testEnv, wsID, projID string) {
	t.Helper()

	body := map[string]interface{}{
		"name":              "test-project",
		"description":       "test project",
		"status":            "active",
		"max_review_cycles": 3,
	}
	_, _, _ = doRequestWithToken(t, env.Client, http.MethodPut,
		fmt.Sprintf("%s/api/workspaces/%s/projects/%s", env.BaseURL, wsID, projID), env.Token, body)
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

// createDaemonForAgent simulates the daemon-first flow for an existing agent:
// the web creates a daemon (one-time td_ token), then the machine API registers
// reporting the agent by name — the upsert binds agents.daemon_id to the daemon.
// Returns (daemonID, daemonToken).
func createDaemonForAgent(t *testing.T, client *http.Client, baseURL, workspaceID, memberToken, agentID string) (string, string) {
	t.Helper()

	// fetch the agent's name/provider for the report
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		baseURL+"/api/workspaces/"+workspaceID+"/agents/"+agentID, memberToken, nil)
	if status != http.StatusOK {
		t.Fatalf("createDaemonForAgent: get agent failed: %d, body: %s", status, respBody)
	}
	var agent map[string]interface{}
	if err := json.Unmarshal(respBody, &agent); err != nil {
		t.Fatalf("createDaemonForAgent: decode agent: %v", err)
	}
	name, _ := agent["name"].(string)
	provider, _ := agent["provider"].(string)
	if name == "" {
		t.Fatalf("createDaemonForAgent: agent %s has no name", agentID)
	}

	// web creates the daemon
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost,
		baseURL+"/api/workspaces/"+workspaceID+"/daemons", memberToken,
		map[string]string{"name": "box-" + uuid.NewString()[:8]})
	if status != http.StatusCreated {
		t.Fatalf("createDaemonForAgent: create daemon: %d, body: %s", status, respBody)
	}
	var created struct {
		ID          string `json:"id"`
		DaemonToken string `json:"daemon_token"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("createDaemonForAgent: decode daemon: %v", err)
	}

	// machine registers, reporting the agent by name
	registerBody := map[string]interface{}{
		"device_name": "test-box",
		"version":     "1.0.0",
		"providers":   []map[string]interface{}{{"provider": provider, "version": "1.0", "installed": true}},
		"agents":      []map[string]string{{"name": name, "provider": provider}},
	}
	_, status, respBody = doRequestWithAPIKey(t, client, http.MethodPost,
		baseURL+"/api/daemons/register", created.DaemonToken, registerBody)
	if status != http.StatusOK {
		t.Fatalf("createDaemonForAgent: register: %d, body: %s", status, respBody)
	}
	return created.ID, created.DaemonToken
}

// daemonHeartbeat sends a daemon heartbeat with the td_ token.
func daemonHeartbeat(t *testing.T, client *http.Client, baseURL, daemonToken string) {
	t.Helper()
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost,
		baseURL+"/api/daemons/heartbeat", daemonToken, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("daemonHeartbeat: expected 200, got %d, body: %s", status, respBody)
	}
}

// pendingNodesForTasks collects the pending nodes of the given tasks
// (member view), replacing the old runtime sync discovery in tests.
func pendingNodesForTasks(t *testing.T, client *http.Client, baseURL, memberToken string, taskIDs ...int32) []map[string]interface{} {
	t.Helper()
	var pending []map[string]interface{}
	for _, taskID := range taskIDs {
		_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
			fmt.Sprintf("%s/api/tasks/%d/nodes", baseURL, taskID), memberToken, nil)
		if status != http.StatusOK {
			t.Fatalf("pendingNodesForTasks: list nodes of task %d: %d, body: %s", taskID, status, respBody)
		}
		var nodes []map[string]interface{}
		if err := json.Unmarshal(respBody, &nodes); err != nil {
			t.Fatalf("pendingNodesForTasks: decode nodes: %v", err)
		}
		for _, n := range nodes {
			if n["status"] == "pending" {
				pending = append(pending, n)
			}
		}
	}
	return pending
}

func dbQueries(t *testing.T, db *sql.DB) *dbgen.Queries {
	t.Helper()
	return dbgen.New(db)
}
