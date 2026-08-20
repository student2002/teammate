// workflow_test.go 覆盖工作流模板数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestCreateWorkflowTemplate_WithNodes 测试创建工作流模板时同时创建节点的功能。
func TestCreateWorkflowTemplate_WithNodes(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, createdNodes := createTestWorkflowTemplate(t, s, ws.ID, 3)

	if tpl.ID == "" {
		t.Fatal("期望获取非空的模板 ID")
	}
	if len(createdNodes) != 3 {
		t.Fatalf("期望 3 个节点，实际得到 %d 个", len(createdNodes))
	}

	// 验证节点可被查询
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("期望 ListTemplateNodes 返回 3 个节点，实际得到 %d 个", len(nodes))
	}
}

// TestUpdateWorkflowTemplateWithNodes_ReplaceAll 测试更新工作流模板时替换全部节点的功能。
func TestUpdateWorkflowTemplateWithNodes_ReplaceAll(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 3)

	// 使用 2 个新节点进行更新
	newNodes := []types.CreateTemplateNodeParams{
		{
			Name:            "new-node-1",
			SortOrder:       1,
			NodeType:        "standard",
			AssigneeType:    "any_agent",
			TimeoutMinutes:  30,
			MaxRejectCycles: 2,
		},
		{
			Name:            "new-node-2",
			SortOrder:       2,
			NodeType:        "review",
			AssigneeType:    "any_agent",
			TimeoutMinutes:  20,
			MaxRejectCycles: 2,
		},
	}

	_, updatedNodes, err := s.UpdateWorkflowTemplateWithNodes(ctx, types.UpdateWorkflowTemplateParams{
		ID:          tpl.ID,
		Name:        tpl.Name,
		Description: &tpl.Description,
	}, newNodes)
	if err != nil {
		t.Fatalf("UpdateWorkflowTemplateWithNodes: %v", err)
	}

	if len(updatedNodes) != 2 {
		t.Fatalf("期望更新后得到 2 个节点，实际得到 %d 个", len(updatedNodes))
	}

	// 验证旧节点已被移除
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("期望更新后得到 2 个节点，实际得到 %d 个", len(nodes))
	}

	// 验证新节点名称
	if nodes[0].Name != "new-node-1" || nodes[1].Name != "new-node-2" {
		t.Fatalf("意外的节点名称: %s, %s", nodes[0].Name, nodes[1].Name)
	}
}

// TestDeleteWorkflowTemplate_Cascade 测试删除工作流模板时级联删除关联节点的功能。
func TestDeleteWorkflowTemplate_Cascade(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 3)

	err := s.DeleteWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("DeleteWorkflowTemplate: %v", err)
	}

	// 验证模板已被删除
	_, err = s.GetWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err == nil {
		t.Fatal("期望获取已删除的模板时返回错误")
	}

	// 验证关联节点已被删除
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("期望删除后返回 0 个节点，实际得到 %d 个", len(nodes))
	}
}
