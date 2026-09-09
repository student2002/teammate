// stats.go provides query operations for project and agent statistics.
//
// Statistics are used for dashboard display, including task counts, node completion rate,
// average completion time, token usage, and other metrics.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ProjectStats encapsulates project statistics.
//
// It includes task counts grouped by status, node completion rate, and average completion time.
type ProjectStats struct {
	TaskCounts         TaskCountsByStatus `json:"task_counts"`                // task counts grouped by status
	NodeCompletionRate float64            `json:"node_completion_rate"`       // node completion rate (0-1)
	AvgTimeToComplete  *float64           `json:"avg_time_to_complete_hours"` // average completion time (hours)
}

// TaskCountsByStatus holds task counts grouped by status.
type TaskCountsByStatus struct {
	Active    int64 `json:"active"`    // active task count
	Completed int64 `json:"completed"` // completed task count
	Cancelled int64 `json:"cancelled"` // cancelled task count
}

// AgentStats encapsulates agent statistics.
//
// It includes completed task count, token usage (input/output), and average completion time.
type AgentStats struct {
	TotalCompletedTasks int32     `json:"total_completed_tasks"`     // total completed task count
	TotalTokens         int64     `json:"total_tokens"`              // total token usage
	InputTokens         int64     `json:"input_tokens"`              // input token usage
	OutputTokens        int64     `json:"output_tokens"`             // output token usage
	AvgCompletionTime   *float64  `json:"avg_completion_time_hours"` // average completion time (hours)
	ComputedAt          time.Time `json:"computed_at"`               // statistics computation time
}

// GetProjectStats queries project statistics (task counts, node completion rate, average completion time).
//
// It performs three steps of queries:
//  1. Count tasks grouped by status
//  2. Calculate the node completion rate (completed node count / total node count)
//  3. Calculate the average completion time of completed tasks
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - *ProjectStats: project statistics
//   - error: error returned when the query fails
func (s *Store) GetProjectStats(ctx context.Context, projectID uuid.UUID) (*ProjectStats, error) {
	var counts TaskCountsByStatus
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'active') AS active,
			COUNT(*) FILTER (WHERE status = 'completed') AS completed,
			COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled
		FROM tasks
		WHERE project_id = $1
	`, projectID).Scan(&counts.Active, &counts.Completed, &counts.Cancelled)
	if err != nil {
		return nil, fmt.Errorf("query task counts: %w", err)
	}

	var nodeCompletionRate float64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			CASE WHEN COUNT(*) = 0 THEN 0
			ELSE (COUNT(*) FILTER (WHERE status = 'completed'))::float / COUNT(*)::float
			END
		FROM task_nodes
		WHERE task_id IN (SELECT id FROM tasks WHERE project_id = $1)
	`, projectID).Scan(&nodeCompletionRate)
	if err != nil {
		return nil, fmt.Errorf("query node completion rate: %w", err)
	}

	var avgTime sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			EXTRACT(EPOCH FROM AVG(updated_at - created_at)) / 3600.0
		FROM tasks
		WHERE project_id = $1
		  AND status = 'completed'
	`, projectID).Scan(&avgTime)
	if err != nil {
		return nil, fmt.Errorf("query avg time: %w", err)
	}

	var avgTimePtr *float64
	if avgTime.Valid {
		avgTimePtr = &avgTime.Float64
	}

	return &ProjectStats{
		TaskCounts:         counts,
		NodeCompletionRate: nodeCompletionRate,
		AvgTimeToComplete:  avgTimePtr,
	}, nil
}

// GetAgentStats queries agent statistics (completed task count, token usage, average completion time).
//
// It performs two steps of queries:
//  1. Aggregate token usage in real time from the token_usage table, and count completed tasks from the task_nodes table
//  2. Join the task_nodes and tasks tables to calculate the average completion time
//
// Parameters:
//   - ctx: request context
//   - agentID: agent UUID
//
// Returns:
//   - *AgentStats: agent statistics
//   - error: error returned when the query fails
func (s *Store) GetAgentStats(ctx context.Context, agentID uuid.UUID) (*AgentStats, error) {
	var totalCompleted int32
	var inputTokens, outputTokens, totalTokens int64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT COUNT(*) FROM task_nodes WHERE assignee_id = $1 AND status = 'completed'), 0),
			COALESCE(SUM(tu.input_tokens), 0),
			COALESCE(SUM(tu.output_tokens), 0),
			COALESCE(SUM(tu.total_tokens), 0)
		FROM token_usage tu
		WHERE tu.agent_id = $1
	`, agentID).Scan(&totalCompleted, &inputTokens, &outputTokens, &totalTokens)
	if err != nil {
		return nil, fmt.Errorf("query agent stats: %w", err)
	}

	var avgTime sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			EXTRACT(EPOCH FROM AVG(t.updated_at - t.created_at)) / 3600.0
		FROM tasks t
		JOIN task_nodes tn ON tn.task_id = t.id
		WHERE tn.assignee_id = $1
		  AND t.status = 'completed'
	`, agentID).Scan(&avgTime)
	if err != nil {
		return nil, fmt.Errorf("query agent avg time: %w", err)
	}

	var avgTimePtr *float64
	if avgTime.Valid {
		avgTimePtr = &avgTime.Float64
	}

	return &AgentStats{
		TotalCompletedTasks: totalCompleted,
		TotalTokens:         totalTokens,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		AvgCompletionTime:   avgTimePtr,
		ComputedAt:          time.Now(),
	}, nil
}
