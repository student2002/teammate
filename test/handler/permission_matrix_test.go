// permission_matrix_test.go 验证生产上线前的权限矩阵。
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

// TestPermissionMatrix_MemberCannotSeeSecretEnvVars 验证人类成员通过普通 API 看不到 MCP 环境变量明文。
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

// TestPermissionMatrix_AgentCannotAccessOtherAgentMcpExecution 验证 Agent 不能通过 execution 端点获取其他 Agent 的 MCP 配置。
func TestPermissionMatrix_AgentCannotAccessOtherAgentMcpExecution(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := newTestServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, _ := createAgent(t, client, srv.URL, wsID, token)

	// Agent1 尝试访问 Agent2 的 execution MCP 端点 → 403
	url := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/execution/mcp-servers", srv.URL, wsID, agent2ID)
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodGet, url, agent1Token, nil)
	if status != http.StatusForbidden {
		t.Errorf("agent1 accessing agent2 execution mcp: expected 403, got %d", status)
	}

	// Agent1 可以访问自己的 execution MCP 端点 → 200
	selfURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/execution/mcp-servers", srv.URL, wsID, agent1ID)
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodGet, selfURL, agent1Token, nil)
	if status != http.StatusOK {
		t.Errorf("agent1 accessing own execution mcp: expected 200, got %d", status)
	}
	_ = agent2ID
}

// TestPermissionMatrix_MemberCannotAccessExecutionEndpoint 验证人类成员无法访问 Agent execution 端点。
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

// TestPermissionMatrix_McpUpdateKeepEnvVars 验证 MCP Update 的 keep 语义（不传 env_vars 时保留现有值）。
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

	// 更新 MCP 但不传 env_vars
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

// TestPermissionMatrix_McpUpdateClearEnvVars 验证 MCP Update 的 clear 语义（env_vars={} 清空）。
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

	// 更新 MCP 传 env_vars={} 清空
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

// ── 测试辅助函数 ──

func newTestServer(router http.Handler) *testServer {
	return &testServer{httptest.NewServer(router)}
}

type testServer struct {
	*httptest.Server
}

func (s *testServer) Close() {
	s.Server.Close()
}
