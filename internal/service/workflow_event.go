// workflow_event.go implements the external event consumer of the V2 engine
// (plan §9.4): the scheduler claims pending workflow_events rows and for
// each delivery (a) starts every enabled template whose trigger.github node
// matches the integration/event/action and (b) wakes the
// flow.wait(github_event) nodes registered as waiters. It also hosts the
// waiter registration called from the engine's applyNodeReady and the
// WakeEventWaiter wake-up path.
//
// First-version limits, by design: failed events are not retried
// automatically, github_event waits carry no timeout, and the waiter filter
// field is stored but not evaluated.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// eventScanLimit bounds one scan of pending events.
const eventScanLimit int32 = 50

const (
	// eventLeaseWindow bounds how long one processing claim may hold an event
	// before a crashed worker's claim is reclaimed.
	eventLeaseWindow = 10 * time.Minute
	// maxEventAttempts bounds the requeues of one event; beyond it the event
	// fails with a processing timeout.
	maxEventAttempts = 3
)

// systemAuthorID is the task author recorded for event-triggered runs: the
// nil UUID marks a system-initiated run (no human/agent principal exists at
// webhook time).
var systemAuthorID = uuid.Nil

// WorkflowEventService consumes pending external events.
type WorkflowEventService struct {
	svc *Service
}

func NewWorkflowEventService(svc *Service) *WorkflowEventService {
	return &WorkflowEventService{svc: svc}
}

// RecordDelivery stores one webhook delivery durably; stored reports whether
// the row was inserted (false for a duplicate delivery id).
func (s *WorkflowEventService) RecordDelivery(ctx context.Context, event types.WorkflowEvent) (bool, error) {
	return s.svc.Store.CreateWorkflowEvent(ctx, event)
}

// ProcessPendingEvents claims and processes up to eventScanLimit pending
// events; it is the scheduler's entry point (task "workflow_events").
func (s *WorkflowEventService) ProcessPendingEvents(ctx context.Context) error {
	if err := s.ReclaimStuckEvents(ctx); err != nil {
		return err
	}
	events, err := s.svc.Store.ListPendingWorkflowEvents(ctx, eventScanLimit)
	if err != nil {
		return fmt.Errorf("list pending workflow events: %w", err)
	}
	for _, event := range events {
		if err := s.processEvent(ctx, event); err != nil {
			slog.Error("process workflow event", "event_id", event.ID, "event_type", event.EventType, "err", err)
		}
	}
	return nil
}

// processEvent handles one delivery: claim → start matching triggers → wake
// matching waiters → complete. Any step error fails the event with its detail.
func (s *WorkflowEventService) processEvent(ctx context.Context, event types.WorkflowEvent) error {
	eventID, err := uuid.Parse(event.ID)
	if err != nil {
		return fmt.Errorf("parse event id: %w", err)
	}
	// Claim (pending → processing, attempts++); another scan already took it.
	if _, err := s.svc.Store.MarkWorkflowEventProcessing(ctx, eventID); err != nil {
		if errors.Is(err, store.ErrEventNotClaimable) {
			return nil
		}
		return fmt.Errorf("claim workflow event: %w", err)
	}

	integrationID, err := uuid.Parse(event.IntegrationID)
	if err != nil {
		return s.failEvent(ctx, eventID, fmt.Errorf("parse integration id: %w", err))
	}
	integration, err := s.svc.Store.GetIntegration(ctx, integrationID)
	if err != nil {
		return s.failEvent(ctx, eventID, fmt.Errorf("get integration: %w", err))
	}

	started, err := s.startMatchingTemplates(ctx, event, integration)
	if err != nil {
		return s.failEvent(ctx, eventID, err)
	}
	woken, err := s.wakeMatchingWaiters(ctx, event, integration)
	if err != nil {
		return s.failEvent(ctx, eventID, err)
	}

	if err := s.svc.Store.CompleteWorkflowEvent(ctx, eventID); err != nil {
		return fmt.Errorf("complete workflow event: %w", err)
	}
	slog.Info("processed workflow event",
		"event_id", event.ID, "event_type", event.EventType, "action", event.Action,
		"started_runs", started, "woken_waiters", woken)
	return nil
}

// startMatchingTemplates starts one run per enabled template whose
// trigger.github node matches the delivery (integration, event, actions).
func (s *WorkflowEventService) startMatchingTemplates(ctx context.Context, event types.WorkflowEvent, integration types.Integration) (int, error) {
	nodes, err := s.svc.Store.ListGitHubTriggerNodesByIntegration(ctx, integration.ID)
	if err != nil {
		return 0, fmt.Errorf("list trigger nodes: %w", err)
	}
	projectID, err := uuid.Parse(integration.ProjectID)
	if err != nil {
		return 0, fmt.Errorf("parse integration project id: %w", err)
	}

	runSvc := NewWorkflowRunService(s.svc)
	started := 0
	seenTemplates := make(map[string]bool)
	for _, node := range nodes {
		if !node.Enabled || seenTemplates[node.TemplateID] {
			continue
		}
		if !triggerNodeMatches(node.Config, event.EventType, event.Action) {
			continue
		}
		templateID, err := uuid.Parse(node.TemplateID)
		if err != nil {
			return started, fmt.Errorf("parse template id: %w", err)
		}
		title := fmt.Sprintf("%s · %s/%s", node.TemplateName, event.EventType, event.Action)
		if _, err := runSvc.Start(ctx, templateID, projectID, event.Payload, title, systemAuthorID); err != nil {
			return started, fmt.Errorf("start run of template %s: %w", node.TemplateName, err)
		}
		seenTemplates[node.TemplateID] = true
		started++
	}
	return started, nil
}

// triggerNodeMatches reports whether one trigger.github node config selects
// the delivery: same event, and the action filter empty or containing the
// delivery's action.
func triggerNodeMatches(config json.RawMessage, eventType, action string) bool {
	var cfg struct {
		Event   string   `json:"event"`
		Actions []string `json:"actions"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return false
	}
	if cfg.Event != eventType {
		return false
	}
	if len(cfg.Actions) == 0 {
		return true
	}
	for _, allowed := range cfg.Actions {
		if allowed == action {
			return true
		}
	}
	return false
}

// wakeMatchingWaiters wakes every live waiter of the delivery's integration
// and event type, then resolves the waiter rows.
func (s *WorkflowEventService) wakeMatchingWaiters(ctx context.Context, event types.WorkflowEvent, integration types.Integration) (int, error) {
	integrationID, err := uuid.Parse(integration.ID)
	if err != nil {
		return 0, fmt.Errorf("parse integration id: %w", err)
	}
	waiters, err := s.svc.Store.ListWaitingEventWaiters(ctx, integrationID, event.EventType, s.svc.Store.Clock.Now())
	if err != nil {
		return 0, fmt.Errorf("list waiting waiters: %w", err)
	}
	runSvc := NewWorkflowRunService(s.svc)
	woken := 0
	for _, waiter := range waiters {
		nodeID, err := uuid.Parse(waiter.TaskNodeID)
		if err != nil {
			return woken, fmt.Errorf("parse waiter node id: %w", err)
		}
		if _, err := runSvc.WakeEventWaiter(ctx, nodeID, event.Payload); err != nil {
			return woken, fmt.Errorf("wake waiter node %s: %w", waiter.TaskNodeID, err)
		}
		waiterID, err := uuid.Parse(waiter.ID)
		if err != nil {
			return woken, fmt.Errorf("parse waiter id: %w", err)
		}
		if err := s.svc.Store.ResolveEventWaiter(ctx, waiterID); err != nil {
			return woken, fmt.Errorf("resolve waiter %s: %w", waiter.ID, err)
		}
		woken++
	}
	return woken, nil
}

// failEvent marks an event failed with its error detail; the failure is not
// retried in this stage.
func (s *WorkflowEventService) failEvent(ctx context.Context, eventID uuid.UUID, cause error) error {
	if err := s.svc.Store.FailWorkflowEvent(ctx, eventID, cause.Error()); err != nil {
		return fmt.Errorf("fail workflow event: %w (cause: %v)", err, cause)
	}
	return cause
}

// ---------------------------------------------------------------------------
// flow.wait(github_event) waiter registration and wake-up
// ---------------------------------------------------------------------------

// createEventWaiterTx registers the github_event waiter of one flow.wait node
// inside the engine transaction (plan §9.4). The integration comes from
// config.integration_id; a wait config without it falls back to the project's
// github integration. First version stores an empty filter (no filtering) and
// no expiry (github_event waits do not time out in this stage).
func (s *WorkflowRunService) createEventWaiterTx(ctx context.Context, tx *store.Tx, n *types.TaskNode) error {
	var cfg struct {
		Event         string `json:"event"`
		IntegrationID string `json:"integration_id"`
	}
	if err := json.Unmarshal(n.Config, &cfg); err != nil {
		return fmt.Errorf("decode flow.wait config: %w", err)
	}
	integrationID := cfg.IntegrationID
	if integrationID == "" {
		resolved, err := s.resolveProjectIntegrationTx(ctx, tx, n.TaskID)
		if err != nil {
			return fmt.Errorf("resolve project github integration: %w", err)
		}
		integrationID = resolved
	}
	if _, err := uuid.Parse(integrationID); err != nil {
		return fmt.Errorf("wait node %s has no resolvable github integration: %w", n.ID, err)
	}
	// Re-activation replaces stale waiting registrations.
	if err := tx.DeleteWaitingWaitersByNode(ctx, n.ID); err != nil {
		return err
	}
	_, err := tx.CreateEventWaiter(ctx, types.CreateEventWaiterParams{
		TaskID:        n.TaskID,
		TaskNodeID:    n.ID,
		IntegrationID: integrationID,
		EventType:     cfg.Event,
		Filter:        json.RawMessage("{}"),
	})
	return err
}

// resolveProjectIntegrationTx finds the task project's github integration.
func (s *WorkflowRunService) resolveProjectIntegrationTx(ctx context.Context, tx *store.Tx, taskID int32) (string, error) {
	task, err := tx.GetTask(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("get task: %w", err)
	}
	projectID, err := uuid.Parse(task.ProjectID)
	if err != nil {
		return "", fmt.Errorf("parse project id: %w", err)
	}
	integration, err := s.svc.Store.GetProjectIntegration(ctx, projectID, "github")
	if err != nil {
		return "", fmt.Errorf("get project github integration: %w", err)
	}
	return integration.ID, nil
}

// WakeEventWaiter completes one flow.wait(github_event) node with the
// normalized event payload and advances its task (plan §9.4: output=事件
// payload, port resumed). A node already woken by another instance is skipped
// (idempotent). woken reports whether this call performed the transition.
func (s *WorkflowRunService) WakeEventWaiter(ctx context.Context, nodeID uuid.UUID, payload json.RawMessage) (woken bool, err error) {
	var (
		st     advanceState
		taskID int32
	)
	err = s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if node.Status != types.TaskNodeStatusWaiting || !strings.HasPrefix(node.WaitKey, "event:") {
			return nil
		}
		// A cancelled/failed task must not be resurrected by a late matching
		// event; its live waiters were resolved at the terminal transition.
		task, err := tx.GetTask(ctx, node.TaskID)
		if err != nil {
			return err
		}
		if task.Status != types.TaskStatusActive {
			return nil
		}
		now := s.svc.Store.Clock.Now()
		node.Status = types.TaskNodeStatusSucceeded
		node.Output = waitEventOutput(payload)
		node.CompletedAt = &now
		node.WaitKey = ""
		node.WaitExpiresAt = nil
		node.AttemptCount++
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			if errors.Is(err, store.ErrNodeVersionConflict) {
				return nil
			}
			return err
		}
		if err := s.writeAttempt(ctx, tx, &node, node.AttemptCount, "succeeded", node.Input, node.Output, json.RawMessage("{}"), now); err != nil {
			return err
		}
		taskID = node.TaskID
		st, err = s.advanceTx(ctx, tx, node.TaskID)
		return err
	})
	if err != nil {
		return false, err
	}
	if taskID != 0 {
		s.publishReadyNodes(ctx, st.projectID, taskID, st.readyAgentd)
	}
	return taskID != 0, nil
}

// waitEventOutput merges the normalized payload into the wait node output and
// stamps outcome=resumed, mirroring the duration-wait output convention.
func waitEventOutput(payload json.RawMessage) json.RawMessage {
	out := map[string]any{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	out["outcome"] = "resumed"
	encoded, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage(`{"outcome":"resumed"}`)
	}
	return encoded
}

// ReclaimStuckEvents recovers events whose processing crashed mid-flight:
// within the attempts budget the claim is returned to the pending queue,
// beyond it the event fails with a processing timeout (P1-1a of the S5
// review — no delivery is silently lost).
func (s *WorkflowEventService) ReclaimStuckEvents(ctx context.Context) error {
	stuck, err := s.svc.Store.ListStuckProcessingEvents(ctx, eventScanLimit)
	if err != nil {
		return err
	}
	for _, event := range stuck {
		if event.Attempts >= maxEventAttempts {
			eventID, parseErr := uuid.Parse(event.ID)
			if parseErr != nil {
				return fmt.Errorf("parse event id: %w", parseErr)
			}
			if err := s.svc.Store.FailWorkflowEvent(ctx, eventID, "processing lease expired"); err != nil {
				return err
			}
			continue
		}
		if err := s.svc.Store.RequeueWorkflowEvent(ctx, event.ID); err != nil {
			return err
		}
	}
	return nil
}
