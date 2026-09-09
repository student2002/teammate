// permission_matrix_test.go verifies the permission matrix before production deployment.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestPermissionMatrix_MemberCannotSeeSecretEnvVars verifies that human members cannot see MCP environment variable plaintext through the normal API.
func TestPermissionMatrix_MemberCannotSeeSecretEnvVars(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := newTestServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	mcpURL := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", srv.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, mcpURL, token, map[string]interface{}{
		"name":      "test-mcp",
		"url":       "http://mcp.example.com",
		"type":      "http",
		"auth_type": "none",
		"env_vars":  map[string]string{"API_KEY": "super-secret-key-12345"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create mcp server: expected 201, got %d, body: %s", status, respBody)
	}

	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, mcpURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list mcp servers: expected 200, got %d", status)
	}
	if strings.Contains(string(respBody), "super-secret-key-12345") {
		t.Errorf("member should not see secret value in response: %s", respBody)
	}
}

// TestPermissionMatrix_AgentCannotAccessOtherAgentMcpExecution verifies that an agent cannot access another agent's MCP configuration via the execution endpoint.
func TestPermissionMatrix_AgentCannotAccessOtherAgentMcpExecution(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := newTestServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, _ := createAgent(t, client, srv.URL, wsID, token)

	// Agent1 tries to access Agent2's execution MCP endpoint → 403
	url := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/execution/mcp-servers", srv.URL, wsID, agent2ID)
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodGet, url, agent1Token, nil)
	if status != http.StatusForbidden {
		t.Errorf("agent1 accessing agent2 execution mcp: expected 403, got %d", status)
	}

	// Agent1 can access its own execution MCP endpoint → 200
	selfURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/execution/mcp-servers", srv.URL, wsID, agent1ID)
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodGet, selfURL, agent1Token, nil)
	if status != http.StatusOK {
		t.Errorf("agent1 accessing own execution mcp: expected 200, got %d", status)
	}
	_ = agent2ID
}

// TestPermissionMatrix_MemberCannotAccessExecutionEndpoint verifies that human members cannot access the agent execution endpoint.
func TestPermissionMatrix_MemberCannotAccessExecutionEndpoint(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := newTestServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)
	agentID, _ := createAgent(t, client, srv.URL, wsID, token)

	url := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/execution/mcp-servers", srv.URL, wsID, agentID)
	_, status, _ := doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusForbidden {
		t.Errorf("member accessing agent execution mcp: expected 403, got %d", status)
	}
}

// TestPermissionMatrix_McpUpdateKeepEnvVars verifies the keep semantics of MCP Update (existing values are preserved when env_vars is not provided).
func TestPermissionMatrix_McpUpdateKeepEnvVars(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := newTestServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	mcpURL := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", srv.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, mcpURL, token, map[string]interface{}{
		"name":      "test-mcp-keep",
		"url":       "http://keep.example.com",
		"type":      "http",
		"auth_type": "none",
		"env_vars":  map[string]string{"KEEP_KEY": "keep-value"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create mcp: expected 201, got %d, body: %s", status, respBody)
	}

	var created map[string]interface{}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	mcpID := created["id"].(string)

	// Update MCP without providing env_vars
	updateURL := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers/%s", srv.URL, wsID, mcpID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodPut, updateURL, token, map[string]interface{}{
		"name": "updated-name",
		"url":  "http://keep.example.com",
	})
	if status != http.StatusOK {
		t.Fatalf("update mcp (keep env): expected 200, got %d, body: %s", status, respBody)
	}

	var updated map[string]interface{}
	if err := json.Unmarshal(respBody, &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if envVars, ok := updated["env_vars"].(map[string]interface{}); ok {
		if _, hasKey := envVars["KEEP_KEY"]; !hasKey {
			t.Errorf("env_vars should retain KEEP_KEY after update without env_vars, got %v", envVars)
		}
	}
}

// TestPermissionMatrix_McpUpdateClearEnvVars verifies the clear semantics of MCP Update (env_vars={} clears all).
func TestPermissionMatrix_McpUpdateClearEnvVars(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := newTestServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	mcpURL := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", srv.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, mcpURL, token, map[string]interface{}{
		"name":      "test-mcp-clear",
		"url":       "http://clear.example.com",
		"type":      "http",
		"auth_type": "none",
		"env_vars":  map[string]string{"CLEAR_KEY": "clear-value"},
	})
	if status != http.StatusCreated {
		t.Fatalf("create mcp: expected 201, got %d, body: %s", status, respBody)
	}

	var created map[string]interface{}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	mcpID := created["id"].(string)

	// Update MCP with env_vars={} to clear
	updateURL := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers/%s", srv.URL, wsID, mcpID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodPut, updateURL, token, map[string]interface{}{
		"name":     "cleared-name",
		"url":      "http://clear.example.com",
		"env_vars": map[string]string{},
	})
	if status != http.StatusOK {
		t.Fatalf("update mcp (clear env): expected 200, got %d, body: %s", status, respBody)
	}

	var updated map[string]interface{}
	if err := json.Unmarshal(respBody, &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if envVars, ok := updated["env_vars"].(map[string]interface{}); ok && len(envVars) > 0 {
		t.Errorf("env_vars should be empty after clear, got %v", envVars)
	}
}

// ── Test helper functions ──

func newTestServer(router http.Handler) *testServer {
	return &testServer{httptest.NewServer(router)}
}

type testServer struct {
	*httptest.Server
}

func (s *testServer) Close() {
	s.Server.Close()
}
