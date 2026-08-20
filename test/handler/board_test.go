// board_test.go 覆盖看板接口的测试。
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetBoard5Columns 验证看板 API 返回 5 列且根据节点状态正确分组。审核节点与标准节点合并到同一列中。
func TestGetBoard5Columns(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// 准备：项目、包含 3 个节点（code -> review -> deploy）的工作流
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// 创建包含 3 个节点的任务（code[pending] -> review[pending] -> deploy[pending]）
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	// 第 1 步：验证看板初始返回 4 列
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

	// 按顺序验证列键（rejected 归入 manual_intervention）
	expectedKeys := []string{"pending", "in_progress", "completed", "manual_intervention"}
	for i, colRaw := range columnsRaw {
		col := colRaw.(map[string]interface{})
		key := col["key"].(string)
		if key != expectedKeys[i] {
			t.Errorf("column %d: expected key %q, got %q", i, expectedKeys[i], key)
		}
	}

	// 初始时，任务的第一个节点（code）为 pending → 任务应处于 "pending" 列
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

	// 第 2 步：认领并批准节点 1（code）→ 节点 2（review）变为 pending
	// review 节点现在合并到同一列，因此 review+pending → "pending"
	node1ID := nodes[0]["id"].(string)
	claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)

	// 现在当前节点是 node 2（review、pending）→ 任务应处于 "pending"
	// （review 节点不再位于单独的列中）
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

	// 第 3 步：认领节点 2（review）→ in_progress
	// 进行中的 review 节点合并到 "in_progress"
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

	// 第 4 步：批准节点 2（review）→ 节点 3（deploy、pending）→ pending
	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, boardURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("board after approve review: expected 200, got %d", status)
	}
	json.Unmarshal(respBody, &boardResult)
	columnsRaw = boardResult["columns"].([]interface{})

	// 节点 3 是 standard + pending → "pending" 列
	colKey = findTaskInColumn(taskID)
	if colKey != "pending" {
		t.Errorf("after review approved: expected task in 'pending' (deploy node pending), got '%s'", colKey)
	}

	// 第 5 步：完成所有节点 → 任务应处于 "completed" 列
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

	// 验证列中的任务数据结构
	for _, colRaw := range columnsRaw {
		col := colRaw.(map[string]interface{})
		tasksRaw, _ := col["tasks"].([]interface{})
		for _, trRaw := range tasksRaw {
			tr := trRaw.(map[string]interface{})
			if int32(tr["id"].(float64)) == taskID {
				// 验证必填字段
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
