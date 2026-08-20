// project.go 提供项目管理的数据访问操作。
//
// 包含项目的 CRUD 操作、项目成员管理、项目审查者管理。
// 项目是工作区内的软件项目，关联代码仓库、任务和 Agent。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateProject 创建一条新的项目记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 项目创建参数，包含工作区 ID、名称、描述、仓库 URL 等
//
// 返回：
//   - types.Project: 创建的项目记录
//   - error: 创建失败时返回错误
func (s *Store) CreateProject(ctx context.Context, params types.CreateProjectParams) (types.Project, error) {
	dbParams, err := FromDomainCreateProjectParams(params)
	if err != nil {
		return types.Project{}, fmt.Errorf("convert create project params: %w", err)
	}
	p, err := s.q.CreateProject(ctx, dbParams)
	if err != nil {
		return types.Project{}, fmt.Errorf("create project: %w", err)
	}
	return ToDomainProject(p)
}

// GetProject 根据 ID 查询单个项目记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 项目的 UUID
//
// 返回：
//   - types.Project: 项目记录
//   - error: 查询失败时返回错误
func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (types.Project, error) {
	p, err := s.q.GetProject(ctx, id)
	if err != nil {
		return types.Project{}, fmt.Errorf("get project: %w", err)
	}
	return ToDomainProject(p)
}

// ListProjects 查询指定工作区内的所有项目。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []types.Project: 项目列表
//   - error: 查询失败时返回错误
func (s *Store) ListProjects(ctx context.Context, workspaceID uuid.UUID) ([]types.Project, error) {
	projects, err := s.q.ListProjects(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return ToDomainProjectSlice(projects)
}

// ListProjectsByAgentMembership 查询指定 Agent 作为成员参与的所有项目。
//
// 参数：
//   - ctx: 请求上下文
//   - arg: 查询参数，包含 Agent ID
//
// 返回：
//   - []types.Project: 项目列表
//   - error: 查询失败时返回错误
func (s *Store) ListProjectsByAgentMembership(ctx context.Context, arg types.ListProjectsByAgentMembershipParams) ([]types.Project, error) {
	dbParams, err := FromDomainListProjectsByAgentMembershipParams(arg)
	if err != nil {
		return nil, fmt.Errorf("convert list projects by agent membership params: %w", err)
	}
	projects, err := s.q.ListProjectsByAgentMembership(ctx, dbParams)
	if err != nil {
		return nil, fmt.Errorf("list projects by agent membership: %w", err)
	}
	return ToDomainProjectSlice(projects)
}

// UpdateProject 更新项目的基本信息。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含项目 ID 和要更新的字段
//
// 返回：
//   - types.Project: 更新后的项目记录
//   - error: 更新失败时返回错误
func (s *Store) UpdateProject(ctx context.Context, params types.UpdateProjectParams) (types.Project, error) {
	dbParams, err := FromDomainUpdateProjectParams(params)
	if err != nil {
		return types.Project{}, fmt.Errorf("convert update project params: %w", err)
	}
	p, err := s.q.UpdateProject(ctx, dbParams)
	if err != nil {
		return types.Project{}, fmt.Errorf("update project: %w", err)
	}
	return ToDomainProject(p)
}

// DeleteProject 根据 ID 删除项目记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 项目的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteProject(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 002_remove_fks 后以下列不再有外键，须显式置空（FK 策略：应用层保证完整性）：
	//   memories.source_task_id、workflow_trigger_runs.task_id
	if _, err := tx.ExecContext(ctx,
		`UPDATE memories SET source_task_id = NULL
		 WHERE source_task_id IN (SELECT id FROM tasks WHERE project_id = $1)`, id); err != nil {
		return fmt.Errorf("clear memories.source_task_id: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE workflow_trigger_runs SET task_id = NULL
		 WHERE project_id = $1`, id); err != nil {
		return fmt.Errorf("clear workflow_trigger_runs.task_id: %w", err)
	}

	if err := s.q.WithTx(tx).DeleteProject(ctx, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	return tx.Commit()
}

// CreateProjectMember 将成员添加到项目中（插入 project_members 记录）。
//
// 成员可以是 Agent 或人类用户，通过 member_type 区分。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 成员创建参数，包含项目 ID、成员类型、角色等
//
// 返回：
//   - types.ProjectMember: 创建的成员记录
//   - error: 创建失败时返回错误
func (s *Store) CreateProjectMember(ctx context.Context, params types.CreateProjectMemberParams) (types.ProjectMember, error) {
	dbParams, err := FromDomainCreateProjectMemberParams(params)
	if err != nil {
		return types.ProjectMember{}, fmt.Errorf("convert create project member params: %w", err)
	}
	m, err := s.q.CreateProjectMember(ctx, dbParams)
	if err != nil {
		return types.ProjectMember{}, fmt.Errorf("create project member: %w", err)
	}
	return ToDomainProjectMember(m)
}

// ListProjectMembers 查询指定项目的所有成员列表。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - []types.ProjectMember: 成员列表
//   - error: 查询失败时返回错误
func (s *Store) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectMember, error) {
	members, err := s.q.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}
	return ToDomainProjectMemberSlice(members)
}

// DeleteProjectMember 从项目中移除成员（删除 project_members 记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员记录的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteProjectMember(ctx context.Context, memberID uuid.UUID) error {
	if err := s.q.DeleteProjectMember(ctx, memberID); err != nil {
		return fmt.Errorf("delete project member: %w", err)
	}
	return nil
}

// CreateProjectReviewer 为项目添加审查者（插入 project_reviewers 记录）。
//
// 审查者可以是 Agent 或人类用户，负责代码审查节点。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 审查者创建参数
//
// 返回：
//   - types.ProjectReviewer: 创建的审查者记录
//   - error: 创建失败时返回错误
func (s *Store) CreateProjectReviewer(ctx context.Context, params types.CreateProjectReviewerParams) (types.ProjectReviewer, error) {
	dbParams, err := FromDomainCreateProjectReviewerParams(params)
	if err != nil {
		return types.ProjectReviewer{}, fmt.Errorf("convert create project reviewer params: %w", err)
	}
	r, err := s.q.CreateProjectReviewer(ctx, dbParams)
	if err != nil {
		return types.ProjectReviewer{}, fmt.Errorf("create project reviewer: %w", err)
	}
	return ToDomainProjectReviewer(r)
}

// ListProjectReviewers 查询指定项目的所有审查者列表。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - []types.ProjectReviewer: 审查者列表
//   - error: 查询失败时返回错误
func (s *Store) ListProjectReviewers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectReviewer, error) {
	reviewers, err := s.q.ListProjectReviewers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project reviewers: %w", err)
	}
	return ToDomainProjectReviewerSlice(reviewers)
}

// DeleteProjectReviewer 从项目中移除审查者（删除 project_reviewers 记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - reviewerID: 审查者记录的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteProjectReviewer(ctx context.Context, reviewerID uuid.UUID) error {
	if err := s.q.DeleteProjectReviewer(ctx, reviewerID); err != nil {
		return fmt.Errorf("delete project reviewer: %w", err)
	}
	return nil
}

// IsAgentProjectMember 检查指定 Agent 是否是项目的成员。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 检查参数，包含项目 ID 和 Agent ID
//
// 返回：
//   - bool: 是否是项目成员
//   - error: 查询失败时返回错误
func (s *Store) IsAgentProjectMember(ctx context.Context, params types.IsAgentProjectMemberParams) (bool, error) {
	dbParams, err := FromDomainIsAgentProjectMemberParams(params)
	if err != nil {
		return false, fmt.Errorf("convert is agent project member params: %w", err)
	}
	isMember, err := s.q.IsAgentProjectMember(ctx, dbParams)
	if err != nil {
		return false, fmt.Errorf("check agent project membership: %w", err)
	}
	return isMember, nil
}

// IsMemberProjectMember 检查指定人类成员是否是项目的成员。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//   - memberID: 成员 UUID
//
// 返回：
//   - bool: 是否是项目成员
//   - error: 查询失败时返回错误
func (s *Store) IsMemberProjectMember(ctx context.Context, projectID uuid.UUID, memberID uuid.UUID) (bool, error) {
	isMember, err := s.q.IsMemberProjectMember(ctx, db.IsMemberProjectMemberParams{
		ProjectID: projectID,
		MemberID:  uuid.NullUUID{UUID: memberID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check member project membership: %w", err)
	}
	return isMember, nil
}

// GetProjectReviewerByID 根据审查者记录 ID 查询项目审查者信息。
//
// 参数：
//   - ctx: 请求上下文
//   - reviewerID: 审查者记录的 UUID
//
// 返回：
//   - types.ProjectReviewer: 审查者记录
//   - error: 查询失败时返回错误
func (s *Store) GetProjectReviewerByID(ctx context.Context, reviewerID uuid.UUID) (types.ProjectReviewer, error) {
	r, err := s.q.GetProjectReviewerByID(ctx, reviewerID)
	if err != nil {
		return types.ProjectReviewer{}, fmt.Errorf("get project reviewer by id: %w", err)
	}
	return ToDomainProjectReviewer(r)
}

// GetProjectMemberRole 查询人类成员在项目中的角色。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//   - memberID: 成员 UUID
//
// 返回：
//   - string: 角色名称（如 "lead"、"developer"、"reviewer"）
//   - error: 查询失败时返回错误
func (s *Store) GetProjectMemberRole(ctx context.Context, projectID uuid.UUID, memberID uuid.UUID) (string, error) {
	role, err := s.q.GetProjectMemberRole(ctx, db.GetProjectMemberRoleParams{
		ProjectID: projectID,
		MemberID:  uuid.NullUUID{UUID: memberID, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("get project member role: %w", err)
	}
	return role, nil
}

// GetProjectMember 根据项目成员记录 ID 查询单个项目成员。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 项目成员记录 ID
//
// 返回：
//   - types.ProjectMember: 项目成员记录
//   - error: 查询失败时返回错误
func (s *Store) GetProjectMember(ctx context.Context, memberID uuid.UUID) (types.ProjectMember, error) {
	m, err := s.q.GetProjectMember(ctx, memberID)
	if err != nil {
		return types.ProjectMember{}, fmt.Errorf("get project member: %w", err)
	}
	return ToDomainProjectMember(m)
}
