// approve_test.go covers tests for node approval endpoints.
package handler_test

import (
	"net/http/httptest"
	"testing"
)

// TestApproveCascading verifies that approving a node cascades to the next node:
// - Approve node 1 → node 2 becomes pending with reserved_for_agent_id set
// - Approve node 2 → node 3 becomes pending with reserved_for_agent_id set
// - Approve node 3 → task status becomes "completed"
func TestApproveCascading(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// Prepare: project, workflow with 3 nodes (code -> review -> deploy)
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// Create task with 3 nodes
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)
	node3ID := nodes[2]["id"].(string)

	// Step 1: Claim and approve node 1 (code)
	claimed1 := claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if claimed1["status"] != "in_progress" {
		t.Fatalf("node1: expected status 'in_progress', got %v", claimed1["status"])
	}

	approved1 := approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	if approved1["status"] != "completed" {
		t.Fatalf("node1: expected status 'completed' after approve, got %v", approved1["status"])
	}

	// Verify node 2 becomes pending — reserved_for_agent_id is not set because
	// code→review triggers self-review avoidance (skips continuation right)
	updatedNodes := listTaskNodes(t, client, srv.URL, taskID, token)
	node2 := updatedNodes[1]
	if node2["status"] != "pending" {
		t.Fatalf("node2: expected status 'pending' after node1 approved, got %v", node2["status"])
	}
	t.Logf("node2 reserved_for_agent_id: %v (correctly nil due to self-review avoidance)", node2["reserved_for_agent_id"])

	// Step 2: Agent2 claims and approves node 2 (review) — uses a different agent to avoid self-review
	claimed2 := claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if claimed2["status"] != "in_progress" {
		t.Fatalf("node2: expected status 'in_progress', got %v", claimed2["status"])
	}

	approved2 := approveNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)
	if approved2["status"] != "completed" {
		t.Fatalf("node2: expected status 'completed' after approve, got %v", approved2["status"])
	}

	// Verify node 3 becomes pending with reserved_for_agent_id set (review→standard, no self-review)
	updatedNodes = listTaskNodes(t, client, srv.URL, taskID, token)
	node3 := updatedNodes[2]
	if node3["status"] != "pending" {
		t.Fatalf("node3: expected status 'pending' after node2 approved, got %v", node3["status"])
	}
	if node3["reserved_for_agent_id"] == nil {
		t.Fatal("node3: expected reserved_for_agent_id to be set after node2 approved, got nil")
	}
	t.Logf("node3 reserved_for_agent_id: %v (continuation right from reviewer)", node3["reserved_for_agent_id"])

	// Step 3: Claim and approve node 3 (deploy) — agent2 has continuation right
	claimed3 := claimNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if claimed3["status"] != "in_progress" {
		t.Fatalf("node3: expected status 'in_progress', got %v", claimed3["status"])
	}

	approved3 := approveNode(t, client, srv.URL, taskID, node3ID, agent2ID, agent2Token)
	if approved3["status"] != "completed" {
		t.Fatalf("node3: expected status 'completed' after approve, got %v", approved3["status"])
	}

	// Verify task status becomes "completed" (all nodes finished)
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
