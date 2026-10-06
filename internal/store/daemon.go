// daemon.go — persistence and token management for agentd instances (daemons).
// A daemon is created from the web with a one-time td_ token; the machine-side
// agentd authenticates with it and reports machine info and its detected-tools
// snapshot. Agent instances are web-created and delivered via desired_agents.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateDaemon inserts a daemon row and issues its initial td_ API token.
// The token plaintext is returned only once, at creation time.
func (s *Store) CreateDaemon(ctx context.Context, workspaceID uuid.UUID, name string) (types.Daemon, string, error) {
	row, err := s.q.CreateDaemon(ctx, db.CreateDaemonParams{
		WorkspaceID: workspaceID,
		Name:        name,
	})
	if err != nil {
		return types.Daemon{}, "", fmt.Errorf("create daemon: %w", err)
	}

	token, err := s.issueOwnerToken(ctx, "daemon", "td", row.ID)
	if err != nil {
		return types.Daemon{}, "", fmt.Errorf("issue daemon token: %w", err)
	}
	return ToDomainDaemon(row), token, nil
}

// GetDaemon queries a daemon by ID.
func (s *Store) GetDaemon(ctx context.Context, id uuid.UUID) (types.Daemon, error) {
	row, err := s.q.GetDaemon(ctx, id)
	if err != nil {
		return types.Daemon{}, fmt.Errorf("get daemon: %w", err)
	}
	return ToDomainDaemon(row), nil
}

// ListDaemonsByWorkspace returns all daemons of a workspace.
func (s *Store) ListDaemonsByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.Daemon, error) {
	rows, err := s.q.ListDaemonsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list daemons: %w", err)
	}
	out := make([]types.Daemon, 0, len(rows))
	for _, row := range rows {
		out = append(out, ToDomainDaemon(row))
	}
	return out, nil
}

// DeleteDaemon removes a daemon, revokes its tokens, and marks its instances
// offline. Instance rows keep their daemon attribution; the daemon row is gone,
// so they are managed (deleted/recreated) from the web.
func (s *Store) DeleteDaemon(ctx context.Context, id uuid.UUID) error {
	daemon, err := s.q.GetDaemon(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("daemon not found: %w", err)
		}
		return fmt.Errorf("get daemon: %w", err)
	}
	if _, err := s.q.DetachAgentsFromDaemon(ctx, uuid.NullUUID{UUID: daemon.ID, Valid: true}); err != nil {
		return fmt.Errorf("detach agents from daemon: %w", err)
	}
	if err := s.revokeOwnerTokens(ctx, "daemon", daemon.ID); err != nil {
		return err
	}
	if _, err := s.q.DeleteDaemon(ctx, daemon.ID); err != nil {
		return fmt.Errorf("delete daemon: %w", err)
	}
	return nil
}

// RotateDaemonToken revokes all existing daemon tokens and issues a new one.
// The old token is invalidated immediately; the new plaintext is returned once.
func (s *Store) RotateDaemonToken(ctx context.Context, daemonID uuid.UUID) (string, error) {
	if _, err := s.q.GetDaemon(ctx, daemonID); err != nil {
		return "", fmt.Errorf("daemon not found: %w", err)
	}
	if err := s.revokeOwnerTokens(ctx, "daemon", daemonID); err != nil {
		return "", err
	}
	token, err := s.issueOwnerToken(ctx, "daemon", "td", daemonID)
	if err != nil {
		return "", fmt.Errorf("issue daemon token: %w", err)
	}
	return token, nil
}

// RegisterDaemonAgent manually binds an existing agent to a daemon and marks it
// online (tests/ops path; machine flows never create or bind agent rows).
func (s *Store) RegisterDaemonAgent(ctx context.Context, workspaceID, daemonID, agentID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE agents SET daemon_id = $1, status = 'online', updated_at = now()
		 WHERE id = $2 AND workspace_id = $3`,
		daemonID, agentID, workspaceID)
	if err != nil {
		return fmt.Errorf("register daemon agent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("register daemon agent: agent not found in workspace")
	}
	return nil
}

// SetDaemonOffline marks a daemon offline (deregister / shutdown path).
func (s *Store) SetDaemonOffline(ctx context.Context, daemonID uuid.UUID) error {
	if _, err := s.q.SetDaemonOffline(ctx, daemonID); err != nil {
		return fmt.Errorf("set daemon offline: %w", err)
	}
	return nil
}

// daemonNullUUID wraps a daemon id as sqlc's nullable parameter type.
func daemonNullUUID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: true}
}

// DaemonRegistrationUpdate carries the machine-reported registration fields.
type DaemonRegistrationUpdate = types.DaemonRegistrationUpdate

// UpdateDaemonRegistration refreshes machine info, the detected-tools snapshot,
// and the public key from a registration, marking the daemon online.
func (s *Store) UpdateDaemonRegistration(ctx context.Context, daemonID uuid.UUID, u DaemonRegistrationUpdate) (types.Daemon, error) {
	row, err := s.q.UpdateDaemonRegistration(ctx, db.UpdateDaemonRegistrationParams{
		ID:         daemonID,
		DeviceName: u.DeviceName,
		Version:    u.Version,
		Providers:  u.Providers,
		Column5:    u.PublicKey,
	})
	if err != nil {
		return types.Daemon{}, fmt.Errorf("update daemon registration: %w", err)
	}
	return ToDomainDaemon(row), nil
}

// TouchDaemonHeartbeat refreshes the daemon heartbeat and marks it online.
// Returns false when the daemon row no longer exists.
func (s *Store) TouchDaemonHeartbeat(ctx context.Context, daemonID uuid.UUID) (bool, error) {
	n, err := s.q.TouchDaemonHeartbeat(ctx, daemonID)
	if err != nil {
		return false, fmt.Errorf("touch daemon heartbeat: %w", err)
	}
	return n > 0, nil
}

// CreateDaemonAgent inserts a web-created instance (pending until adopted).
// personaKey is the cross-workspace memory identity (immutable after
// creation; the service layer guarantees it is non-empty); the git identity
// and instructions come from the operator-provided creation form.
func (s *Store) CreateDaemonAgent(ctx context.Context, workspaceID, daemonID uuid.UUID, name, provider, instructions, personaKey, gitName, gitEmail string) (types.Agent, error) {
	row, err := s.q.CreateDaemonAgent(ctx, db.CreateDaemonAgentParams{
		WorkspaceID:  workspaceID,
		Name:         name,
		Provider:     db.AgentProvider(provider),
		Instructions: instructions,
		DaemonID:     uuid.NullUUID{UUID: daemonID, Valid: true},
		PersonaKey:   personaKey,
		GitName:      sql.NullString{String: gitName, Valid: true},
		GitEmail:     sql.NullString{String: gitEmail, Valid: true},
	})
	if err != nil {
		return types.Agent{}, fmt.Errorf("create daemon agent: %w", err)
	}
	return ToDomainAgent(row)
}

// ListAgentsByDaemon returns the daemon's full instance set — the desired
// set of the declarative reconciliation.
func (s *Store) ListAgentsByDaemon(ctx context.Context, daemonID uuid.UUID) ([]types.Agent, error) {
	rows, err := s.q.ListAgentsByDaemon(ctx, daemonNullUUID(daemonID))
	if err != nil {
		return nil, fmt.Errorf("list agents by daemon: %w", err)
	}
	return ToDomainAgentSlice(rows)
}

// OfflineUnreportedDaemonAgents marks the daemon's online instances that are
// absent from the heartbeat busy list offline; pending instances keep waiting
// for delivery.
func (s *Store) OfflineUnreportedDaemonAgents(ctx context.Context, daemonID uuid.UUID, names []string) error {
	if names == nil {
		names = []string{}
	}
	if _, err := s.q.OfflineUnreportedDaemonAgents(ctx, db.OfflineUnreportedDaemonAgentsParams{
		DaemonID: daemonNullUUID(daemonID),
		Column2:  names,
	}); err != nil {
		return fmt.Errorf("offline unreported daemon agents: %w", err)
	}
	return nil
}

// SetDaemonAgentStatusByName updates one reported agent's status by (daemon, name)
// (heartbeat busy/online payload).
func (s *Store) SetDaemonAgentStatusByName(ctx context.Context, daemonID uuid.UUID, name, status string) error {
	if err := s.q.SetDaemonAgentStatusByName(ctx, db.SetDaemonAgentStatusByNameParams{
		DaemonID: daemonNullUUID(daemonID),
		Name:     name,
		Status:   db.AgentStatus(status),
	}); err != nil {
		return fmt.Errorf("set daemon agent status: %w", err)
	}
	return nil
}

// ListAgentsWithStats assembles the workspace agent list with per-agent aggregates:
// token usage sums, completed-node counts, current task, and daemon attribution.
// Four batch queries total; no N+1.
func (s *Store) ListAgentsWithStats(ctx context.Context, workspaceID uuid.UUID) ([]types.Agent, error) {
	agents, err := s.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return agents, nil
	}

	ids := make([]uuid.UUID, 0, len(agents))
	byID := make(map[uuid.UUID]*types.Agent, len(agents))
	for i := range agents {
		id, err := uuid.Parse(agents[i].ID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		byID[id] = &agents[i]
	}

	if usage, err := s.q.SumTokenUsageByAgents(ctx, ids); err == nil {
		for _, u := range usage {
			if a, ok := byID[u.AgentID]; ok {
				a.TotalInputTokens = u.InputTokens
				a.TotalOutputTokens = u.OutputTokens
				a.TotalTokens = u.InputTokens + u.OutputTokens
			}
		}
	}
	if counts, err := s.q.CountCompletedNodesByAgents(ctx, ids); err == nil {
		for _, c := range counts {
			if !c.AgentID.Valid {
				continue
			}
			if a, ok := byID[c.AgentID.UUID]; ok {
				a.TotalCompleted = int(c.CompletedNodes)
			}
		}
	}
	if current, err := s.q.GetCurrentTasksByAgents(ctx, ids); err == nil {
		for _, c := range current {
			if !c.AgentID.Valid {
				continue
			}
			if a, ok := byID[c.AgentID.UUID]; ok {
				a.CurrentTask = &types.AgentCurrentTask{
					TaskID:   c.TaskID,
					Title:    c.TaskTitle,
					NodeName: c.NodeName,
				}
			}
		}
	}

	// daemon attribution
	daemonNames := make(map[uuid.UUID]string)
	for i := range agents {
		if agents[i].DaemonID == "" {
			continue
		}
		daemonID, err := uuid.Parse(agents[i].DaemonID)
		if err != nil {
			continue
		}
		name, ok := daemonNames[daemonID]
		if !ok {
			if d, err := s.GetDaemon(ctx, daemonID); err == nil {
				name = d.Name
			}
			daemonNames[daemonID] = name
		}
		agents[i].DaemonName = name
	}
	return agents, nil
}

// ListAgentExecutions returns the agent's recent execution sessions with task context.
func (s *Store) ListAgentExecutions(ctx context.Context, agentID uuid.UUID, limit int32) ([]types.AgentExecution, error) {
	rows, err := s.q.ListAgentExecutions(ctx, db.ListAgentExecutionsParams{
		AgentID: uuid.NullUUID{UUID: agentID, Valid: true},
		Limit:   limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list agent executions: %w", err)
	}
	out := make([]types.AgentExecution, 0, len(rows))
	for _, r := range rows {
		e := types.AgentExecution{
			ID:        r.ID,
			TaskTitle: r.TaskTitle.String,
			NodeName:  r.NodeName.String,
			Status:    r.Status,
			Branch:    r.Branch.String,
			StartedAt: r.StartedAt,
		}
		if r.TaskID.Valid {
			e.TaskID = r.TaskID.Int32
		}
		if r.CompletedAt.Valid {
			t := r.CompletedAt.Time
			e.CompletedAt = &t
		}
		out = append(out, e)
	}
	return out, nil
}
