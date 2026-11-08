// task_test.go covers task data access tests.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// TestCreateTask_MainRecordOnly verifies that task creation only writes the
// main record: sequence defaults to the task ID and no task nodes are
// instantiated (node instantiation is the workflow engine's responsibility).
func TestCreateTask_MainRecordOnly(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, _ = createTestWorkflowTemplate(t, s, ws.ID, 3)

	desc := "test description"
	task, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    proj.ID,
		Title:        "Test task",
		Description:  &desc,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     uuid.New().String(),
		WorkflowName: "test-flow",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.ID == 0 {
		t.Fatal("expected non-zero task ID")
	}
	if int32(task.Sequence) != task.ID {
		t.Fatalf("expected sequence == task ID, got %d != %d", task.Sequence, task.ID)
	}

	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 task nodes after creation, got %d", len(nodes))
	}
}

// TestDeleteTask_SoftDelete tests the soft delete behavior when deleting a task.
// It verifies that the task status changes to cancelled after deletion, rather than being physically removed from the database.
func TestDeleteTask_SoftDelete(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)

	task, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Delete me",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	})
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

// TestCancelTaskNodes verifies that cancelling a task moves its running node
// instances to the cancelled status.
func TestCancelTaskNodes(t *testing.T) {
	s, dbConn := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)

	task, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Cancel nodes",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Insert a running node instance directly (the engine owns instantiation)
	node, err := db.New(dbConn).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:         task.ID,
		TemplateNodeID: uuid.MustParse(uuid.NewString()),
		Name:           "code",
		SortOrder:      1,
		TypeKey:        "agent.task",
		Config:         []byte(`{}`),
		Status:         db.TaskNodeStatusRunning,
		ActivationMode: "all",
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	err = s.CancelTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("CancelTaskNodes: %v", err)
	}

	// Verify the running node has been cancelled
	updated, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("GetTaskNode: %v", err)
	}
	if updated.Status != "cancelled" {
		t.Fatalf("expected node cancelled after CancelTaskNodes, got %s", updated.Status)
	}
}
