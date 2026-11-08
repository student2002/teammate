// workflow_event.go provides data access for the durable external events
// (plan §7.5) and the flow.wait(github_event) waiter registrations (plan
// §7.10), both transactional (engine-side) and standalone (scheduler-side).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// ErrEventNotClaimable marks a claim of a pending event that another scan
// already claimed.
var ErrEventNotClaimable = errors.New("workflow event is not claimable")

// ToDomainWorkflowEvent converts one sqlc event row to the domain type.
func ToDomainWorkflowEvent(row db.WorkflowEvent) types.WorkflowEvent {
	event := types.WorkflowEvent{
		ID:            row.ID.String(),
		Provider:      row.Provider,
		IntegrationID: row.IntegrationID.String(),
		ExternalID:    row.ExternalID,
		EventType:     row.EventType,
		Action:        row.Action,
		Payload:       row.Payload,
		Status:        row.Status,
		Attempts:      int(row.Attempts),
		Error:         row.Error,
		ReceivedAt:    row.ReceivedAt,
	}
	if row.ProcessedAt.Valid {
		event.ProcessedAt = &row.ProcessedAt.Time
	}
	return event
}

// CreateWorkflowEvent inserts one delivery; a duplicate (provider,
// integration_id, external_id) row inserts nothing and returns false.
func (s *Store) CreateWorkflowEvent(ctx context.Context, event types.WorkflowEvent) (bool, error) {
	integrationID, err := uuid.Parse(event.IntegrationID)
	if err != nil {
		return false, fmt.Errorf("convert integration id: %w", err)
	}
	rows, err := s.q.CreateWorkflowEvent(ctx, db.CreateWorkflowEventParams{
		Provider:      event.Provider,
		IntegrationID: integrationID,
		ExternalID:    event.ExternalID,
		EventType:     event.EventType,
		Action:        event.Action,
		Payload:       event.Payload,
	})
	if err != nil {
		return false, fmt.Errorf("create workflow event: %w", err)
	}
	return rows == 1, nil
}

// ListPendingWorkflowEvents returns the oldest pending events.
func (s *Store) ListPendingWorkflowEvents(ctx context.Context, limit int32) ([]types.WorkflowEvent, error) {
	rows, err := s.q.ListPendingWorkflowEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending workflow events: %w", err)
	}
	out := make([]types.WorkflowEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, ToDomainWorkflowEvent(row))
	}
	return out, nil
}

// MarkWorkflowEventProcessing claims one pending event for processing and
// bumps its attempt counter; a row already claimed returns not-found.
func (s *Store) MarkWorkflowEventProcessing(ctx context.Context, id uuid.UUID) (types.WorkflowEvent, error) {
	row, err := s.q.MarkWorkflowEventProcessing(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.WorkflowEvent{}, ErrEventNotClaimable
		}
		return types.WorkflowEvent{}, fmt.Errorf("mark workflow event processing: %w", err)
	}
	return ToDomainWorkflowEvent(row), nil
}

// CompleteWorkflowEvent closes one event as processed.
func (s *Store) CompleteWorkflowEvent(ctx context.Context, id uuid.UUID) error {
	if err := s.q.CompleteWorkflowEvent(ctx, id); err != nil {
		return fmt.Errorf("complete workflow event: %w", err)
	}
	return nil
}

// FailWorkflowEvent closes one event as failed with its error detail.
func (s *Store) FailWorkflowEvent(ctx context.Context, id uuid.UUID, message string) error {
	if err := s.q.FailWorkflowEvent(ctx, db.FailWorkflowEventParams{ID: id, Error: message}); err != nil {
		return fmt.Errorf("fail workflow event: %w", err)
	}
	return nil
}

// CreateEventWaiter registers one flow.wait(github_event) waiter inside the
// engine transaction.
// ListStuckProcessingEvents lists events stuck in processing past the lease
// window; the service requeues them within the attempts budget.
func (s *Store) ListStuckProcessingEvents(ctx context.Context, limit int32) ([]types.WorkflowEvent, error) {
	rows, err := s.q.ListStuckProcessingEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list stuck processing events: %w", err)
	}
	out := make([]types.WorkflowEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, ToDomainWorkflowEvent(row))
	}
	return out, nil
}

// RequeueWorkflowEvent returns a stuck processing event to the pending queue.
func (s *Store) RequeueWorkflowEvent(ctx context.Context, id string) error {
	eventID, err := stringToUUID(id)
	if err != nil {
		return fmt.Errorf("convert event id: %w", err)
	}
	if err := s.q.RequeueWorkflowEvent(ctx, eventID); err != nil {
		return fmt.Errorf("requeue workflow event: %w", err)
	}
	return nil
}

// ListLeaseExpiredActionNodes lists server-executed nodes whose claim lease
// expired (executor died before a terminal state).
func (s *Store) ListLeaseExpiredActionNodes(ctx context.Context, limit int32) ([]types.TaskNode, error) {
	rows, err := s.q.ListLeaseExpiredActionNodes(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list lease expired action nodes: %w", err)
	}
	out, err := ToDomainTaskNodeSlice(rows)
	if err != nil {
		return nil, fmt.Errorf("convert lease expired action nodes: %w", err)
	}
	return out, nil
}

// GetRunningAttemptNoByNode returns the attempt_no of a node's running
// attempt, or false when the node has none.
func (t *Tx) GetRunningAttemptNoByNode(ctx context.Context, nodeID string) (int32, bool, error) {
	id, err := stringToUUID(nodeID)
	if err != nil {
		return 0, false, fmt.Errorf("convert node id: %w", err)
	}
	no, err := t.q.GetRunningAttemptNoByNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("get running attempt: %w", err)
	}
	return no, true, nil
}

// GetRunningAttemptNoByNode returns the attempt_no of a node's running
// attempt outside a transaction, or false when the node has none.
func (s *Store) GetRunningAttemptNoByNode(ctx context.Context, nodeID string) (int32, bool, error) {
	id, err := stringToUUID(nodeID)
	if err != nil {
		return 0, false, fmt.Errorf("convert node id: %w", err)
	}
	no, err := s.q.GetRunningAttemptNoByNode(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("get running attempt: %w", err)
	}
	return no, true, nil
}

// ResolveWaitersByTask resolves the live event waiters of one task (terminal
// cleanup: late matching events must not resurrect a finished run).
func (t *Tx) ResolveWaitersByTask(ctx context.Context, taskID int32) error {
	if err := t.q.ResolveWaitersByTask(ctx, taskID); err != nil {
		return fmt.Errorf("resolve waiters by task: %w", err)
	}
	return nil
}

func (t *Tx) CreateEventWaiter(ctx context.Context, params types.CreateEventWaiterParams) (types.WorkflowEventWaiter, error) {
	integrationID, err := uuid.Parse(params.IntegrationID)
	if err != nil {
		return types.WorkflowEventWaiter{}, fmt.Errorf("convert integration id: %w", err)
	}
	nodeID, err := uuid.Parse(params.TaskNodeID)
	if err != nil {
		return types.WorkflowEventWaiter{}, fmt.Errorf("convert task node id: %w", err)
	}
	row, err := t.q.CreateEventWaiter(ctx, db.CreateEventWaiterParams{
		TaskID:        params.TaskID,
		TaskNodeID:    nodeID,
		IntegrationID: integrationID,
		EventType:     params.EventType,
		Filter:        params.Filter,
	})
	if err != nil {
		return types.WorkflowEventWaiter{}, fmt.Errorf("create event waiter: %w", err)
	}
	return ToDomainWorkflowEventWaiter(row), nil
}

// DeleteWaitingWaitersByNode removes the stale waiting registrations of one
// node before a fresh registration is written.
func (t *Tx) DeleteWaitingWaitersByNode(ctx context.Context, nodeID string) error {
	id, err := uuid.Parse(nodeID)
	if err != nil {
		return fmt.Errorf("convert task node id: %w", err)
	}
	if err := t.q.DeleteWaitingWaitersByNode(ctx, id); err != nil {
		return fmt.Errorf("delete waiting waiters: %w", err)
	}
	return nil
}

// ListWaitingEventWaiters returns the live waiters of one integration and
// event type (not expired, ordered by creation).
func (s *Store) ListWaitingEventWaiters(ctx context.Context, integrationID uuid.UUID, eventType string, now time.Time) ([]types.WorkflowEventWaiter, error) {
	rows, err := s.q.ListWaitingEventWaiters(ctx, db.ListWaitingEventWaitersParams{
		IntegrationID: integrationID,
		EventType:     eventType,
		ExpiresAt:     sql.NullTime{Time: now, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list waiting event waiters: %w", err)
	}
	out := make([]types.WorkflowEventWaiter, 0, len(rows))
	for _, row := range rows {
		out = append(out, ToDomainWorkflowEventWaiter(row))
	}
	return out, nil
}

// ResolveEventWaiter closes one waiter as resolved.
func (s *Store) ResolveEventWaiter(ctx context.Context, id uuid.UUID) error {
	if err := s.q.ResolveEventWaiter(ctx, id); err != nil {
		return fmt.Errorf("resolve event waiter: %w", err)
	}
	return nil
}

// ListGitHubTriggerNodesByIntegration loads every trigger.github template
// node bound to one integration with its template identity.
func (s *Store) ListGitHubTriggerNodesByIntegration(ctx context.Context, integrationID string) ([]types.GitHubTriggerNode, error) {
	rows, err := s.q.ListGitHubTriggerNodesByIntegration(ctx, integrationID)
	if err != nil {
		return nil, fmt.Errorf("list github trigger nodes: %w", err)
	}
	out := make([]types.GitHubTriggerNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, types.GitHubTriggerNode{
			NodeID:       row.NodeID.String(),
			Config:       row.Config,
			TemplateID:   row.TemplateID.String(),
			TemplateName: row.TemplateName,
			Enabled:      row.Enabled,
		})
	}
	return out, nil
}

// ListReadyGitHubActionNodes returns ready action.github nodes to execute.
func (s *Store) ListReadyGitHubActionNodes(ctx context.Context, limit int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListReadyGitHubActionNodes(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list ready github action nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// ToDomainWorkflowEventWaiter converts one sqlc waiter row to the domain type.
func ToDomainWorkflowEventWaiter(row db.WorkflowEventWaiter) types.WorkflowEventWaiter {
	waiter := types.WorkflowEventWaiter{
		ID:            row.ID.String(),
		TaskID:        row.TaskID,
		TaskNodeID:    row.TaskNodeID.String(),
		IntegrationID: row.IntegrationID.String(),
		EventType:     row.EventType,
		Filter:        row.Filter,
		Status:        row.Status,
		CreatedAt:     row.CreatedAt,
	}
	if row.ExpiresAt.Valid {
		waiter.ExpiresAt = &row.ExpiresAt.Time
	}
	if row.ResolvedAt.Valid {
		waiter.ResolvedAt = &row.ResolvedAt.Time
	}
	return waiter
}
