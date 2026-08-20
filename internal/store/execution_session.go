// execution_session.go 提供执行会话的数据访问操作。
//
// 执行会话（Execution Session）追踪 Agent 每次执行节点任务的完整生命周期，
// 包含会话创建、状态更新、完成/中断处理。
//
// 每个会话关联一个 Runtime、一个 TaskNode，记录工作目录、分支、
// 提交哈希、Claude 会话 ID 等执行上下文。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateExecutionSession 创建一条新的执行会话记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 会话创建参数，包含 runtime ID、node ID、工作目录等
//
// 返回：
//   - types.ExecutionSession: 创建的会话记录
//   - error: 创建失败时返回错误
func (s *Store) CreateExecutionSession(ctx context.Context, params types.CreateExecutionSessionParams) (types.ExecutionSession, error) {
	dbParams, err := FromDomainCreateExecutionSessionParams(params)
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("convert create execution session params: %w", err)
	}
	session, err := s.q.CreateExecutionSession(ctx, dbParams)
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("create execution session: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// GetExecutionSession 根据 ID 查询单条执行会话记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 会话的 UUID
//
// 返回：
//   - types.ExecutionSession: 会话记录
//   - error: 查询失败时返回错误
func (s *Store) GetExecutionSession(ctx context.Context, id uuid.UUID) (types.ExecutionSession, error) {
	session, err := s.q.GetExecutionSession(ctx, id)
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("get execution session: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// GetActiveSessionByAgentAndWorkdir 查询指定 Agent 和工作目录下最近的已完成会话。
//
// 用于 Agent 重连时恢复之前的执行上下文。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//   - workdir: 工作目录路径
//
// 返回：
//   - types.ExecutionSession: 会话记录
//   - error: 查询失败时返回错误
func (s *Store) GetActiveSessionByAgentAndWorkdir(ctx context.Context, agentID uuid.UUID, workdir string) (types.ExecutionSession, error) {
	session, err := s.q.GetActiveSessionByAgentAndWorkdir(ctx, db.GetActiveSessionByAgentAndWorkdirParams{
		AgentID: uuid.NullUUID{UUID: agentID, Valid: true},
		Workdir: ptrToNullString(&workdir),
	})
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("get active session by agent and workdir: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// UpdateSessionClaudeID 更新执行会话的 Claude 会话 ID。
//
// Claude 会话 ID 用于恢复之前的对话上下文，避免重复注入完整背景。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 会话的 UUID
//   - claudeSessionID: Claude 会话 ID
//
// 返回：
//   - types.ExecutionSession: 更新后的会话记录
//   - error: 更新失败时返回错误
func (s *Store) UpdateSessionClaudeID(ctx context.Context, id uuid.UUID, claudeSessionID string) (types.ExecutionSession, error) {
	session, err := s.q.UpdateSessionClaudeID(ctx, db.UpdateSessionClaudeIDParams{
		ID:              id,
		ClaudeSessionID: ptrToNullString(&claudeSessionID),
	})
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("update session claude id: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// CompleteExecutionSession 将执行会话标记为已完成，记录最终提交哈希。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 会话的 UUID
//   - headCommit: 完成时的 Git HEAD 提交哈希
//
// 返回：
//   - types.ExecutionSession: 更新后的会话记录
//   - error: 更新失败时返回错误
func (s *Store) CompleteExecutionSession(ctx context.Context, id uuid.UUID, headCommit string) (types.ExecutionSession, error) {
	session, err := s.q.CompleteExecutionSession(ctx, db.CompleteExecutionSessionParams{
		ID:         id,
		HeadCommit: ptrToNullString(&headCommit),
	})
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("complete execution session: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// InterruptExecutionSession 将执行会话标记为已中断。
//
// 中断由 task:interrupt 事件触发，Agent 收到后执行 SIGTERM→SIGKILL 停止进程。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 会话的 UUID
//
// 返回：
//   - types.ExecutionSession: 更新后的会话记录
//   - error: 更新失败时返回错误
func (s *Store) InterruptExecutionSession(ctx context.Context, id uuid.UUID) (types.ExecutionSession, error) {
	session, err := s.q.InterruptExecutionSession(ctx, id)
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("interrupt execution session: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// GetLatestCompletedSessionByAgent 查询指定 Agent 最近一次包含 Claude 会话 ID 的已完成会话。
//
// 用于获取可恢复的 Claude 会话上下文。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - types.ExecutionSession: 会话记录
//   - error: 查询失败时返回错误
func (s *Store) GetLatestCompletedSessionByAgent(ctx context.Context, agentID uuid.UUID) (types.ExecutionSession, error) {
	session, err := s.q.GetLatestCompletedSessionByAgent(ctx, uuid.NullUUID{UUID: agentID, Valid: true})
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("get latest completed session by agent: %w", err)
	}
	return ToDomainExecutionSession(session)
}
