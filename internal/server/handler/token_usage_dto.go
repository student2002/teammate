// token_usage_dto.go provides parameter builders for token_usage.go.
package handler

import (
	"database/sql"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ---- parameter builders ----

// buildCreateTokenUsageParams builds types.CreateTokenUsageParams from request fields.
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
	// keep the sql reference silent, reserved for subsequent buildXxxWithNullString pass-through
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
