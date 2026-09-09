// search.go implements the business logic for global search, supporting keyword search over tasks and agents.
//
// This file contains:
//   - SearchService struct: provides encapsulation of search-related business logic
//   - SearchTasks: searches tasks by keyword, supporting ILIKE fuzzy matching on title and description, with optional project filtering
//   - SearchAgents: searches agents by keyword, supporting ILIKE fuzzy matching on name, with optional workspace filtering
//   - search results are ordered by creation time descending
//
// Search strategy:
//   - Uses PostgreSQL ILIKE for case-insensitive fuzzy matching
//   - Keywords are automatically wrapped with % wildcards to implement substring matching
//   - Supports global search (no scope specified) and scoped search (specifying a project or workspace ID)
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// SearchService provides the search-related business logic.
type SearchService struct {
	svc *Service
}

// NewSearchService creates a new SearchService instance.
func NewSearchService(svc *Service) *SearchService {
	return &SearchService{svc: svc}
}

// SearchTasks searches tasks by keyword within the specified workspace, with optional filtering by project ID.
// Uses ILIKE for fuzzy matching on title and description; results are ordered by creation time descending.
//
// Parameters:
//   - ctx: request context
//   - keyword: search keyword
//   - workspaceID: workspace ID
//   - projectID: optional, filter by project ID
//
// Returns:
//   - []db.Task: list of matching tasks
//   - error: possible error (database query failure)
func (s *SearchService) SearchTasks(ctx context.Context, keyword string, workspaceID uuid.UUID, projectID *uuid.UUID) ([]types.Task, error) {
	pattern := "%" + keyword + "%"
	if projectID != nil {
		return s.svc.Store.SearchTasksByWorkspaceAndProject(ctx, workspaceID, *projectID, pattern)
	}
	return s.svc.Store.SearchTasksByWorkspace(ctx, workspaceID, pattern)
}

// SearchAgents searches agents by keyword within the specified workspace.
// Uses ILIKE for fuzzy matching on name; results are ordered by creation time descending.
//
// Parameters:
//   - ctx: request context
//   - keyword: search keyword
//   - workspaceID: workspace ID
//
// Returns:
//   - []db.Agent: list of matching agents
//   - error: possible error (database query failure)
func (s *SearchService) SearchAgents(ctx context.Context, keyword string, workspaceID uuid.UUID) ([]types.Agent, error) {
	return s.svc.Store.SearchAgentsByWorkspace(ctx, workspaceID, "%"+keyword+"%")
}
