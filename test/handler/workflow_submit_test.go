// workflow_submit_test.go covers the human.input submit endpoint (plan
// §12.5): submitted drives the submitted port downstream, cancelled drives
// the cancelled port, a second submit conflicts (409) and viewers are
// rejected (403).
package handler_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// humanInputTemplateBody builds trigger.manual → human.input → flow.end with
// the submitted/cancelled ports routed to the matching end ports.
func humanInputTemplateBody(name string) map[string]interface{} {
	triggerID, humanID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	edge := func(source, srcPort, target, tgtPort string) map[string]interface{} {
		return map[string]interface{}{
			"source_node_id": source,
			"source_port":    srcPort,
			"target_node_id": target,
			"target_port":    tgtPort,
		}
	}
	return map[string]interface{}{
		"name":    name,
		"enabled": true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.manual", "name": "开始", "sort_order": 0, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{}},
			{"id": humanID, "type_key": "human.input", "name": "人工确认", "sort_order": 1, "config": map[string]interface{}{"prompt": "确认发布？"}, "ui_config": map[string]interface{}{}},
			{"id": endID, "type_key": "flow.end", "name": "完成", "sort_order": 2, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{}},
		},
		"edges": []map[string]interface{}{
			edge(triggerID, "success", humanID, "input"),
			edge(humanID, "submitted", endID, "completed"),
			edge(humanID, "cancelled", endID, "failed"),
		},
	}
}

// submitNodeURL builds the submit endpoint URL.
func submitNodeURL(baseURL string, taskID int, nodeID string) string {
	return fmt.Sprintf("%s/api/tasks/%d/nodes/%s/submit", baseURL, taskID, nodeID)
}

// graphEntry finds one node of the task graph by type_key.
func graphEntry(t *testing.T, srvURL, token string, client *http.Client, taskID int, typeKey string) map[string]interface{} {
	t.Helper()
	_, status, body := doRequestWithToken(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/tasks/%d/graph", srvURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("get graph: expected 200, got %d, body %s", status, body)
	}
	var graph struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &graph); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	for _, n := range graph.Nodes {
		if n["type_key"] == typeKey {
			return n
		}
	}
	t.Fatalf("node %s not found in graph", typeKey)
	return nil
}

// taskStatus reads the run task's status through the graph endpoint.
func taskStatus(t *testing.T, srvURL, token string, client *http.Client, taskID int) string {
	t.Helper()
	_, status, body := doRequestWithToken(t, client, http.MethodGet,
		fmt.Sprintf("%s/api/tasks/%d/graph", srvURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("get graph: expected 200, got %d, body %s", status, body)
	}
	var graph struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(body, &graph); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	return graph.Task.Status
}

func TestWorkflowHumanInputSubmit(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)

	_, status, body := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, humanInputTemplateBody("人工确认流"))
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body %s", status, body)
	}
	var tpl struct {
		Template map[string]interface{} `json:"template"`
	}
	_ = json.Unmarshal(body, &tpl)
	tplID, _ := tpl.Template["id"].(string)

	runURL := fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", srv.URL, projID, tplID)
	_, status, body = doRequestWithToken(t, client, http.MethodPost, runURL, token, map[string]interface{}{"input": map[string]interface{}{}})
	if status != http.StatusCreated {
		t.Fatalf("start run: expected 201, got %d, body %s", status, body)
	}
	var run struct {
		TaskID int `json:"task_id"`
	}
	_ = json.Unmarshal(body, &run)

	human := graphEntry(t, srv.URL, token, client, run.TaskID, "human.input")
	nodeID, _ := human["id"].(string)
	if human["status"] != "manual_intervention" {
		t.Fatalf("human node status = %v, want manual_intervention", human["status"])
	}

	// submitted → the submitted port drives the end node and the task completes.
	_, status, body = doRequestWithToken(t, client, http.MethodPost, submitNodeURL(srv.URL, run.TaskID, nodeID), token,
		map[string]interface{}{"action": "submitted", "data": map[string]interface{}{"note": "同意发布"}})
	if status != http.StatusOK {
		t.Fatalf("submit: expected 200, got %d, body %s", status, body)
	}

	human = graphEntry(t, srv.URL, token, client, run.TaskID, "human.input")
	if human["status"] != "succeeded" {
		t.Fatalf("human node status = %v, want succeeded", human["status"])
	}
	output, _ := human["output"].(map[string]interface{})
	if output["choice"] != "submitted" {
		t.Fatalf("human output = %v, want choice submitted", output)
	}
	data, _ := output["data"].(map[string]interface{})
	if data["note"] != "同意发布" {
		t.Fatalf("human output data = %v, want the submitted payload", data)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/graph", srv.URL, run.TaskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("get graph after submit: expected 200, got %d, body %s", status, body)
	}
	if got := taskStatus(t, srv.URL, token, client, run.TaskID); got != "completed" {
		t.Fatalf("task status = %q, want completed", got)
	}

	// A second submit on the closed node conflicts.
	_, status, _ = doRequestWithToken(t, client, http.MethodPost, submitNodeURL(srv.URL, run.TaskID, nodeID), token,
		map[string]interface{}{"action": "submitted"})
	if status != http.StatusConflict {
		t.Fatalf("duplicate submit: expected 409, got %d", status)
	}

	// An unknown action is rejected at the boundary.
	_, status, _ = doRequestWithToken(t, client, http.MethodPost, submitNodeURL(srv.URL, run.TaskID, nodeID), token,
		map[string]interface{}{"action": "approve"})
	if status != http.StatusBadRequest {
		t.Fatalf("unknown action: expected 400, got %d", status)
	}
}

func TestWorkflowHumanInputCancel(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)

	_, status, body := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, humanInputTemplateBody("人工取消流"))
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body %s", status, body)
	}
	var tpl struct {
		Template map[string]interface{} `json:"template"`
	}
	_ = json.Unmarshal(body, &tpl)
	tplID, _ := tpl.Template["id"].(string)

	runURL := fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", srv.URL, projID, tplID)
	_, status, body = doRequestWithToken(t, client, http.MethodPost, runURL, token, map[string]interface{}{})
	if status != http.StatusCreated {
		t.Fatalf("start run: expected 201, got %d, body %s", status, body)
	}
	var run struct {
		TaskID int `json:"task_id"`
	}
	_ = json.Unmarshal(body, &run)

	human := graphEntry(t, srv.URL, token, client, run.TaskID, "human.input")
	nodeID, _ := human["id"].(string)

	_, status, _ = doRequestWithToken(t, client, http.MethodPost, submitNodeURL(srv.URL, run.TaskID, nodeID), token,
		map[string]interface{}{"action": "cancelled"})
	if status != http.StatusOK {
		t.Fatalf("cancel submit: expected 200, got %d", status)
	}

	human = graphEntry(t, srv.URL, token, client, run.TaskID, "human.input")
	if human["status"] != "succeeded" {
		t.Fatalf("human node status = %v, want succeeded", human["status"])
	}
	output, _ := human["output"].(map[string]interface{})
	if output["choice"] != "cancelled" {
		t.Fatalf("human output = %v, want choice cancelled", output)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/graph", srv.URL, run.TaskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("get graph after cancel: expected 200, got %d, body %s", status, body)
	}
	if got := taskStatus(t, srv.URL, token, client, run.TaskID); got != "failed" {
		t.Fatalf("task status = %q, want failed via the cancelled port", got)
	}
}

func TestWorkflowHumanInputViewerForbidden(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)

	_, status, body := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, humanInputTemplateBody("viewer 流"))
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body %s", status, body)
	}
	var tpl struct {
		Template map[string]interface{} `json:"template"`
	}
	_ = json.Unmarshal(body, &tpl)
	tplID, _ := tpl.Template["id"].(string)

	runURL := fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", srv.URL, projID, tplID)
	_, status, body = doRequestWithToken(t, client, http.MethodPost, runURL, token, map[string]interface{}{})
	if status != http.StatusCreated {
		t.Fatalf("start run: expected 201, got %d, body %s", status, body)
	}
	var run struct {
		TaskID int `json:"task_id"`
	}
	_ = json.Unmarshal(body, &run)
	human := graphEntry(t, srv.URL, token, client, run.TaskID, "human.input")
	nodeID, _ := human["id"].(string)

	// A workspace viewer cannot submit (task write needs member+).
	viewerToken, _ := registerTestUser(t, client, srv.URL)
	viewerMemberID := extractMemberIDFromJWT(t, viewerToken)
	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(
		`INSERT INTO workspace_members (workspace_id, member_id, role) VALUES ($1, $2, 'viewer')`,
		wsID, viewerMemberID); err != nil {
		t.Fatalf("insert viewer membership: %v", err)
	}

	_, status, _ = doRequestWithToken(t, client, http.MethodPost, submitNodeURL(srv.URL, run.TaskID, nodeID), viewerToken,
		map[string]interface{}{"action": "submitted"})
	if status != http.StatusForbidden {
		t.Fatalf("viewer submit: expected 403, got %d", status)
	}

	// Unauthenticated requests are rejected.
	_, status, _ = doRequest(t, client, http.MethodPost, submitNodeURL(srv.URL, run.TaskID, nodeID),
		map[string]interface{}{"action": "submitted"})
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated submit: expected 401, got %d", status)
	}
}
