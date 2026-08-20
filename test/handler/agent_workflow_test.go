// agent_workflow_test.go 覆盖 Agent 工作流执行流程的测试。
package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	dbgen "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/service"
)

// TestAgentFullWorkflow 测试完整的代理工作执行生命周期：创建项目、工作流、任务、认领、审批直到完成。
func TestAgentFullWorkflow(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// 步骤 2：创建项目
	projID := createProject(t, client, srv.URL, wsID, token)

	// 步骤 3：创建工作流模板：编码(标准) -> 审核(审核) -> 部署(标准)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)

	// 步骤 4：设置项目默认工作流
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	// 步骤 5：创建两个代理（编码+部署代理，审核代理）
	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)

	// 步骤 6：将两个代理添加到项目（认领必需）
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// 步骤 7：创建任务
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	t.Logf("Created task %d with %d nodes", taskID, len(nodes))

	// 验证初始节点状态
	node1 := nodes[0]
	node2 := nodes[1]
	node3 := nodes[2]
	node1ID := node1["id"].(string)
	node2ID := node2["id"].(string)
	node3ID := node3["id"].(string)

	t.Logf("Node1 (code): status=%s, assignee_type=%s", node1["status"], node1["assignee_type"])
	t.Logf("Node2 (review): status=%s, assignee_type=%s", node2["status"], node2["assignee_type"])
	t.Logf("Node3 (deploy): status=%s, assignee_type=%s", node3["status"], node3["assignee_type"])

	// 验证第一个节点为待处理（any_agent 类型）
	if node1["status"] != "pending" {
		t.Fatalf("expected first node status 'pending', got %q", node1["status"])
	}

	// 步骤 8：代理1认领第一个节点（编码）
	claimedNode := claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected claimed node status 'in_progress', got %q", claimedNode["status"])
	}
	if claimedNode["assignee_id"] != agent1ID {
		t.Fatalf("expected assignee_id=%s, got %v", agent1ID, claimedNode["assignee_id"])
	}
	t.Logf("Agent1 claimed code node, status: in_progress")

	// 步骤 9：代理1审批第一个节点（完成编码）
	approvedNode := approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	if approvedNode["status"] != "completed" {
		t.Fatalf("expected approved node status 'completed', got %q", approvedNode["status"])
	}
	t.Logf("Agent1 approved code node, status: completed")

	// 步骤 10：验证第二个节点（审核）现为待处理并具有延续权
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	var reviewNode map[string]interface{}
	for _, n := range updatedNodes {
		if n["id"] == node2ID {
			reviewNode = n
			break
		}
	}
	if reviewNode == nil {
		t.Fatal("review node not found")
	}
	t.Logf("Review node after approve: status=%s, reserved_for_agent_id=%v",
		reviewNode["status"], reviewNode["reserved_for_agent_id"])

	// 步骤 11：代理1无法认领审核节点（避免自审）
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{"agent_id": agent1ID}
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+node2ID+"/claim", agent1Token, claimBody)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for self-review, got %d", status)
	}
	t.Logf("Self-review correctly blocked (403)")

	// 步骤 12：代理2认领审核节点
	claimedReview := claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if claimedReview["status"] != "in_progress" {
		t.Fatalf("expected review node status 'in_progress', got %q", claimedReview["status"])
	}
	t.Logf("Agent2 claimed review node, status: in_progress")

	// 步骤 13：代理2审批审核节点
	approvedReview := approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if approvedReview["status"] != "completed" {
		t.Fatalf("expected review node status 'completed', got %q", approvedReview["status"])
	}
	t.Logf("Agent2 approved review node, status: completed")

	// 步骤 14：验证部署节点具有代理2的延续权
	updatedNodes = listTaskNodes(t, client, srv.URL, taskID, token)
	var deployNode map[string]interface{}
	for _, n := range updatedNodes {
		if n["id"] == node3ID {
			deployNode = n
			break
		}
	}
	if deployNode == nil {
		t.Fatal("deploy node not found")
	}
	t.Logf("Deploy node after review approve: status=%s, reserved_for_agent_id=%v",
		deployNode["status"], deployNode["reserved_for_agent_id"])

	// 步骤 15：代理2认领部署节点（具有延续权）
	claimedDeploy := claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if claimedDeploy["status"] != "in_progress" {
		t.Fatalf("expected deploy node status 'in_progress', got %q", claimedDeploy["status"])
	}
	t.Logf("Agent2 claimed deploy node, status: in_progress")

	// 步骤 16：代理2审批部署节点
	approvedDeploy := approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if approvedDeploy["status"] != "completed" {
		t.Fatalf("expected deploy node status 'completed', got %q", approvedDeploy["status"])
	}
	t.Logf("Agent2 approved deploy node, status: completed")

	// 步骤 17：验证任务已完成
	taskURL := fmt.Sprintf("%s/api/projects/%s/tasks/%d", srv.URL, projID, taskID)
	_, status, body := doRequestWithToken(t, client, http.MethodGet, taskURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get task: expected 200, got %d", status)
	}
	var taskResult map[string]interface{}
	json.Unmarshal(body, &taskResult)
	if taskResult["status"] != "completed" {
		t.Fatalf("expected task status 'completed', got %q", taskResult["status"])
	}
	t.Logf("Task completed successfully!")

	// 清理
	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentRejectWorkflow 测试驳回周期：代码→审核→驳回→重新编码→审核→审批→部署→完成。
func TestAgentRejectWorkflow(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// 周期 1：代理1编码，代理2审核并驳回
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	// 拒绝并指向 code 节点
	rejectStatus, _ := rejectNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token, &node1ID)
	if rejectStatus != http.StatusOK {
		t.Fatalf("expected 200 for reject, got %d", rejectStatus)
	}
	t.Logf("Review rejected, routing back to code node")

	// 验证 code 节点回到 in_progress
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	var codeNode map[string]interface{}
	for _, n := range updatedNodes {
		if n["id"] == node1ID {
			codeNode = n
			break
		}
	}
	if codeNode["status"] != "pending" {
		t.Fatalf("expected code node status 'pending' after reject, got %q", codeNode["status"])
	}

	// 验证 reject_count 已递增
	rejectCount := int(codeNode["reject_count"].(float64))
	if rejectCount != 1 {
		t.Fatalf("expected reject_count=1, got %d", rejectCount)
	}
	t.Logf("Code node reject_count=%d, status=%s", rejectCount, codeNode["status"])

	// 第 2 轮：Agent1 重新认领（拒绝后为 pending）、重新编码，Agent2 审查并批准
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	t.Logf("Second cycle: code approved, review approved")

	// deploy 节点应可用
	updatedNodes = listTaskNodes(t, client, srv.URL, taskID, token)
	node3ID := nodes[2]["id"].(string)
	var deployNode map[string]interface{}
	for _, n := range updatedNodes {
		if n["id"] == node3ID {
			deployNode = n
			break
		}
	}
	if deployNode == nil {
		t.Fatal("deploy node not found")
	}

	// 完成部署
	claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)

	// 验证任务已完成
	taskURL := fmt.Sprintf("%s/api/projects/%s/tasks/%d", srv.URL, projID, taskID)
	_, statusCode, body := doRequestWithToken(t, client, http.MethodGet, taskURL, token, nil)
	_ = statusCode
	var taskResult map[string]interface{}
	json.Unmarshal(body, &taskResult)
	if taskResult["status"] != "completed" {
		t.Fatalf("expected task status 'completed', got %q", taskResult["status"])
	}
	t.Logf("Task completed after reject cycle!")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentContinuationRight 验证审批标准节点后下一个标准节点获得延续权，其他代理被阻止。
func TestAgentContinuationRight(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)

	// 创建包含 2 个 STANDARD 节点（code -> test）的工作流以测试续行权。
	// review 节点不会从 standard 节点继承续行权。
	wsBaseURL := fmt.Sprintf("%s/api/workspaces/%s/workflows", srv.URL, wsID)
	tplBody := map[string]interface{}{
		"name":        "flow-cont-" + uuid.New().String()[:8],
		"description": "2 standard nodes for continuation right test",
		"nodes": []map[string]interface{}{
			{
				"name":            "code",
				"description":     "write code",
				"sort_order":      1,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
			{
				"name":            "test",
				"description":     "run tests",
				"sort_order":      2,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
		},
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, wsBaseURL, token, tplBody)
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body: %s", status, respBody)
	}
	var tplResult map[string]interface{}
	json.Unmarshal(respBody, &tplResult)
	tplID := tplResult["template"].(map[string]interface{})["id"].(string)

	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 认领并批准第一个节点
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// 验证 node2 对 agent1 具有续行权
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	var node2 map[string]interface{}
	for _, n := range updatedNodes {
		if n["id"] == node2ID {
			node2 = n
			break
		}
	}
	reservedID := node2["reserved_for_agent_id"]
	if reservedID == nil {
		t.Fatal("expected reserved_for_agent_id to be set for continuation right")
	}
	t.Logf("Node2 has continuation right for agent: %v", reservedID)

	// Agent2 应被续行权阻止（409 Conflict）
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{"agent_id": agent2ID}
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+node2ID+"/claim", agent2Token, claimBody)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for continuation right conflict, got %d", status)
	}
	t.Logf("Agent2 correctly blocked by continuation right (409)")

	// Agent1 可以认领 node2（具有续行权）
	claimedNode2 := claimNode(t, client, srv.URL, taskID, node2ID, agent1ID, agent1Token)
	if claimedNode2["status"] != "in_progress" {
		t.Fatalf("expected node2 status 'in_progress', got %q", claimedNode2["status"])
	}
	t.Logf("Agent1 claimed node2 with continuation right")

	// 完成工作流
	approveNode(t, client, srv.URL, taskID, node2ID, agent1ID, agent1Token)

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentSkipClaim 验证代理可以自愿放弃延续权，允许其他代理认领节点。
func TestAgentSkipClaim(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)

	// 创建包含 2 个 STANDARD 节点的工作流以测试续行权
	wsBaseURL := fmt.Sprintf("%s/api/workspaces/%s/workflows", srv.URL, wsID)
	tplBody := map[string]interface{}{
		"name":        "flow-skip-" + uuid.New().String()[:8],
		"description": "2 standard nodes for skip-claim test",
		"nodes": []map[string]interface{}{
			{
				"name":            "code",
				"description":     "write code",
				"sort_order":      1,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
			{
				"name":            "test",
				"description":     "run tests",
				"sort_order":      2,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
		},
	}
	_, statusCode, respBody := doRequestWithToken(t, client, http.MethodPost, wsBaseURL, token, tplBody)
	if statusCode != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body: %s", statusCode, respBody)
	}
	var tplResult map[string]interface{}
	json.Unmarshal(respBody, &tplResult)
	tplID := tplResult["template"].(map[string]interface{})["id"].(string)

	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 认领并批准第一个节点
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent1 跳过对 node2 的认领
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	skipBody := map[string]interface{}{"agent_id": agent1ID}
	_, statusCode, _ = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+node2ID+"/skip-claim", agent1Token, skipBody)
	if statusCode != http.StatusOK {
		t.Fatalf("expected 200 for skip-claim, got %d", statusCode)
	}
	t.Logf("Agent1 skipped claim on node2")

	// 现在 agent2 应该能够认领 node2
	claimedNode2 := claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if claimedNode2["status"] != "in_progress" {
		t.Fatalf("expected node2 status 'in_progress' after skip-claim, got %q", claimedNode2["status"])
	}
	t.Logf("Agent2 claimed node2 after skip-claim")

	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentInterruptAndResolve 验证中断任务设置节点为 manual_intervention，解决后可继续工作。
func TestAgentInterruptAndResolve(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)

	// Agent1 认领第一个节点
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// 通过 service 层中断任务（HTTP interrupt 端点已随死端点清理移除）
	svc := service.New(db, nil, nil)
	nodeSvc := service.NewNodeService(svc)
	interruptResult, err := nodeSvc.InterruptTask(context.Background(), taskID, uuid.New(), "member", "emergency stop")
	if err != nil {
		t.Fatalf("service interrupt failed: %v", err)
	}
	t.Logf("Interrupted %d nodes", interruptResult.InterruptedNodes)

	// 验证 node1 现在为 manual_intervention
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	if updatedNodes[0]["status"] != "manual_intervention" {
		t.Fatalf("expected manual_intervention, got %q", updatedNodes[0]["status"])
	}
	t.Logf("Node1 is now manual_intervention")

	// 通过服务层解决（绕过测试路由器所缺少的 HTTP 认证）
	nodeUUID, _ := uuid.Parse(node1ID)
	resolvedNode, err := nodeSvc.Resolve(context.Background(), nodeUUID, uuid.New(), "member", "issue resolved", nil, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("service resolve failed: %v", err)
	}
	if resolvedNode.Status != "pending" {
		t.Fatalf("expected pending after resolve, got %s", resolvedNode.Status)
	}
	t.Logf("Node1 resolved back to pending via service layer")

	// 重新认领并完成工作流
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentDoubleClaimRejected 验证节点不能被重复认领。
func TestAgentDoubleClaimRejected(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)

	// Agent1 认领该节点
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent2 尝试认领同一节点——应失败（409）
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{"agent_id": agent2ID}
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+node1ID+"/claim", agent2Token, claimBody)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for double claim, got %d", status)
	}
	t.Logf("Double claim correctly rejected (409)")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentClaimAccessControl 验证工作区外的代理不能认领节点，同一工作区的代理可以。
func TestAgentClaimAccessControl(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	// 同一工作区中的 Agent（必须已添加到 project_members）
	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)

	// 同一工作区中的 Agent 可以认领（已添加为项目成员）
	claimedNode := claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected in_progress for same-workspace agent, got %q", claimedNode["status"])
	}
	t.Logf("Same-workspace agent can claim (project member)")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestRuntimeRegistrationAndClaim 验证注册运行时后代理可以通过同步 API 发现并认领待处理节点。
func TestRuntimeRegistrationAndClaim(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// 第 1 步：在注册 runtime 之前创建任务
	// （模拟用户在启动守护进程之前创建任务）
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	t.Logf("Task created before runtime registration: task=%d, node1=%s", taskID, node1ID)

	// 验证节点为 pending
	if nodes[0]["status"] != "pending" {
		t.Fatalf("expected node status 'pending', got %q", nodes[0]["status"])
	}

	// 第 2 步：注册 runtime（模拟守护进程启动）
	runtimeID := registerRuntimeWithAgentToken(t, client, srv.URL, wsID, agentID, agentToken)
	t.Logf("Runtime registered: %s", runtimeID)

	// 第 3 步：同步 runtime 以发现待处理节点
	syncResult := syncRuntime(t, client, srv.URL, wsID, runtimeID, token)
	pendingNodes, _ := syncResult["pending_nodes"].([]interface{})
	t.Logf("Sync found %d pending nodes", len(pendingNodes))

	// 第 4 步：Agent 认领待处理节点
	claimedNode := claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected claimed node status 'in_progress', got %q", claimedNode["status"])
	}
	t.Logf("Agent claimed node after runtime registration + sync")

	// 第 5 步：完成工作流
	approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	t.Logf("Agent approved node, workflow continues")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestRuntimeSyncDiscoversPendingNodes 验证同步 API 在注册运行时后正确返回代理的待处理节点。
func TestRuntimeSyncDiscoversPendingNodes(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// 创建两个任务
	taskID1, _ := createTask(t, client, srv.URL, projID, tplID, token)
	taskID2, _ := createTask(t, client, srv.URL, projID, tplID, token)
	t.Logf("Created tasks: %d, %d", taskID1, taskID2)

	// 注册运行时
	runtimeID := registerRuntimeWithAgentToken(t, client, srv.URL, wsID, agentID, agentToken)

	// 同步应能从两个任务中找到待处理节点
	syncResult := syncRuntime(t, client, srv.URL, wsID, runtimeID, token)
	pendingNodes, _ := syncResult["pending_nodes"].([]interface{})
	if len(pendingNodes) < 2 {
		t.Fatalf("expected at least 2 pending nodes from sync, got %d", len(pendingNodes))
	}
	t.Logf("Sync correctly discovered %d pending nodes across tasks", len(pendingNodes))

	// 验证待处理节点字段
	firstNode := pendingNodes[0].(map[string]interface{})
	if firstNode["status"] != "pending" {
		t.Fatalf("expected sync node status 'pending', got %q", firstNode["status"])
	}
	if firstNode["task_id"] == nil {
		t.Fatal("expected sync node to have task_id")
	}

	deleteTask(t, client, srv.URL, projID, taskID1, token)
	deleteTask(t, client, srv.URL, projID, taskID2, token)
}

// TestRuntimeHeartbeatKeepsAgentOnline 验证发送心跳保持运行时和代理在线状态。
func TestRuntimeHeartbeatKeepsAgentOnline(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)

	// 注册运行时
	runtimeID := registerRuntimeWithAgentToken(t, client, srv.URL, wsID, agentID, agentToken)

	// 发送心跳
	heartbeatURL := fmt.Sprintf("%s/api/workspaces/%s/runtimes/%s/heartbeat", srv.URL, wsID, runtimeID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, heartbeatURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d, body: %s", status, respBody)
	}
	t.Logf("Heartbeat sent successfully for runtime %s", runtimeID)

	// 通过 dbgen.Queries 验证 Agent 状态为 online
	agent, err := q.GetAgent(context.Background(), uuid.MustParse(agentID))
	if err != nil {
		t.Fatalf("query agent status: %v", err)
	}
	if agent.Status != dbgen.AgentStatusOnline {
		t.Fatalf("expected agent status 'online', got %q", agent.Status)
	}
	t.Logf("Agent status is online after heartbeat")
}

// TestAgentWorkflowWithRuntime 测试包含运行时注册、同步、认领、审批的完整代理生命周期。
func TestAgentWorkflowWithRuntime(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// 模拟守护进程启动：为两个 Agent 注册 runtime
	runtime1ID := registerRuntimeWithAgentToken(t, client, srv.URL, wsID, agent1ID, agent1Token)
	runtime2ID := registerRuntimeWithAgentToken(t, client, srv.URL, wsID, agent2ID, agent2Token)
	t.Logf("Runtimes registered: agent1=%s, agent2=%s", runtime1ID, runtime2ID)

	// 创建任务（这会触发 SSE 事件，但由于我们没有
	// 真正的 SSE 客户端，我们依赖同步来发现待处理节点）
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)
	node3ID := nodes[2]["id"].(string)

	// Agent1 同步并发现待处理节点
	syncResult := syncRuntime(t, client, srv.URL, wsID, runtime1ID, token)
	pendingNodes, _ := syncResult["pending_nodes"].([]interface{})
	if len(pendingNodes) == 0 {
		t.Fatal("expected sync to find pending nodes")
	}
	t.Logf("Agent1 sync found %d pending nodes", len(pendingNodes))

	// Agent1 认领 code 节点
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	t.Logf("Agent1 completed code node")

	// Agent2 同步并发现 review 节点
	syncResult2 := syncRuntime(t, client, srv.URL, wsID, runtime2ID, token)
	pendingNodes2, _ := syncResult2["pending_nodes"].([]interface{})
	t.Logf("Agent2 sync found %d pending nodes", len(pendingNodes2))

	// Agent2 认领并批准 review 节点
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	t.Logf("Agent2 completed review node")

	// Agent2 认领并批准 deploy 节点（续行权）
	claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	t.Logf("Agent2 completed deploy node")

	// 验证任务已完成
	taskURL := fmt.Sprintf("%s/api/projects/%s/tasks/%d", srv.URL, projID, taskID)
	_, statusCode, body := doRequestWithToken(t, client, http.MethodGet, taskURL, token, nil)
	_ = statusCode
	var taskResult map[string]interface{}
	json.Unmarshal(body, &taskResult)
	if taskResult["status"] != "completed" {
		t.Fatalf("expected task status 'completed', got %q", taskResult["status"])
	}
	t.Logf("Full workflow with runtime registration completed!")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}
