// runtime.go 提供 Agent 守护进程运行时（Runtime）的数据访问操作。
//
// Runtime 表示一个正在运行的 Agent 守护进程实例，通过心跳维持在线状态。
// 每个 Agent 可以有多个 Runtime（部署在不同机器上）。
//
// 本文件包含：
//   - Runtime CRUD 和心跳更新
//   - 全量同步数据收集（SyncRuntime）
//   - Runtime ID 查询（用于 SSE 事件广播）
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

// CreateRuntime 创建一条新的 Runtime 记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: Runtime 创建参数，包含 Agent ID、守护进程 ID、提供商等
//
// 返回：
//   - db.Runtime: 创建的 Runtime 记录
//   - error: 创建失败时返回错误
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

// UpdateRuntimeHeartbeat 更新 Runtime 的心跳时间戳。
//
// 守护进程每 30 秒发送一次心跳，Server 更新 last_heartbeat 字段。
// 超过 90 秒未收到心跳的 Runtime 被标记为 offline。
//
// 参数：
//   - ctx: 请求上下文
//   - id: Runtime 的 UUID
//
// 返回：
//   - db.Runtime: 更新后的 Runtime 记录
//   - error: 更新失败时返回错误
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

// ListRuntimes 查询所有 Runtime 记录。
//
// 参数：
//   - ctx: 请求上下文
//
// 返回：
//   - []db.Runtime: Runtime 列表
//   - error: 查询失败时返回错误
func (s *Store) ListRuntimes(ctx context.Context) ([]types.Runtime, error) {
	runtimes, err := s.q.ListRuntimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list runtimes: %w", err)
	}
	return ToDomainRuntimeSlice(runtimes)
}

// ListRuntimesByWorkspace 查询指定工作区内所有 Agent 的 Runtime 记录。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []db.Runtime: Runtime 列表
//   - error: 查询失败时返回错误
func (s *Store) ListRuntimesByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.Runtime, error) {
	runtimes, err := s.q.ListRuntimesByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list runtimes by workspace: %w", err)
	}
	return ToDomainRuntimeSlice(runtimes)
}

// SyncResult 封装守护进程全量同步所需的全部状态数据。
//
// 包含三类数据：可认领的待处理节点、Agent 参与的活跃任务、
// 最近提及 Agent 的评论。
type SyncResult struct {
	PendingNodes    []SyncNode    `json:"pending_nodes"`    // 可认领的待处理节点
	ActiveTasks     []SyncTask    `json:"active_tasks"`     // Agent 参与的活跃任务
	MentionComments []SyncComment `json:"mention_comments"` // 最近提及 Agent 的评论
}

// SyncNode 是同步响应中的轻量级节点表示。
//
// 仅包含节点的关键信息，减少同步数据量。
type SyncNode struct {
	ID           uuid.UUID `json:"id"`            // 节点 UUID
	TaskID       string    `json:"task_id"`       // 所属任务 ID
	ProjectID    string    `json:"project_id"`    // 所属项目 ID
	Name         string    `json:"name"`          // 节点名称
	NodeType     string    `json:"node_type"`     // 节点类型（standard/review/manual）
	Status       string    `json:"status"`        // 节点状态
	AssigneeType string    `json:"assignee_type"` // 分配者类型
	SortOrder    int32     `json:"sort_order"`    // 排序顺序
}

// SyncTask 是同步响应中的轻量级任务表示。
type SyncTask struct {
	ID        string `json:"id"`         // 任务 ID
	Title     string `json:"title"`      // 任务标题
	Status    string `json:"status"`     // 任务状态
	ProjectID string `json:"project_id"` // 所属项目 ID
}

// SyncComment 是同步响应中的轻量级评论表示（包含提及信息）。
type SyncComment struct {
	ID        uuid.UUID `json:"id"`         // 评论 UUID
	TaskID    string    `json:"task_id"`    // 所属任务 ID
	Content   string    `json:"content"`    // 评论内容
	AuthorID  uuid.UUID `json:"author_id"`  // 作者 ID
	CreatedAt string    `json:"created_at"` // 创建时间
}

// SyncRuntime 收集守护进程全量同步所需的全部状态。
//
// 查询三类数据：
//  1. 可认领的 pending 节点（Agent 是项目成员且节点未分配或已分配给该 Agent）
//  2. Agent 参与的活跃任务（节点分配给该 Agent 或已保留给该 Agent）
//  3. 最近 1 小时内提及该 Agent 的评论
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: Runtime 的 UUID
//
// 返回：
//   - *SyncResult: 同步数据
//   - error: 查询失败时返回错误
func (s *Store) SyncRuntime(ctx context.Context, runtimeID uuid.UUID) (*SyncResult, error) {
	// 获取 runtime 以找到 Agent ID。
	runtime, err := s.q.GetRuntime(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("get runtime: %w", err)
	}
	agentID := runtime.AgentID

	result := &SyncResult{}

	// 1. 待处理节点：Agent 可以认领的节点（要么是 any_agent，或
	//    为该 Agent 保留，且 Agent 是这些项目的成员）。
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

	// 2. Agent 参与的活动任务。
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

	// 3. 提及该 Agent 的近期评论。
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

// ListOnlineRuntimeIDsByAgent 查询指定 Agent 的所有在线 Runtime ID，用于定向 SSE 事件投递。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - []uuid.UUID: 在线 Runtime ID 列表
//   - error: 查询失败时返回错误
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

// ListRuntimeIDsByAgent 查询指定 Agent 的所有 Runtime ID（不论在线状态）。
// 用于控制事件的离线缓冲，确保 Agent 离线恢复后不丢失控制事件。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - []uuid.UUID: Runtime ID 列表
//   - error: 查询失败时返回错误
func (s *Store) ListRuntimeIDsByAgent(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.q.ListRuntimeIDsByAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list runtime ids by agent: %w", err)
	}
	return ids, nil
}

// MarshalSyncResult 将 SyncResult 序列化为 JSON。
//
// 用于 SSE 事件的 payload 序列化。
//
// 参数：
//   - r: 同步结果
//
// 返回：
//   - json.RawMessage: JSON 序列化结果
//   - error: 序列化失败时返回错误
func MarshalSyncResult(r *SyncResult) (json.RawMessage, error) {
	return json.Marshal(r)
}
