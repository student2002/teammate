// security_statemachine_test.go 覆盖状态机安全约束的测试。
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// TestNode3CannotBeClaimedBeforeNode2Completed 验证在 node2（sort_order=2）未完成时无法认领 node3（sort_order=3）。
func TestNode3CannotBeClaimedBeforeNode2Completed(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	node1ID := nodes[0]["id"].(string)
	node3ID := nodes[2]["id"].(string)

	// 认领 node1 但不完成它
	claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)

	// 尝试认领 node3——应失败，因为 node2 未完成
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{"agent_id": agentID}
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+node3ID+"/claim", agentToken, body)
	if status != http.StatusConflict && status != http.StatusForbidden {
		t.Errorf("claiming node3 before node2 completed: expected 409/403, got %d", status)
	}
}

// TestRejectTargetNodeCanBeReclaimed 验证驳回后目标节点被置为 pending 状态并可被其他代理重新认领。
func TestRejectTargetNodeCanBeReclaimed(t *testing.T) {
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

	// Agent1 认领并完成 node1
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent2 认领 node2（review）
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	// 拒绝 node2 并指向 node1
	status, _ := rejectNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token, &node1ID)
	if status != 200 {
		t.Fatalf("reject: expected 200, got %d", status)
	}

	// 验证 node1 现在为 pending（而非 in_progress）
	q = dbQueries(t, db)
	node1UUID := parseUUID(t, node1ID)
	node1, err := q.GetTaskNode(t.Context(), node1UUID)
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if node1.Status != "pending" {
		t.Errorf("after reject, target node should be pending, got %v", node1.Status)
	}
	if node1.AssigneeID.Valid {
		t.Errorf("after reject, target node assignee_id should be NULL, got %v", node1.AssigneeID)
	}

	// Agent1 可以重新认领 node1
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
}

// TestAgentCanCompleteStandardNode 验证代理可以使用 /complete 端点完成标准节点而无需 task:approve 权限。
func TestAgentCanCompleteStandardNode(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	// 仅授予 task:claim 和 task:execute，不授予 task:approve
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:claim", token)
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:execute", token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	codeNodeID := nodes[0]["id"].(string)

	// Agent 认领 code 节点
	claimNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken)

	// Agent 使用 /complete 端点（而非 /approve）
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{
		"summary": "Code completed by agent",
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+codeNodeID+"/complete", agentToken, body)
	if status != http.StatusOK {
		t.Fatalf("complete standard node: expected 200, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != "completed" {
		t.Errorf("expected status 'completed', got %v", result["status"])
	}
}

// TestResolvedManualInterventionNodeCanBeCompleted 验证节点可以从
// manual_intervention -> pending -> in_progress -> completed 流转，前提是已完成人工处理。
func TestResolvedManualInterventionNodeCanBeCompleted(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:claim", token)
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:execute", token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	nodeID := nodes[0]["id"].(string)
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)

	claimNode(t, client, srv.URL, taskID, nodeID, agentID, agentToken)

	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/manual", agentToken, map[string]interface{}{
		"comment": "needs OpenClaw explanation",
	})
	if status != http.StatusOK {
		t.Fatalf("manual intervention: expected 200, got %d, body: %s", status, respBody)
	}

	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/resolve", token, map[string]interface{}{
		"comment": "OpenClaw is a code agent tool; continue",
	})
	if status != http.StatusOK {
		t.Fatalf("resolve manual intervention: expected 200, got %d, body: %s", status, respBody)
	}
	var resolved map[string]interface{}
	if err := json.Unmarshal(respBody, &resolved); err != nil {
		t.Fatalf("decode resolved node: %v", err)
	}
	if resolved["status"] != "pending" {
		t.Fatalf("expected pending after resolve, got %v", resolved["status"])
	}

	claimed := claimNode(t, client, srv.URL, taskID, nodeID, agentID, agentToken)
	if claimed["status"] != "in_progress" {
		t.Fatalf("expected in_progress after reclaim, got %v", claimed["status"])
	}

	_, status, respBody = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/complete", agentToken, map[string]interface{}{
		"summary": "已完成 OpenClaw 介绍文章的编写",
	})
	if status != http.StatusOK {
		t.Fatalf("complete resolved node: expected 200, got %d, body: %s", status, respBody)
	}
	var completed map[string]interface{}
	if err := json.Unmarshal(respBody, &completed); err != nil {
		t.Fatalf("decode completed node: %v", err)
	}
	if completed["status"] != "completed" {
		t.Fatalf("expected completed after second execution, got %v", completed["status"])
	}
}

// TestAgentCannotCompleteReviewNode 验证 /complete 端点拒绝审核节点（审核节点需要 approve/reject）。
func TestAgentCannotCompleteReviewNode(t *testing.T) {
	router, db, _ := setupTestRouter(t)
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
	addAgentToProject(t, dbgen.New(db), projID, agent1ID)
	addAgentToProject(t, dbgen.New(db), projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentPermission(t, client, srv.URL, wsID, agent2ID, "task:claim", token)
	grantAgentPermission(t, client, srv.URL, wsID, agent2ID, "task:execute", token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	codeNodeID := nodes[0]["id"].(string)
	reviewNodeID := nodes[1]["id"].(string)

	// Agent1 完成 code 节点
	claimNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)

	// Agent2 认领 review 节点
	claimNode(t, client, srv.URL, taskID, reviewNodeID, agent2ID, agent2Token)

	// Agent2 尝试在 review 节点上调用 /complete——应失败
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{"summary": "should fail"}
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+reviewNodeID+"/complete", agent2Token, body)
	if status != http.StatusBadRequest {
		t.Errorf("complete on review node: expected 400, got %d", status)
	}
}

// TestInterruptAckEndpoint 验证代理可以通过 /interrupt-ack 端点确认中断。
func TestInterruptAckEndpoint(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	codeNodeID := nodes[0]["id"].(string)

	// Agent 认领 code 节点
	claimNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken)

	// Agent 发送中断确认
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{
		"comment": "interrupt acknowledged",
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+codeNodeID+"/interrupt-ack", agentToken, body)
	if status != http.StatusOK {
		t.Fatalf("interrupt-ack: expected 200, got %d, body: %s", status, respBody)
	}
}

// TestLogUploadCannotForgeNode 验证日志消息不能上传到 URL 中不属于该任务的节点。
func TestLogUploadCannotForgeNode(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// 创建两个任务
	taskID1, nodes1 := createTask(t, client, srv.URL, projID, tplID, token)
	taskID2, _ := createTask(t, client, srv.URL, projID, tplID, token)

	// Agent 认领 task1 中的节点
	claimNode(t, client, srv.URL, taskID1, nodes1[0]["id"].(string), agentID, agentToken)

	// Agent 尝试使用 task1 的节点向 task2 上传日志
	body := map[string]interface{}{
		"node_id": nodes1[0]["id"].(string), // 该节点属于 task1
		"type":    "stdout",
		"content": "forged log",
	}
	url := fmt.Sprintf("%s/api/tasks/%d/messages", srv.URL, taskID2) // 但 URL 指向 task2
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, url, agentToken, body)
	if status != http.StatusForbidden {
		t.Errorf("log upload with wrong task: expected 403, got %d, body: %s", status, string(respBody))
	}
}

func TestTaskLogsFallbackToDatabaseWhenRedisBufferEmpty(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	nodeID := nodes[0]["id"].(string)
	claimNode(t, client, srv.URL, taskID, nodeID, agentID, agentToken)

	body := map[string]interface{}{
		"node_id": nodeID,
		"type":    "stdout",
		"content": "persistent log line",
	}
	messagesURL := fmt.Sprintf("%s/api/tasks/%d/messages", srv.URL, taskID)
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, messagesURL, agentToken, body)
	if status != http.StatusAccepted {
		t.Fatalf("post log: expected 202, got %d, body: %s", status, respBody)
	}

	logsURL := fmt.Sprintf("%s/api/tasks/%d/logs?node_id=%s", srv.URL, taskID, nodeID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, logsURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("get logs: expected 200, got %d, body: %s", status, respBody)
	}

	var logs []map[string]interface{}
	if err := json.Unmarshal(respBody, &logs); err != nil {
		t.Fatalf("decode logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one durable log, got %d: %s", len(logs), respBody)
	}
	if logs[0]["node_id"] != nodeID {
		t.Fatalf("expected node_id %s, got %v", nodeID, logs[0]["node_id"])
	}
	if logs[0]["content"] != "persistent log line" {
		t.Fatalf("expected persisted content, got %v", logs[0]["content"])
	}
}

func TestTaskLogsRejectsInvalidNodeIDFilter(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)
	taskID, _ := createTask(t, client, srv.URL, projID, tplID, token)

	logsURL := fmt.Sprintf("%s/api/tasks/%d/logs?node_id=not-a-uuid", srv.URL, taskID)
	_, status, _ := doRequestWithToken(t, client, http.MethodGet, logsURL, token, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("get logs with invalid node_id: expected 400, got %d", status)
	}
}
