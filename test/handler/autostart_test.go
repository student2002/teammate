// autostart_test.go covers tests for auto-start endpoints.
package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// TestAutoStartFirstNode verifies that CreateTask auto-starts the first node when assignee_type is 'auto', and keeps it 'pending' when it's 'any_agent'.
func TestAutoStartFirstNode(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	// Test 1: first node with assignee_type='auto' should start as 'in_progress'
	projID := createProject(t, client, srv.URL, wsID, token)

	// Create a workflow template with first node assignee_type='auto'
	// We need to create it via database since the API doesn't easily support 'auto' type
	q := dbQueries(t, db)
	wsUUID := parseUUID(t, wsID)

	autoTpl, err := q.CreateWorkflowTemplate(t.Context(), dbgen.CreateWorkflowTemplateParams{
		WorkspaceID:    wsUUID,
		Name:           "auto-start-flow",
		Description:    sql.NullString{String: "auto start first node", Valid: true},
		TriggerType:    dbgen.WorkflowTriggerTypeManual,
		TriggerConfig:  json.RawMessage(`{}`),
		TriggerEnabled: true,
	})
	if err != nil {
		t.Fatalf("create auto workflow template: %v", err)
	}

	// Create the auto-type first node
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      autoTpl.ID,
		Name:            "auto-code",
		Description:     sql.NullString{String: "auto code node", Valid: true},
		SortOrder:       1,
		NodeType:        dbgen.NodeTypeStandard,
		AssigneeType:    dbgen.AssigneeTypeAuto,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  60,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create auto template node: %v", err)
	}

	// Create the standard-type second node
	_, err = q.CreateTemplateNode(t.Context(), dbgen.CreateTemplateNodeParams{
		TemplateID:      autoTpl.ID,
		Name:            "review",
		Description:     sql.NullString{String: "review node", Valid: true},
		SortOrder:       2,
		NodeType:        dbgen.NodeTypeReview,
		AssigneeType:    dbgen.AssigneeTypeAnyAgent,
		AssigneeID:      uuid.NullUUID{},
		TimeoutMinutes:  30,
		ReadonlyDirs:    pqtype.NullRawMessage{},
		FullControlDirs: pqtype.NullRawMessage{},
		Artifact:        pqtype.NullRawMessage{},
	})
	if err != nil {
		t.Fatalf("create review template node: %v", err)
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
		DefaultWorkflowID: uuid.NullUUID{UUID: autoTpl.ID, Valid: true},
		MaxReviewCycles:   sql.NullInt32{Int32: 3, Valid: true},
	})

	// Create task → verify first node is 'in_progress' not 'pending'
	taskID, nodes := createTask(t, client, srv.URL, projID, autoTpl.ID.String(), token)
	defer deleteTask(t, client, srv.URL, projID, taskID, token)

	if len(nodes) < 1 {
		t.Fatal("expected at least 1 node")
	}

	firstNode := nodes[0]
	if firstNode["status"] != "in_progress" {
		t.Fatalf("auto first node: expected status 'in_progress', got %v", firstNode["status"])
	}
	t.Logf("Auto first node status: %v (correctly auto-started)", firstNode["status"])

	// Test 2: first node with assignee_type='any_agent' should be 'pending'
	// Register a second user to get an independent workspace
	token2, wsID2 := registerTestUser(t, client, srv.URL)

	projID2 := createProject(t, client, srv.URL, wsID2, token2)
	tplID2 := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID2, token2)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID2, projID2, tplID2, token2)

	// Create task → verify first node is 'pending'
	taskID2, nodes2 := createTask(t, client, srv.URL, projID2, tplID2, token2)
	defer deleteTask(t, client, srv.URL, projID2, taskID2, token2)

	if len(nodes2) < 1 {
		t.Fatal("expected at least 1 node")
	}

	firstNode2 := nodes2[0]
	if firstNode2["status"] != "pending" {
		t.Fatalf("any_agent first node: expected status 'pending', got %v", firstNode2["status"])
	}
	t.Logf("Any_agent first node status: %v (correctly stays pending)", firstNode2["status"])
}
