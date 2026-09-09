// board_test.go tests covering the board API.
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetBoard5Columns verifies that the board API returns columns and groups correctly by node status. Review nodes are merged into the same column as standard nodes.
func TestGetBoard5Columns(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// Setup: project, workflow with 3 nodes (code -> review -> deploy)
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// Create task with 3 nodes (code[pending] -> review[pending] -> deploy[pending])
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	// Step 1: verify board initially returns 4 columns
	boardURL := srv.URL + "/api/projects/" + projID + "/board"
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, boardURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("board: expected 200, got %d, body: %s", status, respBody)
	}

	var boardResult map[string]interface{}
	if err := json.Unmarshal(respBody, &boardResult); err != nil {
		t.Fatalf("decode board: %v", err)
	}

	columnsRaw, ok := boardResult["columns"].([]interface{})
	if !ok {
		t.Fatalf("expected 'columns' array in response, got: %v", boardResult)
	}
	if len(columnsRaw) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columnsRaw))
	}

	// Verify column keys in order (rejected goes to manual_intervention)
	expectedKeys := []string{"pending", "in_progress", "completed", "manual_intervention"}
	for i, colRaw := range columnsRaw {
		col := colRaw.(map[string]interface{})
		key := col["key"].(string)
		if key != expectedKeys[i] {
			t.Errorf("column %d: expected key %q, got %q", i, expectedKeys[i], key)
		}
	}

	// Initially, the task's first node (code) is pending → task should be in "pending" column
	findTaskInColumn := func(taskID int32) string {
		for _, colRaw := range columnsRaw {
			col := colRaw.(map[string]interface{})
			tasksRaw, _ := col["tasks"].([]interface{})
			for _, trRaw := range tasksRaw {
				tr := trRaw.(map[string]interface{})
				if int32(tr["id"].(float64)) == taskID {
					return col["key"].(string)
				}
			}
		}
		return ""
	}

	colKey := findTaskInColumn(taskID)
	if colKey != "pending" {
		t.Errorf("initial: expected task in 'pending' column, got '%s'", colKey)
	}

	// Step 2: claim and approve node 1 (code) → node 2 (review) becomes pending
	// Review node is now merged into the same column, so review+pending → "pending"
	node1ID := nodes[0]["id"].(string)
	claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)

	// Now the current node is node 2 (review, pending) → task should be in "pending"
	// (review node is no longer in a separate column)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, boardURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("board after approve: expected 200, got %d", status)
	}
	json.Unmarshal(respBody, &boardResult)
	columnsRaw = boardResult["columns"].([]interface{})

	colKey = findTaskInColumn(taskID)
	if colKey != "pending" {
		t.Errorf("after node1 approved: expected task in 'pending' (review node merged), got '%s'", colKey)
	}

	// Step 3: claim node 2 (review) → in_progress
	// In-progress review node merges into "in_progress"
	node2ID := nodes[1]["id"].(string)
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, boardURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("board after claim review: expected 200, got %d", status)
	}
	json.Unmarshal(respBody, &boardResult)
	columnsRaw = boardResult["columns"].([]interface{})

	colKey = findTaskInColumn(taskID)
	if colKey != "in_progress" {
		t.Errorf("after review claimed: expected task in 'in_progress' (review node merged), got '%s'", colKey)
	}

	// Step 4: approve node 2 (review) → node 3 (deploy, pending) → pending
	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, boardURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("board after approve review: expected 200, got %d", status)
	}
	json.Unmarshal(respBody, &boardResult)
	columnsRaw = boardResult["columns"].([]interface{})

	// Node 3 is standard + pending → "pending" column
	colKey = findTaskInColumn(taskID)
	if colKey != "pending" {
		t.Errorf("after review approved: expected task in 'pending' (deploy node pending), got '%s'", colKey)
	}

	// Step 5: complete all nodes → task should be in "completed" column
	node3ID := nodes[2]["id"].(string)
	claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)

	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, boardURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("board after all approved: expected 200, got %d", status)
	}
	json.Unmarshal(respBody, &boardResult)
	columnsRaw = boardResult["columns"].([]interface{})

	colKey = findTaskInColumn(taskID)
	if colKey != "completed" {
		t.Errorf("after all approved: expected task in 'completed', got '%s'", colKey)
	}

	// Verify task data structure in columns
	for _, colRaw := range columnsRaw {
		col := colRaw.(map[string]interface{})
		tasksRaw, _ := col["tasks"].([]interface{})
		for _, trRaw := range tasksRaw {
			tr := trRaw.(map[string]interface{})
			if int32(tr["id"].(float64)) == taskID {
				// Verify required fields
				if _, ok := tr["title"]; !ok {
					t.Error("task missing 'title' field")
				}
				if _, ok := tr["priority"]; !ok {
					t.Error("task missing 'priority' field")
				}
				if _, ok := tr["type"]; !ok {
					t.Error("task missing 'type' field")
				}
				if _, ok := tr["current_node_name"]; !ok {
					t.Error("task missing 'current_node_name' field")
				}
				if _, ok := tr["current_node_status"]; !ok {
					t.Error("task missing 'current_node_status' field")
				}
				if _, ok := tr["current_node_type"]; !ok {
					t.Error("task missing 'current_node_type' field")
				}
				if _, ok := tr["assignee_id"]; !ok {
					t.Error("task missing 'assignee_id' field")
				}
				t.Logf("Task in column '%s': title=%v, priority=%v, type=%v, node=%v, status=%v, nodeType=%v",
					col["key"], tr["title"], tr["priority"], tr["type"], tr["current_node_name"], tr["current_node_status"], tr["current_node_type"])
			}
		}
	}

	t.Log("TestGetBoard5Columns passed: 4 columns returned with correct grouping")
}
