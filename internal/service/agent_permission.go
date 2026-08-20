// agent_permission.go 实现代理权限管理的业务逻辑，包括权限的授予、撤销、检查。
//
// 本文件包含：
//   - AgentPermissionService 结构体：权限管理服务，支持资源级权限匹配和缓存
//   - Grant：为代理授予权限，授权后通过 SSE 发送权限变更事件并使缓存失效
//   - Revoke：撤销代理权限，撤销后通过 SSE 发送权限变更事件并使缓存失效
//   - HasPermission：检查代理是否拥有指定权限（任意资源），使用 Redis 缓存加速
//   - HasResourcePermission：检查代理是否拥有指定资源的特定权限
//   - ListPermissions：列出代理的所有权限
//   - GrantDefaultPermissions：为新代理授予默认权限集
//   - GrantRolePermissions：为代理授予预定义角色的所有权限
//
// 权限支持资源级匹配（精确匹配或通配符匹配），通过 Redis 缓存加速判断。
// 权限变更时通过 SSE 控制事件通知代理，确保代理能及时感知权限变化。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

import "github.com/teammate/server/internal/types"

// AgentPermissionService 提供代理权限管理相关的业务逻辑。
// 权限支持资源级匹配（精确匹配或通配符匹配），通过 Redis 缓存加速判断。
type AgentPermissionService struct {
	svc       *Service
	permCache *PermissionCache
}

// NewAgentPermissionService 创建一个新的 AgentPermissionService 实例，初始化权限缓存。
func NewAgentPermissionService(svc *Service) *AgentPermissionService {
	return &AgentPermissionService{
		svc:       svc,
		permCache: NewPermissionCache(svc.Redis),
	}
}

// GetPermission 根据 ID 获取一条权限记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 权限记录 ID
//
// 返回：
//   - db.AgentPermission: 权限记录
//   - error: 可能的错误（记录不存在）
func (s *AgentPermissionService) GetPermission(ctx context.Context, id uuid.UUID) (types.AgentPermission, error) {
	perm, err := s.svc.Store.GetAgentPermission(ctx, id)
	if err != nil {
		return types.AgentPermission{}, fmt.Errorf("get agent permission: %w", err)
	}
	return perm, nil
}

// Grant 为代理授予一个权限，授权后通过 SSE 发送权限变更事件并使缓存失效。
//
// 步骤：
//  1. 调用 Store 将权限记录写入数据库
//  2. 使该代理的权限缓存失效（Redis）
//  3. 通过 SSE 发送 permission:changed 控制事件通知代理
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - permission: 权限标识符（如 task:claim、git:push 等）
//   - resourceType: 资源类型（如 project、workspace，"*" 表示通配符）
//   - resourceID: 资源 ID（可选，与 resourceType 配合实现精确匹配）
//   - grantedBy: 授予权限的操作者 ID
//
// 返回：
//   - db.AgentPermission: 创建的权限记录
//   - error: 可能的错误（数据库写入失败）
func (s *AgentPermissionService) Grant(ctx context.Context, agentID uuid.UUID, permission string, resourceType string, resourceID *uuid.UUID, grantedBy uuid.UUID) (types.AgentPermission, error) {
	result, err := s.svc.Store.GrantAgentPermission(ctx, agentID, permission, resourceType, resourceID, grantedBy)
	if err != nil {
		return result, err
	}
	s.permCache.Invalidate(ctx, agentID)
	s.svc.PublishControlEvent(ctx, agentID, types.EventPermissionChanged, map[string]interface{}{
		"action":     "grant",
		"permission": permission,
	})
	return result, nil
}

// Revoke 撤销代理的一个权限，撤销后通过 SSE 发送权限变更事件并使缓存失效。
// 权限 ID 用于唯一标识一条权限记录。
//
// 步骤：
//  1. 根据权限 ID 查询权限记录，获取代理 ID 和权限名称
//  2. 调用 Store 从数据库删除权限记录
//  3. 使该代理的权限缓存失效（Redis）
//  4. 通过 SSE 发送 permission:changed 控制事件通知代理
//
// 参数：
//   - ctx: 请求上下文
//   - id: 权限记录 ID
//
// 返回：
//   - error: 可能的错误（数据库删除失败）
func (s *AgentPermissionService) Revoke(ctx context.Context, id uuid.UUID) error {
	perm, err := s.svc.Store.GetAgentPermission(ctx, id)
	if err != nil {
		return fmt.Errorf("get agent permission: %w", err)
	}
	if err := s.svc.Store.RevokeAgentPermission(ctx, id); err != nil {
		return fmt.Errorf("revoke agent permission: %w", err)
	}
	agentID, _ := uuid.Parse(perm.AgentID)
	s.permCache.Invalidate(ctx, agentID)
	s.svc.PublishControlEvent(ctx, agentID, types.EventPermissionChanged, map[string]interface{}{
		"action":     "revoke",
		"permission": perm.Permission,
	})
	return nil
}

// HasPermission 检查代理是否拥有指定权限（任意资源），使用 Redis 缓存加速判断。
// 匹配规则：精确匹配（resource_type + resource_id）或通配符匹配（resource_type = '*'）。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - permission: 权限标识符
//
// 返回：
//   - bool: 代理是否拥有该权限
//   - error: 可能的错误（Redis 查询失败、数据库查询失败）
func (s *AgentPermissionService) HasPermission(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
	return s.permCache.HasPermission(ctx, agentID, permission, func() (bool, error) {
		return s.svc.Store.HasAgentPermissionAny(ctx, agentID, permission)
	})
}

// HasResourcePermission 检查代理是否拥有指定资源的特定权限。
// 匹配规则：精确匹配（resource_type + resource_id）或通配符匹配（resource_type = '*'）。
// 使用 Redis 缓存加速判断。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - permission: 权限标识符
//   - resourceType: 资源类型（如 project、workspace）
//   - resourceID: 资源 ID（可选，传入 nil 匹配通配符权限）
//
// 返回：
//   - bool: 代理是否拥有该资源的指定权限
//   - error: 可能的错误（Redis 查询失败、数据库查询失败）
func (s *AgentPermissionService) HasResourcePermission(ctx context.Context, agentID uuid.UUID, permission string, resourceType string, resourceID *uuid.UUID) (bool, error) {
	return s.permCache.HasPermission(ctx, agentID, permission, func() (bool, error) {
		return s.svc.Store.HasAgentPermission(ctx, agentID, permission, resourceType, resourceID)
	})
}

// HasAgentPermissionAny 检查代理是否对任意资源拥有特定权限（不限定 resource_id）。
// 与 HasPermission 的区别：不使用 Redis 缓存，直接查询数据库。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - permission: 权限标识符
//
// 返回：
//   - bool: 代理是否拥有该权限
//   - error: 可能的错误（数据库查询失败）
func (s *AgentPermissionService) HasAgentPermissionAny(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
	has, err := s.svc.Store.HasAgentPermissionAny(ctx, agentID, permission)
	if err != nil {
		return false, fmt.Errorf("check agent permission any: %w", err)
	}
	return has, nil
}

// ListPermissions 列出代理的所有权限。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//
// 返回：
//   - []db.AgentPermission: 代理权限列表
//   - error: 可能的错误（数据库查询失败）
func (s *AgentPermissionService) ListPermissions(ctx context.Context, agentID uuid.UUID) ([]types.AgentPermission, error) {
	return s.svc.Store.ListAgentPermissions(ctx, agentID)
}

// GrantDefaultPermissions 为新创建的代理授予默认权限集，授权后使缓存失效。
// 默认权限包括：task:claim、task:execute、task:comment、memory:read。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - grantedBy: 授予权限的操作者 ID
//
// 返回：
//   - error: 可能的错误（数据库写入失败）
func (s *AgentPermissionService) GrantDefaultPermissions(ctx context.Context, agentID uuid.UUID, grantedBy uuid.UUID) error {
	err := s.svc.Store.GrantDefaultPermissions(ctx, agentID, grantedBy)
	if err != nil {
		return err
	}
	s.permCache.Invalidate(ctx, agentID)
	return nil
}

// GrantRolePermissions 为代理授予预定义角色的所有权限，任何一个权限授予失败则返回错误。
// 角色定义在 types.AgentRoles 中，如 developer、reviewer 等。
//
// 步骤：
//  1. 从 types.AgentRoles 查找角色定义
//  2. 遍历角色的所有权限，逐个授予（资源类型设为 "*" 通配符）
//  3. 任一权限授予失败则返回错误
//  4. 全部成功后使权限缓存失效
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - roleName: 角色名称（如 developer、reviewer）
//   - grantedBy: 授予权限的操作者 ID
//
// 返回：
//   - error: 可能的错误（角色不存在、权限授予失败）
func (s *AgentPermissionService) GrantRolePermissions(ctx context.Context, agentID uuid.UUID, roleName string, grantedBy uuid.UUID) error {
	role, ok := types.AgentRoles[roleName]
	if !ok {
		return fmt.Errorf("unknown agent role: %s", roleName)
	}
	for _, perm := range role.Permissions {
		if _, err := s.svc.Store.GrantAgentPermission(ctx, agentID, perm, "*", nil, grantedBy); err != nil {
			return fmt.Errorf("grant role permission %s: %w", perm, err)
		}
	}
	s.permCache.Invalidate(ctx, agentID)
	return nil
}
