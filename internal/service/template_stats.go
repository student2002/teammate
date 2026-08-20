// template_stats.go 提供工作流模板统计的业务逻辑。
// 统计数据包括使用次数、平均完成时间和拒绝率。
package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// TemplateStatsService 提供工作流模板统计相关的业务逻辑。
// 统计数据包括使用次数、平均完成时间和拒绝率。
type TemplateStatsService struct {
	svc *Service
}

// NewTemplateStatsService 创建一个新的 TemplateStatsService 实例。
func NewTemplateStatsService(svc *Service) *TemplateStatsService {
	return &TemplateStatsService{svc: svc}
}

// TemplateStatsResult 保存工作流模板的统计数据。
type TemplateStatsResult struct {
	UsageCount           int64   `json:"usage_count"`            // 使用次数
	AvgCompletionSeconds float64 `json:"avg_completion_seconds"` // 平均完成时间（秒）
	RejectRate           float64 `json:"reject_rate"`            // 拒绝率（0-1）
}

// GetStats 获取指定工作流模板的统计数据，包括使用次数、平均完成时间和拒绝率。
//
// 步骤：
//  1. 根据模板 ID 获取模板信息（获取模板名称）
//  2. 使用模板名称查询统计数据（GetTemplateStats 按 workflow_name 查询）
//  3. 处理类型断言（avgCompletionSeconds 和 rejectRate 可能是 float64 或 int64）
//  4. 返回结构化的统计数据
//
// 参数：
//   - ctx: 请求上下文
//   - id: 工作流模板 ID
//
// 返回：
//   - *TemplateStatsResult: 模板统计数据
//   - error: 可能的错误（模板不存在、数据库查询失败）
func (s *TemplateStatsService) GetStats(ctx context.Context, id uuid.UUID) (*TemplateStatsResult, error) {
	template, err := s.svc.Store.GetWorkflowTemplate(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("template not found")
		}
		return nil, fmt.Errorf("get workflow template: %w", err)
	}

	stats, err := s.svc.Store.GetTemplateStats(ctx, template.Name)
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
