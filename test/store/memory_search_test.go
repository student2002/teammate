// memory_search_test.go covers memory semantic search tests.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestSearchMemories_ILIKE verifies searching memory titles and content via ILIKE.
func TestSearchMemories_ILIKE(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// Create memories
	_, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: ws.ID,
		Type:        "insight",
		Title:       "API Design Pattern",
		Content:     "REST API design follows specific patterns",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	_, err = s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: ws.ID,
		Type:        "architecture",
		Title:       "Database Schema",
		Content:     "PostgreSQL schema design",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	// Search by title
	results, err := s.SearchMemories(ctx, "API", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "API Design Pattern" {
		t.Fatalf("expected 'API Design Pattern', got %s", results[0].Title)
	}

	// Search by content
	results, err = s.SearchMemories(ctx, "PostgreSQL", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestSearchMemories_SpecialChars verifies searching memories containing special characters (% and _).
func TestSearchMemories_SpecialChars(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// Create a memory with special characters
	_, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: ws.ID,
		Type:        "convention",
		Title:       "Pattern: 100% complete",
		Content:     "Use underscore_like_this for variables",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	// Search with special characters — should escape % and _
	results, err := s.SearchMemories(ctx, "100%", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for escaped search, got %d", len(results))
	}

	results, err = s.SearchMemories(ctx, "underscore_like", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for underscore escape, got %d", len(results))
	}
}

// TestMarkMemoriesStaleByTask verifies marking memories as stale by task.
func TestMarkMemoriesStaleByTask(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// Create a task
	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Task with memory",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent.ID,
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Create a memory associated with the task
	_, err = s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID:  ws.ID,
		SourceTaskID: &task.ID,
		Type:         "decision",
		Title:        "Task insight",
		Content:      "Important learning from this task",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	// Mark the memory as stale
	err = s.MarkMemoriesStaleByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("MarkMemoriesStaleByTask: %v", err)
	}

	// Verify the memory is now stale via search (stale memories should not appear)
	results, err := s.SearchMemories(ctx, "Task insight", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 non-stale results, got %d", len(results))
	}
}
