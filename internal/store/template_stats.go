// template_stats.go provides data access operations for workflow template statistics.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// GetTemplateStats fetches usage statistics aggregated over the tasks created
// from the specified template.
func (s *Store) GetTemplateStats(ctx context.Context, workflowTemplateID uuid.UUID) (types.GetTemplateStatsRow, error) {
	row, err := s.q.GetTemplateStats(ctx, uuid.NullUUID{UUID: workflowTemplateID, Valid: true})
	if err != nil {
		return types.GetTemplateStatsRow{}, fmt.Errorf("get template stats: %w", err)
	}
	return ToDomainGetTemplateStatsRow(row), nil
}
