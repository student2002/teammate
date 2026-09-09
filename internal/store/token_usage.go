// token_usage.go provides data access operations for Token usage records.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// toInt64 safely converts the sqlc-generated interface{} (from COALESCE SUM) to int64.
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// CreateTokenUsage creates a Token usage record.
//
// Parameters:
//   - ctx: request context
//   - params: Token usage parameters (including task_node_id, agent_id, various token counts, cost estimate)
//
// Returns:
//   - types.TokenUsage: the created usage record
//   - error: error if creation fails
func (s *Store) CreateTokenUsage(ctx context.Context, params types.CreateTokenUsageParams) (types.TokenUsage, error) {
	dbParams, err := FromDomainCreateTokenUsageParams(params)
	if err != nil {
		return types.TokenUsage{}, fmt.Errorf("convert create token usage params: %w", err)
	}
	usage, err := s.q.CreateTokenUsage(ctx, dbParams)
	if err != nil {
		return types.TokenUsage{}, fmt.Errorf("create token usage: %w", err)
	}
	return ToDomainTokenUsage(usage)
}

// GetTokenUsageByTask fetches the Token usage summary for the specified task.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//
// Returns:
//   - types.GetTokenUsageByTaskRow: usage summary (input/output/total tokens + cost estimate)
//   - error: error if the query fails
func (s *Store) GetTokenUsageByTask(ctx context.Context, taskID int32) (types.GetTokenUsageByTaskRow, error) {
	usage, err := s.q.GetTokenUsageByTask(ctx, taskID)
	if err != nil {
		return types.GetTokenUsageByTaskRow{}, fmt.Errorf("get token usage by task: %w", err)
	}
	var cost *string
	if s, ok := usage.CostEstimate.(string); ok && s != "" {
		cost = &s
	}
	return types.GetTokenUsageByTaskRow{
		InputTokens:  toInt64(usage.InputTokens),
		OutputTokens: toInt64(usage.OutputTokens),
		TotalTokens:  toInt64(usage.TotalTokens),
		CostEstimate: cost,
	}, nil
}

// GetTokenUsageByAgent fetches the Token usage summary for a single Agent (aggregated in real time from the token_usage table).
//
// Parameters:
//   - ctx: request context
//   - agentID: Agent UUID
//
// Returns:
//   - types.GetTokenUsageByAgentRow: usage summary
//   - error: error if the query fails
func (s *Store) GetTokenUsageByAgent(ctx context.Context, agentID uuid.UUID) (types.GetTokenUsageByAgentRow, error) {
	row, err := s.q.GetTokenUsageByAgent(ctx, agentID)
	if err != nil {
		return types.GetTokenUsageByAgentRow{}, fmt.Errorf("get token usage by agent: %w", err)
	}
	return types.GetTokenUsageByAgentRow{
		InputTokens:  toInt64(row.InputTokens),
		OutputTokens: toInt64(row.OutputTokens),
		TotalTokens:  toInt64(row.TotalTokens),
	}, nil
}

// GetTokenUsageByAgents batch-fetches the Token usage summary for multiple Agents (a single query, grouped by agent_id).
//
// Parameters:
//   - ctx: request context
//   - agentIDs: list of Agent UUIDs
//
// Returns:
//   - map[uuid.UUID]types.GetTokenUsageByAgentsRow: usage summary grouped by Agent UUID
//   - error: error if the query fails
func (s *Store) GetTokenUsageByAgents(ctx context.Context, agentIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByAgentsRow, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	rows, err := s.q.GetTokenUsageByAgents(ctx, agentIDs)
	if err != nil {
		return nil, fmt.Errorf("get token usage by agents: %w", err)
	}
	result := make(map[uuid.UUID]types.GetTokenUsageByAgentsRow, len(rows))
	for _, r := range rows {
		result[r.AgentID] = types.GetTokenUsageByAgentsRow{
			AgentID:      r.AgentID.String(),
			InputTokens:  toInt64(r.InputTokens),
			OutputTokens: toInt64(r.OutputTokens),
			TotalTokens:  toInt64(r.TotalTokens),
		}
	}
	return result, nil
}

// GetTokenUsageByTaskNodes batch-fetches the Token usage summary for multiple nodes (a single query, grouped by task_node_id).
//
// Parameters:
//   - ctx: request context
//   - nodeIDs: list of node UUIDs
//
// Returns:
//   - map[uuid.UUID]types.GetTokenUsageByTaskNodesRow: usage summary grouped by node UUID
//   - error: error if the query fails
func (s *Store) GetTokenUsageByTaskNodes(ctx context.Context, nodeIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByTaskNodesRow, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	rows, err := s.q.GetTokenUsageByTaskNodes(ctx, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("get token usage by task nodes: %w", err)
	}
	result := make(map[uuid.UUID]types.GetTokenUsageByTaskNodesRow, len(rows))
	for _, r := range rows {
		result[r.TaskNodeID] = types.GetTokenUsageByTaskNodesRow{
			TaskNodeID:   r.TaskNodeID.String(),
			InputTokens:  toInt64(r.InputTokens),
			OutputTokens: toInt64(r.OutputTokens),
			TotalTokens:  toInt64(r.TotalTokens),
		}
	}
	return result, nil
}
