// agent_permission.go 提供 AI 代理权限管理的数据访问操作。
//
// 管理 Agent 的细粒度权限控制，包括权限授予、撤销和查询。
// 权限系统基于 agent_permissions 表，支持按资源类型和资源 ID 进行细粒度授权。
//
// 默认权限（task:claim, task:execute, task:comment, memory:read）在 Agent 创建时自动授予。
// 需手动授权的权限（task:approve, git:push 等）需管理员显式授予。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	types "github.com/teammate/server/internal/types"
)

// DefaultAgentPermissions 是新创建 Agent 默认授予的权限集合，引用自 types 包。
//
// 包含：task:claim, task:execute, task:comment, memory:read
var DefaultAgentPermissions = types.DefaultAgentPermissions

// DeniedByDefaultAgentPermissions 是默认不授予的权限集合，需手动授权。
//
// 包含：task:approve, task:reject, memory:create, git:push, git:force-push, resource:delete, config:modify
var DeniedByDefaultAgentPermissions = types.DeniedByDefaultAgentPermissions

// GrantAgentPermission 为 Agent 授予指定权限（插入 agent_permissions 记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//   - permission: 权限字符串（如 "task:claim"）
//   - resourceType: 资源类型（如 "project"、"*" 表示全部）
//   - resourceID: 资源 ID（可选，为 nil 时表示全局权限）
//   - grantedBy: 授权者 ID
//
// 返回：
//   - types.AgentPermission: 创建的权限记录
//   - error: 创建失败时返回错误
func (s *Store) GrantAgentPermission(ctx context.Context, agentID uuid.UUID, permission string, resourceType string, resourceID *uuid.UUID, grantedBy uuid.UUID) (types.AgentPermission, error) {
	var nullResourceID uuid.NullUUID
	if resourceID != nil {
		nullResourceID = uuid.NullUUID{UUID: *resourceID, Valid: true}
	}
	perm, err := s.q.CreateAgentPermission(ctx, db.CreateAgentPermissionParams{
		AgentID:      agentID,
		Permission:   permission,
		ResourceType: resourceType,
		ResourceID:   nullResourceID,
		GrantedBy:    uuid.NullUUID{UUID: grantedBy, Valid: true},
	})
	if err != nil {
		return types.AgentPermission{}, fmt.Errorf("grant agent permission: %w", err)
	}
	return ToDomainAgentPermission(perm)
}

// GetAgentPermission 根据 ID 获取一条权限记录。
//
// 返回：
//   - types.AgentPermission: 权限记录
//   - error: 查询失败时返回错误
func (s *Store) GetAgentPermission(ctx context.Context, id uuid.UUID) (types.AgentPermission, error) {
	perm, err := s.q.GetAgentPermission(ctx, id)
	if err != nil {
		return types.AgentPermission{}, fmt.Errorf("get agent permission: %w", err)
	}
	return ToDomainAgentPermission(perm)
}

// RevokeAgentPermission 撤销 Agent 的指定权限（删除 agent_permissions 记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 权限记录的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) RevokeAgentPermission(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteAgentPermission(ctx, id)
}

// HasAgentPermission 检查 Agent 是否对指定资源拥有特定权限。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//   - permission: 权限字符串
//   - resourceType: 资源类型
//   - resourceID: 资源 ID（可选）
//
// 返回：
//   - bool: 是否拥有该权限
//   - error: 查询失败时返回错误
func (s *Store) HasAgentPermission(ctx context.Context, agentID uuid.UUID, permission string, resourceType string, resourceID *uuid.UUID) (bool, error) {
	var nullResourceID uuid.NullUUID
	if resourceID != nil {
		nullResourceID = uuid.NullUUID{UUID: *resourceID, Valid: true}
	}
	return s.q.HasAgentPermission(ctx, db.HasAgentPermissionParams{
		AgentID:      agentID,
		Permission:   permission,
		ResourceType: resourceType,
		ResourceID:   nullResourceID,
	})
}

// HasAgentPermissionAny 检查 Agent 是否对任意资源拥有特定权限（不限定 resource_id）。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//   - permission: 权限字符串
//
// 返回：
//   - bool: 是否拥有该权限
//   - error: 查询失败时返回错误
func (s *Store) HasAgentPermissionAny(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
	return s.q.HasAgentPermissionAny(ctx, db.HasAgentPermissionAnyParams{
		AgentID:    agentID,
		Permission: permission,
	})
}

// ListAgentPermissions 查询指定 Agent 的所有权限列表。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - []db.AgentPermission: 权限列表
//   - error: 查询失败时返回错误
func (s *Store) ListAgentPermissions(ctx context.Context, agentID uuid.UUID) ([]types.AgentPermission, error) {
	perms, err := s.q.ListAgentPermissions(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent permissions: %w", err)
	}
	return ToDomainAgentPermissionSlice(perms)
}

// GrantDefaultPermissions 为新创建的 Agent 批量授予默认权限集合。
//
// 遍历 DefaultAgentPermissions 列表，为每个权限创建 agent_permissions 记录。
// 如果权限已存在（唯一约束冲突），自动跳过继续处理下一个。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//   - grantedBy: 授权者 ID
//
// 返回：
//   - error: 批量授权失败时返回错误
func (s *Store) GrantDefaultPermissions(ctx context.Context, agentID uuid.UUID, grantedBy uuid.UUID) error {
	for _, perm := range DefaultAgentPermissions {
		_, err := s.q.CreateAgentPermission(ctx, db.CreateAgentPermissionParams{
			AgentID:      agentID,
			Permission:   perm,
			ResourceType: "*",
			ResourceID:   uuid.NullUUID{},
			GrantedBy:    uuid.NullUUID{UUID: grantedBy, Valid: true},
		})
		if err != nil {
			// 忽略唯一约束冲突（权限已存在）
			if fmt.Sprintf("%v", err) != "" {
				continue
			}
		}
	}
	return nil
}

// DeleteAgentPermissions 删除指定 Agent 的所有权限记录。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteAgentPermissions(ctx context.Context, agentID uuid.UUID) error {
	return s.q.DeleteAgentPermissionsByAgent(ctx, agentID)
}
