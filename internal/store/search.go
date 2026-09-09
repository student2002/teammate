// search.go provides data access operations for global search.
//
// This file includes:
//   - SearchTasksByWorkspace: search tasks by keyword within the specified workspace (ILIKE fuzzy match on title and description)
//   - SearchTasksByWorkspaceAndProject: search tasks by keyword within the specified workspace and project
//   - SearchAgentsByWorkspace: search agents by keyword within the specified workspace (ILIKE fuzzy match on name)
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// SearchTasksByWorkspace searches tasks by keyword within the specified workspace.
// Uses ILIKE for fuzzy matching of title and description, with results ordered by creation time descending.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - pattern: search pattern (already includes the % wildcard)
//
// Returns:
//   - []types.Task: list of matching tasks
//   - error: possible error (database query failure)
func (s *Store) SearchTasksByWorkspace(ctx context.Context, workspaceID uuid.UUID, pattern string) ([]types.Task, error) {
	tasks, err := s.q.SearchTasksByWorkspace(ctx, FromDomainSearchTasksByWorkspaceParams(workspaceID, pattern))
	if err != nil {
		return nil, fmt.Errorf("search tasks by workspace: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// SearchTasksByWorkspaceAndProject searches tasks by keyword within the specified workspace and project.
// Uses ILIKE for fuzzy matching of title and description, with results ordered by creation time descending.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - projectID: project ID
//   - pattern: search pattern (already includes the % wildcard)
//
// Returns:
//   - []types.Task: list of matching tasks
//   - error: possible error (database query failure)
func (s *Store) SearchTasksByWorkspaceAndProject(ctx context.Context, workspaceID, projectID uuid.UUID, pattern string) ([]types.Task, error) {
	tasks, err := s.q.SearchTasksByWorkspaceAndProject(ctx, FromDomainSearchTasksByWorkspaceAndProjectParams(workspaceID, projectID, pattern))
	if err != nil {
		return nil, fmt.Errorf("search tasks by workspace and project: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// SearchAgentsByWorkspace searches agents by keyword within the specified workspace.
// Uses ILIKE for fuzzy matching of name, with results ordered by creation time descending.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - pattern: search pattern (already includes the % wildcard)
//
// Returns:
//   - []types.Agent: list of matching agents
//   - error: possible error (database query failure)
func (s *Store) SearchAgentsByWorkspace(ctx context.Context, workspaceID uuid.UUID, pattern string) ([]types.Agent, error) {
	agents, err := s.q.SearchAgentsByWorkspace(ctx, FromDomainSearchAgentsByWorkspaceParams(workspaceID, pattern))
	if err != nil {
		return nil, fmt.Errorf("search agents by workspace: %w", err)
	}
	return ToDomainAgentSlice(agents)
}
