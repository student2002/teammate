// agent_permission.go implements the business logic for agent permission management, including granting, revoking, and checking permissions.
//
// This file contains:
//   - AgentPermissionService struct: the permission management service, supporting resource-level permission matching and caching
//   - Grant: grants a permission to an agent; after granting, it sends a permission-changed event via SSE and invalidates the cache
//   - Revoke: revokes an agent permission; after revoking, it sends a permission-changed event via SSE and invalidates the cache
//   - HasPermission: checks whether the agent has the specified permission (any resource), accelerated by a Redis cache
//   - HasResourcePermission: checks whether the agent has a specific permission on a specified resource
//   - ListPermissions: lists all permissions of the agent
//   - GrantDefaultPermissions: grants the default permission set to a new agent
//   - GrantRolePermissions: grants all permissions of a predefined role to the agent
//
// Permissions support resource-level matching (exact match or wildcard match), accelerated by a Redis cache.
// On permission changes, a control event is sent to the agent via SSE to ensure the agent promptly perceives permission changes.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

import "github.com/teammate/server/internal/types"

// AgentPermissionService provides the business logic for agent permission management.
// Permissions support resource-level matching (exact match or wildcard match), accelerated by a Redis cache.
type AgentPermissionService struct {
	svc       *Service
	permCache *PermissionCache
}

// NewAgentPermissionService creates a new AgentPermissionService instance, initializing the permission cache.
func NewAgentPermissionService(svc *Service) *AgentPermissionService {
	return &AgentPermissionService{
		svc:       svc,
		permCache: NewPermissionCache(svc.Redis),
	}
}

// GetPermission retrieves a permission record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: permission record ID
//
// Returns:
//   - db.AgentPermission: permission record
//   - error: possible errors (record not found)
func (s *AgentPermissionService) GetPermission(ctx context.Context, id uuid.UUID) (types.AgentPermission, error) {
	perm, err := s.svc.Store.GetAgentPermission(ctx, id)
	if err != nil {
		return types.AgentPermission{}, fmt.Errorf("get agent permission: %w", err)
	}
	return perm, nil
}

// Grant grants a permission to an agent; after granting, it sends a permission-changed event via SSE and invalidates the cache.
//
// Steps:
//  1. Call the Store to write the permission record to the database
//  2. Invalidate the agent's permission cache (Redis)
//  3. Send a permission:changed control event via SSE to notify the agent
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - permission: permission identifier (e.g. task:claim, git:push, etc.)
//   - resourceType: resource type (e.g. project, workspace; "*" means wildcard)
//   - resourceID: resource ID (optional, combined with resourceType for exact match)
//   - grantedBy: the operator ID that grants the permission
//
// Returns:
//   - db.AgentPermission: the created permission record
//   - error: possible errors (database write failure)
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

// Revoke revokes a permission from an agent; after revoking, it sends a permission-changed event via SSE and invalidates the cache.
// The permission ID uniquely identifies a permission record.
//
// Steps:
//  1. Query the permission record by permission ID to obtain the agent ID and permission name
//  2. Call the Store to delete the permission record from the database
//  3. Invalidate the agent's permission cache (Redis)
//  4. Send a permission:changed control event via SSE to notify the agent
//
// Parameters:
//   - ctx: request context
//   - id: permission record ID
//
// Returns:
//   - error: possible errors (database deletion failure)
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

// HasPermission checks whether the agent has the specified permission (any resource), accelerated by a Redis cache.
// Matching rules: exact match (resource_type + resource_id) or wildcard match (resource_type = '*').
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - permission: permission identifier
//
// Returns:
//   - bool: whether the agent has the permission
//   - error: possible errors (Redis query failure, database query failure)
func (s *AgentPermissionService) HasPermission(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
	return s.permCache.HasPermission(ctx, agentID, permission, func() (bool, error) {
		return s.svc.Store.HasAgentPermissionAny(ctx, agentID, permission)
	})
}

// HasResourcePermission checks whether the agent has a specific permission on the specified resource.
// Matching rules: exact match (resource_type + resource_id) or wildcard match (resource_type = '*').
// Accelerated by a Redis cache.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - permission: permission identifier
//   - resourceType: resource type (e.g. project, workspace)
//   - resourceID: resource ID (optional; pass nil to match wildcard permissions)
//
// Returns:
//   - bool: whether the agent has the specified permission on the resource
//   - error: possible errors (Redis query failure, database query failure)
func (s *AgentPermissionService) HasResourcePermission(ctx context.Context, agentID uuid.UUID, permission string, resourceType string, resourceID *uuid.UUID) (bool, error) {
	return s.permCache.HasPermission(ctx, agentID, permission, func() (bool, error) {
		return s.svc.Store.HasAgentPermission(ctx, agentID, permission, resourceType, resourceID)
	})
}

// HasAgentPermissionAny checks whether the agent has a specific permission on any resource (not restricted by resource_id).
// Difference from HasPermission: it does not use a Redis cache and queries the database directly.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - permission: permission identifier
//
// Returns:
//   - bool: whether the agent has the permission
//   - error: possible errors (database query failure)
func (s *AgentPermissionService) HasAgentPermissionAny(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
	has, err := s.svc.Store.HasAgentPermissionAny(ctx, agentID, permission)
	if err != nil {
		return false, fmt.Errorf("check agent permission any: %w", err)
	}
	return has, nil
}

// ListPermissions lists all permissions of the agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//
// Returns:
//   - []db.AgentPermission: agent permission list
//   - error: possible errors (database query failure)
func (s *AgentPermissionService) ListPermissions(ctx context.Context, agentID uuid.UUID) ([]types.AgentPermission, error) {
	return s.svc.Store.ListAgentPermissions(ctx, agentID)
}

// GrantDefaultPermissions grants the default permission set to a newly created agent; after granting, it invalidates the cache.
// Default permissions include: task:claim, task:execute, task:comment, memory:read.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - grantedBy: the operator ID that grants the permissions
//
// Returns:
//   - error: possible errors (database write failure)
func (s *AgentPermissionService) GrantDefaultPermissions(ctx context.Context, agentID uuid.UUID, grantedBy uuid.UUID) error {
	err := s.svc.Store.GrantDefaultPermissions(ctx, agentID, grantedBy)
	if err != nil {
		return err
	}
	s.permCache.Invalidate(ctx, agentID)
	return nil
}

// GrantRolePermissions grants all permissions of a predefined role to the agent; if any permission grant fails, it returns an error.
// Role definitions are in types.AgentRoles, e.g. developer, reviewer, etc.
//
// Steps:
//  1. Look up the role definition from types.AgentRoles
//  2. Iterate over all permissions of the role and grant them one by one (resource type set to "*" wildcard)
//  3. If any permission grant fails, return an error
//  4. After all succeed, invalidate the permission cache
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - roleName: role name (e.g. developer, reviewer)
//   - grantedBy: the operator ID that grants the permissions
//
// Returns:
//   - error: possible errors (role not found, permission grant failure)
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
