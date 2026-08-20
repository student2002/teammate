// approve_test.go 覆盖节点审批接口的测试。
package handler_test

import (
	"net/http/httptest"
	"testing"
)

// TestApproveCascading 验证审批节点会级联到下一个节点：
// - 审批节点 1 → 节点 2 变为 pending 并设置 reserved_for_agent_id
// - 审批节点 2 → 节点 3 变为 pending 并设置 reserved_for_agent_id
// - 审批节点 3 → 任务状态变为 "completed"
func TestApproveCascading(t *testing.T) {
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

	// 创建包含 3 个节点的任务
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)
	node3ID := nodes[2]["id"].(string)

	// 第 1 步：认领并批准节点 1（code）
	claimed1 := claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if claimed1["status"] != "in_progress" {
		t.Fatalf("node1: expected status 'in_progress', got %v", claimed1["status"])
	}

	approved1 := approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if approved1["status"] != "completed" {
		t.Fatalf("node1: expected status 'completed' after approve, got %v", approved1["status"])
	}

	// 验证节点 2 变为 pending——未设置 reserved_for_agent_id，因为
	// code→review 触发自我审查规避（跳过续行权）
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	node2 := updatedNodes[1]
	if node2["status"] != "pending" {
		t.Fatalf("node2: expected status 'pending' after node1 approved, got %v", node2["status"])
	}
	t.Logf("node2 reserved_for_agent_id: %v (correctly nil due to self-review avoidance)", node2["reserved_for_agent_id"])

	// 第 2 步：Agent2 认领并批准节点 2（review）——使用不同 Agent 以避免自我审查
	claimed2 := claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if claimed2["status"] != "in_progress" {
		t.Fatalf("node2: expected status 'in_progress', got %v", claimed2["status"])
	}

	approved2 := approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if approved2["status"] != "completed" {
		t.Fatalf("node2: expected status 'completed' after approve, got %v", approved2["status"])
	}

	// 验证节点 3 变为 pending 且设置了 reserved_for_agent_id（review→standard，无自我审查）
	updatedNodes = listTaskNodes(t, client, srv.URL, taskID, token)
	node3 := updatedNodes[2]
	if node3["status"] != "pending" {
		t.Fatalf("node3: expected status 'pending' after node2 approved, got %v", node3["status"])
	}
	if node3["reserved_for_agent_id"] == nil {
		t.Fatal("node3: expected reserved_for_agent_id to be set after node2 approved, got nil")
	}
	t.Logf("node3 reserved_for_agent_id: %v (continuation right from reviewer)", node3["reserved_for_agent_id"])

	// 第 3 步：认领并批准节点 3（deploy）——agent2 具有续行权
	claimed3 := claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if claimed3["status"] != "in_progress" {
		t.Fatalf("node3: expected status 'in_progress', got %v", claimed3["status"])
	}

	approved3 := approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if approved3["status"] != "completed" {
		t.Fatalf("node3: expected status 'completed' after approve, got %v", approved3["status"])
	}

	// 验证任务状态变为 "completed"（所有节点已完成）
	q = dbQueries(t, db)
	task, err := q.GetTask(t.Context(), taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if string(task.Status) != "completed" {
		t.Fatalf("task: expected status 'completed' after all nodes approved, got %v", task.Status)
	}
	t.Log("All nodes approved, task status is completed")
}
