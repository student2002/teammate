// workflow_run_test.go covers the workflow run HTTP endpoints: manual run
// creation (POST .../runs) and the run view data source (GET /tasks/{id}/graph).
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// runTemplateBody builds a minimal workflow template graph: manual trigger →
// agent.task → flow.end with completed/failed routing.
func runTemplateBody(name string) map[string]interface{} {
	triggerID := uuid.NewString()
	agentID := uuid.NewString()
	endID := uuid.NewString()
	edge := func(source, srcPort, target, tgtPort string) map[string]interface{} {
		return map[string]interface{}{
			"source_node_id": source,
			"source_port":    srcPort,
			"target_node_id": target,
			"target_port":    tgtPort,
		}
	}
	return map[string]interface{}{
		"name":        name,
		"description": "run endpoint test",
		"enabled":     true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.manual", "name": "开始", "sort_order": 0, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{"x": 0, "y": 0}},
			{"id": agentID, "type_key": "agent.task", "name": "干活", "sort_order": 1, "config": map[string]interface{}{
				"instruction":     "处理输入",
				"assignee":        map[string]interface{}{"mode": "any_agent"},
				"input_map":       map[string]interface{}{"title": "{{ trigger.title }}"},
				"timeout_minutes": 5,
				"retry":           map[string]interface{}{"max_attempts": 1, "backoff_seconds": 0},
			}, "ui_config": map[string]interface{}{"x": 100, "y": 0}},
			{"id": endID, "type_key": "flow.end", "name": "完成", "sort_order": 2, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{"x": 200, "y": 0}},
		},
		"edges": []map[string]interface{}{
			edge(triggerID, "success", agentID, "input"),
			edge(agentID, "success", endID, "completed"),
			edge(agentID, "failed", endID, "failed"),
		},
	}
}

// TestWorkflowRunEndpoints verifies the manual run endpoint end to end:
// 201 with task_id/title, the task/nodes persisted, and the graph endpoint
// returning the full run view structure.
func TestWorkflowRunEndpoints(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)

	// Create the template via the full-graph endpoint.
	body := runTemplateBody("run-endpoint-flow")
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body: %s", status, respBody)
	}
	var tpl struct {
		Template map[string]interface{} `json:"template"`
	}
	if err := json.Unmarshal(respBody, &tpl); err != nil {
		t.Fatalf("decode template response: %v", err)
	}
	tplID, _ := tpl.Template["id"].(string)
	if tplID == "" {
		t.Fatalf("missing template id: %s", respBody)
	}

	runURL := fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", srv.URL, projID, tplID)

	// Unauthenticated requests are rejected.
	_, status, _ = doRequest(t, client, http.MethodPost, runURL, map[string]interface{}{"input": map[string]interface{}{}})
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated run: expected 401, got %d", status)
	}

	// Start a run with an input payload and a title.
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, runURL, token, map[string]interface{}{
		"input": map[string]interface{}{"title": "来自测试"},
		"title": "手动运行一次",
	})
	if status != http.StatusCreated {
		t.Fatalf("start run: expected 201, got %d, body: %s", status, respBody)
	}
	var runResult struct {
		TaskID int    `json:"task_id"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(respBody, &runResult); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	if runResult.TaskID == 0 {
		t.Fatalf("missing task_id: %s", respBody)
	}
	if runResult.Title != "手动运行一次" {
		t.Fatalf("title = %q, want the requested title", runResult.Title)
	}

	// Start without a title falls back to the template name.
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, runURL, token, map[string]interface{}{
		"input": map[string]interface{}{},
	})
	if status != http.StatusCreated {
		t.Fatalf("start run without title: expected 201, got %d, body: %s", status, respBody)
	}
	var fallback struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(respBody, &fallback); err != nil || fallback.Title != "run-endpoint-flow" {
		t.Fatalf("fallback title = %q, want template name (body %s)", fallback.Title, respBody)
	}

	// Unknown template → 404.
	missingURL := fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", srv.URL, projID, uuid.NewString())
	_, status, _ = doRequestWithToken(t, client, http.MethodPost, missingURL, token, map[string]interface{}{})
	if status != http.StatusNotFound {
		t.Fatalf("unknown template run: expected 404, got %d", status)
	}

	// Run view graph.
	graphURL := fmt.Sprintf("%s/api/tasks/%d/graph", srv.URL, runResult.TaskID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, graphURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get graph: expected 200, got %d, body: %s", status, respBody)
	}
	var graph struct {
		Task struct {
			ID             int             `json:"id"`
			Status         string          `json:"status"`
			WorkflowName   string          `json:"workflow_name"`
			TriggerPayload json.RawMessage `json:"trigger_payload"`
		} `json:"task"`
		Nodes []struct {
			ID             string `json:"id"`
			TemplateNodeID string `json:"template_node_id"`
			TypeKey        string `json:"type_key"`
			Status         string `json:"status"`
			Attempts       []struct {
				AttemptNo int    `json:"attempt_no"`
				Status    string `json:"status"`
			} `json:"attempts"`
		} `json:"nodes"`
		Edges []struct {
			SourceNodeID string `json:"source_node_id"`
			Resolution   string `json:"resolution"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(respBody, &graph); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	if graph.Task.ID != runResult.TaskID || graph.Task.Status != "active" {
		t.Fatalf("graph task = %+v", graph.Task)
	}
	if graph.Task.WorkflowName != "run-endpoint-flow" {
		t.Fatalf("workflow_name = %q", graph.Task.WorkflowName)
	}
	if len(graph.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(graph.Nodes))
	}
	for _, n := range graph.Nodes {
		if n.TemplateNodeID == "" {
			t.Fatalf("node %s missing template_node_id", n.ID)
		}
		if n.Attempts == nil {
			t.Fatalf("node %s missing attempts array", n.ID)
		}
	}
	triggerSucceeded := false
	for _, n := range graph.Nodes {
		if n.TypeKey == "trigger.manual" {
			triggerSucceeded = n.Status == "succeeded"
			if len(n.Attempts) != 1 || n.Attempts[0].Status != "succeeded" {
				t.Fatalf("trigger attempts = %+v, want one succeeded", n.Attempts)
			}
		}
	}
	if !triggerSucceeded {
		t.Fatalf("trigger node not succeeded: %s", respBody)
	}
	agentReady := false
	for _, n := range graph.Nodes {
		if n.TypeKey == "agent.task" {
			agentReady = n.Status == "ready"
		}
	}
	if !agentReady {
		t.Fatalf("agent node not ready: %s", respBody)
	}
	activated := 0
	for _, e := range graph.Edges {
		if e.Resolution == "activated" {
			activated++
		}
	}
	if activated != 1 {
		t.Fatalf("activated edges = %d, want 1 (trigger → agent)", activated)
	}
}
