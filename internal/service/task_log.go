// task_log.go 实现任务日志的业务逻辑。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

type TaskLogService struct {
	svc *Service
}

type TaskLogRecord struct {
	TaskID    int32
	NodeID    string
	Type      string
	Content   string
	Timestamp time.Time
}

func NewTaskLogService(svc *Service) *TaskLogService {
	return &TaskLogService{svc: svc}
}

func (s *TaskLogService) Create(ctx context.Context, taskID int32, nodeID uuid.UUID, logType, content string, timestamp time.Time) error {
	if _, err := s.svc.Store.CreateTaskLog(ctx, types.CreateTaskLogParams{
		TaskID:    taskID,
		NodeID:    nodeID.String(),
		Type:      logType,
		Content:   content,
		Timestamp: timestamp,
	}); err != nil {
		return fmt.Errorf("create task log: %w", err)
	}
	return nil
}

func (s *TaskLogService) List(ctx context.Context, taskID int32, nodeID *uuid.UUID) ([]TaskLogRecord, error) {
	var (
		logs []types.TaskLog
		err  error
	)
	if nodeID != nil {
		logs, err = s.svc.Store.ListTaskLogsByTaskNode(ctx, taskID, *nodeID)
	} else {
		logs, err = s.svc.Store.ListTaskLogsByTask(ctx, taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("list task logs: %w", err)
	}

	records := make([]TaskLogRecord, 0, len(logs))
	for _, log := range logs {
		records = append(records, TaskLogRecord{
			TaskID:    log.TaskID,
			NodeID:    log.NodeID,
			Type:      log.Type,
			Content:   log.Content,
			Timestamp: log.Timestamp,
		})
	}
	return records, nil
}
