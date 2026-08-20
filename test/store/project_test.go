// project_test.go 覆盖项目数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestProjectCRUD 测试项目的增删改查（CRUD）操作：
// 1. 创建项目后通过 GetProject 查询，验证名称一致
// 2. 通过 ListProjects 列出工作区下的所有项目
// 3. 通过 UpdateProject 更新项目名称并验证
func TestProjectCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)

	fetched, err := s.GetProject(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if fetched.Name != proj.Name {
		t.Fatalf("expected name %s, got %s", proj.Name, fetched.Name)
	}

	projs, err := s.ListProjects(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projs) == 0 {
		t.Fatal("expected at least 1 project")
	}

	updated, err := s.UpdateProject(ctx, types.UpdateProjectParams{
		ID:          proj.ID,
		Name:        "updated-proj",
		Description: &proj.Description,
		Status:      proj.Status,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != "updated-proj" {
		t.Fatalf("expected updated name, got %s", updated.Name)
	}
}

// TestProjectMemberCRUD 测试项目成员的增删改查（CRUD）操作：
// 1. 创建项目成员（agent 类型），验证成员关联到正确的项目
// 2. 通过 ListProjectMembers 列出项目成员，验证数量
// 3. 通过 IsAgentProjectMember 验证 agent 是否为项目成员
// 4. 通过 DeleteProjectMember 删除成员并验证
func TestProjectMemberCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)

	pm, err := s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    &agent.ID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("CreateProjectMember: %v", err)
	}
	if pm.ProjectID != proj.ID {
		t.Fatalf("expected project ID match")
	}

	members, err := s.ListProjectMembers(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}

	isMember, err := s.IsAgentProjectMember(ctx, types.IsAgentProjectMemberParams{ProjectID: proj.ID, AgentID: agent.ID})
	if err != nil {
		t.Fatalf("IsAgentProjectMember: %v", err)
	}
	if !isMember {
		t.Fatal("expected agent to be project member")
	}

	err = s.DeleteProjectMember(ctx, uuid.MustParse(pm.ID))
	if err != nil {
		t.Fatalf("DeleteProjectMember: %v", err)
	}
}

// TestProjectReviewerCRUD 测试项目审核者的增删改查（CRUD）操作：
// 1. 创建项目审核者（member 类型），验证创建成功
// 2. 通过 ListProjectReviewers 列出项目审核者，验证数量
// 3. 通过 DeleteProjectReviewer 删除审核者并验证
func TestProjectReviewerCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	member := createTestMember(t, s, ws.ID)

	_, err := s.CreateProjectReviewer(ctx, types.CreateProjectReviewerParams{
		ProjectID:  proj.ID,
		MemberType: "member",
		MemberID:   &member.ID,
	})
	if err != nil {
		t.Fatalf("CreateProjectReviewer: %v", err)
	}

	reviewers, err := s.ListProjectReviewers(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("ListProjectReviewers: %v", err)
	}
	if len(reviewers) != 1 {
		t.Fatalf("expected 1 reviewer, got %d", len(reviewers))
	}

	err = s.DeleteProjectReviewer(ctx, uuid.MustParse(reviewers[0].ID))
	if err != nil {
		t.Fatalf("DeleteProjectReviewer: %v", err)
	}
}
