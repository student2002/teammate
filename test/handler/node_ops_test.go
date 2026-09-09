// node_ops_test.go tests the node operation APIs.
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

// TestContinuationRightConflict verifies that the self-review prevention mechanism skips continuation right in code→review flow.
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

	// Agent1 claims and approves node1
	claimNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)

	// After approval, node2's reserved_for_agent_id should be agent1
	// (continuation right, unless a review node follows a standard node)
	// In our 3-node flow: code(standard) -> review(review) -> deploy(standard)
	// code->review: self-review prevention, so reserved_for_agent_id should NOT be set
	// We verify by having agent2 try to claim node2 — should succeed (no reservation)

	_, status, _ := doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, node2ID),
		agent2Token, map[string]interface{}{"agent_id": agent2ID})

	// Since code->review skips continuation right, agent2 should be able to claim
	if status != http.StatusOK {
		t.Fatalf("agent2 should be able to claim review node (no continuation right for code->review), got %d", status)
	}
}

// TestContinuationRightHoldsForDeploy verifies continuation right holds in standard→standard flow, preventing other agents from claiming.
func TestContinuationRightHoldsForDeploy(t *testing.T) {
	ts, _, q := setupNodeOpsTestRouter(t)
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)
	agent1ID, agent1Token := createAgent(t, client, ts.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, ts.URL, wsID, token)

	// Create workflow: code(standard) -> test(standard) -> deploy(standard)
	// So code->test has continuation right, and test->deploy also has continuation right
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

	// Agent1 claims and approves node1 (code)
	claimNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)

	// Now node2 (test) should have reserved_for_agent_id = agent1 (continuation right)
	// Agent2 tries to claim — should return 409
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, node2ID),
		agent2Token, map[string]interface{}{"agent_id": agent2ID})

	if status != http.StatusConflict {
		t.Fatalf("agent2 should get 409 (continuation right), got %d", status)
	}
}

// TestSkipClaim verifies that an agent can skip continuation right, allowing other agents to claim the node.
func TestSkipClaim(t *testing.T) {
	ts, _, q := setupNodeOpsTestRouter(t)
	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)
	agent1ID, agent1Token := createAgent(t, client, ts.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, ts.URL, wsID, token)

	// Same 3 standard node workflow
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

	// Agent1 claims and approves node1
	claimNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)
	approveNode(t, client, ts.URL, taskID, node1ID, agent1ID, agent1Token)

	// Agent1 skips claim on node2
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/skip-claim", ts.URL, taskID, node2ID),
		agent1Token, map[string]interface{}{"agent_id": agent1ID})
	if status != http.StatusOK {
		t.Fatalf("skip-claim: expected 200, got %d", status)
	}

	// Now agent2 should be able to claim node2
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, node2ID),
		agent2Token, map[string]interface{}{"agent_id": agent2ID})
	if status != http.StatusOK {
		t.Fatalf("agent2 should claim after skip-claim, got %d", status)
	}
}
