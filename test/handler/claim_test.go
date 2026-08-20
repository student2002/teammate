// claim_test.go 覆盖节点认领接口的测试。
package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClaimSelfReview 验证代理不能认领自己编写的前一个标准节点的审核节点（自审预防）。
func TestClaimSelfReview(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// 准备：项目、包含 code + review 节点的工作流
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// 创建任务
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	codeNodeID := nodes[0]["id"].(string)
	reviewNodeID := nodes[1]["id"].(string)

	// Agent 1 认领并完成 code 节点
	claimedCode := claimNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)
	if claimedCode["status"] != "in_progress" {
		t.Fatalf("code node: expected status 'in_progress', got %v", claimedCode["status"])
	}

	approvedCode := approveNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)
	if approvedCode["status"] != "completed" {
		t.Fatalf("code node: expected status 'completed' after approve, got %v", approvedCode["status"])
	}

	// 同一 Agent 尝试认领 review 节点 → 应返回 403（自我审查）
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{
		"agent_id": agent1ID,
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+reviewNodeID+"/claim", agent1Token, claimBody)
	if status != http.StatusForbidden {
		t.Fatalf("self-review claim: expected 403 Forbidden, got %d, body: %s", status, respBody)
	}
	t.Logf("Self-review correctly blocked with 403, body: %s", respBody)

	// 不同 Agent 尝试认领 review 节点 → 应成功
	claimBody2 := map[string]interface{}{
		"agent_id": agent2ID,
	}
	_, status, respBody = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+reviewNodeID+"/claim", agent2Token, claimBody2)
	if status != http.StatusOK {
		t.Fatalf("different agent claim: expected 200, got %d, body: %s", status, respBody)
	}
	t.Log("Different agent successfully claimed review node")
}
