// workflow_update_test.go verifies workflow template partial-update semantics
// (nil fields keep stored values) and depends_on survival across node-list
// replacement.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func createWorkflowTemplateWithIDs(t *testing.T, ts *httptest.Server, client *http.Client, wsID, token, name string, nodes []map[string]interface{}) map[string]interface{} {
	t.Helper()
	body := map[string]interface{}{"name": name, "nodes": nodes}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/workflows", ts.URL, wsID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create workflow template: expected 201, got %d, body: %s", status, respBody)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode created template: %v", err)
	}
	return result
}

func TestWorkflowTemplatePartialUpdate(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	created := createWorkflowTemplateWithIDs(t, ts, client, wsID, token, "partial-flow", []map[string]interface{}{
		{"name": "s1", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent"},
	})
	tplID := created["template"].(map[string]interface{})["id"].(string)
	tplURL := fmt.Sprintf("%s/api/workspaces/%s/workflows/%s", ts.URL, wsID, tplID)

	// partial update: only trigger_config — name must survive
	_, status, body := doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"trigger_config": map[string]interface{}{"interval_minutes": 10},
	})
	if status != http.StatusOK {
		t.Fatalf("partial update: expected 200, got %d, body: %s", status, body)
	}
	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode updated template: %v", err)
	}
	if updated["template"].(map[string]interface{})["name"] != "partial-flow" {
		t.Fatalf("partial update cleared name: %v", updated["template"].(map[string]interface{})["name"])
	}

	// partial update: only next_run_at — must persist without trigger fields
	nextRun := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	_, status, body = doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"next_run_at": nextRun.Format(time.RFC3339),
	})
	if status != http.StatusOK {
		t.Fatalf("next_run_at update: expected 200, got %d, body: %s", status, body)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodGet, tplURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get template: expected 200, got %d", status)
	}
	var fetched map[string]interface{}
	if err := json.Unmarshal(body, &fetched); err != nil {
		t.Fatalf("decode fetched template: %v", err)
	}
	if fetched["next_run_at"] == nil {
		t.Fatal("next_run_at was dropped by partial update")
	}
	if fetched["name"] != "partial-flow" {
		t.Fatalf("name lost after next_run_at update: %v", fetched["name"])
	}

	// explicit empty name is rejected
	_, status, _ = doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"name": "",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("empty name: expected 400, got %d", status)
	}
}

// TestWorkflowTemplateNodeLimits verifies the documented node field bounds on
// both the create and update paths: timeout_minutes 0-480 (0 = no timeout),
// max_reject_cycles capped at 20 with <=0 still defaulting to 5.
func TestWorkflowTemplateNodeLimits(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	wsURL := fmt.Sprintf("%s/api/workspaces/%s/workflows", ts.URL, wsID)

	node := func(timeout, reject interface{}) map[string]interface{} {
		return map[string]interface{}{
			"name": "limits-node", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent",
			"timeout_minutes": timeout, "max_reject_cycles": reject,
		}
	}
	post := func(nodes []map[string]interface{}) int {
		_, status, body := doRequestWithToken(t, client, http.MethodPost, wsURL, token, map[string]interface{}{
			"name": "limits-flow-" + uuid.NewString()[:8], "nodes": nodes,
		})
		_ = body
		return status
	}

	if s := post([]map[string]interface{}{node(480, 20)}); s != http.StatusCreated {
		t.Fatalf("boundary values (480, 20): expected 201, got %d", s)
	}
	if s := post([]map[string]interface{}{node(0, 5)}); s != http.StatusCreated {
		t.Fatalf("zero timeout (no timeout): expected 201, got %d", s)
	}
	if s := post([]map[string]interface{}{node(481, 5)}); s != http.StatusBadRequest {
		t.Fatalf("timeout 481: expected 400, got %d", s)
	}
	if s := post([]map[string]interface{}{node(-1, 5)}); s != http.StatusBadRequest {
		t.Fatalf("negative timeout: expected 400, got %d", s)
	}
	if s := post([]map[string]interface{}{node(60, 21)}); s != http.StatusBadRequest {
		t.Fatalf("reject cycles 21: expected 400, got %d", s)
	}

	// update path carries the same bounds
	created := createWorkflowTemplateWithIDs(t, ts, client, wsID, token, "limits-update-flow", []map[string]interface{}{node(60, 5)})
	tplID := created["template"].(map[string]interface{})["id"].(string)
	tplURL := fmt.Sprintf("%s/api/workspaces/%s/workflows/%s", ts.URL, wsID, tplID)
	_, status, body := doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"nodes": []map[string]interface{}{node(481, 5)},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("update timeout 481: expected 400, got %d, body: %s", status, body)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"nodes": []map[string]interface{}{node(0, 20)},
	})
	if status != http.StatusOK {
		t.Fatalf("update boundary (0, 20): expected 200, got %d, body: %s", status, body)
	}
}

func TestWorkflowTemplateDependsOnSurvivesNodeReplacement(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	projID := createProject(t, client, ts.URL, wsID, token)

	d1 := uuid.NewString()
	d2 := uuid.NewString()
	created := createWorkflowTemplateWithIDs(t, ts, client, wsID, token, "dag-flow", []map[string]interface{}{
		{"id": d1, "name": "dag-s1", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent"},
		{"id": d2, "name": "dag-s2", "sort_order": 2, "node_type": "standard", "assignee_type": "any_agent"},
	})
	tplID := created["template"].(map[string]interface{})["id"].(string)
	tplURL := fmt.Sprintf("%s/api/workspaces/%s/workflows/%s", ts.URL, wsID, tplID)

	// unknown dependency ids are rejected up front
	_, status, body := doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"nodes": []map[string]interface{}{
			{"id": d1, "name": "dag-s1", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent"},
			{"id": d2, "name": "dag-s2", "sort_order": 2, "node_type": "standard", "assignee_type": "any_agent", "depends_on": []string{uuid.NewString()}},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("dangling depends_on: expected 400, got %d, body: %s", status, body)
	}

	// full node replacement carrying stable ids plus a dependency
	_, status, body = doRequestWithToken(t, client, http.MethodPut, tplURL, token, map[string]interface{}{
		"nodes": []map[string]interface{}{
			{"id": d1, "name": "dag-s1", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent"},
			{"id": d2, "name": "dag-s2", "sort_order": 2, "node_type": "standard", "assignee_type": "any_agent", "depends_on": []string{d1}},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("update with depends_on: expected 200, got %d, body: %s", status, body)
	}

	// instantiate a task; the instance dependency must point at the instance id of dag-s1
	taskID, _ := createTask(t, client, ts.URL, projID, tplID, token)
	nodes := listTaskNodes(t, client, ts.URL, taskID, token)
	var s1ID string
	for _, n := range nodes {
		if n["name"] == "dag-s1" {
			s1ID = n["id"].(string)
		}
	}
	if s1ID == "" {
		t.Fatal("instance node dag-s1 not found")
	}
	for _, n := range nodes {
		if n["name"] != "dag-s2" {
			continue
		}
		deps, _ := n["depends_on"].([]interface{})
		if len(deps) != 1 || deps[0] != s1ID {
			t.Fatalf("dag-s2 depends_on = %v, want [%s]", n["depends_on"], s1ID)
		}
		return
	}
	t.Fatal("instance node dag-s2 not found")
}
