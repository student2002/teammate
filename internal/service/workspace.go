// workspace.go implements the business logic for workspace management, including workspace CRUD,
// member management, built-in template initialization, and automatic workspace creation on OAuth login.
// A workspace is the top-level organizational unit of the system, containing projects, members,
// agents, skills, and other resources.
//
// This file contains:
//   - WorkspaceService struct: provides business-logic encapsulation for workspace management
//   - Create / Get / List / Update / Delete: basic workspace CRUD operations
//   - CreateMember / ListMembers / GetMember / UpdateMemberRole / DeleteMember: member management
//   - SeedBuiltinTemplates: initializes built-in workflow templates for a workspace
//   - FindOrCreateForOAuth: automatically creates a workspace and member on OAuth login
//
// Core flows:
//  1. When a workspace is created, built-in workflow templates are auto-initialized
//     (e.g. the standard implement → self-test → review → deploy)
//  2. On first OAuth login, a personal workspace and member record are auto-created
//     and assigned the owner role
//  3. Member roles use a hierarchical model: owner > admin > member > viewer
package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// WorkspaceService provides the business logic for workspace management.
type WorkspaceService struct {
	svc *Service
}

// NewWorkspaceService creates a new WorkspaceService instance.
func NewWorkspaceService(svc *Service) *WorkspaceService {
	return &WorkspaceService{svc: svc}
}

// Create creates a new workspace and initializes built-in templates.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating the workspace, including name, description, issue prefix, etc.
//
// Returns:
//   - types.Workspace: the created workspace info
//   - error: possible error (database write failure)
func (s *WorkspaceService) Create(ctx context.Context, params types.CreateWorkspaceParams) (types.Workspace, error) {
	ws, err := s.svc.Store.CreateWorkspace(ctx, params)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// CreateForMember creates a workspace and sets the creator as owner.
func (s *WorkspaceService) CreateForMember(ctx context.Context, memberID uuid.UUID, params types.CreateWorkspaceParams) (types.Workspace, error) {
	ws, err := s.svc.Store.CreateWorkspaceForMember(ctx, memberID, params)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// Get fetches workspace info by ID.
//
// Parameters:
//   - ctx: request context
//   - id: workspace ID
//
// Returns:
//   - types.Workspace: workspace info
//   - error: possible error (workspace does not exist)
func (s *WorkspaceService) Get(ctx context.Context, id uuid.UUID) (types.Workspace, error) {
	ws, err := s.svc.Store.GetWorkspace(ctx, id)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// List lists all workspaces.
//
// Parameters:
//   - ctx: request context
//
// Returns:
//   - []types.Workspace: workspace list
//   - error: possible error (database query failure)
func (s *WorkspaceService) List(ctx context.Context) ([]types.Workspace, error) {
	wss, err := s.svc.Store.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	return wss, nil
}

// ListForMember lists the workspaces a member belongs to.
func (s *WorkspaceService) ListForMember(ctx context.Context, memberID uuid.UUID) ([]types.Workspace, error) {
	wss, err := s.svc.Store.ListWorkspacesByMemberID(ctx, memberID)
	if err != nil {
		return nil, err
	}
	return wss, nil
}

// Update updates workspace info.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for updating the workspace, including ID and fields to update
//
// Returns:
//   - types.Workspace: updated workspace info
//   - error: possible error (workspace does not exist, database update failure)
func (s *WorkspaceService) Update(ctx context.Context, params types.UpdateWorkspaceParams) (types.Workspace, error) {
	ws, err := s.svc.Store.UpdateWorkspace(ctx, params)
	if err != nil {
		return types.Workspace{}, err
	}
	return ws, nil
}

// Delete deletes a workspace.
//
// Parameters:
//   - ctx: request context
//   - id: workspace ID
//
// Returns:
//   - error: possible error (workspace does not exist, database delete failure)
func (s *WorkspaceService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteWorkspace(ctx, id)
}

// CreateMember creates a member within a workspace.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating the member, including name and email
//
// Returns:
//   - types.Member: the created member info
//   - error: possible error (email already exists, database write failure)
func (s *WorkspaceService) CreateMember(ctx context.Context, params types.CreateMemberParams) (types.Member, error) {
	return s.svc.Store.CreateMember(ctx, params)
}

// ListMembers lists all members in a workspace, including role info.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.ListMembersByWorkspaceRow: member list (including roles)
//   - error: possible error (database query failure)
func (s *WorkspaceService) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]types.ListMembersByWorkspaceRow, error) {
	members, err := s.svc.Store.ListMembersByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return members, nil
}

// GetMember fetches member info by ID.
//
// Parameters:
//   - ctx: request context
//   - id: member ID
//
// Returns:
//   - types.Member: member info
//   - error: possible error (member does not exist)
func (s *WorkspaceService) GetMember(ctx context.Context, id uuid.UUID) (types.Member, error) {
	return s.svc.Store.GetMember(ctx, id)
}

// UpdateMemberRole updates a member's role within a workspace.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for updating the role, including workspace ID, member ID, and the new role
//
// Returns:
//   - types.WorkspaceMember: the updated member-workspace association record
//   - error: possible error (database update failure)
func (s *WorkspaceService) UpdateMemberRole(ctx context.Context, params types.UpdateMemberRoleParams) (types.WorkspaceMember, error) {
	return s.svc.Store.UpdateMemberRole(ctx, params)
}

// DeleteMember deletes a member by ID.
//
// Parameters:
//   - ctx: request context
//   - id: member ID
//
// Returns:
//   - error: possible error (member does not exist, database delete failure)
func (s *WorkspaceService) DeleteMember(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteMember(ctx, id)
}

// GetMembership fetches the workspace membership by workspace ID and member ID.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - memberID: member ID
//
// Returns:
//   - types.WorkspaceMember: the workspace membership record
//   - error: possible error (record does not exist)
func (s *WorkspaceService) GetMembership(ctx context.Context, workspaceID, memberID uuid.UUID) (types.WorkspaceMember, error) {
	return s.svc.Store.GetWorkspaceMember(ctx, types.GetWorkspaceMemberParams{
		WorkspaceID: workspaceID.String(),
		MemberID:    memberID.String(),
	})
}

// GetWorkspaceMemberRole gets a member's role in the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - userID: member ID
//   - workspaceID: workspace ID
//
// Returns:
//   - string: member role (owner/admin/member/viewer)
//   - error: possible error (member does not belong to this workspace)
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

// GetMemberByEmail fetches member info by email.
//
// Parameters:
//   - ctx: request context
//   - email: member email
//
// Returns:
//   - types.Member: member info
//   - error: possible error (member does not exist)
func (s *WorkspaceService) GetMemberByEmail(ctx context.Context, email string) (types.Member, error) {
	return s.svc.Store.GetMemberByEmail(ctx, email)
}

// GetFirstWorkspaceForMember gets the first workspace a member belongs to
// (used to find an existing workspace on OAuth login).
//
// Parameters:
//   - ctx: request context
//   - memberID: member ID
//
// Returns:
//   - types.WorkspaceMember: the workspace membership record
//   - error: possible error (member has no workspace)
func (s *WorkspaceService) GetFirstWorkspaceForMember(ctx context.Context, memberID uuid.UUID) (types.WorkspaceMember, error) {
	return s.svc.Store.GetFirstWorkspaceForMember(ctx, memberID)
}

// SeedBuiltinTemplates initializes built-in workflow templates for a workspace.
// Built-in templates include the standard implement → self-test → review → deploy workflows.
//
// Parameters:
//   - ctx: request context
//   - svc: Service instance
//   - workspaceID: workspace ID
//
// Returns:
//   - error: possible error (database operation failure)
func SeedBuiltinTemplates(ctx context.Context, svc *Service, workspaceID uuid.UUID) error {
	return svc.Store.SeedBuiltinTemplates(ctx, workspaceID)
}

// FindOrCreateForOAuth finds or creates a workspace and member for OAuth login.
// If the member already exists, it is returned directly; otherwise a new workspace,
// member, and owner role assignment are created.
//
// Steps:
//  1. Look up an existing member by email
//  2. If the member exists: return directly
//  3. If the member does not exist:
//     a. Create a new workspace (named after the user)
//     b. Initialize built-in workflow templates
//     c. Create a new member
//     d. Add the member to the workspace and assign the owner role
//
// Parameters:
//   - ctx: request context
//   - email: user email
//   - name: user name
//
// Returns:
//   - types.Member: member info
//   - error: possible error (database operation failure)
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
