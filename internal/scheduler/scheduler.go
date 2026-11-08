// scheduler.go provides the server-side scheduler, responsible for automatically running various periodic maintenance tasks.
// The scheduler uses Redis distributed locks to ensure tasks are not executed redundantly across multiple instances.
//
// Scheduled tasks:
//   - Heartbeat-timeout detection (15s): marks offline daemons and updates agent status
//   - Agent auto-recover (60s): recovers busy agents with no running nodes back to online
//   - Ready-node renotify (30s): re-notifies ready nodes that have gone unclaimed for a long time
//   - Workflow retry-due scan (10s): wakes workflow V2 nodes whose retry time has passed
//   - Workflow wait-due scan (10s): wakes workflow V2 duration waits that expired
//   - Workflow async-node scan (5s): executes ready action.http / action.create_task /
//     workflow.call nodes and wakes sub-workflow parents of terminal child runs
//   - Low-confidence memory GC (daily at 3:00): deletes low-confidence memories older than 30 days
//   - Old-workspace cleanup (daily at 4:00): identifies completed tasks older than 7 days for later cleanup
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// EventPublisher defines the minimal event-publishing interface required by the scheduler.
type EventPublisher interface {
	Publish(ctx context.Context, subscriberID string, event types.SSEEvent) error
	BufferEvent(ctx context.Context, subscriberID string, event types.SSEEvent) error
}

// dueNodeScanLimit bounds one scan of due workflow nodes.
const dueNodeScanLimit int32 = 100

// WorkflowRunAdvancer wakes due workflow V2 nodes; implemented by the service
// layer (WorkflowRunService.WakeDueNode), which performs the waiting→ready
// transition, the graph advance and the node:ready notifications.
type WorkflowRunAdvancer interface {
	WakeDueNode(ctx context.Context, nodeID uuid.UUID) error
}

// EventProcessor consumes pending external workflow events; implemented by
// the service layer (WorkflowEventService.ProcessPendingEvents).
type EventProcessor interface {
	ProcessPendingEvents(ctx context.Context) error
}

// NodeActionRunner executes ready server-side action nodes; implemented by
// the service layer (WorkflowNodeActionService.ProcessGitHubActionNodes).
// ScheduleRunner fires due trigger.schedule templates; implemented by the
// service layer (WorkflowScheduleService.ProcessDueSchedules).
type ScheduleRunner interface {
	ProcessDueSchedules(ctx context.Context) error
}

type NodeActionRunner interface {
	ProcessGitHubActionNodes(ctx context.Context) error
	// ReclaimLeasedNodes fails server-executed nodes whose claim lease
	// expired (the executor died before reaching a terminal state).
	ReclaimLeasedNodes(ctx context.Context) error
}

// AsyncNodeRunner executes ready async action nodes (HTTP, task creation,
// sub-workflow calls) and drives the sub-workflow parent wake-ups; implemented
// by the service layer (WorkflowAsyncNodeService.ProcessWorkflowAsyncNodes).
type AsyncNodeRunner interface {
	ProcessWorkflowAsyncNodes(ctx context.Context) error
}

// Scheduler is the server-side scheduler, responsible for managing various periodic maintenance tasks.
// It uses Redis distributed locks to ensure the same task is not executed concurrently across instances.
type Scheduler struct {
	Store *store.Store
	Hub   EventPublisher
	Redis *redis.Client
	Clock clock.Clock
	// WorkflowRuns advances due workflow V2 nodes; optional, jobs are no-ops
	// when unset (nil in tests that do not exercise the run engine).
	WorkflowRuns WorkflowRunAdvancer
	// WorkflowEvents consumes pending external events (S5); optional no-op.
	WorkflowEvents EventProcessor
	// WorkflowNodeActions executes ready action.github nodes (S5); optional no-op.
	WorkflowNodeActions NodeActionRunner
	// WorkflowSchedules fires due trigger.schedule templates; optional no-op.
	WorkflowSchedules ScheduleRunner
	// WorkflowAsyncNodes executes ready action.http / action.create_task /
	// workflow.call nodes and wakes sub-workflow parents (S6); optional no-op.
	WorkflowAsyncNodes AsyncNodeRunner
}

// NewScheduler creates a new scheduler instance.
//
// Parameters:
//   - st: the store instance wrapping the sqlc-generated queries
//   - hub: the SSE event publisher (ws.Hub satisfies this interface)
//   - rdb: the Redis client, used for distributed locks
//
// Returns:
//   - *Scheduler: the initialized scheduler instance, using real system time by default
func NewScheduler(st *store.Store, hub EventPublisher, rdb *redis.Client) *Scheduler {
	return &Scheduler{Store: st, Hub: hub, Redis: rdb, Clock: clock.RealClock{}}
}

// Start launches all scheduled tasks and blocks until the context is canceled.
// It spawns multiple goroutines to run tasks at different intervals.
func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("scheduler started")

	go s.runPeriodic(ctx, 15*time.Second, "heartbeat_timeout", s.CheckHeartbeatTimeout)
	go s.runPeriodic(ctx, 60*time.Second, "agent_auto_recover", s.AgentAutoRecoverOnline)
	go s.runPeriodic(ctx, 30*time.Second, "ready_node_renotify", s.RenotifyReadyNodes)
	go s.runPeriodic(ctx, 10*time.Second, "workflow_retry_due", s.ProcessRetryDueNodes)
	go s.runPeriodic(ctx, 10*time.Second, "workflow_wait_due", s.ProcessWaitDueNodes)
	go s.runPeriodic(ctx, 5*time.Second, "workflow_events", s.ProcessWorkflowEvents)
	go s.runPeriodic(ctx, 30*time.Second, "workflow_schedule_triggers", s.ProcessWorkflowSchedules)
	go s.runPeriodic(ctx, 5*time.Second, "workflow_node_actions", s.ProcessWorkflowNodeActions)
	go s.runPeriodic(ctx, time.Minute, "workflow_node_lease", s.ProcessNodeLeaseReclaim)
	go s.runPeriodic(ctx, 5*time.Second, "workflow_async_nodes", s.ProcessWorkflowAsyncNodes)
	go s.runDaily(ctx, 3, 0, "memory_gc", s.lowConfidenceMemoryGC)
	go s.runDaily(ctx, 4, 0, "workspace_cleanup", s.CleanupOldWorkspaces)

	<-ctx.Done()
	slog.Info("scheduler stopped")
}

// ---------------------------------------------------------------------------
// Scheduling primitives
// ---------------------------------------------------------------------------

// runPeriodic runs a task at a fixed interval, using a Redis distributed lock to prevent concurrent execution across instances.
//
// Parameters:
//   - ctx: context, used to cancel the task
//   - interval: the execution interval
//   - name: the task name, used as the Redis lock key
//   - fn: the task function to execute
func (s *Scheduler) runPeriodic(ctx context.Context, interval time.Duration, name string, fn func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.WithLock(ctx, name, interval, fn)
		}
	}
}

// runDaily runs a task at a specified time every day, suitable for low-frequency maintenance tasks.
//
// Parameters:
//   - ctx: context, used to cancel the task
//   - hour: the hour of execution (0-23)
//   - minute: the minute of execution (0-59)
//   - name: the task name, used as the Redis lock key
//   - fn: the task function to execute
func (s *Scheduler) runDaily(ctx context.Context, hour, minute int, name string, fn func(context.Context) error) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.WithLock(ctx, name, 24*time.Hour, fn)
		}
	}
}

// WithLock executes a task under a Redis distributed lock, preventing concurrent execution of the same task across instances.
// The lock TTL is the interval +5s: it only thins out duplicate scans across
// instances; long tasks stay correct through the node/event-level CAS guards
// even when a lock expires mid-run.
//
// Parameters:
//   - ctx: context
//   - name: the task name, used as the suffix of the Redis lock key
//   - lockTTL: the lock expiration time
//   - fn: the task function to execute
func (s *Scheduler) WithLock(ctx context.Context, name string, lockTTL time.Duration, fn func(context.Context) error) {
	lockKey := fmt.Sprintf("scheduler:lock:%s", name)
	// Acquire the lock with SETNX; set the TTL to the task interval + 5s to avoid duplicate execution caused by lock expiry
	ok, err := s.Redis.SetNX(ctx, lockKey, "locked", lockTTL+5*time.Second).Result()
	if err != nil {
		slog.Error("redis setnx error", "task", name, "err", err)
		return
	}
	if !ok {
		return // Another instance already holds the lock
	}
	defer s.Redis.Del(ctx, lockKey)

	if err := fn(ctx); err != nil {
		slog.Error("scheduler task error", "task", name, "err", err)
	}
}

// ---------------------------------------------------------------------------
// Task implementations
// ---------------------------------------------------------------------------

// CheckHeartbeatTimeout marks daemons with stale heartbeats offline and brings
// the agents they report offline as well.
// It runs every 15 seconds, detecting daemons that have not sent a heartbeat in over 100 seconds.
func (s *Scheduler) CheckHeartbeatTimeout(ctx context.Context) error {
	staleDaemons, err := s.Store.MarkStaleDaemons(ctx)
	if err != nil {
		slog.Error("mark stale daemons", "err", err)
		return nil
	}
	if len(staleDaemons) > 0 {
		slog.Info("marked stale daemons offline", "count", len(staleDaemons))
	}

	offlineAgents := 0
	for _, daemonID := range staleDaemons {
		if err := s.Store.SetAgentsStatusByDaemon(ctx, daemonID, types.AgentStatusOffline); err != nil {
			slog.Error("set agents offline by daemon", "daemon_id", daemonID, "err", err)
			continue
		}
	}

	slog.Info("heartbeat_timeout complete",
		"stale_daemons", len(staleDaemons),
		"offline_agents", offlineAgents,
	)
	return nil
}

// AgentAutoRecoverOnline recovers agents with no running nodes and a "busy" status
// back to "online", preventing agents from being stuck in "busy" after all nodes complete.
// It runs every 60 seconds.
func (s *Scheduler) AgentAutoRecoverOnline(ctx context.Context) error {
	n, err := s.Store.AutoRecoverIdleAgents(ctx)
	if err != nil {
		slog.Error("agent auto recover online", "err", err)
		return nil
	}
	if n > 0 {
		slog.Info("auto recovered agents to online", "count", n)
	}
	return nil
}

// lowConfidenceMemoryGC deletes memories with confidence below 0.1, unverified, and older than 30 days.
// It runs daily at 3:00 AM, cleaning low-quality memories to save storage space.
func (s *Scheduler) lowConfidenceMemoryGC(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-30 * 24 * time.Hour)
	n, err := s.Store.DeleteLowConfidenceMemories(ctx, cutoff)
	if err != nil {
		slog.Error("low-confidence memory GC", "err", err)
		return nil
	}
	if n > 0 {
		slog.Info("low-confidence memory GC deleted memories", "count", n)
	}
	return nil
}

// RenotifyReadyNodes scans ready nodes that have gone unclaimed for a long time and re-publishes node:pending events to their project-member agents.
// It runs every 30 seconds, compensating for SSE events lost while daemons were offline.
// Delivery is at the project granularity: only project-member agents are notified,
// published on each agent's daemon stream with an agent_id envelope.
func (s *Scheduler) RenotifyReadyNodes(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-30 * time.Second)
	projectIDs, err := s.Store.ListPendingRenotifyProjectIDs(ctx, cutoff)
	if err != nil {
		slog.Error("renotify ready nodes query", "err", err)
		return nil
	}

	for _, projectID := range projectIDs {
		members, err := s.Store.ListProjectMembers(ctx, projectID)
		if err != nil {
			slog.Error("renotify list project members", "project_id", projectID, "err", err)
			continue
		}

		for _, m := range members {
			if m.MemberType != "agent" || m.AgentID == nil {
				continue
			}
			agentUUID, err := uuid.Parse(*m.AgentID)
			if err != nil {
				continue
			}
			if err := s.publishToAgentDaemon(ctx, agentUUID, types.EventNodePending, map[string]string{
				"reason": "renotify_ready",
			}); err != nil {
				continue
			}
		}
	}

	if len(projectIDs) > 0 {
		slog.Info("renotify ready nodes", "projects", len(projectIDs))
	}
	return nil
}

// ProcessRetryDueNodes wakes the workflow V2 nodes whose scheduled retry time
// has passed: each node returns to ready and its task advances. It runs every
// 10 seconds.
func (s *Scheduler) ProcessRetryDueNodes(ctx context.Context) error {
	return s.wakeDueNodes(ctx, "retry", func(now time.Time) ([]types.TaskNode, error) {
		return s.Store.ListRetryDueNodes(ctx, now, dueNodeScanLimit)
	})
}

// ProcessWaitDueNodes wakes the workflow V2 duration-wait nodes whose wake
// time has passed, emitting the resumed port downstream. It runs every 10
// seconds.
func (s *Scheduler) ProcessWaitDueNodes(ctx context.Context) error {
	return s.wakeDueNodes(ctx, "wait", func(now time.Time) ([]types.TaskNode, error) {
		return s.Store.ListWaitDueNodes(ctx, now, dueNodeScanLimit)
	})
}

// ProcessWorkflowEvents consumes pending external workflow events (webhook
// deliveries): trigger matching and waiter wake-up. It runs every 5 seconds.
func (s *Scheduler) ProcessWorkflowEvents(ctx context.Context) error {
	if s.WorkflowEvents == nil {
		return nil
	}
	return s.WorkflowEvents.ProcessPendingEvents(ctx)
}

// ProcessWorkflowSchedules fires due trigger.schedule templates. It runs
// every 30 seconds.
func (s *Scheduler) ProcessWorkflowSchedules(ctx context.Context) error {
	if s.WorkflowSchedules == nil {
		return nil
	}
	return s.WorkflowSchedules.ProcessDueSchedules(ctx)
}

// ProcessWorkflowNodeActions executes ready action.github nodes. It runs
// every 5 seconds.
func (s *Scheduler) ProcessWorkflowNodeActions(ctx context.Context) error {
	if s.WorkflowNodeActions == nil {
		return nil
	}
	return s.WorkflowNodeActions.ProcessGitHubActionNodes(ctx)
}

// ProcessNodeLeaseReclaim fails nodes whose server executor died mid-flight.
// It runs every minute.
func (s *Scheduler) ProcessNodeLeaseReclaim(ctx context.Context) error {
	if s.WorkflowNodeActions == nil {
		return nil
	}
	return s.WorkflowNodeActions.ReclaimLeasedNodes(ctx)
}

// ProcessWorkflowAsyncNodes executes ready async action nodes (HTTP, task
// creation, sub-workflow calls) and wakes parents of terminal child runs.
// It runs every 5 seconds.
func (s *Scheduler) ProcessWorkflowAsyncNodes(ctx context.Context) error {
	if s.WorkflowAsyncNodes == nil {
		return nil
	}
	return s.WorkflowAsyncNodes.ProcessWorkflowAsyncNodes(ctx)
}

// wakeDueNodes scans due nodes and wakes each one through the run service;
// WakeDueNode is idempotent, so nodes already woken by another instance no-op.
func (s *Scheduler) wakeDueNodes(ctx context.Context, kind string, scan func(time.Time) ([]types.TaskNode, error)) error {
	if s.WorkflowRuns == nil {
		return nil
	}
	due, err := scan(s.Clock.Now())
	if err != nil {
		return fmt.Errorf("scan %s-due nodes: %w", kind, err)
	}
	for _, node := range due {
		nodeID, err := uuid.Parse(node.ID)
		if err != nil {
			continue
		}
		if err := s.WorkflowRuns.WakeDueNode(ctx, nodeID); err != nil {
			slog.Error("wake due workflow node", "kind", kind, "node_id", node.ID, "err", err)
		}
	}
	if len(due) > 0 {
		slog.Info("woke due workflow nodes", "kind", kind, "count", len(due))
	}
	return nil
}

// CleanupOldWorkspaces identifies completed/cancelled tasks older than 7 days
// and records them as cleanup candidates. The actual file cleanup is performed by the CLI command or by the agent upon receiving a cleanup event.
// It runs daily at 4:00 AM, querying in batches (500 per batch) to keep memory usage low.
func (s *Scheduler) CleanupOldWorkspaces(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-7 * 24 * time.Hour)
	const batchSize int32 = 500

	var allCandidates []types.GetCompletedTasksOlderThanRow
	var lastID int32

	for {
		rows, err := s.Store.GetCompletedTasksOlderThan(ctx, cutoff, lastID, batchSize)
		if err != nil {
			slog.Error("workspace cleanup query", "err", err)
			return nil
		}
		if len(rows) == 0 {
			break
		}
		allCandidates = append(allCandidates, rows...)
		lastID = rows[len(rows)-1].ID
		if int32(len(rows)) < batchSize {
			break
		}
	}

	if len(allCandidates) == 0 {
		slog.Info("workspace_cleanup complete, no candidates found")
		return nil
	}

	// Log grouped by workspace
	byWorkspace := make(map[uuid.UUID][]types.GetCompletedTasksOlderThanRow)
	for _, r := range allCandidates {
		wsUUID, _ := uuid.Parse(r.WorkspaceID)
		byWorkspace[wsUUID] = append(byWorkspace[wsUUID], r)
	}

	for wsID, tasks := range byWorkspace {
		taskIDs := make([]int32, 0, len(tasks))
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.ID)
		}
		slog.Info("workspace_cleanup candidates",
			"workspace_id", wsID,
			"count", len(taskIDs),
			"task_ids", taskIDs,
		)
	}

	slog.Info("workspace_cleanup complete",
		"total_candidates", len(allCandidates),
		"workspaces", len(byWorkspace),
		"cutoff", cutoff.Format(time.RFC3339),
	)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// publishToAgentDaemon publishes an SSE event on the agent's daemon stream with
// an agent_id envelope (daemon-side routing), buffering for offline replay.
func (s *Scheduler) publishToAgentDaemon(ctx context.Context, agentID uuid.UUID, eventType string, payload interface{}) error {
	if s.Hub == nil {
		return nil
	}

	dataBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	envelope := map[string]json.RawMessage{
		"agent_id": json.RawMessage(`"` + agentID.String() + `"`),
		"payload":  json.RawMessage(dataBytes),
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	event := types.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: eventType,
		Data:  envelopeBytes,
	}

	daemonID, online, err := s.Store.GetOnlineDaemonIDByAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("resolve agent daemon: %w", err)
	}
	key := "daemon:" + daemonID.String()
	if !online {
		if err := s.Hub.BufferEvent(ctx, key, event); err != nil {
			slog.Error("buffer event for offline daemon", "daemon_id", daemonID, "err", err)
		}
		return nil
	}
	if err := s.Hub.Publish(ctx, key, event); err != nil {
		slog.Error("publish event to daemon", "daemon_id", daemonID, "err", err)
	}
	return nil
}
