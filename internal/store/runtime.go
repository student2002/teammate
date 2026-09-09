// runtime.go provides data access operations for the agent daemon runtime (Runtime).
//
// A Runtime represents a running agent daemon instance that stays online via heartbeats.
// Each agent can have multiple Runtimes (deployed on different machines).
//
// This file includes:
//   - Runtime CRUD and heartbeat updates
//   - Full sync data collection (SyncRuntime)
//   - Runtime ID queries (used for SSE event broadcasting)
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateRuntime creates a new Runtime record.
//
// Parameters:
//   - ctx: request context
//   - params: Runtime creation parameters, including agent ID, daemon ID, provider, etc.
//
// Returns:
//   - db.Runtime: the created Runtime record
//   - error: error returned when creation fails
func (s *Store) CreateRuntime(ctx context.Context, params types.CreateRuntimeParams) (types.Runtime, error) {
	dbParams, err := FromDomainCreateRuntimeParams(params)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("convert create runtime params: %w", err)
	}
	runtime, err := s.q.CreateRuntime(ctx, dbParams)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("create runtime: %w", err)
	}
	return ToDomainRuntime(runtime)
}

// UpdateRuntimeHeartbeat updates the heartbeat timestamp of a Runtime.
//
// The daemon sends a heartbeat every 30 seconds, and the Server updates the last_heartbeat field.
// A Runtime that has not received a heartbeat for more than 90 seconds is marked as offline.
//
// Parameters:
//   - ctx: request context
//   - id: Runtime UUID
//
// Returns:
//   - db.Runtime: the updated Runtime record
//   - error: error returned when the update fails
func (s *Store) UpdateRuntimeHeartbeat(ctx context.Context, id uuid.UUID) (types.Runtime, error) {
	runtime, err := s.q.UpdateRuntimeHeartbeat(ctx, db.UpdateRuntimeHeartbeatParams{
		ID:            id,
		LastHeartbeat: sql.NullTime{Time: s.Clock.Now(), Valid: true},
	})
	if err != nil {
		return types.Runtime{}, fmt.Errorf("update runtime heartbeat: %w", err)
	}
	return ToDomainRuntime(runtime)
}

// ListRuntimes queries all Runtime records.
//
// Parameters:
//   - ctx: request context
//
// Returns:
//   - []db.Runtime: Runtime list
//   - error: error returned when the query fails
func (s *Store) ListRuntimes(ctx context.Context) ([]types.Runtime, error) {
	runtimes, err := s.q.ListRuntimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtimes: %w", err)
	}
	return ToDomainRuntimeSlice(runtimes)
}

// ListRuntimesByWorkspace queries the Runtime records of all agents within the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - []db.Runtime: Runtime list
//   - error: error returned when the query fails
func (s *Store) ListRuntimesByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.Runtime, error) {
	runtimes, err := s.q.ListRuntimesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list runtimes by workspace: %w", err)
	}
	return ToDomainRuntimeSlice(runtimes)
}

// SyncResult encapsulates all state data needed for a full daemon sync.
//
// It contains three categories of data: pending nodes that can be claimed,
// active tasks the agent participates in, and recent comments that mention the agent.
type SyncResult struct {
	PendingNodes    []SyncNode    `json:"pending_nodes"`    // pending nodes that can be claimed
	ActiveTasks     []SyncTask    `json:"active_tasks"`     // active tasks the agent participates in
	MentionComments []SyncComment `json:"mention_comments"` // recent comments that mention the agent
}

// SyncNode is a lightweight node representation in the sync response.
//
// It contains only key node information to reduce the amount of sync data.
type SyncNode struct {
	ID           uuid.UUID `json:"id"`            // node UUID
	TaskID       string    `json:"task_id"`       // owning task ID
	ProjectID    string    `json:"project_id"`    // owning project ID
	Name         string    `json:"name"`          // node name
	NodeType     string    `json:"node_type"`     // node type (standard/review/manual)
	Status       string    `json:"status"`        // node status
	AssigneeType string    `json:"assignee_type"` // assignee type
	SortOrder    int32     `json:"sort_order"`    // sort order
}

// SyncTask is a lightweight task representation in the sync response.
type SyncTask struct {
	ID        string `json:"id"`         // task ID
	Title     string `json:"title"`      // task title
	Status    string `json:"status"`     // task status
	ProjectID string `json:"project_id"` // owning project ID
}

// SyncComment is a lightweight comment representation in the sync response (including mention information).
type SyncComment struct {
	ID        uuid.UUID `json:"id"`         // comment UUID
	TaskID    string    `json:"task_id"`    // owning task ID
	Content   string    `json:"content"`    // comment content
	AuthorID  uuid.UUID `json:"author_id"`  // author ID
	CreatedAt string    `json:"created_at"` // creation time
}

// SyncRuntime collects all state needed for a full daemon sync.
//
// It queries three categories of data:
//  1. Pending nodes that can be claimed (the agent is a project member and the node is unassigned or assigned to the agent)
//  2. Active tasks the agent participates in (nodes assigned to the agent or reserved for the agent)
//  3. Comments mentioning the agent within the last 1 hour
//
// Parameters:
//   - ctx: request context
//   - runtimeID: Runtime UUID
//
// Returns:
//   - *SyncResult: sync data
//   - error: error returned when the query fails
func (s *Store) SyncRuntime(ctx context.Context, runtimeID uuid.UUID) (*SyncResult, error) {
	// Get the runtime to find the agent ID.
	runtime, err := s.q.GetRuntime(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("get runtime: %w", err)
	}
	agentID := runtime.AgentID

	result := &SyncResult{}

	// 1. Pending nodes: nodes the agent can claim (either any_agent,
	//    or reserved for the agent, and the agent is a member of these projects).
	rows, err := s.db.QueryContext(ctx, `
		SELECT tn.id, tn.task_id, t.project_id, tn.name, tn.node_type, tn.status, tn.assignee_type, tn.sort_order
		FROM task_nodes tn
		JOIN tasks t ON tn.task_id = t.id
		JOIN project_members pm ON t.project_id = pm.project_id AND pm.member_type = 'agent' AND pm.agent_id = $1
		WHERE tn.status = 'pending'
		  AND (tn.assignee_type = 'any_agent' OR tn.reserved_for_agent_id = $1)
		  AND t.status = 'active'
		ORDER BY tn.updated_at DESC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("query pending nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n SyncNode
		if err := rows.Scan(&n.ID, &n.TaskID, &n.ProjectID, &n.Name, &n.NodeType, &n.Status, &n.AssigneeType, &n.SortOrder); err != nil {
			return nil, fmt.Errorf("scan pending node: %w", err)
		}
		result.PendingNodes = append(result.PendingNodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending nodes: %w", err)
	}

	// 2. Active tasks the agent participates in.
	taskRows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.title, t.status, t.project_id
		FROM tasks t
		JOIN task_nodes tn ON tn.task_id = t.id
		JOIN project_members pm ON t.project_id = pm.project_id AND pm.member_type = 'agent' AND pm.agent_id = $1
		WHERE t.status = 'active'
		  AND (tn.assignee_id = $1 OR tn.reserved_for_agent_id = $1)
		GROUP BY t.id, t.title, t.status, t.project_id, t.updated_at
		ORDER BY t.updated_at DESC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("query active tasks: %w", err)
	}
	defer taskRows.Close()
	for taskRows.Next() {
		var t SyncTask
		if err := taskRows.Scan(&t.ID, &t.Title, &t.Status, &t.ProjectID); err != nil {
			return nil, fmt.Errorf("scan active task: %w", err)
		}
		result.ActiveTasks = append(result.ActiveTasks, t)
	}
	if err := taskRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active tasks: %w", err)
	}

	// 3. Recent comments mentioning the agent.
	commentRows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.task_id, c.content, c.author_id, c.created_at
		FROM comments c
		WHERE $1::uuid = ANY(c.mentions)
		  AND c.created_at > NOW() - INTERVAL '1 hour'
		ORDER BY c.created_at DESC
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("query mention comments: %w", err)
	}
	defer commentRows.Close()
	for commentRows.Next() {
		var c SyncComment
		if err := commentRows.Scan(&c.ID, &c.TaskID, &c.Content, &c.AuthorID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan mention comment: %w", err)
		}
		result.MentionComments = append(result.MentionComments, c)
	}
	if err := commentRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mention comments: %w", err)
	}

	return result, nil
}

// ListOnlineRuntimeIDsByAgent queries all online Runtime IDs for the specified agent, used for targeted SSE event delivery.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent UUID
//
// Returns:
//   - []uuid.UUID: list of online Runtime IDs
//   - error: error returned when the query fails
func (s *Store) ListOnlineRuntimeIDsByAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id FROM runtimes
		WHERE agent_id = $1 AND status = 'online'
	`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list online runtime ids by agent: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan runtime id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime ids: %w", err)
	}
	return ids, nil
}

// ListRuntimeIDsByAgent queries all Runtime IDs for the specified agent (regardless of online status).
// Used for offline buffering of control events, ensuring the agent does not lose control events after recovering from being offline.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent UUID
//
// Returns:
//   - []uuid.UUID: Runtime ID list
//   - error: error returned when the query fails
func (s *Store) ListRuntimeIDsByAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.q.ListRuntimeIDsByAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list runtime ids by agent: %w", err)
	}
	return ids, nil
}

// MarshalSyncResult serializes a SyncResult to JSON.
//
// Used for payload serialization of SSE events.
//
// Parameters:
//   - r: sync result
//
// Returns:
//   - json.RawMessage: JSON serialization result
//   - error: error returned when serialization fails
func MarshalSyncResult(r *SyncResult) (json.RawMessage, error) {
	return json.Marshal(r)
}
