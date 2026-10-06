// stats_routes_test.go verifies the read endpoints for template detail/stats,
// project stats, agent stats, and per-task token usage summaries.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatsAndDetailRoutes(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)
	projID := createProject(t, client, server.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, server.URL, wsID, token)
	agentID, _ := createAgent(t, client, server.URL, wsID, token)

	// GET single workflow template
	tplURL := fmt.Sprintf("%s/api/workspaces/%s/workflows/%s", server.URL, wsID, tplID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, tplURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get workflow template: expected 200, got %d, body: %s", status, respBody)
	}
	var tpl map[string]interface{}
	if err := json.Unmarshal(respBody, &tpl); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	if nodes, _ := tpl["nodes"].([]interface{}); len(nodes) != 2 {
		t.Errorf("template detail node count = %d, want 2", len(nodes))
	}

	// GET workflow template stats
	statsURL := fmt.Sprintf("%s/api/workspaces/%s/workflows/%s/stats", server.URL, wsID, tplID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, statsURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get template stats: expected 200, got %d, body: %s", status, respBody)
	}
	var tplStats map[string]interface{}
	if err := json.Unmarshal(respBody, &tplStats); err != nil {
		t.Fatalf("decode template stats: %v", err)
	}
	if _, ok := tplStats["usage_count"]; !ok {
		t.Error("template stats missing usage_count")
	}

	// GET project stats
	projStatsURL := fmt.Sprintf("%s/api/projects/%s/stats", server.URL, projID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, projStatsURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get project stats: expected 200, got %d, body: %s", status, respBody)
	}
	var projStats map[string]interface{}
	if err := json.Unmarshal(respBody, &projStats); err != nil {
		t.Fatalf("decode project stats: %v", err)
	}
	if _, ok := projStats["task_counts"]; !ok {
		t.Error("project stats missing task_counts")
	}

	// GET agent stats
	agentStatsURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/stats", server.URL, wsID, agentID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, agentStatsURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get agent stats: expected 200, got %d, body: %s", status, respBody)
	}
	var agentStats map[string]interface{}
	if err := json.Unmarshal(respBody, &agentStats); err != nil {
		t.Fatalf("decode agent stats: %v", err)
	}
	if _, ok := agentStats["total_tokens"]; !ok {
		t.Error("agent stats missing total_tokens")
	}

	// GET per-task token usage summary (aggregate row even without reported usage)
	taskID, _ := createTask(t, client, server.URL, projID, tplID, token)
	tokenUsageURL := fmt.Sprintf("%s/api/tasks/%d/token-usage", server.URL, taskID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, tokenUsageURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get task token usage: expected 200, got %d, body: %s", status, respBody)
	}
	var usage map[string]interface{}
	if err := json.Unmarshal(respBody, &usage); err != nil {
		t.Fatalf("decode token usage: %v", err)
	}
	if v, ok := usage["total_tokens"].(float64); !ok || v != 0 {
		t.Errorf("task token usage total_tokens = %v, want 0", usage["total_tokens"])
	}
}

// TestProjectStatsCrossWorkspaceDenied verifies project stats inherit the
// project-member gate: a stranger workspace member gets 403.
func TestProjectStatsCrossWorkspaceDenied(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)
	projID := createProject(t, client, server.URL, wsID, token)

	// second user, member of a different workspace
	strangerToken, _ := registerTestUser(t, client, server.URL)
	projStatsURL := fmt.Sprintf("%s/api/projects/%s/stats", server.URL, projID)
	_, status, _ := doRequestWithToken(t, client, http.MethodGet, projStatsURL, strangerToken, nil)
	if status != http.StatusForbidden {
		t.Errorf("stranger project stats: expected 403, got %d", status)
	}
}
