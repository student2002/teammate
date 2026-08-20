// node_ops_test.go 覆盖节点操作接口的测试。
package handler_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	dbgen "github.com/teammate/server/internal/db/generated"
)

func setupNodeOpsTestRouter(t *testing.T) (*httptest.Server, *sql.DB, *dbgen.Queries) {
	t.Helper()

	router, db, q := setupTestRouter(t)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, db, q
}

// TestContinuationRightConflict 验证代码→审核流程中自审预防机制跳过延续权。
func TestContinuationRightConflict(t *testing.T) {
	ts, _, q := setupNodeOpsTestRouter(t)
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)
	agent1ID, agent1Token := createAgent(t, client, ts.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, ts.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, ts.URL, wsID, token)
	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, ts.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, ts.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, ts.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 认领并批准 node1
	claimNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)

	// 批准后，node2 的 reserved_for_agent_id 应为 agent1
	// （续行权，除非 standard 之后是 review 节点）
	// 在我们的 3 节点流程中：code(standard) -> review(review) -> deploy(standard)
	// code->review：防止自我审查，因此不应设置 reserved_for_agent_id
	// 我们通过尝试让 agent2 认领 node2 来验证——应成功（无预留）

	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, node2ID),
		agent2Token, map[string]interface{}{"agent_id": agent2ID})

	// 由于 code->review 跳过续行权，agent2 应该能够认领
	if status != http.StatusOK {
		t.Fatalf("agent2 should be able to claim review node (no continuation right for code->review), got %d", status)
	}
}

// TestContinuationRightHoldsForDeploy 验证标准→标准流程中延续权生效，其他代理无法认领。
func TestContinuationRightHoldsForDeploy(t *testing.T) {
	ts, _, q := setupNodeOpsTestRouter(t)
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)
	agent1ID, agent1Token := createAgent(t, client, ts.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, ts.URL, wsID, token)

	// 创建工作流：code(standard) -> test(standard) -> deploy(standard)
	// 这样 code->test 具有续行权，test->deploy 也具有续行权
	tplBody := map[string]interface{}{
		"name":        "flow-continuation",
		"description": "3 standard nodes",
		"nodes": []map[string]interface{}{
			{"name": "code", "description": "write code", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent", "timeout_minutes": 60},
			{"name": "test", "description": "run tests", "sort_order": 2, "node_type": "standard", "assignee_type": "any_agent", "timeout_minutes": 30},
			{"name": "deploy", "description": "deploy", "sort_order": 3, "node_type": "standard", "assignee_type": "any_agent", "timeout_minutes": 60},
		},
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, ts.URL+"/api/workspaces/"+wsID+"/workflows", token, tplBody)
	if status != http.StatusCreated {
		t.Fatalf("create workflow: expected 201, got %d, body: %s", status, respBody)
	}
	var tplResult map[string]interface{}
	json.Unmarshal(respBody, &tplResult)
	tplID := tplResult["template"].(map[string]interface{})["id"].(string)

	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, ts.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, ts.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, ts.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 认领并批准 node1（code）
	claimNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)

	// 现在 node2（test）的 reserved_for_agent_id 应为 agent1（续行权）
	// Agent2 尝试认领——应返回 409
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, node2ID),
		agent2Token, map[string]interface{}{"agent_id": agent2ID})

	if status != http.StatusConflict {
		t.Fatalf("agent2 should get 409 (continuation right), got %d", status)
	}
}

// TestSkipClaim 验证代理可以跳过延续权，使其他代理能够认领节点。
func TestSkipClaim(t *testing.T) {
	ts, _, q := setupNodeOpsTestRouter(t)
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)
	agent1ID, agent1Token := createAgent(t, client, ts.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, ts.URL, wsID, token)

	// 相同的 3 个 standard 节点工作流
	tplBody := map[string]interface{}{
		"name":        "flow-skip-claim",
		"description": "3 standard nodes",
		"nodes": []map[string]interface{}{
			{"name": "code", "description": "write code", "sort_order": 1, "node_type": "standard", "assignee_type": "any_agent", "timeout_minutes": 60},
			{"name": "test", "description": "run tests", "sort_order": 2, "node_type": "standard", "assignee_type": "any_agent", "timeout_minutes": 30},
			{"name": "deploy", "description": "deploy", "sort_order": 3, "node_type": "standard", "assignee_type": "any_agent", "timeout_minutes": 60},
		},
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, ts.URL+"/api/workspaces/"+wsID+"/workflows", token, tplBody)
	if status != http.StatusCreated {
		t.Fatalf("create workflow: expected 201, got %d", status)
	}
	var tplResult map[string]interface{}
	json.Unmarshal(respBody, &tplResult)
	tplID := tplResult["template"].(map[string]interface{})["id"].(string)

	projID := createProject(t, client, ts.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, ts.URL, wsID, projID, tplID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, ts.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, ts.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, ts.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 认领并批准 node1
	claimNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent1 跳过对 node2 的认领
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/skip-claim", ts.URL, taskID, node2ID),
		agent1Token, map[string]interface{}{"agent_id": agent1ID})
	if status != http.StatusOK {
		t.Fatalf("skip-claim: expected 200, got %d", status)
	}

	// 现在 agent2 应该能够认领 node2
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, node2ID),
		agent2Token, map[string]interface{}{"agent_id": agent2ID})
	if status != http.StatusOK {
		t.Fatalf("agent2 should claim after skip-claim, got %d", status)
	}
}
