// template_stats.go 提供工作流模板统计的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/teammate/server/internal/types"
)

// GetTemplateStats 获取指定模板名称的统计数据。
func (s *Store) GetTemplateStats(ctx context.Context, name string) (types.GetTemplateStatsRow, error) {
	row, err := s.q.GetTemplateStats(ctx, name)
	if err != nil {
		return types.GetTemplateStatsRow{}, fmt.Errorf("get template stats: %w", err)
	}
	return ToDomainGetTemplateStatsRow(row), nil
}
