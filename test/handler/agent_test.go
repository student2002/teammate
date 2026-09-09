// agent_test.go covers tests for Agent management API endpoints.
package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	dbgen "github.com/teammate/server/internal/db/generated"
	servercrypto "github.com/teammate/server/internal/crypto"
)

// ==========================================
// Agent CRUD Tests
// ==========================================

func setupAgentTestRouter(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	router, db, _ := setupTestRouter(t)

	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
		db.Close()
	})
	return ts, ts.Client()
}

func TestCreateAgent(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	body := map[string]interface{}{
		"name":         "Test Agent",
		"provider":     "claude",
		"instructions": "You are a test agent",
		"model":        "claude-sonnet-4",
		"git_name":     "Test Agent",
		"git_email":    "agent@test.com",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents", token, body)

	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Verify agent fields
	if result["name"] != "Test Agent" {
		t.Fatalf("expected name 'Test Agent', got %v", result["name"])
	}
	if result["provider"] != "claude" {
		t.Fatalf("expected provider 'claude', got %v", result["provider"])
	}
	if result["status"] != "offline" {
		t.Fatalf("expected status 'offline', got %v", result["status"])
	}

	// Verify API token was returned
	apiToken, ok := result["api_token"].(string)
	if !ok || apiToken == "" {
		t.Fatal("expected api_token to be returned on creation")
	}
	if !strings.HasPrefix(apiToken, "tm_") {
		t.Fatalf("expected api_token to start with 'tm_', got %s", apiToken[:10])
	}
}

func TestCreateAgentDefaultProvider(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	body := map[string]interface{}{
		"name":      "No Provider Agent",
		"git_name":  "Test Agent",
		"git_email": "agent@test.com",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents", token, body)

	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result["provider"] != "claude" {
		t.Fatalf("expected default provider 'claude', got %v", result["provider"])
	}
}

func TestListAgents(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	// Create 2 agents
	for i := 0; i < 2; i++ {
		body := map[string]interface{}{
			"name":      "Agent " + uuid.New().String()[:8],
			"provider":  "claude",
			"git_name":  "Test Agent",
			"git_email": "agent@test.com",
		}
		_, status, _ := doRequestWithToken(t, client, http.MethodPost,
			ts.URL+"/api/workspaces/"+wsID+"/agents", token, body)
		if status != http.StatusCreated {
			t.Fatalf("create agent %d: expected 201, got %d", i, status)
		}
	}

	// List agents
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents", token, nil)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var agents []map[string]interface{}
	if err := json.Unmarshal(respBody, &agents); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(agents) < 2 {
		t.Fatalf("expected at least 2 agents, got %d", len(agents))
	}
}

func TestGetAgent(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)

	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID, token, nil)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result["id"] != agentID {
		t.Fatalf("expected id %s, got %v", agentID, result["id"])
	}
}

func TestUpdateAgent(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)

	body := map[string]interface{}{
		"instructions": "Updated instructions",
		"model":        "claude-opus-4",
		"status":       "offline",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPut,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID, token, body)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result["instructions"] != "Updated instructions" {
		t.Fatalf("expected updated instructions, got %v", result["instructions"])
	}
}

func TestDeleteAgent(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)

	// Delete agent
	_, status, _ := doRequestWithToken(t, client, http.MethodDelete,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID, token, nil)

	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// Verify agent was deleted
	_, status, _ = doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID, token, nil)

	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", status)
	}
}

func TestDeleteAgentWithAssignedNodes(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// Setup: project -> workflow -> task -> claim node -> delete agent
	agentID, agentAPIToken := createAgent(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	projID := createProject(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	nodeID := nodes[0]["id"].(string)

	// Claim the first node with agent
	claimedNode := claimNode(t, client, srv.URL, taskID, nodeID, agentID, agentAPIToken)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected in_progress after claim, got %v", claimedNode["status"])
	}

	// Now delete the agent — should succeed despite foreign key references
	_, status, respBody := doRequestWithToken(t, client, http.MethodDelete,
		srv.URL+"/api/workspaces/"+wsID+"/agents/"+agentID, token, nil)

	if status != http.StatusNoContent {
		t.Fatalf("expected 204 when deleting agent with assigned nodes, got %d, body: %s", status, respBody)
	}

	// Verify task node assignee_id was cleared
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	for _, n := range updatedNodes {
		if n["id"] == nodeID {
			if n["assignee_id"] != nil {
				t.Fatalf("expected assignee_id to be nil after agent deletion, got %v", n["assignee_id"])
			}
		}
	}
}

func TestDeleteNonexistentAgent(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	_, status, _ := doRequestWithToken(t, client, http.MethodDelete,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+uuid.New().String(), token, nil)

	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent agent, got %d", status)
	}
}

func TestAgentAPITokenFormat(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	body := map[string]interface{}{
		"name":      "Token Format Agent",
		"provider":  "claude",
		"git_name":  "Test Agent",
		"git_email": "agent@test.com",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents", token, body)

	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d", status)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	apiToken := result["api_token"].(string)
	parts := strings.SplitN(apiToken, "_", 3)
	if len(parts) != 3 || parts[0] != "tm" {
		t.Fatalf("expected token format tm_{id}_{hex}, got %s", apiToken)
	}
	if len(parts[2]) != 40 {
		t.Fatalf("expected 40 hex chars in token, got %d chars: %s", len(parts[2]), parts[2])
	}
}

func TestAgentAPITokenNotInGet(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)

	// Get agent — api_token should not be present
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID, token, nil)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, exists := result["api_token"]; exists {
		t.Fatal("api_token should NOT be present in GET response")
	}
}

func TestAgentAPITokenNotInList(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	// Create agent
	_, _ = createAgent(t, client, ts.URL, wsID, token)

	// List agents — api_token should not be present
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents", token, nil)

	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var agents []map[string]interface{}
	if err := json.Unmarshal(respBody, &agents); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, a := range agents {
		if _, exists := a["api_token"]; exists {
			t.Fatal("api_token should NOT be present in list response")
		}
	}
}

func TestAgentMcpServerBindingListAndWorkspaceIsolation(t *testing.T) {
	router, db, q := setupTestRouter(t)
	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
		db.Close()
	})
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)
	_, otherWsID := registerTestUser(t, client, ts.URL)

	agentID, _ := createAgent(t, client, ts.URL, wsID, token)
	mcpID := createMcpServerRecordForTest(t, q, wsID, "Docs MCP")
	otherMcpID := createMcpServerRecordForTest(t, q, otherWsID, "Other MCP")

	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID+"/mcp-servers",
		token,
		map[string]interface{}{"mcp_server_id": mcpID, "enabled": true},
	)
	if status != http.StatusCreated {
		t.Fatalf("bind mcp: expected 201, got %d, body: %s", status, body)
	}

	_, status, body = doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID+"/mcp-servers",
		token,
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("list agent mcp servers: expected 200, got %d, body: %s", status, body)
	}
	var listed []map[string]interface{}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 1 || listed[0]["id"] != mcpID || listed[0]["enabled"] != true {
		t.Fatalf("unexpected listed MCP servers: %#v", listed)
	}

	_, status, _ = doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID+"/mcp-servers",
		token,
		map[string]interface{}{"mcp_server_id": otherMcpID, "enabled": true},
	)
	if status != http.StatusNotFound {
		t.Fatalf("cross-workspace mcp bind: expected 404, got %d", status)
	}
}

func createMcpServerRecordForTest(t *testing.T, q *dbgen.Queries, wsID, name string) string {
	t.Helper()
	server, err := q.CreateMcpServer(context.Background(), dbgen.CreateMcpServerParams{
		WorkspaceID: uuid.MustParse(wsID),
		Name:        name,
		Url:         "https://mcp.example.test/sse",
		Type:        sqlNullString("sse"),
		AuthType:    dbgen.McpAuthTypeNone,
		EnvVars:     pqtype.NullRawMessage{RawMessage: json.RawMessage(`{}`), Valid: true},
		Status:      "connected",
	})
	if err != nil {
		t.Fatalf("create mcp server record: %v", err)
	}
	return server.ID.String()
}

func sqlNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func TestMcpEnvVarsEncryptedAtRestAndMaskedForMembers(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	router, dbConn, q := setupTestRouter(t)
	ts := httptest.NewServer(router)
	t.Cleanup(func() {
		ts.Close()
		dbConn.Close()
	})
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	secretValue := "super-secret-token"
	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/mcp-servers/",
		token,
		map[string]interface{}{
			"name":      "Secure MCP",
			"url":       "https://mcp.example.test/sse",
			"type":      "sse",
			"auth_type": "api_key",
			"env_vars": map[string]interface{}{
				"API_TOKEN": secretValue,
			},
		},
	)
	if status != http.StatusCreated {
		t.Fatalf("create mcp: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	createdEnv, ok := created["env_vars"].(map[string]interface{})
	if !ok || createdEnv["API_TOKEN"] != "********" {
		t.Fatalf("expected masked env_vars in create response, got %#v", created["env_vars"])
	}

	mcpID := uuid.MustParse(created["id"].(string))
	stored, err := q.GetMcpServer(context.Background(), mcpID)
	if err != nil {
		t.Fatalf("get stored mcp: %v", err)
	}
	storedRaw := string(stored.EnvVars.RawMessage)
	if strings.Contains(storedRaw, secretValue) {
		t.Fatalf("stored env_vars contains plaintext secret: %s", storedRaw)
	}
	if !strings.Contains(storedRaw, "teammate-mcp-env-v1") {
		t.Fatalf("stored env_vars missing encrypted envelope marker: %s", storedRaw)
	}

	_, status, body = doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/mcp-servers/",
		token,
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("list mcp: expected 200, got %d, body: %s", status, body)
	}
	if strings.Contains(string(body), secretValue) {
		t.Fatalf("list response leaked plaintext secret: %s", string(body))
	}

	agentID, _ := createAgent(t, client, ts.URL, wsID, token)
	_, status, body = doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID+"/mcp-servers",
		token,
		map[string]interface{}{"mcp_server_id": mcpID.String(), "enabled": true},
	)
	if status != http.StatusCreated {
		t.Fatalf("bind mcp: expected 201, got %d, body: %s", status, body)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents/"+agentID+"/mcp-servers",
		token,
		nil,
	)
	if status != http.StatusOK {
		t.Fatalf("list agent mcp: expected 200, got %d, body: %s", status, body)
	}
	if strings.Contains(string(body), secretValue) {
		t.Fatalf("member agent-mcp response leaked plaintext secret: %s", string(body))
	}
	var bindings []map[string]interface{}
	if err := json.Unmarshal(body, &bindings); err != nil {
		t.Fatalf("decode bindings: %v", err)
	}
	bindingEnv, ok := bindings[0]["env_vars"].(map[string]interface{})
	if !ok || bindingEnv["API_TOKEN"] != "********" {
		t.Fatalf("expected masked env_vars in member agent-mcp response, got %#v", bindings[0]["env_vars"])
	}
}
