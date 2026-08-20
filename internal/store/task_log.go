// task_log.go 提供任务日志的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateTaskLog 创建一条任务日志记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 任务日志参数（含 task_id、node_id、type、content、timestamp）
//
// 返回：
//   - types.TaskLog: 创建的日志记录
//   - error: 创建失败时返回错误
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

// ListTaskLogsByTask 列出指定任务的所有日志记录。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//
// 返回：
//   - []types.TaskLog: 日志记录列表
//   - error: 查询失败时返回错误
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

// ListTaskLogsByTaskNode 列出指定任务和节点的日志记录。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//   - nodeID: 节点 UUID
//
// 返回：
//   - []types.TaskLog: 日志记录列表
//   - error: 查询失败时返回错误
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
