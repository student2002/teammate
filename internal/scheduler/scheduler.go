// scheduler.go provides the server-side scheduler, responsible for automatically running various periodic maintenance tasks.
// The scheduler uses Redis distributed locks to ensure tasks are not executed redundantly across multiple instances.
//
// Scheduled tasks:
//   - Heartbeat-timeout detection (15s): marks offline runtimes and updates agent status
//   - Continuation-reservation cleanup (30s): clears expired node continuation reservations
//   - Claim-timeout release (60min): releases in_progress nodes with no progress for a long time
//   - Node-timeout handling (5min): handles nodes that exceed the template timeout
//   - Offline fallback (5min): moves nodes held by offline agents to manual intervention
//   - Agent auto-recover (60s): recovers busy agents with no tasks back to online
//   - Pending-node renotify (30s): re-notifies pending nodes that have gone unclaimed for a long time
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
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// EventPublisher defines the minimal event-publishing interface required by the scheduler.
type EventPublisher interface {
	Publish(ctx context.Context, subscriberID string, event types.SSEEvent) error
	BufferEvent(ctx context.Context, subscriberID string, event types.SSEEvent) error
}

// Scheduler is the server-side scheduler, responsible for managing various periodic maintenance tasks.
// It uses Redis distributed locks to ensure the same task is not executed concurrently across instances.
type Scheduler struct {
	Store *store.Store
	Hub   EventPublisher
	Redis *redis.Client
	Clock clock.Clock
}

// NewScheduler creates a new scheduler instance.
//
// Parameters:
//   - q: the sqlc-generated database query instance
//   - db: the database connection pool, used for operations that require a transaction
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
	go s.runPeriodic(ctx, 30*time.Second, "reservation_cleanup", s.ClearExpiredReservations)
	go s.runPeriodic(ctx, 60*time.Second, "claim_timeout", s.ReleaseClaimTimeoutNodes)
	go s.runPeriodic(ctx, 300*time.Second, "node_timeout", s.CheckNodeTimeout)
	go s.runPeriodic(ctx, 300*time.Second, "offline_fallback", s.OfflineAgentFallback)
	go s.runPeriodic(ctx, 60*time.Second, "agent_auto_recover", s.AgentAutoRecoverOnline)
	go s.runPeriodic(ctx, 30*time.Second, "pending_node_renotify", s.RenotifyPendingNodes)
	go s.runPeriodic(ctx, 30*time.Second, "workflow_triggers", s.ProcessWorkflowTriggers)
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
// The lock TTL is slightly longer than the task interval (+5s) to ensure the lock does not expire before the task completes.
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

// CheckHeartbeatTimeout marks stale runtimes offline and updates the status of all agents whose runtimes are offline.
// It runs every 15 seconds, detecting runtimes that have not sent a heartbeat in over 100 seconds.
func (s *Scheduler) CheckHeartbeatTimeout(ctx context.Context) error {
	staleRuntimes, err := s.Store.MarkStaleRuntimes(ctx, s.Clock.Now().Add(-100*time.Second))
	if err != nil {
		slog.Error("mark stale runtimes", "err", err)
	} else if len(staleRuntimes) > 0 {
		slog.Info("marked stale runtimes offline", "count", len(staleRuntimes))
	}

	offlineAgents, err := s.Store.UpdateOfflineAgents(ctx)
	if err != nil {
		slog.Error("update offline agents", "err", err)
	} else if len(offlineAgents) > 0 {
		slog.Info("updated agents to offline", "count", len(offlineAgents))
	}

	slog.Info("heartbeat_timeout complete",
		"stale_runtimes", len(staleRuntimes),
		"offline_agents", len(offlineAgents),
	)
	return nil
}

// ClearExpiredReservations clears expired reservations on continuation nodes.
// It runs every 30 seconds, releasing continuation reservations unclaimed for over 30 seconds so other agents can claim them.
func (s *Scheduler) ClearExpiredReservations(ctx context.Context) error {
	clearedReservations, err := s.Store.ClearExpiredReservations(ctx, s.Clock.Now().Add(-30*time.Second))
	if err != nil {
		slog.Error("clear expired reservations", "err", err)
		return nil
	}
	if len(clearedReservations) > 0 {
		slog.Info("cleared expired reservations", "count", len(clearedReservations))
	}
	return nil
}

// ReleaseClaimTimeoutNodes releases nodes that have exceeded the claim timeout (in_progress with no progress for 30 minutes),
// setting them to manual_intervention and sending an interrupt event to the original agent.
// It runs every 60 seconds.
func (s *Scheduler) ReleaseClaimTimeoutNodes(ctx context.Context) error {
	releasedNodes, err := s.Store.ReleaseClaimTimeoutNodes(ctx, s.Clock.Now().Add(-30*time.Minute))
	if err != nil {
		slog.Error("release claim timeout nodes", "err", err)
		return nil
	}
	if len(releasedNodes) > 0 {
		slog.Info("claim timeout nodes detected", "count", len(releasedNodes))
		for _, node := range releasedNodes {
			// Send an interrupt event to the original agent (the SQL retains the assignee_id)
			if node.AssigneeID != nil {
				assigneeUUID, _ := uuid.Parse(*node.AssigneeID)
				nodeUUID, _ := uuid.Parse(node.ID)
				s.publishNodeTimeout(ctx, nodeUUID, assigneeUUID)
			}

			// Create a state-transition record
			_, _ = s.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
				TaskNodeID:   node.ID,
				FromStatus:   types.TaskNodeStatusInProgress,
				ToStatus:     types.TaskNodeStatusManualIntervention,
				Action:       types.TransitionActionTimeout,
				OperatorType: "system",
			})
		}
	}
	return nil
}

// CheckNodeTimeout handles nodes that exceed the template timeout_minutes, setting them to manual_intervention.
// It runs every 5 minutes; each node uses a transaction to ensure the atomicity of the status change and the transition record.
func (s *Scheduler) CheckNodeTimeout(ctx context.Context) error {
	timedOutNodes, err := s.Store.GetTimedOutNodes(ctx, s.Clock.Now())
	if err != nil {
		return err
	}

	if len(timedOutNodes) > 0 {
		slog.Info("found timed-out nodes", "count", len(timedOutNodes))
	}

	for _, node := range timedOutNodes {
		if err := s.Store.MoveTimedOutNodeToManual(ctx, node); err != nil {
			slog.Error("move timed-out node to manual_intervention", "node_id", node.ID, "err", err)
			continue
		}
		if node.AssigneeID != nil {
			assigneeUUID, _ := uuid.Parse(*node.AssigneeID)
			nodeUUID, _ := uuid.Parse(node.ID)
			s.publishNodeTimeout(ctx, nodeUUID, assigneeUUID)
		}
	}

	slog.Info("node_timeout complete", "timed_out_nodes", len(timedOutNodes))
	return nil
}

// OfflineAgentFallback moves in_progress nodes held by agents that have been offline for over 1 hour
// to manual_intervention, ensuring offline agents do not permanently block task flow.
// It runs every 5 minutes.
func (s *Scheduler) OfflineAgentFallback(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-1 * time.Hour)
	affectedNodes, err := s.Store.OfflineAgentFallback(ctx, cutoff)
	if err != nil {
		slog.Error("offline agent fallback", "err", err)
		return nil
	}
	if len(affectedNodes) > 0 {
		slog.Info("offline agent fallback moved nodes to manual_intervention", "count", len(affectedNodes))
	}
	return nil
}

// AgentAutoRecoverOnline recovers agents with no in_progress nodes and a "busy" status
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

// RenotifyPendingNodes scans pending nodes that have gone unclaimed for a long time and re-publishes node:pending events to their project-member agents.
// It runs every 30 seconds, compensating for SSE events lost while runtimes were offline.
// Delivery is at the project granularity: only project-member agents are notified (consistent with service.publishToProject semantics).
func (s *Scheduler) RenotifyPendingNodes(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-30 * time.Second)
	projectIDs, err := s.Store.ListPendingRenotifyProjectIDs(ctx, cutoff)
	if err != nil {
		slog.Error("renotify pending nodes query", "err", err)
		return nil
	}

	for _, projectID := range projectIDs {
		data, _ := json.Marshal(map[string]string{
			"reason": "renotify_pending",
		})
		event := types.SSEEvent{
			ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
			Event: types.EventNodePending,
			Data:  data,
		}

		members, err := s.Store.ListProjectMembers(ctx, projectID)
		if err != nil {
			slog.Error("renotify list project members", "project_id", projectID, "err", err)
			continue
		}

		onlineSent := false
		for _, m := range members {
			if m.MemberType != "agent" || m.AgentID == nil {
				continue
			}
			agentUUID, err := uuid.Parse(*m.AgentID)
			if err != nil {
				continue
			}
			runtimeIDs, err := s.Store.ListOnlineRuntimeIDsByAgent(ctx, agentUUID)
			if err != nil {
				slog.Error("renotify list runtimes", "agent_id", m.AgentID, "err", err)
				continue
			}
			for _, runtimeID := range runtimeIDs {
				if err := s.Hub.Publish(ctx, runtimeID.String(), event); err != nil {
					slog.Error("renotify publish", "runtime_id", runtimeID, "err", err)
					continue
				}
				onlineSent = true
			}
		}

		// No online member runtimes: buffer to all runtimes of the member agents (including offline ones) for replay upon reconnection
		if !onlineSent {
			for _, m := range members {
				if m.MemberType != "agent" || m.AgentID == nil {
					continue
				}
				agentUUID, err := uuid.Parse(*m.AgentID)
				if err != nil {
					continue
				}
				runtimeIDs, err := s.Store.ListRuntimeIDsByAgent(ctx, agentUUID)
				if err != nil {
					continue
				}
				for _, runtimeID := range runtimeIDs {
					if err := s.Hub.BufferEvent(ctx, runtimeID.String(), event); err != nil {
						slog.Error("renotify buffer", "runtime_id", runtimeID, "err", err)
					}
				}
			}
		}
	}

	if len(projectIDs) > 0 {
		slog.Info("renotify pending nodes", "projects", len(projectIDs))
	}
	return nil
}

// CleanupOldWorkspaces identifies completed/cancelled tasks older than 7 days
// and records them as cleanup candidates. The actual file cleanup is performed by the CLI command or by the agent upon receiving a cleanup event.
// It runs daily at 4:00 AM, querying in batches (500 per batch) to keep memory usage low.
func (s *Scheduler) ProcessWorkflowTriggers(ctx context.Context) error {
	svc := &service.Service{
		Store: s.Store,
		Hub:   s.Hub,
		Redis: s.Redis,
	}
	return service.NewWorkflowTriggerService(svc).ProcessDueSchedules(ctx, s.Clock.Now())
}

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

// publishNodeTimeout sends a node-timeout SSE event to the specified agent.
//
// Parameters:
//   - ctx: context
//   - nodeID: the ID of the timed-out node
//   - agentID: the ID of the node's agent
func (s *Scheduler) publishNodeTimeout(ctx context.Context, nodeID, agentID uuid.UUID) {
	rt, err := s.Store.GetRuntimeByAgent(ctx, agentID)
	if err != nil {
		slog.Error("get runtime for agent", "agent_id", agentID, "err", err)
		return
	}

	runtimeID := rt.ID
	data, _ := json.Marshal(map[string]string{
		"node_id":  nodeID.String(),
		"action":   "timeout",
		"status":   "manual_intervention",
		"agent_id": agentID.String(),
	})

	if err := s.Hub.Publish(ctx, runtimeID, types.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: types.EventNodeTimeout,
		Data:  data,
	}); err != nil {
		slog.Error("publish SSE node timeout", "runtime_id", runtimeID, "err", err)
	}
}
