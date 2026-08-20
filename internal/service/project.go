// project.go 实现项目管理的业务逻辑，包括项目 CRUD、成员管理、
// 审查者管理，以及代理和成员的项目访问权限检查。
// 项目支持多级权限模型：工作区 owner/admin 自动拥有所有项目权限，
// 其他成员需要显式的项目成员关系。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ProjectService 提供项目管理相关的业务逻辑。
type ProjectService struct {
	svc *Service
}

// NewProjectService 创建一个新的 ProjectService 实例。
func NewProjectService(svc *Service) *ProjectService {
	return &ProjectService{svc: svc}
}

// Create 创建一个新的项目。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建项目的参数，包含工作区 ID、名称、描述、Git 仓库地址等
//
// 返回：
//   - types.Project: 创建的项目信息
//   - error: 可能的错误（数据库写入失败）
func (s *ProjectService) Create(ctx context.Context, params types.CreateProjectParams) (types.Project, error) {
	return s.svc.Store.CreateProject(ctx, params)
}

// Get 根据 ID 获取项目信息。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 项目 ID
//
// 返回：
//   - types.Project: 项目信息
//   - error: 可能的错误（项目不存在）
func (s *ProjectService) Get(ctx context.Context, id uuid.UUID) (types.Project, error) {
	return s.svc.Store.GetProject(ctx, id)
}

// List 列出指定工作区的所有项目。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.Project: 项目列表
//   - error: 可能的错误（数据库查询失败）
func (s *ProjectService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Project, error) {
	return s.svc.Store.ListProjects(ctx, workspaceID)
}

// ListByAgentMembership 仅返回指定代理作为成员的项目。
// 用于 Agentd 守护进程获取可操作的项目列表。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - agentID: 代理 ID
//
// 返回：
//   - []types.Project: 代理作为成员的项目列表
//   - error: 可能的错误（数据库查询失败）
func (s *ProjectService) ListByAgentMembership(ctx context.Context, workspaceID, agentID uuid.UUID) ([]types.Project, error) {
	return s.svc.Store.ListProjectsByAgentMembership(ctx, types.ListProjectsByAgentMembershipParams{
		WorkspaceID: workspaceID.String(),
		AgentID:     strPtr(agentID.String()),
	})
}

// strPtr 返回字符串指针的辅助函数。
func strPtr(s string) *string {
	return &s
}

// Update 更新项目信息。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新项目的参数，包含 ID 和要更新的字段
//
// 返回：
//   - types.Project: 更新后的项目信息
//   - error: 可能的错误（项目不存在、数据库更新失败）
func (s *ProjectService) Update(ctx context.Context, params types.UpdateProjectParams) (types.Project, error) {
	return s.svc.Store.UpdateProject(ctx, params)
}

// Delete 删除一个项目。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 项目 ID
//
// 返回：
//   - error: 可能的错误（项目不存在、数据库删除失败）
func (s *ProjectService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteProject(ctx, id)
}

// GetProjectMember 根据 ID 获取项目成员记录。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 项目成员记录 ID
//
// 返回：
//   - types.ProjectMember: 项目成员记录
//   - error: 可能的错误（记录不存在）
func (s *ProjectService) GetProjectMember(ctx context.Context, memberID uuid.UUID) (types.ProjectMember, error) {
	return s.svc.Store.GetProjectMember(ctx, memberID)
}

// AddMember 将代理添加为项目成员。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 添加项目成员的参数，包含项目 ID、代理 ID 和项目角色
//
// 返回：
//   - types.ProjectMember: 创建的项目成员记录
//   - error: 可能的错误（数据库写入失败）
func (s *ProjectService) AddMember(ctx context.Context, params types.CreateProjectMemberParams) (types.ProjectMember, error) {
	return s.svc.Store.CreateProjectMember(ctx, params)
}

// ListMembers 列出项目的所有成员（代理）。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []types.ProjectMember: 项目成员列表
//   - error: 可能的错误（数据库查询失败）
func (s *ProjectService) ListMembers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectMember, error) {
	return s.svc.Store.ListProjectMembers(ctx, projectID)
}

// RemoveMember 从项目中移除一个成员。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 项目成员记录 ID
//
// 返回：
//   - error: 可能的错误（记录不存在、数据库删除失败）
func (s *ProjectService) RemoveMember(ctx context.Context, memberID uuid.UUID) error {
	return s.svc.Store.DeleteProjectMember(ctx, memberID)
}

// AddReviewer 为项目添加审查者。审查者可以审查项目中的 review 类型节点。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 添加审查者的参数，包含项目 ID 和代理 ID
//
// 返回：
//   - types.ProjectReviewer: 创建的项目审查者记录
//   - error: 可能的错误（数据库写入失败）
func (s *ProjectService) AddReviewer(ctx context.Context, params types.CreateProjectReviewerParams) (types.ProjectReviewer, error) {
	return s.svc.Store.CreateProjectReviewer(ctx, params)
}

// ListReviewers 列出项目的所有审查者。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []types.ProjectReviewer: 项目审查者列表
//   - error: 可能的错误（数据库查询失败）
func (s *ProjectService) ListReviewers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectReviewer, error) {
	return s.svc.Store.ListProjectReviewers(ctx, projectID)
}

// RemoveReviewer 从项目中移除一个审查者。
//
// 参数：
//   - ctx: 请求上下文
//   - reviewerID: 项目审查者记录 ID
//
// 返回：
//   - error: 可能的错误（记录不存在、数据库删除失败）
func (s *ProjectService) RemoveReviewer(ctx context.Context, reviewerID uuid.UUID) error {
	return s.svc.Store.DeleteProjectReviewer(ctx, reviewerID)
}

// IsAgentMember 检查代理是否是项目的成员。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 检查参数，包含项目 ID 和代理 ID
//
// 返回：
//   - bool: 代理是否为项目成员
//   - error: 可能的错误（数据库查询失败）
func (s *ProjectService) IsAgentMember(ctx context.Context, params types.IsAgentProjectMemberParams) (bool, error) {
	return s.svc.Store.IsAgentProjectMember(ctx, params)
}

// CheckAgentProjectAccess 检查代理是否有权访问指定项目。
// 如果代理不是项目成员，返回错误。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - projectID: 项目 ID
//
// 返回：
//   - error: 代理无权访问时返回错误
func (s *ProjectService) CheckAgentProjectAccess(ctx context.Context, agentID, projectID uuid.UUID) error {
	isMember, err := s.svc.Store.IsAgentProjectMember(ctx, types.IsAgentProjectMemberParams{
		ProjectID: projectID.String(),
		AgentID:   agentID.String(),
	})
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("agent is not a member of this project")
	}
	return nil
}

// GetProjectReviewerByReviewerID 根据审查者记录 ID 查询项目审查者信息。
// 用于验证审查者记录是否存在以及是否属于指定项目。
//
// 参数：
//   - ctx: 请求上下文
//   - reviewerID: 审查者记录 ID
//
// 返回：
//   - types.ProjectReviewer: 审查者记录（包含 project_id）
//   - error: 可能的错误（记录不存在）
func (s *ProjectService) GetProjectReviewerByReviewerID(ctx context.Context, reviewerID uuid.UUID) (types.ProjectReviewer, error) {
	reviewer, err := s.svc.Store.GetProjectReviewerByID(ctx, reviewerID)
	if err != nil {
		return types.ProjectReviewer{}, fmt.Errorf("get project reviewer: %w", err)
	}
	return reviewer, nil
}

// CheckMemberProjectAccess 检查成员是否有权访问指定项目。
// 继承规则：
//  1. 工作区 owner/admin 自动拥有其工作区内所有项目的完全访问权限
//  2. 工作区 member 需要显式的项目成员关系
//  3. 工作区 viewer 即使被添加为项目成员也只能查看
//
// 如果指定了 requiredRole，成员的项目角色必须达到或超过该级别（lead > developer > reviewer）。
// 工作区 owner/admin 会跳过角色级别检查。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员 ID
//   - projectID: 项目 ID
//   - workspaceRole: 成员在工作区中的角色
//   - requiredRole: 可选，要求的最低项目角色级别
//
// 返回：
//   - error: 成员无权访问时返回错误
func (s *ProjectService) CheckMemberProjectAccess(ctx context.Context, memberID, projectID uuid.UUID, workspaceRole string, requiredRole ...string) error {
	project, err := s.svc.Store.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("project not found")
	}

	if workspaceRole == "owner" || workspaceRole == "admin" {
		_ = project
		return nil
	}

	isMember, err := s.svc.Store.IsMemberProjectMember(ctx, projectID, memberID)
	if err != nil {
		return fmt.Errorf("check project membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("member does not have access to this project")
	}

	if len(requiredRole) > 0 && requiredRole[0] != "" {
		role, err := s.svc.Store.GetProjectMemberRole(ctx, projectID, memberID)
		if err != nil {
			return fmt.Errorf("check project role: %w", err)
		}
		if types.ProjectRoleLevel(role) < types.ProjectRoleLevel(requiredRole[0]) {
			return fmt.Errorf("insufficient project role: requires %s, has %s", requiredRole[0], role)
		}
	}

	return nil
}
