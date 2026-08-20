// board_runtime_test.go 覆盖看板与运行时数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

func TestGetBoardData_ColumnMapping(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// 创建任务
	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Board task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	columns, err := s.GetBoardData(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetBoardData: %v", err)
	}

	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}

	// 查找 pending 列
	var pendingCol store.BoardColumn
	for _, col := range columns {
		if col.Key == "pending" {
			pendingCol = col
			break
		}
	}

	// 任务应处于 pending 列
	found := false
	for _, taskItem := range pendingCol.Tasks {
		if taskItem.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("task not found in pending column")
	}
}

func TestGetBoardData_EmptyProject(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)

	columns, err := s.GetBoardData(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetBoardData: %v", err)
	}

	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}

	for _, col := range columns {
		if len(col.Tasks) != 0 {
			t.Fatalf("column %s should be empty, got %d tasks", col.Key, len(col.Tasks))
		}
	}
}

func TestMapNodeStatusToColumn(t *testing.T) {
	tests := []struct {
		name   string
		status db.TaskNodeStatus
		column string
	}{
		{"pending", db.TaskNodeStatusPending, "pending"},
		{"in_progress", db.TaskNodeStatusInProgress, "in_progress"},
		{"completed", db.TaskNodeStatusCompleted, "completed"},
		{"rejected", db.TaskNodeStatusRejected, "manual_intervention"},
		{"manual_intervention", db.TaskNodeStatusManualIntervention, "manual_intervention"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := store.MapNodeStatusToColumn(tt.status)
			if got != tt.column {
				t.Errorf("MapNodeStatusToColumn(%s) = %s, want %s", tt.status, got, tt.column)
			}
		})
	}
}

func TestSyncRuntime_PendingNodes(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// 创建包含待处理节点的任务
	_, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Pending task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// 为 Agent 创建 runtime
	ver := "1.0.0"
	runtime, err := s.CreateRuntime(ctx, types.CreateRuntimeParams{
		AgentID:  agent.ID,
		Provider: "claude",
		Version:  &ver,
		Status:   "online",
	})
	if err != nil {
		t.Fatalf("CreateRuntime: %v", err)
	}

	result, err := s.SyncRuntime(ctx, uuid.MustParse(runtime.ID))
	if err != nil {
		t.Fatalf("SyncRuntime: %v", err)
	}

	if len(result.PendingNodes) == 0 {
		t.Fatal("expected at least 1 pending node")
	}
}

func TestSyncRuntime_MentionComments(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// 创建任务
	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Comment task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// 创建一条提及该 Agent 的评论
	_, err = s.CreateComment(ctx, types.CreateCommentParams{
		TaskID:      task.ID,
		AuthorType:  "member",
		AuthorID:    uuid.New().String(),
		Content:     "Hey @" + agent.ID + " check this",
		CommentType: "text",
		Mentions:    []string{agent.ID},
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	// 创建 runtime
	ver := "1.0.0"
	runtime, err := s.CreateRuntime(ctx, types.CreateRuntimeParams{
		AgentID:  agent.ID,
		Provider: "claude",
		Version:  &ver,
		Status:   "online",
	})
	if err != nil {
		t.Fatalf("CreateRuntime: %v", err)
	}

	result, err := s.SyncRuntime(ctx, uuid.MustParse(runtime.ID))
	if err != nil {
		t.Fatalf("SyncRuntime: %v", err)
	}

	if len(result.MentionComments) == 0 {
		t.Fatal("expected at least 1 mention comment")
	}
}
