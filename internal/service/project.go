// project.go implements the business logic for project management, including project CRUD,
// member management, reviewer management, and project access checks for agents and members.
// Projects support a multi-level permission model: workspace owner/admin automatically
// have all project permissions; other members require explicit project membership.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ProjectService provides the business logic for project management.
type ProjectService struct {
	svc *Service
}

// NewProjectService creates a new ProjectService instance.
func NewProjectService(svc *Service) *ProjectService {
	return &ProjectService{svc: svc}
}

// Create creates a new project.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating the project, including workspace ID, name, description, Git repository URL, etc.
//
// Returns:
//   - types.Project: the created project information
//   - error: possible error (database write failure)
func (s *ProjectService) Create(ctx context.Context, params types.CreateProjectParams) (types.Project, error) {
	return s.svc.Store.CreateProject(ctx, params)
}

// Get retrieves project information by ID.
//
// Parameters:
//   - ctx: request context
//   - id: project ID
//
// Returns:
//   - types.Project: project information
//   - error: possible error (project does not exist)
func (s *ProjectService) Get(ctx context.Context, id uuid.UUID) (types.Project, error) {
	return s.svc.Store.GetProject(ctx, id)
}

// List lists all projects in the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.Project: list of projects
//   - error: possible error (database query failure)
func (s *ProjectService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Project, error) {
	return s.svc.Store.ListProjects(ctx, workspaceID)
}

// ListByAgentMembership returns only the projects in which the specified agent is a member.
// Used by the Agentd daemon to retrieve the list of actionable projects.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - agentID: agent ID
//
// Returns:
//   - []types.Project: list of projects in which the agent is a member
//   - error: possible error (database query failure)
func (s *ProjectService) ListByAgentMembership(ctx context.Context, workspaceID, agentID uuid.UUID) ([]types.Project, error) {
	return s.svc.Store.ListProjectsByAgentMembership(ctx, types.ListProjectsByAgentMembershipParams{
		WorkspaceID: workspaceID.String(),
		AgentID:     strPtr(agentID.String()),
	})
}

// strPtr is a helper function that returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

// Update updates project information.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for updating the project, including the ID and the fields to update
//
// Returns:
//   - types.Project: the updated project information
//   - error: possible error (project does not exist, database update failure)
func (s *ProjectService) Update(ctx context.Context, params types.UpdateProjectParams) (types.Project, error) {
	return s.svc.Store.UpdateProject(ctx, params)
}

// Delete deletes a project.
//
// Parameters:
//   - ctx: request context
//   - id: project ID
//
// Returns:
//   - error: possible error (project does not exist, database delete failure)
func (s *ProjectService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteProject(ctx, id)
}

// GetProjectMember retrieves a project member record by ID.
//
// Parameters:
//   - ctx: request context
//   - memberID: project member record ID
//
// Returns:
//   - types.ProjectMember: project member record
//   - error: possible error (record does not exist)
func (s *ProjectService) GetProjectMember(ctx context.Context, memberID uuid.UUID) (types.ProjectMember, error) {
	return s.svc.Store.GetProjectMember(ctx, memberID)
}

// AddMember adds an agent as a project member.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for adding a project member, including project ID, agent ID, and project role
//
// Returns:
//   - types.ProjectMember: the created project member record
//   - error: possible error (database write failure)
func (s *ProjectService) AddMember(ctx context.Context, params types.CreateProjectMemberParams) (types.ProjectMember, error) {
	return s.svc.Store.CreateProjectMember(ctx, params)
}

// ListMembers lists all members (agents) of a project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - []types.ProjectMember: list of project members
//   - error: possible error (database query failure)
func (s *ProjectService) ListMembers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectMember, error) {
	return s.svc.Store.ListProjectMembers(ctx, projectID)
}

// RemoveMember removes a member from a project.
//
// Parameters:
//   - ctx: request context
//   - memberID: project member record ID
//
// Returns:
//   - error: possible error (record does not exist, database delete failure)
func (s *ProjectService) RemoveMember(ctx context.Context, memberID uuid.UUID) error {
	return s.svc.Store.DeleteProjectMember(ctx, memberID)
}

// AddReviewer adds a reviewer to a project. Reviewers can review review-type nodes within the project.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for adding a reviewer, including project ID and agent ID
//
// Returns:
//   - types.ProjectReviewer: the created project reviewer record
//   - error: possible error (database write failure)
func (s *ProjectService) AddReviewer(ctx context.Context, params types.CreateProjectReviewerParams) (types.ProjectReviewer, error) {
	return s.svc.Store.CreateProjectReviewer(ctx, params)
}

// ListReviewers lists all reviewers of a project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - []types.ProjectReviewer: list of project reviewers
//   - error: possible error (database query failure)
func (s *ProjectService) ListReviewers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectReviewer, error) {
	return s.svc.Store.ListProjectReviewers(ctx, projectID)
}

// RemoveReviewer removes a reviewer from a project.
//
// Parameters:
//   - ctx: request context
//   - reviewerID: project reviewer record ID
//
// Returns:
//   - error: possible error (record does not exist, database delete failure)
func (s *ProjectService) RemoveReviewer(ctx context.Context, reviewerID uuid.UUID) error {
	return s.svc.Store.DeleteProjectReviewer(ctx, reviewerID)
}

// IsAgentMember checks whether an agent is a member of a project.
//
// Parameters:
//   - ctx: request context
//   - params: check parameters, including project ID and agent ID
//
// Returns:
//   - bool: whether the agent is a project member
//   - error: possible error (database query failure)
func (s *ProjectService) IsAgentMember(ctx context.Context, params types.IsAgentProjectMemberParams) (bool, error) {
	return s.svc.Store.IsAgentProjectMember(ctx, params)
}

// CheckAgentProjectAccess checks whether an agent has permission to access the specified project.
// If the agent is not a project member, returns an error.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - projectID: project ID
//
// Returns:
//   - error: returns an error when the agent has no access permission
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

// GetProjectReviewerByReviewerID retrieves project reviewer information by reviewer record ID.
// Used to verify whether a reviewer record exists and belongs to the specified project.
//
// Parameters:
//   - ctx: request context
//   - reviewerID: reviewer record ID
//
// Returns:
//   - types.ProjectReviewer: reviewer record (including project_id)
//   - error: possible error (record does not exist)
func (s *ProjectService) GetProjectReviewerByReviewerID(ctx context.Context, reviewerID uuid.UUID) (types.ProjectReviewer, error) {
	reviewer, err := s.svc.Store.GetProjectReviewerByID(ctx, reviewerID)
	if err != nil {
		return types.ProjectReviewer{}, fmt.Errorf("get project reviewer: %w", err)
	}
	return reviewer, nil
}

// CheckMemberProjectAccess checks whether a member has permission to access the specified project.
// Inheritance rules:
//  1. Workspace owner/admin automatically has full access to all projects within their workspace
//  2. Workspace member requires explicit project membership
//  3. Workspace viewer can only view, even if added as a project member
//
// If requiredRole is specified, the member's project role must meet or exceed that level (lead > developer > reviewer).
// Workspace owner/admin skip the role level check.
//
// Parameters:
//   - ctx: request context
//   - memberID: member ID
//   - projectID: project ID
//   - workspaceRole: the member's role in the workspace
//   - requiredRole: optional, the minimum project role level required
//
// Returns:
//   - error: returns an error when the member has no access permission
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
