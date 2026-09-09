// stats.go implements the business logic for statistics queries on projects and agents.
// It provides project-dimension aggregation queries (total tasks, completed count, token consumption, etc.)
// and agent-dimension aggregation queries (completed task count, token consumption, success rate, etc.).
//
// This file contains:
//   - StatsService struct: provides the business-logic encapsulation for statistics queries
//   - GetProjectStats: retrieves aggregated statistics for a specified project, including task counts, token consumption, etc.
//   - GetAgentStats: retrieves aggregated statistics for a specified agent, including completed task count, token consumption, etc.
//
// Statistics dimensions:
//   - Project dimension: total tasks, completed count, in-progress count, rejected count, total token consumption, etc.
//   - Agent dimension: completed task count, total token consumption, average completion time, etc.
//
// Data source: statistics are obtained via SQL aggregation queries in the Store layer, supporting real-time computation.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
)

// StatsService provides the business logic for statistics queries.
type StatsService struct {
	svc *Service
}

// NewStatsService creates a new StatsService instance.
func NewStatsService(svc *Service) *StatsService {
	return &StatsService{svc: svc}
}

// GetProjectStats retrieves the statistics for the specified project, including total task count, completed count,
// in-progress count, rejected count, total token consumption, and other aggregated metrics.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - *store.ProjectStats: project statistics (task counts, token consumption, etc.)
//   - error: possible errors (database query failure)
func (s *StatsService) GetProjectStats(ctx context.Context, projectID uuid.UUID) (*store.ProjectStats, error) {
	stats, err := s.svc.Store.GetProjectStats(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project stats: %w", err)
	}
	return stats, nil
}

// GetAgentStats retrieves the statistics for the specified agent, including completed task count, total token consumption,
// average completion time, and other aggregated metrics.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//
// Returns:
//   - *store.AgentStats: agent statistics (task counts, token consumption, etc.)
//   - error: possible errors (database query failure)
func (s *StatsService) GetAgentStats(ctx context.Context, agentID uuid.UUID) (*store.AgentStats, error) {
	stats, err := s.svc.Store.GetAgentStats(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent stats: %w", err)
	}
	return stats, nil
}
