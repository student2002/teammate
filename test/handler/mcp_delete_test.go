// mcp_delete_test.go verifies that deleting an MCP server still bound to
// agents is refused with 409 and the occupying agent list, and that the delete
// succeeds once the binding is removed.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestMcpDeleteProtectedWhileBound(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)

	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", ts.URL, wsID), token, map[string]interface{}{
			"name":      "mcp-del-tst",
			"url":       "http://127.0.0.1:9/mcp",
			"type":      "sse",
			"auth_type": "none",
		})
	if status != http.StatusCreated {
		t.Fatalf("create mcp server: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created mcp: %v", err)
	}
	mcpID := created["id"].(string)
	bindURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/mcp-servers", ts.URL, wsID, agentID)
	delURL := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers/%s", ts.URL, wsID, mcpID)

	_, status, body = doRequestWithToken(t, client, http.MethodPost, bindURL, token, map[string]interface{}{
		"mcp_server_id": mcpID,
		"enabled":       true,
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("bind mcp: expected 201/200, got %d, body: %s", status, body)
	}

	// deletion while bound is refused with the occupant list
	_, status, body = doRequestWithToken(t, client, http.MethodDelete, delURL, token, nil)
	if status != http.StatusConflict {
		t.Fatalf("delete bound mcp: expected 409, got %d, body: %s", status, body)
	}
	var conflict map[string]interface{}
	if err := json.Unmarshal(body, &conflict); err != nil {
		t.Fatalf("decode conflict body: %v", err)
	}
	agents, _ := conflict["agents"].([]interface{})
	if len(agents) == 0 {
		t.Fatalf("conflict body missing occupying agents: %s", body)
	}

	// unbind, then delete succeeds
	_, status, body = doRequestWithToken(t, client, http.MethodDelete,
		fmt.Sprintf("%s/%s", bindURL, mcpID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("unbind mcp: expected 204, got %d, body: %s", status, body)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodDelete, delURL, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete unbound mcp: expected 204, got %d, body: %s", status, body)
	}
}
