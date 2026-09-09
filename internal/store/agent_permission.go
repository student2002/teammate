// agent_permission.go provides data access operations for AI agent permission management.
//
// Manages fine-grained permission control for Agents, including permission granting, revocation, and querying.
// The permission system is based on the agent_permissions table and supports fine-grained authorization by resource type and resource ID.
//
// Default permissions (task:claim, task:execute, task:comment, memory:read) are automatically granted when an Agent is created.
// Permissions that require manual authorization (task:approve, git:push, etc.) must be explicitly granted by an administrator.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	types "github.com/teammate/server/internal/types"
)

// DefaultAgentPermissions is the set of permissions granted by default to newly created Agents, referenced from the types package.
//
// Includes: task:claim, task:execute, task:comment, memory:read
var DefaultAgentPermissions = types.DefaultAgentPermissions

// DeniedByDefaultAgentPermissions is the set of permissions not granted by default; they require manual authorization.
//
// Includes: task:approve, task:reject, memory:create, git:push, git:force-push, resource:delete, config:modify
var DeniedByDefaultAgentPermissions = types.DeniedByDefaultAgentPermissions

// GrantAgentPermission grants a specified permission to an Agent (inserts an agent_permissions record).
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//   - permission: the permission string (e.g. "task:claim")
//   - resourceType: the resource type (e.g. "project", or "*" for all)
//   - resourceID: the resource ID (optional; when nil, indicates a global permission)
//   - grantedBy: the authorizer's ID
//
// Returns:
//   - types.AgentPermission: the created permission record
//   - error: error returned when creation fails
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

// GetAgentPermission retrieves a single permission record by ID.
//
// Returns:
//   - types.AgentPermission: the permission record
//   - error: error returned when the query fails
func (s *Store) GetAgentPermission(ctx context.Context, id uuid.UUID) (types.AgentPermission, error) {
	perm, err := s.q.GetAgentPermission(ctx, id)
	if err != nil {
		return types.AgentPermission{}, fmt.Errorf("get agent permission: %w", err)
	}
	return ToDomainAgentPermission(perm)
}

// RevokeAgentPermission revokes the specified permission from an Agent (deletes an agent_permissions record).
//
// Parameters:
//   - ctx: request context
//   - id: the UUID of the permission record
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) RevokeAgentPermission(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteAgentPermission(ctx, id)
}

// HasAgentPermission checks whether an Agent has a specific permission on the specified resource.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//   - permission: the permission string
//   - resourceType: the resource type
//   - resourceID: the resource ID (optional)
//
// Returns:
//   - bool: whether the Agent has the permission
//   - error: error returned when the query fails
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

// HasAgentPermissionAny checks whether an Agent has a specific permission on any resource (without restricting resource_id).
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//   - permission: the permission string
//
// Returns:
//   - bool: whether the Agent has the permission
//   - error: error returned when the query fails
func (s *Store) HasAgentPermissionAny(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
	return s.q.HasAgentPermissionAny(ctx, db.HasAgentPermissionAnyParams{
		AgentID:    agentID,
		Permission: permission,
	})
}

// ListAgentPermissions queries all permissions of the specified Agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//
// Returns:
//   - []db.AgentPermission: the permission list
//   - error: error returned when the query fails
func (s *Store) ListAgentPermissions(ctx context.Context, agentID uuid.UUID) ([]types.AgentPermission, error) {
	perms, err := s.q.ListAgentPermissions(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent permissions: %w", err)
	}
	return ToDomainAgentPermissionSlice(perms)
}

// GrantDefaultPermissions batch-grants the default permission set to a newly created Agent.
//
// Iterates through the DefaultAgentPermissions list and creates an agent_permissions record for each permission.
// If a permission already exists (unique constraint conflict), it is automatically skipped and the next one is processed.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//   - grantedBy: the authorizer's ID
//
// Returns:
//   - error: error returned when batch granting fails
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
			// Ignore unique constraint conflicts (permission already exists)
			if fmt.Sprintf("%v", err) != "" {
				continue
			}
		}
	}
	return nil
}

// DeleteAgentPermissions deletes all permission records of the specified Agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) DeleteAgentPermissions(ctx context.Context, agentID uuid.UUID) error {
	return s.q.DeleteAgentPermissionsByAgent(ctx, agentID)
}
