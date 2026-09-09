// task_test.go covers task data access tests.
package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestCreateTask_GeneratesNodes tests whether workflow nodes are automatically generated when creating a task.
// It verifies that the task ID is non-zero, the sequence matches the ID, the generated node count is correct,
// and the first node (any_agent) has an initial status of pending.
func TestCreateTask_GeneratesNodes(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 3)

	desc := "test description"
	task, createdNodes, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    proj.ID,
		Title:        "Test task",
		Description:  &desc,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     uuid.New().String(),
		WorkflowName: "test-flow",
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.ID == 0 {
		t.Fatal("expected non-zero task ID")
	}
	if int32(task.Sequence) != task.ID {
		t.Fatalf("expected sequence == task ID, got %d != %d", task.Sequence, task.ID)
	}
	if len(createdNodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(createdNodes))
	}

	// Verify node status
	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes from ListTaskNodes, got %d", len(nodes))
	}

	// First node (any_agent) should be in pending status
	if nodes[0].Status != "pending" {
		t.Fatalf("node 0: expected pending, got %s", nodes[0].Status)
	}
}

// TestCreateTask_CopiesDirectoryPermissions verifies that template node directory permission fields
// (readonly_dirs / full_control_dirs) are copied to task nodes when a task is instantiated.
func TestCreateTask_CopiesDirectoryPermissions(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// Set directory permissions for template nodes
	tplNodes[0].ReadonlyDirs = json.RawMessage(`["/docs","/README.md"]`)
	tplNodes[0].FullControlDirs = json.RawMessage(`["/src"]`)
	tplNodes[1].ReadonlyDirs = json.RawMessage(`[]`)

	desc := "test description"
	task, createdNodes, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    proj.ID,
		Title:        "Test task with dirs",
		Description:  &desc,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     uuid.New().String(),
		WorkflowName: "test-flow",
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if len(createdNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(createdNodes))
	}
	// jsonb columns are normalized by PostgreSQL (e.g., adding spaces), so compare JSON array semantics instead of string comparison
	assertDirsEqual(t, createdNodes[0].ReadonlyDirs, `["/docs","/README.md"]`)
	assertDirsEqual(t, createdNodes[0].FullControlDirs, `["/src"]`)
	assertDirsEqual(t, createdNodes[1].ReadonlyDirs, `[]`)
	if len(createdNodes[1].FullControlDirs) > 0 {
		t.Errorf("node 1 FullControlDirs = %s, want empty (not configured)", createdNodes[1].FullControlDirs)
	}

	// Re-read from database to verify persistence
	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes: %v", err)
	}
	assertDirsEqual(t, nodes[0].ReadonlyDirs, `["/docs","/README.md"]`)
	assertDirsEqual(t, nodes[0].FullControlDirs, `["/src"]`)
}

// assertDirsEqual asserts that the elements of a parsed json.RawMessage match the expected JSON array (ignoring format/order differences).
func assertDirsEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotDirs, wantDirs []string
	if len(got) > 0 {
		if err := json.Unmarshal(got, &gotDirs); err != nil {
			t.Fatalf("parse got dirs %q: %v", got, err)
		}
	}
	if err := json.Unmarshal([]byte(want), &wantDirs); err != nil {
		t.Fatalf("parse want dirs %q: %v", want, err)
	}
	if len(gotDirs) != len(wantDirs) {
		t.Fatalf("dirs = %v, want %v", gotDirs, wantDirs)
	}
	for i := range wantDirs {
		if gotDirs[i] != wantDirs[i] {
			t.Fatalf("dirs = %v, want %v", gotDirs, wantDirs)
		}
	}
}

// TestCreateTask_AutoStartSpecificAgent tests whether a workflow node with a specific agent (SpecificAgent)
// automatically enters in_progress status after task creation.
func TestCreateTask_AutoStartSpecificAgent(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)

	nodes := []types.WorkflowTemplateNode{
		{
			Name:            "auto-node",
			SortOrder:       1,
			NodeType:        "standard",
			AssigneeType:    "specific_agent",
			AssigneeID:      &agent.ID,
			TimeoutMinutes:  60,
			MaxRejectCycles: 3,
		},
	}

	_, createdNodes, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Auto-start task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent.ID,
	}, nodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if createdNodes[0].Status != "in_progress" {
		t.Fatalf("auto-start node: expected in_progress, got %s", createdNodes[0].Status)
	}
}

// TestDeleteTask_SoftDelete tests the soft delete behavior when deleting a task.
// It verifies that the task status changes to cancelled after deletion, rather than being physically removed from the database.
func TestDeleteTask_SoftDelete(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Delete me",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = s.DeleteTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	// Verify task status is cancelled
	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s", updated.Status)
	}
}

// TestCancelTaskNodes tests the cancel task nodes functionality.
// It first manually marks the second node as in_progress,
// then calls CancelTaskNodes and verifies all nodes are reset to pending status.
func TestCancelTaskNodes(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Cancel nodes",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Manually mark the second node as in_progress
	nodes, _ := s.ListTaskNodes(ctx, task.ID)
	_, err = s.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
		ID:          nodes[1].ID,
		Status:      "in_progress",
		AssigneeType: nodes[1].AssigneeType,
		AssigneeID:  nodes[1].AssigneeID,
		ReservedForAgentID: nodes[1].ReservedForAgentID,
		RejectCount: int32(nodes[1].RejectCount),
		Version:     int32(nodes[1].Version),
		ExpectedCurrentStatus: nodes[1].Status,
	})
	if err != nil {
		t.Fatalf("UpdateTaskNodeStatus: %v", err)
	}

	err = s.CancelTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("CancelTaskNodes: %v", err)
	}

	// Verify in_progress nodes have been reset to pending status
	nodes, _ = s.ListTaskNodes(ctx, task.ID)
	for _, n := range nodes {
		if n.Status == "in_progress" {
			t.Fatalf("node %s still in_progress after cancel", n.ID)
		}
	}
}
