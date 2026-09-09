// token_usage.go provides the business logic for token usage statistics.
// It records the token consumption of each execution for cost analysis and budget control.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// TokenUsageService provides the business logic for token usage statistics.
type TokenUsageService struct {
	svc *Service
}

// NewTokenUsageService creates a new TokenUsageService instance.
func NewTokenUsageService(svc *Service) *TokenUsageService {
	return &TokenUsageService{svc: svc}
}

// Create records a token usage entry.
func (s *TokenUsageService) Create(ctx context.Context, params types.CreateTokenUsageParams) (types.TokenUsage, error) {
	return s.svc.Store.CreateTokenUsage(ctx, params)
}

// GetByTask retrieves the token usage summary for the specified task.
func (s *TokenUsageService) GetByTask(ctx context.Context, taskID int32) (types.GetTokenUsageByTaskRow, error) {
	return s.svc.Store.GetTokenUsageByTask(ctx, taskID)
}

// GetByAgent retrieves the token usage summary for a single agent.
func (s *TokenUsageService) GetByAgent(ctx context.Context, agentID uuid.UUID) (types.GetTokenUsageByAgentRow, error) {
	return s.svc.Store.GetTokenUsageByAgent(ctx, agentID)
}

// GetByAgents retrieves the token usage summary for multiple agents in batch.
func (s *TokenUsageService) GetByAgents(ctx context.Context, agentIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByAgentsRow, error) {
	return s.svc.Store.GetTokenUsageByAgents(ctx, agentIDs)
}

// GetByTaskNodes retrieves the token usage summary for multiple nodes in batch.
func (s *TokenUsageService) GetByTaskNodes(ctx context.Context, nodeIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByTaskNodesRow, error) {
	return s.svc.Store.GetTokenUsageByTaskNodes(ctx, nodeIDs)
}

// Silently reference store to avoid an unused-import warning (GetByAgent etc. already pass through store return values, but this file may be standalone)
var _ = store.Store{}
