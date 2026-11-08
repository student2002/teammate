// scheduler.go provides the daemon/agent state queries required by the scheduler
// (e.g. marking stale daemons offline, recovering idle agents).
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// MarkStaleDaemons marks daemons whose heartbeat is stale as offline and returns
// their ids so the scheduler can bring their reported agents offline as well.
func (s *Store) MarkStaleDaemons(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := s.q.MarkStaleDaemons(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark stale daemons: %w", err)
	}
	return ids, nil
}

// SetAgentsStatusByDaemon sets the status of all agents reported by a daemon
// (used when a daemon goes offline or comes back online).
func (s *Store) SetAgentsStatusByDaemon(ctx context.Context, daemonID uuid.UUID, status string) error {
	_, err := s.q.SetAgentsStatusByDaemon(ctx, db.SetAgentsStatusByDaemonParams{
		DaemonID: uuid.NullUUID{UUID: daemonID, Valid: true},
		Status:   db.AgentStatus(status),
	})
	if err != nil {
		return fmt.Errorf("set agents status by daemon: %w", err)
	}
	return nil
}

func (s *Store) AutoRecoverIdleAgents(ctx context.Context) (int64, error) {
	const query = `
		UPDATE agents
		SET status = 'online', updated_at = now()
		WHERE status = 'busy'
		  AND id NOT IN (
		    SELECT DISTINCT reserved_for_agent_id FROM task_nodes
		    WHERE status = 'running' AND reserved_for_agent_id IS NOT NULL
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

// ListPendingRenotifyProjectIDs queries project IDs that have ready nodes unclaimed for a while,
// used to re-broadcast node:ready at the project granularity (only notify project member agents).
func (s *Store) ListPendingRenotifyProjectIDs(ctx context.Context, cutoff time.Time) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (t.project_id) t.project_id
		FROM task_nodes tn
		JOIN tasks t ON tn.task_id = t.id
		WHERE tn.status = 'ready'
		  AND t.status = 'active'
		  AND tn.updated_at < $1
	`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("renotify ready nodes by project query: %w", err)
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

// GetDaemonIDByAgent resolves the daemon that reports the agent
// (empty id and false when the agent is unmanaged).
func (s *Store) GetDaemonIDByAgent(ctx context.Context, agentID uuid.UUID) (uuid.UUID, bool, error) {
	id, err := s.q.GetDaemonIDByAgent(ctx, agentID)
	if err != nil {
		return uuid.Nil, false, nil
	}
	return id, true, nil
}

// GetOnlineDaemonIDByAgent resolves the daemon when it is online
// (false when the daemon is offline or the agent is unmanaged).
func (s *Store) GetOnlineDaemonIDByAgent(ctx context.Context, agentID uuid.UUID) (uuid.UUID, bool, error) {
	id, err := s.q.GetOnlineDaemonIDByAgent(ctx, agentID)
	if err != nil {
		return uuid.Nil, false, nil
	}
	return id, true, nil
}
