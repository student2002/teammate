// workspace_test.go 覆盖工作区数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestSeedBuiltinTemplates 验证创建的工作区自动包含 5 个内置模板，且每个模板都有节点。
func TestSeedBuiltinTemplates(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// 验证已创建 5 个内置模板
	templates, err := s.ListWorkflowTemplates(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListWorkflowTemplates: %v", err)
	}
	if len(templates) != 5 {
		t.Fatalf("expected 5 built-in templates, got %d", len(templates))
	}

	// 验证每个模板都有节点
	for _, tpl := range templates {
		nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
		if err != nil {
			t.Fatalf("ListTemplateNodes for %s: %v", tpl.Name, err)
		}
		if len(nodes) == 0 {
			t.Fatalf("template %s has no nodes", tpl.Name)
		}
	}
}

// TestGetWorkspaceOwner 验证获取工作区所有者的功能。
func TestGetWorkspaceOwner(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	// 将该成员设置为所有者
	_, err := s.UpdateMemberRole(ctx, types.UpdateMemberRoleParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "owner",
	})
	if err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}

	owner, err := s.GetWorkspaceOwner(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("GetWorkspaceOwner: %v", err)
	}
	if owner.WorkspaceRole != "owner" {
		t.Fatalf("expected role 'owner', got %s", owner.WorkspaceRole)
	}
}

// TestGetWorkspaceOwner_NoOwner 验证工作区没有所有者时返回错误。
func TestGetWorkspaceOwner_NoOwner(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "no-owner-ws-" + uuid.New().String()[:8],
		IssuePrefix: "NOW",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// 创建一个非所有者的成员
	nonOwner, err := s.CreateMember(ctx, types.CreateMemberParams{
		Name:  "member",
		Email: "member-" + uuid.New().String()[:8] + "@test.com",
	})
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	// 将成员添加到工作区，角色为 member（非 owner）
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    nonOwner.ID,
		Role:        "member",
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceMember: %v", err)
	}

	_, err = s.GetWorkspaceOwner(ctx, uuid.MustParse(ws.ID))
	if err == nil {
		t.Fatal("expected error when no owner exists")
	}
}
