// workflow_graph_test.go covers the workflow template full-graph CRUD API
// contract: POST/PUT validate and persist the whole graph (nodes + port-based
// edges), version bumps on every full-graph save, and write operations require
// member-level roles.
package handler_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// setupWorkflowGraphServer boots a test server with a registered owner.
func setupWorkflowGraphServer(t *testing.T) (*httptest.Server, *http.Client, string, string) {
	t.Helper()
	router, db, _ := setupTestRouter(t)
	t.Cleanup(func() { db.Close() })
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)
	return srv, client, token, wsID
}

// workflowsURL builds the workspace workflow collection URL.
func workflowsURL(baseURL, wsID string) string {
	return fmt.Sprintf("%s/api/workspaces/%s/workflows", baseURL, wsID)
}

// graphClone deep-copies a graph map through JSON round-trip.
func graphClone(t *testing.T, g map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal graph: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}
	return out
}

// graphNodes returns the graph's node list as maps.
func graphNodes(t *testing.T, g map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(g["nodes"])
	if err != nil {
		t.Fatalf("marshal nodes: %v", err)
	}
	var nodes []map[string]interface{}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatalf("unmarshal nodes: %v", err)
	}
	return nodes
}

// graphEdges returns the graph's edge list as maps.
func graphEdges(t *testing.T, g map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(g["edges"])
	if err != nil {
		t.Fatalf("marshal edges: %v", err)
	}
	var edges []map[string]interface{}
	if err := json.Unmarshal(raw, &edges); err != nil {
		t.Fatalf("unmarshal edges: %v", err)
	}
	return edges
}

// setNodes replaces the graph's node list.
func setNodes(g map[string]interface{}, nodes []map[string]interface{}) {
	g["nodes"] = nodes
}

// setEdges replaces the graph's edge list.
func setEdges(g map[string]interface{}, edges []map[string]interface{}) {
	g["edges"] = edges
}

// contractNodeIDs returns the fixed node ids of the contract graph in sort order.
func contractNodeIDs() []string {
	ids := make([]string, 7)
	for i := range ids {
		ids[i] = fmt.Sprintf("a1000000-0000-4000-8000-%012d", i+1)
	}
	return ids
}

// contractEdgeIDs returns the fixed edge ids of the contract graph.
func contractEdgeIDs() []string {
	ids := make([]string, 11)
	for i := range ids {
		ids[i] = fmt.Sprintf("e1000000-0000-4000-8000-%012d", i+1)
	}
	return ids
}

// TestWorkflowGraphCreateValidGraph verifies that POST with the contract sample
// graph creates the template together with its 7 nodes and 11 edges, the
// response is {template, nodes, edges} with version=1, and the client-supplied
// node/edge ids are preserved.
func TestWorkflowGraphCreateValidGraph(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)

	body := workflowContractGraph()
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create workflow: expected 201, got %d, body: %s", status, respBody)
	}

	var got struct {
		Template map[string]interface{}   `json:"template"`
		Nodes    []map[string]interface{} `json:"nodes"`
		Edges    []map[string]interface{} `json:"edges"`
	}
	if err := json.Unmarshal(respBody, &got); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if got.Template["version"].(float64) != 1 {
		t.Errorf("template version = %v, want 1", got.Template["version"])
	}
	if got.Template["name"] != body["name"] {
		t.Errorf("template name = %v, want %v", got.Template["name"], body["name"])
	}
	if len(got.Nodes) != 7 {
		t.Fatalf("nodes count = %d, want 7", len(got.Nodes))
	}
	if len(got.Edges) != 11 {
		t.Fatalf("edges count = %d, want 11", len(got.Edges))
	}

	wantNodeIDs := contractNodeIDs()
	gotNodeIDs := make(map[string]bool, len(got.Nodes))
	for _, n := range got.Nodes {
		id, _ := n["id"].(string)
		if id == "" {
			t.Errorf("node missing id: %v", n)
		}
		gotNodeIDs[id] = true
		if _, ok := n["type_key"].(string); !ok {
			t.Errorf("node %s missing type_key", id)
		}
	}
	for _, want := range wantNodeIDs {
		if !gotNodeIDs[want] {
			t.Errorf("node id %s from the request not found in response", want)
		}
	}

	wantEdgeIDs := contractEdgeIDs()
	gotEdgeIDs := make(map[string]bool, len(got.Edges))
	for _, e := range got.Edges {
		id, _ := e["id"].(string)
		gotEdgeIDs[id] = true
	}
	for _, want := range wantEdgeIDs {
		if !gotEdgeIDs[want] {
			t.Errorf("edge id %s from the request not found in response", want)
		}
	}
}

// TestWorkflowGraphCreateRejectsInvalidGraphs verifies the whole-graph validation
// failures: missing flow.end, agent.task without instruction, cycles, and
// multiple edges out of one source port.
func TestWorkflowGraphCreateRejectsInvalidGraphs(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)

	post := func(g map[string]interface{}) (int, []byte) {
		_, status, respBody := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, g)
		return status, respBody
	}
	expect400 := func(name string, status int, respBody []byte) {
		t.Helper()
		if status != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d, body: %s", name, status, respBody)
		}
	}

	// missing flow.end node: drop the end node and every edge into it
	g := graphClone(t, workflowContractGraph())
	endID := contractNodeIDs()[6]
	nodes := graphNodes(t, g)
	kept := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		if n["id"] != endID {
			kept = append(kept, n)
		}
	}
	setNodes(g, kept)
	edges := graphEdges(t, g)
	keptEdges := make([]map[string]interface{}, 0, len(edges))
	for _, e := range edges {
		if e["target_node_id"] != endID {
			keptEdges = append(keptEdges, e)
		}
	}
	setEdges(g, keptEdges)
	status, respBody := post(g)
	expect400("missing flow.end", status, respBody)

	// malformed node id (not a UUID): the schema keys nodes by UUID, so a
	// malformed id is a 400 from validation rather than a store-write failure
	g = graphClone(t, workflowContractGraph())
	nodes = graphNodes(t, g)
	nodes[0]["id"] = "n-not-a-uuid"
	setNodes(g, nodes)
	status, respBody = post(g)
	expect400("malformed node id", status, respBody)

	// two nodes sharing the same non-empty name: the run context exposes
	// nodes.<名字>.output aliases keyed by name, so duplicates are invalid
	g = graphClone(t, workflowContractGraph())
	nodes = graphNodes(t, g)
	nodes[0]["name"] = "重名节点"
	nodes[1]["name"] = "重名节点"
	setNodes(g, nodes)
	status, respBody = post(g)
	expect400("duplicate node name", status, respBody)

	// agent.task without instruction
	g = graphClone(t, workflowContractGraph())
	nodes = graphNodes(t, g)
	for _, n := range nodes {
		if n["type_key"] == "agent.task" {
			n["config"] = map[string]interface{}{}
		}
	}
	setNodes(g, nodes)
	status, respBody = post(g)
	expect400("agent.task missing instruction", status, respBody)

	// cycle A -> B -> A inside an otherwise well-formed graph
	cycleA := uuid.NewString()
	cycleB := uuid.NewString()
	endID = uuid.NewString()
	triggerID := uuid.NewString()
	cycleGraph := map[string]interface{}{
		"name":    "cycle-flow",
		"enabled": true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.manual", "name": "t", "sort_order": 0, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{}},
			{"id": cycleA, "type_key": "agent.task", "name": "A", "sort_order": 1, "config": map[string]interface{}{"instruction": "do A"}, "ui_config": map[string]interface{}{}},
			{"id": cycleB, "type_key": "flow.condition", "name": "B", "sort_order": 2, "config": map[string]interface{}{"condition": map[string]interface{}{"all": []map[string]interface{}{{"path": "trigger.issue.title", "op": "eq", "value": "x"}}}}, "ui_config": map[string]interface{}{}},
			{"id": endID, "type_key": "flow.end", "name": "end", "sort_order": 3, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{}},
		},
		"edges": []map[string]interface{}{
			{"id": uuid.NewString(), "source_node_id": triggerID, "source_port": "success", "target_node_id": cycleA, "target_port": "input"},
			{"id": uuid.NewString(), "source_node_id": cycleA, "source_port": "success", "target_node_id": cycleB, "target_port": "input"},
			{"id": uuid.NewString(), "source_node_id": cycleB, "source_port": "true", "target_node_id": cycleA, "target_port": "input"},
			{"id": uuid.NewString(), "source_node_id": cycleB, "source_port": "false", "target_node_id": endID, "target_port": "failed"},
			{"id": uuid.NewString(), "source_node_id": cycleA, "source_port": "failed", "target_node_id": endID, "target_port": "failed"},
		},
	}
	status, respBody = post(cycleGraph)
	expect400("cycle", status, respBody)

	// two edges out of the same source port (trigger.success fan-out)
	taskA := uuid.NewString()
	taskB := uuid.NewString()
	endID = uuid.NewString()
	triggerID = uuid.NewString()
	fanoutGraph := map[string]interface{}{
		"name":    "fanout-flow",
		"enabled": true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.manual", "name": "t", "sort_order": 0, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{}},
			{"id": taskA, "type_key": "agent.task", "name": "A", "sort_order": 1, "config": map[string]interface{}{"instruction": "do A"}, "ui_config": map[string]interface{}{}},
			{"id": taskB, "type_key": "agent.task", "name": "B", "sort_order": 2, "config": map[string]interface{}{"instruction": "do B"}, "ui_config": map[string]interface{}{}},
			{"id": endID, "type_key": "flow.end", "name": "end", "sort_order": 3, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{}},
		},
		"edges": []map[string]interface{}{
			{"id": uuid.NewString(), "source_node_id": triggerID, "source_port": "success", "target_node_id": taskA, "target_port": "input"},
			{"id": uuid.NewString(), "source_node_id": triggerID, "source_port": "success", "target_node_id": taskB, "target_port": "input"},
			{"id": uuid.NewString(), "source_node_id": taskA, "source_port": "success", "target_node_id": endID, "target_port": "completed"},
			{"id": uuid.NewString(), "source_node_id": taskA, "source_port": "failed", "target_node_id": endID, "target_port": "failed"},
			{"id": uuid.NewString(), "source_node_id": taskB, "source_port": "success", "target_node_id": endID, "target_port": "completed"},
			{"id": uuid.NewString(), "source_node_id": taskB, "source_port": "failed", "target_node_id": endID, "target_port": "failed"},
		},
	}
	status, respBody = post(fanoutGraph)
	expect400("same-port double out-edge", status, respBody)
}

// TestWorkflowGraphUpdateReplacesGraph verifies that PUT replaces the whole graph
// atomically: the version bumps to 2, renamed nodes keep their ids, and the
// edge set is the requested one (one edge removed, one edge added).
func TestWorkflowGraphUpdateReplacesGraph(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)

	tplID, _ := createV2WorkflowTemplate(t, client, srv.URL, wsID, token)

	// Rename one node, remove one edge, add one edge back on the same port
	updated := graphClone(t, workflowContractGraph())
	renamedID := contractNodeIDs()[2]
	nodes := graphNodes(t, updated)
	for _, n := range nodes {
		if n["id"] == renamedID {
			n["name"] = "分析并修复 V2"
		}
	}
	setNodes(updated, nodes)

	removedEdgeID := contractEdgeIDs()[8] // condition(false) -> end(failed)
	edges := graphEdges(t, updated)
	keptEdges := make([]map[string]interface{}, 0, len(edges))
	for _, e := range edges {
		if e["id"] != removedEdgeID {
			keptEdges = append(keptEdges, e)
		}
	}
	keptEdges = append(keptEdges, map[string]interface{}{
		"id":             "e2000000-0000-4000-8000-000000000001",
		"source_node_id": contractNodeIDs()[4],
		"source_port":    "false",
		"target_node_id": contractNodeIDs()[5],
		"target_port":    "input",
	})
	setEdges(updated, keptEdges)

	putURL := fmt.Sprintf("%s/%s", workflowsURL(srv.URL, wsID), tplID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPut, putURL, token, updated)
	if status != http.StatusOK {
		t.Fatalf("update workflow: expected 200, got %d, body: %s", status, respBody)
	}

	var got struct {
		Template map[string]interface{}   `json:"template"`
		Nodes    []map[string]interface{} `json:"nodes"`
		Edges    []map[string]interface{} `json:"edges"`
	}
	if err := json.Unmarshal(respBody, &got); err != nil {
		t.Fatalf("decode update response: %v", err)
	}

	if got.Template["version"].(float64) != 2 {
		t.Errorf("template version = %v, want 2 after full-graph save", got.Template["version"])
	}
	if len(got.Nodes) != 7 {
		t.Fatalf("nodes count = %d, want 7", len(got.Nodes))
	}
	if len(got.Edges) != 11 {
		t.Fatalf("edges count = %d, want 11 (10 kept + 1 added)", len(got.Edges))
	}

	// node ids are stable and the rename landed
	renamed := false
	for _, n := range got.Nodes {
		if n["id"] == renamedID {
			if n["name"] == "分析并修复 V2" {
				renamed = true
			}
		}
	}
	if !renamed {
		t.Errorf("renamed node %s not persisted", renamedID)
	}

	// the removed edge is gone, the added edge is present
	edgeIDs := make(map[string]bool, len(got.Edges))
	for _, e := range got.Edges {
		edgeIDs[e["id"].(string)] = true
	}
	if edgeIDs[removedEdgeID] {
		t.Errorf("removed edge %s still present after replace", removedEdgeID)
	}
	if !edgeIDs["e2000000-0000-4000-8000-000000000001"] {
		t.Error("added edge missing after replace")
	}
}

// TestWorkflowGraphGetSingleAndList verifies GET single returns {template, nodes,
// edges} and GET list contains the created template with its graph.
func TestWorkflowGraphGetSingleAndList(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)

	tplID, _ := createV2WorkflowTemplate(t, client, srv.URL, wsID, token)

	// GET single
	getURL := fmt.Sprintf("%s/%s", workflowsURL(srv.URL, wsID), tplID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, getURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get workflow: expected 200, got %d, body: %s", status, respBody)
	}
	var got struct {
		Template map[string]interface{}   `json:"template"`
		Nodes    []map[string]interface{} `json:"nodes"`
		Edges    []map[string]interface{} `json:"edges"`
	}
	if err := json.Unmarshal(respBody, &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Template["id"] != tplID {
		t.Errorf("template id = %v, want %s", got.Template["id"], tplID)
	}
	if len(got.Nodes) != 7 || len(got.Edges) != 11 {
		t.Errorf("get single: nodes=%d edges=%d, want 7/11", len(got.Nodes), len(got.Edges))
	}

	// GET list contains the template with its graph
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, workflowsURL(srv.URL, wsID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list workflows: expected 200, got %d, body: %s", status, respBody)
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(respBody, &list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, item := range list {
		tpl, _ := item["template"].(map[string]interface{})
		if tpl == nil || tpl["id"] != tplID {
			continue
		}
		found = true
		nodes, _ := item["nodes"].([]interface{})
		edges, _ := item["edges"].([]interface{})
		if len(nodes) != 7 || len(edges) != 11 {
			t.Errorf("list item graph: nodes=%d edges=%d, want 7/11", len(nodes), len(edges))
		}
	}
	if !found {
		t.Error("created template not found in workspace list")
	}
}

// TestWorkflowGraphDelete verifies DELETE returns 204 and the template becomes
// unreachable (GET 404).
func TestWorkflowGraphDelete(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)

	tplID, _ := createV2WorkflowTemplate(t, client, srv.URL, wsID, token)

	deleteURL := fmt.Sprintf("%s/%s", workflowsURL(srv.URL, wsID), tplID)
	_, status, _ := doRequestWithToken(t, client, http.MethodDelete, deleteURL, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete workflow: expected 204, got %d", status)
	}

	_, status, _ = doRequestWithToken(t, client, http.MethodGet, deleteURL, token, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", status)
	}
}

// TestWorkflowGraphViewerForbidden verifies that a workspace viewer cannot create,
// update or delete workflow templates (write operations need member or higher).
func TestWorkflowGraphViewerForbidden(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)

	// Register a second user and attach them to the first workspace as viewer
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

	tplID, _ := createV2WorkflowTemplate(t, client, srv.URL, wsID, token)

	// viewer create -> 403
	_, status, _ := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), viewerToken, workflowContractGraph())
	if status != http.StatusForbidden {
		t.Errorf("viewer create: expected 403, got %d", status)
	}

	// viewer update -> 403
	putURL := fmt.Sprintf("%s/%s", workflowsURL(srv.URL, wsID), tplID)
	_, status, _ = doRequestWithToken(t, client, http.MethodPut, putURL, viewerToken, workflowContractGraph())
	if status != http.StatusForbidden {
		t.Errorf("viewer update: expected 403, got %d", status)
	}

	// viewer delete -> 403
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, putURL, viewerToken, nil)
	if status != http.StatusForbidden {
		t.Errorf("viewer delete: expected 403, got %d", status)
	}
}
