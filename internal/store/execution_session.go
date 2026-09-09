// execution_session.go provides data access operations for execution sessions.
//
// An Execution Session tracks the full lifecycle of an Agent executing a node task each time,
// including session creation, state updates, and completion/interruption handling.
//
// Each session is associated with one Runtime and one TaskNode, and records execution context
// such as the working directory, branch, commit hash, and Claude session ID.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateExecutionSession creates a new execution session record.
//
// Parameters:
//   - ctx: request context
//   - params: session creation parameters, including runtime ID, node ID, working directory, etc.
//
// Returns:
//   - types.ExecutionSession: the created session record
//   - error: returns an error if creation fails
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

// GetExecutionSession queries a single execution session record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: the session UUID
//
// Returns:
//   - types.ExecutionSession: the session record
//   - error: returns an error if the query fails
func (s *Store) GetExecutionSession(ctx context.Context, id uuid.UUID) (types.ExecutionSession, error) {
	session, err := s.q.GetExecutionSession(ctx, id)
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("get execution session: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// GetActiveSessionByAgentAndWorkdir queries the most recent completed session for the specified Agent and working directory.
//
// Used to restore the previous execution context when an Agent reconnects.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent UUID
//   - workdir: working directory path
//
// Returns:
//   - types.ExecutionSession: the session record
//   - error: returns an error if the query fails
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

// UpdateSessionClaudeID updates the Claude session ID of an execution session.
//
// The Claude session ID is used to restore the previous conversation context, avoiding re-injecting the full background.
//
// Parameters:
//   - ctx: request context
//   - id: the session UUID
//   - claudeSessionID: Claude session ID
//
// Returns:
//   - types.ExecutionSession: the updated session record
//   - error: returns an error if the update fails
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

// CompleteExecutionSession marks an execution session as completed and records the final commit hash.
//
// Parameters:
//   - ctx: request context
//   - id: the session UUID
//   - headCommit: the Git HEAD commit hash at completion
//
// Returns:
//   - types.ExecutionSession: the updated session record
//   - error: returns an error if the update fails
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

// InterruptExecutionSession marks an execution session as interrupted.
//
// Interruption is triggered by a task:interrupt event; after receiving it, the Agent executes SIGTERM->SIGKILL to stop the process.
//
// Parameters:
//   - ctx: request context
//   - id: the session UUID
//
// Returns:
//   - types.ExecutionSession: the updated session record
//   - error: returns an error if the update fails
func (s *Store) InterruptExecutionSession(ctx context.Context, id uuid.UUID) (types.ExecutionSession, error) {
	session, err := s.q.InterruptExecutionSession(ctx, id)
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("interrupt execution session: %w", err)
	}
	return ToDomainExecutionSession(session)
}

// GetLatestCompletedSessionByAgent queries the most recent completed session containing a Claude session ID for the specified Agent.
//
// Used to obtain a recoverable Claude session context.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent UUID
//
// Returns:
//   - types.ExecutionSession: the session record
//   - error: returns an error if the query fails
func (s *Store) GetLatestCompletedSessionByAgent(ctx context.Context, agentID uuid.UUID) (types.ExecutionSession, error) {
	session, err := s.q.GetLatestCompletedSessionByAgent(ctx, uuid.NullUUID{UUID: agentID, Valid: true})
	if err != nil {
		return types.ExecutionSession{}, fmt.Errorf("get latest completed session by agent: %w", err)
	}
	return ToDomainExecutionSession(session)
}
