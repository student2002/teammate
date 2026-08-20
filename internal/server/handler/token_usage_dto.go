// token_usage_dto.go 为 token_usage.go 提供参数构建器。
package handler

import (
	"database/sql"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ---- 参数构建器 ----

// buildCreateTokenUsageParams 从请求字段构建 types.CreateTokenUsageParams。
func buildCreateTokenUsageParams(
	taskNodeID uuid.UUID,
	agentID uuid.UUID,
	inputTokens int32,
	outputTokens int32,
	totalTokens int32,
	costEstimate string,
) types.CreateTokenUsageParams {
	var costPtr *string
	if costEstimate != "" {
		costPtr = &costEstimate
	}
	// 静默 sql 引用，保留以备后续 buildXxxWithNullString 透传用
	_ = sql.NullString{}
	return types.CreateTokenUsageParams{
		TaskNodeID:   taskNodeID.String(),
		AgentID:      agentID.String(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
		CostEstimate: costPtr,
	}
}
