// node_claim_permission_test.go verifies that revoking an agent's task:claim
// permission is enforced at claim time with 403 (not a 500), and that
// re-granting restores claiming.
package handler_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestClaimAfterPermissionRevocation(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	projID := createProject(t, client, ts.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, ts.URL, wsID, token)
	agentID, agentToken := createAgent(t, client, ts.URL, wsID, token)

	// the claiming agent must be a project member
	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/projects/%s/members", ts.URL, wsID, projID), token, map[string]interface{}{
			"member_type": "agent",
			"agent_id":    agentID,
		})
	if status != http.StatusCreated {
		t.Fatalf("add agent to project: expected 201, got %d, body: %s", status, body)
	}

	taskID, nodes := createTask(t, client, ts.URL, projID, tplID, token)

	// revoke task:claim, then the claim must be rejected with 403
	revokeURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/permissions/task:claim", ts.URL, wsID, agentID)
	_, status, body = doRequestWithToken(t, client, http.MethodDelete, revokeURL, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("revoke task:claim: expected 204, got %d, body: %s", status, body)
	}

	claimURL := fmt.Sprintf("%s/api/tasks/%d/nodes/%s/claim", ts.URL, taskID, nodes[0]["id"].(string))
	_, status, body = doRequestWithAgentKey(t, client, http.MethodPost, claimURL, agentToken, agentID, map[string]interface{}{})
	if status != http.StatusForbidden {
		t.Fatalf("claim after revoke: expected 403, got %d, body: %s", status, body)
	}

	// re-grant restores claiming
	grantAgentPermission(t, client, ts.URL, wsID, agentID, "task:claim", token)
	_, status, body = doRequestWithAgentKey(t, client, http.MethodPost, claimURL, agentToken, agentID, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("claim after re-grant: expected 200, got %d, body: %s", status, body)
	}
}
