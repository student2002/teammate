// memory_search_test.go 覆盖记忆语义搜索的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestSearchMemories_ILIKE 验证通过 ILIKE 搜索记忆标题和内容。
func TestSearchMemories_ILIKE(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// 创建记忆
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

	// 按标题搜索
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

	// 按内容搜索
	results, err = s.SearchMemories(ctx, "PostgreSQL", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// TestSearchMemories_SpecialChars 验证搜索含特殊字符（% 和 _）的记忆。
func TestSearchMemories_SpecialChars(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// 创建包含特殊字符的记忆
	_, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: ws.ID,
		Type:        "convention",
		Title:       "Pattern: 100% complete",
		Content:     "Use underscore_like_this for variables",
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	// 使用特殊字符搜索——应转义 % 和 _
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

// TestMarkMemoriesStaleByTask 验证按任务标记记忆为过时。
func TestMarkMemoriesStaleByTask(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// 创建任务
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

	// 创建与任务关联的记忆
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

	// 将记忆标记为过期
	err = s.MarkMemoriesStaleByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("MarkMemoriesStaleByTask: %v", err)
	}

	// 通过搜索验证记忆现在已过期（过期记忆不应出现）
	results, err := s.SearchMemories(ctx, "Task insight", uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 non-stale results, got %d", len(results))
	}
}
