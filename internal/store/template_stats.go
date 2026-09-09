// template_stats.go provides data access operations for workflow template statistics.
package store

import (
	"context"
	"fmt"

	"github.com/teammate/server/internal/types"
)

// GetTemplateStats fetches statistics for the specified template name.
func (s *Store) GetTemplateStats(ctx context.Context, name string) (types.GetTemplateStatsRow, error) {
	row, err := s.q.GetTemplateStats(ctx, name)
	if err != nil {
		return types.GetTemplateStatsRow{}, fmt.Errorf("get template stats: %w", err)
	}
	return ToDomainGetTemplateStatsRow(row), nil
}
