// workspace.go 实现工作区管理的业务逻辑，包括工作区 CRUD、成员管理、
// 内置模板初始化，以及 OAuth 登录时的自动工作区创建。
// 工作区是系统的顶层组织单元，包含项目、成员、代理、技能等资源。
//
// 本文件包含：
//   - WorkspaceService 结构体：提供工作区管理相关的业务逻辑封装
//   - Create / Get / List / Update / Delete：工作区基本 CRUD 操作
//   - CreateMember / ListMembers / GetMember / UpdateMemberRole / DeleteMember：成员管理
//   - SeedBuiltinTemplates：为工作区初始化内置工作流模板
//   - FindOrCreateForOAuth：OAuth 登录时自动创建工作区和成员
//
// 核心流程：
//  1. 工作区创建时自动初始化内置工作流模板（如标准的实现→自测→审查→部署）
//  2. OAuth 首次登录时自动创建个人工作区、成员记录并分配 owner 角色
//  3. 成员角色采用层级模型：owner > admin > member > viewer
package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// WorkspaceService 提供工作区管理相关的业务逻辑。
type WorkspaceService struct {
	svc *Service
}

// NewWorkspaceService 创建一个新的 WorkspaceService 实例。
func NewWorkspaceService(svc *Service) *WorkspaceService {
	return &WorkspaceService{svc: svc}
}

// Create 创建一个新的工作区并初始化内置模板。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建工作区的参数，包含名称、描述、Issue 前缀等
//
// 返回：
//   - types.Workspace: 创建的工作区信息
//   - error: 可能的错误（数据库写入失败）
func (s *WorkspaceService) Create(ctx context.Context, params types.CreateWorkspaceParams) (types.Workspace, error) {
	ws, err := s.svc.Store.CreateWorkspace(ctx, params)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// CreateForMember 创建工作区并把创建者设为 owner。
func (s *WorkspaceService) CreateForMember(ctx context.Context, memberID uuid.UUID, params types.CreateWorkspaceParams) (types.Workspace, error) {
	ws, err := s.svc.Store.CreateWorkspaceForMember(ctx, memberID, params)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// Get 根据 ID 获取工作区信息。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 工作区 ID
//
// 返回：
//   - types.Workspace: 工作区信息
//   - error: 可能的错误（工作区不存在）
func (s *WorkspaceService) Get(ctx context.Context, id uuid.UUID) (types.Workspace, error) {
	ws, err := s.svc.Store.GetWorkspace(ctx, id)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// List 列出所有工作区。
//
// 参数：
//   - ctx: 请求上下文
//
// 返回：
//   - []types.Workspace: 工作区列表
//   - error: 可能的错误（数据库查询失败）
func (s *WorkspaceService) List(ctx context.Context) ([]types.Workspace, error) {
	wss, err := s.svc.Store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	return wss, nil
}

// ListForMember 列出成员所属的工作区。
func (s *WorkspaceService) ListForMember(ctx context.Context, memberID uuid.UUID) ([]types.Workspace, error) {
	wss, err := s.svc.Store.ListWorkspacesByMemberID(ctx, memberID)
	if err != nil {
		return nil, err
	}
	return wss, nil
}

// Update 更新工作区信息。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新工作区的参数，包含 ID 和要更新的字段
//
// 返回：
//   - types.Workspace: 更新后的工作区信息
//   - error: 可能的错误（工作区不存在、数据库更新失败）
func (s *WorkspaceService) Update(ctx context.Context, params types.UpdateWorkspaceParams) (types.Workspace, error) {
	ws, err := s.svc.Store.UpdateWorkspace(ctx, params)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// Delete 删除一个工作区。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 工作区 ID
//
// 返回：
//   - error: 可能的错误（工作区不存在、数据库删除失败）
func (s *WorkspaceService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteWorkspace(ctx, id)
}

// CreateMember 在工作区中创建一个成员。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建成员的参数，包含名称和邮箱
//
// 返回：
//   - types.Member: 创建的成员信息
//   - error: 可能的错误（邮箱已存在、数据库写入失败）
func (s *WorkspaceService) CreateMember(ctx context.Context, params types.CreateMemberParams) (types.Member, error) {
	return s.svc.Store.CreateMember(ctx, params)
}

// ListMembers 列出工作区中的所有成员，包含角色信息。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.ListMembersByWorkspaceRow: 成员列表（包含角色）
//   - error: 可能的错误（数据库查询失败）
func (s *WorkspaceService) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]types.ListMembersByWorkspaceRow, error) {
	members, err := s.svc.Store.ListMembersByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return members, nil
}

// GetMember 根据 ID 获取成员信息。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 成员 ID
//
// 返回：
//   - types.Member: 成员信息
//   - error: 可能的错误（成员不存在）
func (s *WorkspaceService) GetMember(ctx context.Context, id uuid.UUID) (types.Member, error) {
	return s.svc.Store.GetMember(ctx, id)
}

// UpdateMemberRole 更新成员在工作区中的角色。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新角色的参数，包含工作区 ID、成员 ID 和新角色
//
// 返回：
//   - types.WorkspaceMember: 更新后的成员-工作区关联记录
//   - error: 可能的错误（数据库更新失败）
func (s *WorkspaceService) UpdateMemberRole(ctx context.Context, params types.UpdateMemberRoleParams) (types.WorkspaceMember, error) {
	return s.svc.Store.UpdateMemberRole(ctx, params)
}

// DeleteMember 根据 ID 删除一个成员。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 成员 ID
//
// 返回：
//   - error: 可能的错误（成员不存在、数据库删除失败）
func (s *WorkspaceService) DeleteMember(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteMember(ctx, id)
}

// GetMembership 根据工作区 ID 和成员 ID 获取工作区成员关系。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - memberID: 成员 ID
//
// 返回：
//   - types.WorkspaceMember: 工作区成员关系记录
//   - error: 可能的错误（记录不存在）
func (s *WorkspaceService) GetMembership(ctx context.Context, workspaceID, memberID uuid.UUID) (types.WorkspaceMember, error) {
	return s.svc.Store.GetWorkspaceMember(ctx, types.GetWorkspaceMemberParams{
		WorkspaceID: workspaceID.String(),
		MemberID:    memberID.String(),
	})
}

// GetWorkspaceMemberRole 获取成员在指定工作区中的角色。
//
// 参数：
//   - ctx: 请求上下文
//   - userID: 成员 ID
//   - workspaceID: 工作区 ID
//
// 返回：
//   - string: 成员角色（owner/admin/member/viewer）
//   - error: 可能的错误（成员不属于该工作区）
func (s *WorkspaceService) GetWorkspaceMemberRole(ctx context.Context, userID uuid.UUID, workspaceID uuid.UUID) (string, error) {
	role, err := s.svc.Store.GetWorkspaceMemberRole(ctx, types.GetWorkspaceMemberRoleParams{
		WorkspaceID: workspaceID.String(),
		MemberID:    userID.String(),
	})
	if err != nil {
		return "", fmt.Errorf("get workspace member role: %w", err)
	}
	return role, nil
}

// GetMemberByEmail 根据邮箱获取成员信息。
//
// 参数：
//   - ctx: 请求上下文
//   - email: 成员邮箱
//
// 返回：
//   - types.Member: 成员信息
//   - error: 可能的错误（成员不存在）
func (s *WorkspaceService) GetMemberByEmail(ctx context.Context, email string) (types.Member, error) {
	return s.svc.Store.GetMemberByEmail(ctx, email)
}

// GetFirstWorkspaceForMember 获取成员所属的第一个工作区（用于 OAuth 登录时查找已有工作区）。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员 ID
//
// 返回：
//   - types.WorkspaceMember: 工作区成员关系记录
//   - error: 可能的错误（成员无工作区）
func (s *WorkspaceService) GetFirstWorkspaceForMember(ctx context.Context, memberID uuid.UUID) (types.WorkspaceMember, error) {
	return s.svc.Store.GetFirstWorkspaceForMember(ctx, memberID)
}

// SeedBuiltinTemplates 为工作区初始化内置工作流模板。
// 内置模板包括标准的实现→自测→审查→部署等工作流。
//
// 参数：
//   - ctx: 请求上下文
//   - svc: Service 实例
//   - workspaceID: 工作区 ID
//
// 返回：
//   - error: 可能的错误（数据库操作失败）
func SeedBuiltinTemplates(ctx context.Context, svc *Service, workspaceID uuid.UUID) error {
	return svc.Store.SeedBuiltinTemplates(ctx, workspaceID)
}

// FindOrCreateForOAuth 为 OAuth 登录查找或创建工作区和成员。
// 如果成员已存在则直接返回，否则创建新工作区、成员并分配 owner 角色。
//
// 步骤：
//  1. 根据邮箱查找已有成员
//  2. 成员已存在：直接返回
//  3. 成员不存在：
//     a. 创建新工作区（以用户名称命名）
//     b. 初始化内置工作流模板
//     c. 创建新成员
//     d. 将成员添加到工作区并分配 owner 角色
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱
//   - name: 用户名称
//
// 返回：
//   - types.Member: 成员信息
//   - error: 可能的错误（数据库操作失败）
func (s *WorkspaceService) FindOrCreateForOAuth(ctx context.Context, email, name string) (types.Member, error) {
	member, err := s.svc.Store.GetMemberByEmail(ctx, email)
	if err == nil {
		return member, nil
	}

	member, err = s.svc.Store.CreateMember(ctx, types.CreateMemberParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return types.Member{}, fmt.Errorf("create member for oauth: %w", err)
	}

	workspaceName := name
	if workspaceName == "" {
		workspaceName = email
	}
	memberUUID, err := uuid.Parse(member.ID)
	if err != nil {
		return types.Member{}, fmt.Errorf("parse member id: %w", err)
	}
	descStr := "Personal workspace"
	ws, err := s.svc.Store.CreateWorkspaceWithOwnerInTx(ctx, memberUUID, types.CreateWorkspaceParams{
		Name:        workspaceName + "'s Workspace",
		Description: &descStr,
		IssuePrefix: "MUL",
		IsDefault:   true,
	})
	if err != nil {
		return types.Member{}, fmt.Errorf("create workspace for oauth: %w", err)
	}
	wsUUID, err := uuid.Parse(ws.ID)
	if err != nil {
		return types.Member{}, fmt.Errorf("parse workspace id: %w", err)
	}
	if err := s.svc.Store.SeedBuiltinTemplates(ctx, wsUUID); err != nil {
		slog.Warn("workspace created but builtin templates failed", "workspace_id", ws.ID, "err", err)
	}

	return member, nil
}
