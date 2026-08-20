// node_interrupt.go 提供任务节点中断操作的数据访问。
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

type InterruptTarget struct {
	NodeID     uuid.UUID
	AssigneeID uuid.UUID
}

func (s *Store) InterruptInProgressNodes(ctx context.Context, taskID int32, operatorID uuid.UUID, operatorType, comment string) ([]InterruptTarget, int, error) {
	nodes, err := s.ListTaskNodes(ctx, taskID)
	if err != nil {
		return nil, 0, fmt.Errorf("list task nodes: %w", err)
	}

	targets := make([]InterruptTarget, 0)
	for _, node := range nodes {
		if node.Status == types.TaskNodeStatusInProgress && node.AssigneeID != nil {
			nodeID, _ := uuid.Parse(node.ID)
			assigneeID, _ := uuid.Parse(*node.AssigneeID)
			targets = append(targets, InterruptTarget{
				NodeID:     nodeID,
				AssigneeID: assigneeID,
			})
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)
	interruptedCount := 0
	for _, node := range nodes {
		if node.Status != types.TaskNodeStatusInProgress {
			continue
		}
		nodeID, _ := uuid.Parse(node.ID)
		var assigneeID uuid.NullUUID
		if node.AssigneeID != nil {
			if u, err := uuid.Parse(*node.AssigneeID); err == nil {
				assigneeID = uuid.NullUUID{UUID: u, Valid: true}
			}
		}
		if _, err := qtx.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
			ID:                   nodeID,
			Status:               db.TaskNodeStatusManualIntervention,
			AssigneeType:         db.AssigneeTypeHuman,
			AssigneeID:           assigneeID,
			ReservedForAgentID:   uuid.NullUUID{},
			RejectCount:          int32(node.RejectCount),
			CompletedAt:          sql.NullTime{},
			CompletedBy:          uuid.NullUUID{},
			ReservationExpiresAt: sql.NullTime{},
			Version:              int32(node.Version),
			Status_2:             db.TaskNodeStatusInProgress,
		}); err != nil {
			return nil, 0, fmt.Errorf("interrupt node %s: %w", node.ID, err)
		}

		if _, err := qtx.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
			TaskNodeID:   nodeID,
			FromStatus:   db.TaskNodeStatusInProgress,
			ToStatus:     db.TaskNodeStatusManualIntervention,
			Action:       db.TransitionActionManual,
			Comment:      sql.NullString{String: comment, Valid: comment != ""},
			OperatorID:   uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil},
			OperatorType: operatorType,
		}); err != nil {
			return nil, 0, fmt.Errorf("create transition for node %s: %w", node.ID, err)
		}
		interruptedCount++
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("commit tx: %w", err)
	}
	return targets, interruptedCount, nil
}
