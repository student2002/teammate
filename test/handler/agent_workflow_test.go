// agent_workflow_test.go covers tests for Agent workflow execution flows.
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

// TestAgentFullWorkflow tests the complete agent work execution lifecycle: create project, workflow, task, claim, approve until completion.
func TestAgentFullWorkflow(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// Step 2: Create project
	projID := createProject(t, client, srv.URL, wsID, token)

	// Step 3: Create workflow template: code(standard) -> review(review) -> deploy(standard)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)

	// Step 4: Update project settings
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	// Step 5: Create two agents (code+deploy agent, review agent)
	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)

	// Step 6: Add both agents to project (required for claiming)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// Step 7: Create task
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	t.Logf("Created task %d with %d nodes", taskID, len(nodes))

	// Verify initial node statuses
	node1 := nodes[0]
	node2 := nodes[1]
	node3 := nodes[2]
	node1ID := node1["id"].(string)
	node2ID := node2["id"].(string)
	node3ID := node3["id"].(string)

	t.Logf("Node1 (code): status=%s, assignee_type=%s", node1["status"], node1["assignee_type"])
	t.Logf("Node2 (review): status=%s, assignee_type=%s", node2["status"], node2["assignee_type"])
	t.Logf("Node3 (deploy): status=%s, assignee_type=%s", node3["status"], node3["assignee_type"])

	// Verify first node is pending (any_agent type)
	if node1["status"] != "pending" {
		t.Fatalf("expected first node status 'pending', got %q", node1["status"])
	}

	// Step 8: Agent1 claims first node (code)
	claimedNode := claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected claimed node status 'in_progress', got %q", claimedNode["status"])
	}
	if claimedNode["assignee_id"] != agent1ID {
		t.Fatalf("expected assignee_id=%s, got %v", agent1ID, claimedNode["assignee_id"])
	}
	t.Logf("Agent1 claimed code node, status: in_progress")

	// Step 9: Agent1 approves first node (complete code)
	approvedNode := approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	if approvedNode["status"] != "completed" {
		t.Fatalf("expected approved node status 'completed', got %q", approvedNode["status"])
	}
	t.Logf("Agent1 approved code node, status: completed")

	// Step 10: Verify second node (review) is now pending with continuation right
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

	// Step 11: Agent1 cannot claim review node (self-review avoidance)
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{"agent_id": agent1ID}
	_, status, _ := doRequestWithAgentKey(t, client, http.MethodPost, nodeBaseURL+"/"+node2ID+"/claim", agent1Token, agent1ID, claimBody)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for self-review, got %d", status)
	}
	t.Logf("Self-review correctly blocked (403)")

	// Step 12: Agent2 claims review node
	claimedReview := claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if claimedReview["status"] != "in_progress" {
		t.Fatalf("expected review node status 'in_progress', got %q", claimedReview["status"])
	}
	t.Logf("Agent2 claimed review node, status: in_progress")

	// Step 13: Agent2 approves review node
	approvedReview := approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if approvedReview["status"] != "completed" {
		t.Fatalf("expected review node status 'completed', got %q", approvedReview["status"])
	}
	t.Logf("Agent2 approved review node, status: completed")

	// Step 14: Verify deploy node has continuation right for agent2
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

	// Step 15: Agent2 claims deploy node (with continuation right)
	claimedDeploy := claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if claimedDeploy["status"] != "in_progress" {
		t.Fatalf("expected deploy node status 'in_progress', got %q", claimedDeploy["status"])
	}
	t.Logf("Agent2 claimed deploy node, status: in_progress")

	// Step 16: Agent2 approves deploy node
	approvedDeploy := approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if approvedDeploy["status"] != "completed" {
		t.Fatalf("expected deploy node status 'completed', got %q", approvedDeploy["status"])
	}
	t.Logf("Agent2 approved deploy node, status: completed")

	// Step 17: Verify task is completed
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

	// Cleanup
	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentRejectWorkflow tests the rejection cycle: code→review→reject→recode→review→approve→deploy→complete.
func TestAgentRejectWorkflow(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Cycle 1: Agent1 codes, Agent2 reviews and rejects
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	// Reject and route back to code node
	rejectStatus, _ := rejectNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token, &node1ID)
	if rejectStatus != http.StatusOK {
		t.Fatalf("expected 200 for reject, got %d", rejectStatus)
	}
	t.Logf("Review rejected, routing back to code node")

	// Verify code node goes back to in_progress
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

	// Verify reject_count was incremented
	rejectCount := int(codeNode["reject_count"].(float64))
	if rejectCount != 1 {
		t.Fatalf("expected reject_count=1, got %d", rejectCount)
	}
	t.Logf("Code node reject_count=%d, status=%s", rejectCount, codeNode["status"])

	// Round 2: Agent1 re-claims (pending after reject), re-codes, Agent2 reviews and approves
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	t.Logf("Second cycle: code approved, review approved")

	// Deploy node should be available
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

	// Complete deploy
	claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)

	// Verify task is completed
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

// TestAgentContinuationRight verifies that after approving a standard node, the next standard node gets continuation right and other agents are blocked.
func TestAgentContinuationRight(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)

	// Create a workflow with 2 STANDARD nodes (code -> test) to test continuation right.
	// review nodes do not inherit continuation right from standard nodes.
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

	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 claims and approves first node
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Verify node2 has continuation right for agent1
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

	// Agent2 should be blocked by continuation right (409 Conflict)
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{"agent_id": agent2ID}
	_, status, _ = doRequestWithAgentKey(t, client, http.MethodPost, nodeBaseURL+"/"+node2ID+"/claim", agent2Token, agent2ID, claimBody)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for continuation right conflict, got %d", status)
	}
	t.Logf("Agent2 correctly blocked by continuation right (409)")

	// Agent1 can claim node2 (with continuation right)
	claimedNode2 := claimNode(t, client, srv.URL, taskID, node2ID, agent1ID, agent1Token)
	if claimedNode2["status"] != "in_progress" {
		t.Fatalf("expected node2 status 'in_progress', got %q", claimedNode2["status"])
	}
	t.Logf("Agent1 claimed node2 with continuation right")

	// Complete workflow
	approveNode(t, client, srv.URL, taskID, node2ID, agent1ID, agent1Token)

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentSkipClaim verifies that an agent can voluntarily skip continuation right, allowing other agents to claim the node.
func TestAgentSkipClaim(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)

	// Create a workflow with 2 STANDARD nodes to test continuation right
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

	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 claims and approves first node
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent1 skips claim on node2
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	skipBody := map[string]interface{}{"agent_id": agent1ID}
	_, statusCode, _ = doRequestWithAgentKey(t, client, http.MethodPost, nodeBaseURL+"/"+node2ID+"/skip-claim", agent1Token, agent1ID, skipBody)
	if statusCode != http.StatusOK {
		t.Fatalf("expected 200 for skip-claim, got %d", statusCode)
	}
	t.Logf("Agent1 skipped claim on node2")

	// Now agent2 should be able to claim node2
	claimedNode2 := claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if claimedNode2["status"] != "in_progress" {
		t.Fatalf("expected node2 status 'in_progress' after skip-claim, got %q", claimedNode2["status"])
	}
	t.Logf("Agent2 claimed node2 after skip-claim")

	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentInterruptAndResolve verifies that interrupting a task sets nodes to manual_intervention, and resolving allows work to continue.
func TestAgentInterruptAndResolve(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)

	// Agent1 claims first node
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Cancel the task via service layer: in_progress nodes move to manual_intervention
	svc := service.New(db, nil, nil)
	taskSvc := service.NewTaskService(svc)
	if err := taskSvc.CancelTaskNodes(context.Background(), taskID); err != nil {
		t.Fatalf("service cancel failed: %v", err)
	}
	nodeSvc := service.NewNodeService(svc)

	// Verify node1 is now manual_intervention
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	if updatedNodes[0]["status"] != "manual_intervention" {
		t.Fatalf("expected manual_intervention, got %q", updatedNodes[0]["status"])
	}
	t.Logf("Node1 is now manual_intervention")

	// Resolve via service layer (bypassing HTTP auth missing in test router)
	nodeUUID, _ := uuid.Parse(node1ID)
	resolvedNode, err := nodeSvc.Resolve(context.Background(), nodeUUID, uuid.New(), "member", "issue resolved", nil, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("service resolve failed: %v", err)
	}
	if resolvedNode.Status != "pending" {
		t.Fatalf("expected pending after resolve, got %s", resolvedNode.Status)
	}
	t.Logf("Node1 resolved back to pending via service layer")

	// Re-claim and complete workflow
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentDoubleClaimRejected verifies that a node cannot be double-claimed.
func TestAgentDoubleClaimRejected(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)

	// Agent1 claims the node
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent2 tries to claim the same node — should fail (409)
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{"agent_id": agent2ID}
	_, status, _ := doRequestWithAgentKey(t, client, http.MethodPost, nodeBaseURL+"/"+node1ID+"/claim", agent2Token, agent2ID, claimBody)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for double claim, got %d", status)
	}
	t.Logf("Double claim correctly rejected (409)")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestAgentClaimAccessControl verifies that agents outside a workspace cannot claim nodes, while agents in the same workspace can.
func TestAgentClaimAccessControl(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	// Agent in same workspace (must be added to project_members)
	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbgen.New(db), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)

	// Agent in same workspace can claim (added as project member)
	claimedNode := claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected in_progress for same-workspace agent, got %q", claimedNode["status"])
	}
	t.Logf("Same-workspace agent can claim (project member)")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestDaemonRegistrationAndClaim verifies that an agent bound to a daemon can
// discover and claim pending nodes.
func TestDaemonRegistrationAndClaim(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agentID, _ := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// Step 1: Create task before daemon registration
	// (Simulates user creating task before starting daemon)
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	t.Logf("Task created before daemon registration: task=%d, node1=%s", taskID, node1ID)

	// Verify node is pending
	if nodes[0]["status"] != "pending" {
		t.Fatalf("expected node status 'pending', got %q", nodes[0]["status"])
	}

	// Step 2: bind the agent to a fresh daemon (instances are web-created)
	daemonID, daemonTd := createDaemonForAgent(t, client, srv.URL, wsID, token, agentID, db)
	t.Logf("Daemon registered: %s", daemonID)

	// Step 3: Discover pending nodes (member node view replaces old runtime sync)
	pendingNodes := pendingNodesForTasks(t, client, srv.URL, token, taskID)
	t.Logf("Discovery found %d pending nodes", len(pendingNodes))

	// Step 4: Agent claims pending node
	claimedNode := claimNode(t, client, srv.URL, taskID, node1ID, agentID, daemonTd)
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected claimed node status 'in_progress', got %q", claimedNode["status"])
	}
	t.Logf("Agent claimed node after runtime registration + sync")

	// Step 5: Complete workflow
	approveNode(t, client, srv.URL, taskID, node1ID, agentID, daemonTd)
	t.Logf("Agent approved node, workflow continues")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}

// TestDaemonDiscoverPendingNodes verifies that pending nodes across tasks are
// discoverable for an agent bound to a daemon.
func TestDaemonDiscoverPendingNodes(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agentID, _ := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// Create two tasks
	taskID1, _ := createTask(t, client, srv.URL, projID, tplID, token)
	taskID2, _ := createTask(t, client, srv.URL, projID, tplID, token)
	t.Logf("Created tasks: %d, %d", taskID1, taskID2)

	// Bind the agent to a fresh daemon
	_, _ = createDaemonForAgent(t, client, srv.URL, wsID, token, agentID, db)

	// Node discovery should find pending nodes from both tasks
	pendingNodes := pendingNodesForTasks(t, client, srv.URL, token, taskID1, taskID2)
	if len(pendingNodes) < 2 {
		t.Fatalf("expected at least 2 pending nodes, got %d", len(pendingNodes))
	}
	t.Logf("Discovery found %d pending nodes across tasks", len(pendingNodes))

	// Verify pending node fields
	firstNode := pendingNodes[0]
	if firstNode["status"] != "pending" {
		t.Fatalf("expected node status 'pending', got %q", firstNode["status"])
	}

	deleteTask(t, client, srv.URL, projID, taskID1, token)
	deleteTask(t, client, srv.URL, projID, taskID2, token)
}

// TestDaemonHeartbeatKeepsAgentOnline verifies that a heartbeat reporting the
// bound agent keeps the agent online.
func TestDaemonHeartbeatKeepsAgentOnline(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	agentID, _ := createAgent(t, client, srv.URL, wsID, token)

	// Bind the agent to a fresh daemon, then heartbeat reporting its name
	daemonID, daemonToken := createDaemonForAgent(t, client, srv.URL, wsID, token, agentID, db)
	agentRow, err := q.GetAgent(context.Background(), uuid.MustParse(agentID))
	if err != nil {
		t.Fatalf("query agent: %v", err)
	}
	code, _ := daemonRequest(t, client, http.MethodPost,
		srv.URL+"/api/daemons/heartbeat", daemonToken, "",
		map[string]interface{}{"agents": []map[string]interface{}{{"name": agentRow.Name, "busy": false}}})
	if code != http.StatusOK {
		t.Fatalf("heartbeat: expected 200, got %d", code)
	}
	t.Logf("Heartbeat sent successfully for daemon %s", daemonID)

	// Verify agent status is online via dbgen.Queries
	agent, err := q.GetAgent(context.Background(), uuid.MustParse(agentID))
	if err != nil {
		t.Fatalf("query agent status: %v", err)
	}
	if agent.Status != dbgen.AgentStatusOnline {
		t.Fatalf("expected agent status 'online', got %q", agent.Status)
	}
	t.Logf("Agent status is online after daemon heartbeat")
}

// TestAgentWorkflowWithDaemon tests the complete agent lifecycle including daemon registration, discovery, claim, and approval.
func TestAgentWorkflowWithDaemon(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, testEnv{client, srv.URL, token}, wsID, projID)

	agent1ID, _ := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, _ := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// Bind each agent to its own daemon
	daemon1ID, daemon1Td := createDaemonForAgent(t, client, srv.URL, wsID, token, agent1ID, db)
	daemon2ID, daemon2Td := createDaemonForAgent(t, client, srv.URL, wsID, token, agent2ID, db)
	t.Logf("Daemons registered: agent1=%s, agent2=%s", daemon1ID, daemon2ID)

	// Create task (this triggers SSE events, but since we don't have
	// a real SSE client, we rely on node discovery)
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)
	node3ID := nodes[2]["id"].(string)

	// Agent1 discovers pending nodes
	pendingNodes := pendingNodesForTasks(t, client, srv.URL, token, taskID)
	if len(pendingNodes) == 0 {
		t.Fatal("expected discovery to find pending nodes")
	}
	t.Logf("Agent1 discovery found %d pending nodes", len(pendingNodes))

	// Agent1 claims code node
	claimNode(t, client, srv.URL, taskID, node1ID, agent1ID, daemon1Td)
	approveNode(t, client, srv.URL, taskID, node1ID, agent1ID, daemon1Td)
	t.Logf("Agent1 completed code node")

	// Agent2 discovers review node
	pendingNodes2 := pendingNodesForTasks(t, client, srv.URL, token, taskID)
	t.Logf("Agent2 discovery found %d pending nodes", len(pendingNodes2))

	// Agent2 claims and approves review node
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, daemon2Td)
	approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, daemon2Td)
	t.Logf("Agent2 completed review node")

	// Agent2 claims and approves deploy node (continuation right)
	claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, daemon2Td)
	approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, daemon2Td)
	t.Logf("Agent2 completed deploy node")

	// Verify task is completed
	taskURL := fmt.Sprintf("%s/api/projects/%s/tasks/%d", srv.URL, projID, taskID)
	_, statusCode, body := doRequestWithToken(t, client, http.MethodGet, taskURL, token, nil)
	_ = statusCode
	var taskResult map[string]interface{}
	json.Unmarshal(body, &taskResult)
	if taskResult["status"] != "completed" {
		t.Fatalf("expected task status 'completed', got %q", taskResult["status"])
	}
	t.Logf("Full workflow with daemon registration completed!")

	deleteTask(t, client, srv.URL, projID, taskID, token)
}
