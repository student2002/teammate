// project.go provides data access operations for project management.
//
// It includes CRUD operations for projects, project member management, and
// project reviewer management. A project is a software project within a
// workspace, associated with code repositories, tasks, and agents.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateProject creates a new project record.
//
// Parameters:
//   - ctx: request context
//   - params: project creation parameters, including workspace ID, name, description, repository URL, etc.
//
// Returns:
//   - types.Project: the created project record
//   - error: error returned when creation fails
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

// GetProject queries a single project record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: project UUID
//
// Returns:
//   - types.Project: the project record
//   - error: error returned when the query fails
func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (types.Project, error) {
	p, err := s.q.GetProject(ctx, id)
	if err != nil {
		return types.Project{}, fmt.Errorf("get project: %w", err)
	}
	return ToDomainProject(p)
}

// ListProjects queries all projects within the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - []types.Project: project list
//   - error: error returned when the query fails
func (s *Store) ListProjects(ctx context.Context, workspaceID uuid.UUID) ([]types.Project, error) {
	projects, err := s.q.ListProjects(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	return ToDomainProjectSlice(projects)
}

// ListProjectsByAgentMembership queries all projects in which the specified agent participates as a member.
//
// Parameters:
//   - ctx: request context
//   - arg: query parameters, including agent ID
//
// Returns:
//   - []types.Project: project list
//   - error: error returned when the query fails
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

// UpdateProject updates basic project information.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including project ID and fields to update
//
// Returns:
//   - types.Project: the updated project record
//   - error: error returned when the update fails
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

// DeleteProject deletes a project record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: project UUID
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) DeleteProject(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// After 002_remove_fks, the following columns no longer have foreign keys and must be explicitly nulled (FK strategy: integrity enforced at the application layer):
	//   memories.source_task_id, workflow_trigger_runs.task_id
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

// CreateProjectMember adds a member to a project (inserts a project_members record).
//
// A member can be an agent or a human user, distinguished by member_type.
//
// Parameters:
//   - ctx: request context
//   - params: member creation parameters, including project ID, member type, role, etc.
//
// Returns:
//   - types.ProjectMember: the created member record
//   - error: error returned when creation fails
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

// ListProjectMembers queries the list of all members of the specified project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - []types.ProjectMember: member list
//   - error: error returned when the query fails
func (s *Store) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectMember, error) {
	members, err := s.q.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}
	return ToDomainProjectMemberSlice(members)
}

// DeleteProjectMember removes a member from a project (deletes a project_members record).
//
// Parameters:
//   - ctx: request context
//   - memberID: UUID of the member record
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) DeleteProjectMember(ctx context.Context, memberID uuid.UUID) error {
	if err := s.q.DeleteProjectMember(ctx, memberID); err != nil {
		return fmt.Errorf("delete project member: %w", err)
	}
	return nil
}

// CreateProjectReviewer adds a reviewer to a project (inserts a project_reviewers record).
//
// A reviewer can be an agent or a human user, responsible for code review nodes.
//
// Parameters:
//   - ctx: request context
//   - params: reviewer creation parameters
//
// Returns:
//   - types.ProjectReviewer: the created reviewer record
//   - error: error returned when creation fails
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

// ListProjectReviewers queries the list of all reviewers of the specified project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - []types.ProjectReviewer: reviewer list
//   - error: error returned when the query fails
func (s *Store) ListProjectReviewers(ctx context.Context, projectID uuid.UUID) ([]types.ProjectReviewer, error) {
	reviewers, err := s.q.ListProjectReviewers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project reviewers: %w", err)
	}
	return ToDomainProjectReviewerSlice(reviewers)
}

// DeleteProjectReviewer removes a reviewer from a project (deletes a project_reviewers record).
//
// Parameters:
//   - ctx: request context
//   - reviewerID: UUID of the reviewer record
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) DeleteProjectReviewer(ctx context.Context, reviewerID uuid.UUID) error {
	if err := s.q.DeleteProjectReviewer(ctx, reviewerID); err != nil {
		return fmt.Errorf("delete project reviewer: %w", err)
	}
	return nil
}

// IsAgentProjectMember checks whether the specified agent is a member of the project.
//
// Parameters:
//   - ctx: request context
//   - params: check parameters, including project ID and agent ID
//
// Returns:
//   - bool: whether it is a project member
//   - error: error returned when the query fails
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

// IsMemberProjectMember checks whether the specified human member is a member of the project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//   - memberID: member UUID
//
// Returns:
//   - bool: whether it is a project member
//   - error: error returned when the query fails
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

// GetProjectReviewerByID queries project reviewer information by the reviewer record ID.
//
// Parameters:
//   - ctx: request context
//   - reviewerID: UUID of the reviewer record
//
// Returns:
//   - types.ProjectReviewer: the reviewer record
//   - error: error returned when the query fails
func (s *Store) GetProjectReviewerByID(ctx context.Context, reviewerID uuid.UUID) (types.ProjectReviewer, error) {
	r, err := s.q.GetProjectReviewerByID(ctx, reviewerID)
	if err != nil {
		return types.ProjectReviewer{}, fmt.Errorf("get project reviewer by id: %w", err)
	}
	return ToDomainProjectReviewer(r)
}

// GetProjectMemberRole queries the role of a human member within the project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//   - memberID: member UUID
//
// Returns:
//   - string: role name (e.g. "lead", "developer", "reviewer")
//   - error: error returned when the query fails
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

// GetProjectMember queries a single project member by the project member record ID.
//
// Parameters:
//   - ctx: request context
//   - memberID: project member record ID
//
// Returns:
//   - types.ProjectMember: the project member record
//   - error: error returned when the query fails
func (s *Store) GetProjectMember(ctx context.Context, memberID uuid.UUID) (types.ProjectMember, error) {
	m, err := s.q.GetProjectMember(ctx, memberID)
	if err != nil {
		return types.ProjectMember{}, fmt.Errorf("get project member: %w", err)
	}
	return ToDomainProjectMember(m)
}
