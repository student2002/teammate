// claim_test.go tests covering the node claim API.
package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClaimSelfReview verifies that an agent cannot claim a review node for a previous standard node it authored (self-review prevention).
func TestClaimSelfReview(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// Setup: project, workflow with code + review nodes
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agent1ID, agent1Token := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agent1ID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent1ID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// Create task
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	codeNodeID := nodes[0]["id"].(string)
	reviewNodeID := nodes[1]["id"].(string)

	// Agent 1 claims and completes the code node
	claimedCode := claimNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)
	if claimedCode["status"] != "in_progress" {
		t.Fatalf("code node: expected status 'in_progress', got %v", claimedCode["status"])
	}

	approvedCode := approveNode(t, client, srv.URL, taskID, codeNodeID, agent1ID, agent1Token)
	if approvedCode["status"] != "completed" {
		t.Fatalf("code node: expected status 'completed' after approve, got %v", approvedCode["status"])
	}

	// Same agent attempts to claim review node → should return 403 (self-review)
	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)
	claimBody := map[string]interface{}{
		"agent_id": agent1ID,
	}
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+reviewNodeID+"/claim", agent1Token, claimBody)
	if status != http.StatusForbidden {
		t.Fatalf("self-review claim: expected 403 Forbidden, got %d, body: %s", status, respBody)
	}
	t.Logf("Self-review correctly blocked with 403, body: %s", respBody)

	// Different agent attempts to claim review node → should succeed
	claimBody2 := map[string]interface{}{
		"agent_id": agent2ID,
	}
	_, status, respBody = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+reviewNodeID+"/claim", agent2Token, claimBody2)
	if status != http.StatusOK {
		t.Fatalf("different agent claim: expected 200, got %d, body: %s", status, respBody)
	}
	t.Log("Different agent successfully claimed review node")
}
