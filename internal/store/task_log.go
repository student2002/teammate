// task_log.go provides data access operations for task logs.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateTaskLog creates a task log record.
//
// Parameters:
//   - ctx: request context
//   - params: task log parameters (including task_id, node_id, type, content, timestamp)
//
// Returns:
//   - types.TaskLog: the created log record
//   - error: error if creation fails
func (s *Store) CreateTaskLog(ctx context.Context, params types.CreateTaskLogParams) (types.TaskLog, error) {
	nodeID, err := stringToUUID(params.NodeID)
	if err != nil {
		return types.TaskLog{}, fmt.Errorf("convert node id: %w", err)
	}
	log, err := s.q.CreateTaskLog(ctx, db.CreateTaskLogParams{
		TaskID:    params.TaskID,
		NodeID:    nodeID,
		Type:      params.Type,
		Content:   params.Content,
		Timestamp: params.Timestamp,
	})
	if err != nil {
		return types.TaskLog{}, fmt.Errorf("create task log: %w", err)
	}
	return types.TaskLog{
		ID:        log.ID.String(),
		TaskID:    log.TaskID,
		NodeID:    log.NodeID.String(),
		Type:      log.Type,
		Content:   log.Content,
		Timestamp: log.Timestamp,
		CreatedAt: log.CreatedAt,
	}, nil
}

// ListTaskLogsByTask lists all log records of the specified task.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//
// Returns:
//   - []types.TaskLog: list of log records
//   - error: error if the query fails
func (s *Store) ListTaskLogsByTask(ctx context.Context, taskID int32) ([]types.TaskLog, error) {
	logs, err := s.q.ListTaskLogsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task logs by task: %w", err)
	}
	out := make([]types.TaskLog, 0, len(logs))
	for _, l := range logs {
		out = append(out, types.TaskLog{
			ID:        l.ID.String(),
			TaskID:    l.TaskID,
			NodeID:    l.NodeID.String(),
			Type:      l.Type,
			Content:   l.Content,
			Timestamp: l.Timestamp,
			CreatedAt: l.CreatedAt,
		})
	}
	return out, nil
}

// ListTaskLogsByTaskNode lists log records of the specified task and node.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//   - nodeID: node UUID
//
// Returns:
//   - []types.TaskLog: list of log records
//   - error: error if the query fails
func (s *Store) ListTaskLogsByTaskNode(ctx context.Context, taskID int32, nodeID uuid.UUID) ([]types.TaskLog, error) {
	logs, err := s.q.ListTaskLogsByTaskNode(ctx, db.ListTaskLogsByTaskNodeParams{
		TaskID: taskID,
		NodeID: nodeID,
	})
	if err != nil {
		return nil, fmt.Errorf("list task logs by task node: %w", err)
	}
	out := make([]types.TaskLog, 0, len(logs))
	for _, l := range logs {
		out = append(out, types.TaskLog{
			ID:        l.ID.String(),
			TaskID:    l.TaskID,
			NodeID:    l.NodeID.String(),
			Type:      l.Type,
			Content:   l.Content,
			Timestamp: l.Timestamp,
			CreatedAt: l.CreatedAt,
		})
	}
	return out, nil
}
