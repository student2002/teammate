// security_statemachine_test.go tests covering state machine security constraints.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// TestNode3CannotBeClaimedBeforeNode2Completed verifies that node3 (sort_order=3) cannot be claimed when node2 (sort_order=2) is not completed.
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

	// Claim node1 but do not complete it
	claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)

	// Attempt to claim node3 — should fail because node2 is not completed
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{"agent_id": agentID}
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+node3ID+"/claim", agentToken, body)
	if status != http.StatusConflict && status != http.StatusForbidden {
		t.Errorf("claiming node3 before node2 completed: expected 409/403, got %d", status)
	}
}

// TestRejectTargetNodeCanBeReclaimed verifies that after rejection, the target node is set to pending status and can be reclaimed by other agents.
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

	// Agent1 claims and completes node1
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent2 claims node2 (review)
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	// Reject node2 targeting node1
	status, _ := rejectNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token, &node1ID)
	if status != 200 {
		t.Fatalf("reject: expected 200, got %d", status)
	}

	// Verify node1 is now pending (not in_progress)
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

	// Agent1 can reclaim node1
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
}

// TestAgentCanCompleteStandardNode verifies that an agent can complete a standard node using the /complete endpoint without task:approve permission.
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
	// Only grant task:claim and task:execute, not task:approve
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:claim", token)
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:execute", token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	codeNodeID := nodes[0]["id"].(string)

	// Agent claims code node
	claimNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken)

	// Agent uses /complete endpoint (not /approve)
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

// TestResolvedManualInterventionNodeCanBeCompleted verifies that a node can transition from
// manual_intervention -> pending -> in_progress -> completed after manual resolution.
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
		"summary": "Completed writing the OpenClaw introductory article",
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

// TestAgentCannotCompleteReviewNode verifies that the /complete endpoint rejects review nodes (review nodes require approve/reject).
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

	// Agent1 completes code node
	claimNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)

	// Agent2 claims review node
	claimNode(t, client, srv.URL, taskID, reviewNodeID, agent2ID, agent2Token)

	// Agent2 attempts to call /complete on review node — should fail
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{"summary": "should fail"}
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+reviewNodeID+"/complete", agent2Token, body)
	if status != http.StatusBadRequest {
		t.Errorf("complete on review node: expected 400, got %d", status)
	}
}

// TestInterruptAckEndpoint verifies that an agent can acknowledge an interrupt via the /interrupt-ack endpoint.
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

	// Agent claims code node
	claimNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken)

	// Agent sends interrupt acknowledgment
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	body := map[string]interface{}{
		"comment": "interrupt acknowledged",
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+codeNodeID+"/interrupt-ack", agentToken, body)
	if status != http.StatusOK {
		t.Fatalf("interrupt-ack: expected 200, got %d, body: %s", status, respBody)
	}
}

// TestLogUploadCannotForgeNode verifies that log messages cannot be uploaded to a node that does not belong to the task in the URL.
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

	// Create two tasks
	taskID1, nodes1 := createTask(t, client, srv.URL, projID, tplID, token)
	taskID2, _ := createTask(t, client, srv.URL, projID, tplID, token)

	// Agent claims a node in task1
	claimNode(t, client, srv.URL, taskID1, nodes1[0]["id"].(string), agentID, agentToken)

	// Agent attempts to upload a log to task2 using a node from task1
	body := map[string]interface{}{
		"node_id": nodes1[0]["id"].(string), // this node belongs to task1
		"type":    "stdout",
		"content": "forged log",
	}
	url := fmt.Sprintf("%s/api/tasks/%d/messages", srv.URL, taskID2) // but URL points to task2
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
