// task_test.go 覆盖任务数据访问的测试。
package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestCreateTask_GeneratesNodes 测试创建任务时是否自动生成工作流节点。
// 验证任务 ID 非零、序号与 ID 一致、生成的节点数量正确，
// 且第一个节点（any_agent）初始状态为待处理（pending）。
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

	// 验证节点状态
	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes from ListTaskNodes, got %d", len(nodes))
	}

	// 第一个节点（any_agent）应为待处理状态
	if nodes[0].Status != "pending" {
		t.Fatalf("node 0: expected pending, got %s", nodes[0].Status)
	}
}

// TestCreateTask_CopiesDirectoryPermissions 验证模板节点的目录权限字段
// （readonly_dirs / full_control_dirs）在任务实例化时被复制到任务节点。
func TestCreateTask_CopiesDirectoryPermissions(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	// 为模板节点设置目录权限
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
	// jsonb 列由 PostgreSQL 规范化格式（如加空格），用 JSON 数组语义比较而非字符串比较
	assertDirsEqual(t, createdNodes[0].ReadonlyDirs, `["/docs","/README.md"]`)
	assertDirsEqual(t, createdNodes[0].FullControlDirs, `["/src"]`)
	assertDirsEqual(t, createdNodes[1].ReadonlyDirs, `[]`)
	if len(createdNodes[1].FullControlDirs) > 0 {
		t.Errorf("node 1 FullControlDirs = %s, want empty (not configured)", createdNodes[1].FullControlDirs)
	}

	// 从数据库重新读取验证持久化
	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes: %v", err)
	}
	assertDirsEqual(t, nodes[0].ReadonlyDirs, `["/docs","/README.md"]`)
	assertDirsEqual(t, nodes[0].FullControlDirs, `["/src"]`)
}

// assertDirsEqual 断言 json.RawMessage 解析后与期望 JSON 数组的元素一致（忽略格式/顺序差异）。
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

// TestCreateTask_AutoStartSpecificAgent 测试当工作流节点指定了特定代理人（SpecificAgent）时，
// 创建任务后该节点是否自动进入进行中（in_progress）状态。
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

// TestDeleteTask_SoftDelete 测试删除任务时的软删除行为。
// 验证删除后任务状态变为已取消（cancelled），而非从数据库中物理删除。
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

	// 验证任务状态已取消
	updated, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updated.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s", updated.Status)
	}
}

// TestCancelTaskNodes 测试取消任务节点功能。
// 先手动将第二个节点标记为进行中（in_progress），
// 然后调用 CancelTaskNodes，验证所有节点恢复为待处理（pending）状态。
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

	// 手动将第二个节点标记为进行中
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

	// 验证进行中的节点已重置为待处理状态
	nodes, _ = s.ListTaskNodes(ctx, task.ID)
	for _, n := range nodes {
		if n.Status == "in_progress" {
			t.Fatalf("node %s still in_progress after cancel", n.ID)
		}
	}
}
