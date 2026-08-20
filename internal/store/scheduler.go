// scheduler.go 提供调度器所需的运行时状态查询（如标记过期运行时、离线回退节点）。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

type OfflineFallbackNode struct {
	ID     uuid.UUID
	TaskID int32
}

func (s *Store) MarkStaleRuntimes(ctx context.Context, cutoff time.Time) ([]types.Runtime, error) {
	rows, err := s.q.MarkStaleRuntimes(ctx, sql.NullTime{Time: cutoff, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("mark stale runtimes: %w", err)
	}
	return ToDomainRuntimeSlice(rows)
}

func (s *Store) UpdateOfflineAgents(ctx context.Context) ([]types.Agent, error) {
	rows, err := s.q.UpdateOfflineAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("update offline agents: %w", err)
	}
	return ToDomainAgentSlice(rows)
}

func (s *Store) ClearExpiredReservations(ctx context.Context, cutoff time.Time) ([]types.TaskNode, error) {
	rows, err := s.q.ClearExpiredReservations(ctx, sql.NullTime{Time: cutoff, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("clear expired reservations: %w", err)
	}
	return ToDomainTaskNodeSlice(rows)
}

func (s *Store) ReleaseClaimTimeoutNodes(ctx context.Context, cutoff time.Time) ([]types.TaskNode, error) {
	rows, err := s.q.ReleaseClaimTimeoutNodes(ctx, cutoff)
	if err != nil {
		return nil, fmt.Errorf("release claim timeout nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(rows)
}

func (s *Store) GetTimedOutNodes(ctx context.Context, now time.Time) ([]types.TaskNode, error) {
	rows, err := s.q.GetTimedOutNodes(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("get timed out nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(rows)
}

func (s *Store) MoveTimedOutNodeToManual(ctx context.Context, node types.TaskNode) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin timeout tx: %w", err)
	}
	defer tx.Rollback()

	nodeUUID, _ := stringToUUID(node.ID)
	qtx := s.q.WithTx(tx)
	if _, err := qtx.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
		TaskNodeID:   nodeUUID,
		FromStatus:   db.TaskNodeStatus(node.Status),
		ToStatus:     db.TaskNodeStatusManualIntervention,
		Action:       db.TransitionActionTimeout,
		OperatorType: "system",
	}); err != nil {
		return fmt.Errorf("create timeout transition: %w", err)
	}

	if _, err := qtx.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
		ID:                   nodeUUID,
		Status:               db.TaskNodeStatusManualIntervention,
		AssigneeType:         db.AssigneeTypeHuman,
		AssigneeID:           stringToNullUUID(node.AssigneeID),
		ReservedForAgentID:   uuid.NullUUID{},
		RejectCount:          int32(node.RejectCount),
		CompletedAt:          sql.NullTime{},
		CompletedBy:          uuid.NullUUID{},
		ReservationExpiresAt: sql.NullTime{},
		Version:              int32(node.Version),
		Status_2:             db.TaskNodeStatusInProgress,
	}); err != nil {
		return fmt.Errorf("set timed-out node to manual_intervention: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit timeout tx: %w", err)
	}
	return nil
}

func (s *Store) OfflineAgentFallback(ctx context.Context, cutoff time.Time) ([]OfflineFallbackNode, error) {
	const query = `
		UPDATE task_nodes
		SET status = 'manual_intervention',
		    assignee_type = 'human',
		    version = version + 1,
		    updated_at = now()
		WHERE status = 'in_progress'
		  AND assignee_type != 'human'
		  AND assignee_id IN (
		    SELECT a.id FROM agents a
		    WHERE a.status = 'offline'
		      AND a.updated_at < $1
		  )
		RETURNING id, task_id
	`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin offline fallback tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("offline agent fallback: %w", err)
	}
	defer rows.Close()

	var affected []OfflineFallbackNode
	for rows.Next() {
		var node OfflineFallbackNode
		if err := rows.Scan(&node.ID, &node.TaskID); err != nil {
			return nil, fmt.Errorf("scan offline fallback node: %w", err)
		}
		affected = append(affected, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate offline fallback nodes: %w", err)
	}

	qtx := s.q.WithTx(tx)
	for _, node := range affected {
		if _, err := qtx.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
			TaskNodeID:   node.ID,
			FromStatus:   db.TaskNodeStatusInProgress,
			ToStatus:     db.TaskNodeStatusManualIntervention,
			Action:       db.TransitionActionManual,
			Comment:      sql.NullString{String: "agent offline fallback", Valid: true},
			OperatorType: "system",
		}); err != nil {
			return nil, fmt.Errorf("create offline fallback transition: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit offline fallback tx: %w", err)
	}
	return affected, nil
}

func (s *Store) AutoRecoverIdleAgents(ctx context.Context) (int64, error) {
	const query = `
		UPDATE agents
		SET status = 'online', updated_at = now()
		WHERE status = 'busy'
		  AND id NOT IN (
		    SELECT DISTINCT assignee_id FROM task_nodes
		    WHERE status = 'in_progress' AND assignee_id IS NOT NULL
		  )
	`
	res, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("agent auto recover online: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read auto recover rows affected: %w", err)
	}
	return count, nil
}

func (s *Store) DeleteLowConfidenceMemories(ctx context.Context, cutoff time.Time) (int64, error) {
	const query = `
		DELETE FROM memories
		WHERE confidence < 0.1
		  AND verified = false
		  AND stale = false
		  AND created_at < $1
	`
	res, err := s.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("low-confidence memory gc: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read memory gc rows affected: %w", err)
	}
	return count, nil
}

// ListPendingRenotifyProjectIDs 查询存在超时未认领 pending 节点的项目 ID，
// 用于按项目粒度重新广播 node:pending（只通知项目成员 Agent）。
func (s *Store) ListPendingRenotifyProjectIDs(ctx context.Context, cutoff time.Time) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (t.project_id) t.project_id
		FROM task_nodes tn
		JOIN tasks t ON tn.task_id = t.id
		WHERE tn.status = 'pending'
		  AND tn.assignee_type != 'human'
		  AND tn.updated_at < $1
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("renotify pending nodes by project query: %w", err)
	}
	defer rows.Close()

	var projectIDs []uuid.UUID
	for rows.Next() {
		var projectID uuid.UUID
		if err := rows.Scan(&projectID); err != nil {
			return nil, fmt.Errorf("scan renotify project id: %w", err)
		}
		projectIDs = append(projectIDs, projectID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate renotify project ids: %w", err)
	}
	return projectIDs, nil
}

func (s *Store) GetCompletedTasksOlderThan(ctx context.Context, cutoff time.Time, lastID int32, limit int32) ([]types.GetCompletedTasksOlderThanRow, error) {
	rows, err := s.q.GetCompletedTasksOlderThan(ctx, db.GetCompletedTasksOlderThanParams{
		UpdatedAt: cutoff,
		ID:        lastID,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("get completed tasks older than: %w", err)
	}
	out := make([]types.GetCompletedTasksOlderThanRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.GetCompletedTasksOlderThanRow{
			ID:          r.ID,
			ProjectID:   r.ProjectID.String(),
			WorkspaceID: r.WorkspaceID.String(),
		})
	}
	return out, nil
}

func (s *Store) GetRuntimeByAgent(ctx context.Context, agentID uuid.UUID) (types.Runtime, error) {
	runtime, err := s.q.GetRuntimeByAgent(ctx, agentID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("get runtime by agent: %w", err)
	}
	return ToDomainRuntime(runtime)
}
