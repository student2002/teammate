// reject_test.go tests the node rejection API.
package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// TestRejectCascading verifies that rejecting a node cascades back:
// - Rejecting node 2 targeting node 1 → node 1 returns to pending, node 2 marked as rejected
// - Reject count increments
func TestRejectCascading(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// Setup: project, workflow with 3 nodes (code -> review -> deploy)

	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	agent2ID, agent2Token := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	addAgentToProject(t, q, projID, agent2ID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agent2ID, token)

	// Create a task with 3 nodes
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	// Note: skip deleteTask cleanup since rejection may prevent deletion

	node1ID := nodes[0]["id"].(string)
	node2ID := nodes[1]["id"].(string)

	// Agent1 claims and approves node 1 (code)
	claimNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)
	approveNode(t, client, srv.URL, taskID, node1ID, agentID, agentToken)

	// Agent2 claims node 2 (review) — using a different agent to avoid self-review
	claimNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token)

	// Reject node 2 targeting node 1
	status, rejectedNode := rejectNode(t, client, srv.URL, taskID, node2ID, agent2ID, agent2Token, &node1ID)
	if status != 200 {
		t.Fatalf("rejectNode: expected 200, got %d", status)
	}
	if rejectedNode["status"] != "rejected" {
		t.Fatalf("node2: expected status 'rejected' after reject, got %v", rejectedNode["status"])
	}

	// Verify reject count has incremented
	rejectCount, ok := rejectedNode["reject_count"].(float64)
	if !ok {
		t.Fatalf("node2: reject_count is not a number, got %v", rejectedNode["reject_count"])
	}
	if rejectCount != 1 {
		t.Fatalf("node2: expected reject_count 1, got %v", rejectCount)
	}
	t.Logf("node2 reject_count: %v", rejectCount)

	// Verify node 1 returns to in_progress via database query
	q = dbQueries(t, db)
	node1UUID := parseUUID(t, node1ID)
	node1, err := q.GetTaskNode(t.Context(), node1UUID)
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if node1.Status != "pending" {
		t.Fatalf("node1: expected status 'pending' after reject targeting it, got %v", node1.Status)
	}
	t.Log("node1 is back to pending after reject targeting it (needs re-claim)")
}

// TestRejectToManualNode verifies that rejecting to a manual/human node returns an error
func TestRejectToManualNode(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	projID := createProject(t, client, srv.URL, wsID, token)

	// Create a workflow with first node as manual, second node as standard
	q = dbQueries(t, db)

	wsUUID := parseUUID(t, wsID)
	tpl, err := q.CreateWorkflowTemplate(t.Context(), dbgen.CreateWorkflowTemplateParams{
		WorkspaceID:    wsUUID,
		Name:           "manual-first-flow",
		Description:    sql.NullString{String: "manual first node flow", Valid: true},
		TriggerType:    dbgen.WorkflowTriggerTypeManual,
		TriggerConfig:  json.RawMessage(`{}`),
		TriggerEnabled: true,
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	// Create the first node of manual type
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      tpl.ID,
		Name:            "manual-review",
		Description:     sql.NullString{String: "manual review gate", Valid: true},
		SortOrder:       1,
		NodeType:        dbgen.NodeTypeManual,
		AssigneeType:    dbgen.AssigneeTypeHuman,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  60,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create manual template node: %v", err)
	}

	// Create the second node of standard type
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      tpl.ID,
		Name:            "code",
		Description:     sql.NullString{String: "write code", Valid: true},
		SortOrder:       2,
		NodeType:        dbgen.NodeTypeStandard,
		AssigneeType:    dbgen.AssigneeTypeAnyAgent,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  60,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create standard template node: %v", err)
	}

	// Setup
	projUUID := parseUUID(t, projID)
	_, _ = q.UpdateProject(t.Context(), dbgen.UpdateProjectParams{
		ID:                projUUID,
		Name:              "test-project",
		Description:       sql.NullString{String: "test", Valid: true},
		Status:            dbgen.ProjectStatusActive,
		RepoUrl:           sql.NullString{},
		Context:           sql.NullString{},
		DefaultWorkflowID: uuid.NullUUID{UUID: tpl.ID, Valid: true},
		MaxReviewCycles:   sql.NullInt32{Int32: 3, Valid: true},
	})

	agentID, agentToken := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// Create task
	taskID, nodes := createTask(t, client, srv.URL, projID, tpl.ID.String(), token)
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	if len(nodes) < 2 {
		t.Fatalf("expected at least 2 nodes, got %d", len(nodes))
	}

	manualNodeID := nodes[0]["id"].(string)
	codeNodeID := nodes[1]["id"].(string)

	// Claim code node
	claimNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken)

	// Reject targeting manual node — should be rejected by design
	// (cannot reject to a manual or human-assigned node)
	status, _ := rejectNode(t, client, srv.URL, taskID, codeNodeID, agentID, agentToken, &manualNodeID)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request when rejecting to manual node, got %d", status)
	}
	t.Logf("reject to manual node correctly rejected with status: %d", status)
}

func parseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse UUID %q: %v", s, err)
	}
	return id
}
