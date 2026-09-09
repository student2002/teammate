// stats_test.go covers statistics data access tests.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestGetProjectStats tests retrieving project statistics with 3 active tasks
func TestGetProjectStats(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// Create tasks
	for i := 0; i < 3; i++ {
		_, _, err := s.CreateTask(ctx, types.CreateTaskParams{
			ProjectID:  proj.ID,
			Title:      "Task",
			Type:       "task",
			Priority:   "medium",
			Status:     "active",
			AuthorType: "member",
			AuthorID:   uuid.New().String(),
		}, tplNodes)
		if err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}

	stats, err := s.GetProjectStats(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetProjectStats: %v", err)
	}

	if stats.TaskCounts.Active != 3 {
		t.Fatalf("expected 3 active tasks, got %d", stats.TaskCounts.Active)
	}
	if stats.TaskCounts.Completed != 0 {
		t.Fatalf("expected 0 completed tasks, got %d", stats.TaskCounts.Completed)
	}
}

// TestGetProjectStats_EmptyProject tests statistics for an empty project
func TestGetProjectStats_EmptyProject(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)

	stats, err := s.GetProjectStats(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetProjectStats: %v", err)
	}

	if stats.TaskCounts.Active != 0 {
		t.Fatalf("expected 0 active tasks, got %d", stats.TaskCounts.Active)
	}
	if stats.NodeCompletionRate != 0 {
		t.Fatalf("expected 0 node completion rate, got %f", stats.NodeCompletionRate)
	}
	if stats.AvgTimeToComplete != nil {
		t.Fatal("expected nil avg time for empty project")
	}
}

// TestGetAgentStats tests retrieving agent statistics
func TestGetAgentStats(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	agent, _ := createTestAgent(t, s, ws.ID)

	stats, err := s.GetAgentStats(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("GetAgentStats: %v", err)
	}

	if stats.TotalCompletedTasks != 0 {
		t.Fatalf("expected 0 completed tasks, got %d", stats.TotalCompletedTasks)
	}
	if stats.TotalTokens != 0 {
		t.Fatalf("expected 0 tokens, got %d", stats.TotalTokens)
	}
}
