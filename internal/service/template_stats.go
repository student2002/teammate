// template_stats.go provides the business logic for workflow template statistics.
// Statistics include usage count, average completion time, and reject rate.
package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// TemplateStatsService provides the business logic for workflow template statistics.
// Statistics include usage count, average completion time, and reject rate.
type TemplateStatsService struct {
	svc *Service
}

// NewTemplateStatsService creates a new TemplateStatsService instance.
func NewTemplateStatsService(svc *Service) *TemplateStatsService {
	return &TemplateStatsService{svc: svc}
}

// TemplateStatsResult holds the statistics of a workflow template.
type TemplateStatsResult struct {
	UsageCount           int64   `json:"usage_count"`            // usage count
	AvgCompletionSeconds float64 `json:"avg_completion_seconds"` // average completion time (seconds)
	RejectRate           float64 `json:"reject_rate"`            // reject rate (0-1)
}

// GetStats retrieves the statistics for the specified workflow template, including usage count, average completion time, and reject rate.
//
// Steps:
//  1. Verify the template exists
//  2. Query statistics aggregated by template id (rename-proof)
//  3. Return the structured statistics
//
// Parameters:
//   - ctx: request context
//   - id: workflow template ID
//
// Returns:
//   - *TemplateStatsResult: template statistics
//   - error: possible errors (template not found, database query failure)
func (s *TemplateStatsService) GetStats(ctx context.Context, id uuid.UUID) (*TemplateStatsResult, error) {
	if _, err := s.svc.Store.GetWorkflowTemplate(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("template not found")
		}
		return nil, fmt.Errorf("get workflow template: %w", err)
	}

	stats, err := s.svc.Store.GetTemplateStats(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("template not found")
		}
		return nil, fmt.Errorf("get template stats: %w", err)
	}

	return &TemplateStatsResult{
		UsageCount:           stats.UsageCount,
		AvgCompletionSeconds: stats.AvgCompletionSeconds,
		RejectRate:           stats.RejectRate,
	}, nil
}
