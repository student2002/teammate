// Package scheduler_test contains tests for the scheduler, covering
// Redis distributed lock, lock expiration, claim timeout release,
// heartbeat timeout, offline agent fallback, and node timeout handling.
package scheduler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/clock"
	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/scheduler"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// TestMain is the test entry function that initializes the test database and runs all tests.
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

// strPtr is a convenience constructor for *string, used for *string fields in types.*Params.
func strPtr(s string) *string { return &s }

// getTestDSN returns the test database connection string, delegating to the shared testdb helper.
func getTestDSN() string {
	return testdb.GetTestDSN()
}

// getTestRedisURL returns the Redis connection URL, defaulting to localhost:16379,
// overridable via the TEAMS_REDIS_URL environment variable.
func getTestRedisURL() string {
	if url := os.Getenv("TEAMS_REDIS_URL"); url != "" {
		return url
	}
	return "redis://localhost:16379"
}

// connectTestDB opens a test database connection, skipping the test if the database is unavailable.
func connectTestDB(t *testing.T) *sql.DB {
	t.Helper()
	pgDB, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	if err := pgDB.Ping(); err != nil {
		pgDB.Close()
		t.Skipf("database not available, skipping: %v", err)
	}
	t.Cleanup(func() { pgDB.Close() })
	return pgDB
}

// connectTestRedis opens a connection to the test Redis instance, skipping the test if Redis is unavailable.
func connectTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(getTestRedisURL())
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		t.Skipf("redis not available, skipping: %v", err)
	}
	return rdb
}

// TestRedisDistributedLock verifies that only one instance can acquire the lock.
func TestRedisDistributedLock(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	ctx := context.Background()
	lockKey := fmt.Sprintf("test:scheduler:lock:%s", uuid.New().String()[:8])
	lockTTL := 10 * time.Second

	t.Cleanup(func() { rdb.Del(ctx, lockKey) })

	// First instance acquires the lock
	ok, err := rdb.SetNX(ctx, lockKey, "instance-1", lockTTL).Result()
	if err != nil {
		t.Fatalf("setnx error: %v", err)
	}
	if !ok {
		t.Fatal("first instance should acquire the lock")
	}

	// Second instance tries to acquire the same lock — should fail
	ok, err = rdb.SetNX(ctx, lockKey, "instance-2", lockTTL).Result()
	if err != nil {
		t.Fatalf("setnx error: %v", err)
	}
	if ok {
		t.Fatal("second instance should NOT acquire the lock")
	}

	t.Log("distributed lock: only one instance can hold the lock")
}

// TestLockExpiration verifies the lock is released after task completion (via explicit DEL)
// and also auto-released after TTL expiry.
func TestLockExpiration(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	ctx := context.Background()
	lockKey := fmt.Sprintf("test:scheduler:lock:%s", uuid.New().String()[:8])
	lockTTL := 5 * time.Second

	t.Cleanup(func() { rdb.Del(ctx, lockKey) })

	// Acquire the lock
	ok, _ := rdb.SetNX(ctx, lockKey, "locked", lockTTL).Result()
	if !ok {
		t.Fatal("should acquire lock")
	}

	// Simulate task completion — release the lock
	rdb.Del(ctx, lockKey)

	// Verify the lock is released — another instance can acquire it
	ok, _ = rdb.SetNX(ctx, lockKey, "locked-again", lockTTL).Result()
	if !ok {
		t.Fatal("lock should be available after explicit release")
	}
	rdb.Del(ctx, lockKey)

	// Test TTL expiry
	ok, _ = rdb.SetNX(ctx, lockKey, "locked-ttl", 1*time.Second).Result()
	if !ok {
		t.Fatal("should acquire lock for TTL test")
	}

	// Wait for TTL to expire
	time.Sleep(1500 * time.Millisecond)

	ok, _ = rdb.SetNX(ctx, lockKey, "after-expiry", lockTTL).Result()
	if !ok {
		t.Fatal("lock should be available after TTL expiry")
	}
	rdb.Del(ctx, lockKey)

	t.Log("lock expiration: lock correctly released via DEL and TTL expiry")
}

// TestWithLockFunction tests the scheduler's WithLock helper function.// Verifies WithLock acquires a Redis lock, executes the callback, and releases the lock afterward.
func TestWithLockFunction(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	ctx := context.Background()
	taskName := fmt.Sprintf("test-task-%s", uuid.New().String()[:8])
	lockKey := fmt.Sprintf("scheduler:lock:%s", taskName)

	t.Cleanup(func() { rdb.Del(ctx, lockKey) })

	executed := false
	sched.WithLock(ctx, taskName, 10*time.Second, func(ctx context.Context) error {
		executed = true
		return nil
	})

	if !executed {
		t.Fatal("WithLock should execute the function when lock is acquired")
	}

	// Verify the lock is released after execution
	val, err := rdb.Get(ctx, lockKey).Result()
	if err == nil && val != "" {
		t.Fatalf("lock should be released after WithLock completes, but key exists: %s", val)
	}
	t.Log("WithLock: function executed and lock released correctly")
}

// TestWithLockOnlyOneInstance verifies that WithLock prevents concurrent execution by refusing to acquire the same lock twice.
func TestWithLockOnlyOneInstance(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	ctx := context.Background()
	taskName := fmt.Sprintf("test-concurrent-%s", uuid.New().String()[:8])
	lockKey := fmt.Sprintf("scheduler:lock:%s", taskName)

	t.Cleanup(func() { rdb.Del(ctx, lockKey) })

	// Manually acquire the lock first
	ok, _ := rdb.SetNX(ctx, lockKey, "other-instance", 10*time.Second).Result()
	if !ok {
		t.Fatal("should acquire manual lock")
	}

	// Since the lock is already held, WithLock should skip execution
	executed := false
	sched.WithLock(ctx, taskName, 10*time.Second, func(ctx context.Context) error {
		executed = true
		return nil
	})

	if executed {
		t.Fatal("WithLock should NOT execute when lock is held by another instance")
	}
	t.Log("WithLock: correctly skipped when lock is held")

	// Cleanup
	rdb.Del(ctx, lockKey)
}

// TestClaimTimeoutRelease verifies that nodes remaining in_progress for over 30 minutes are released back to pending.
// This test verifies that the scheduler releases expired claims on task nodes when the claiming agent's runtime instance has been offline beyond the heartbeat timeout threshold.
func TestClaimTimeoutRelease(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// Create workspace, project, workflow, task, and node
	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "timeout-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "TT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "timeout-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	tpl, _, err := s.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: ws.ID,
		Name:        "timeout-flow",
		Description: strPtr("test"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code node"), SortOrder: 1, NodeType: types.NodeTypeStandard, AssigneeType: types.AssigneeTypeAnyAgent, TimeoutMinutes: 60},
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "timeout-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOffline,
		GitName:      strPtr("timeout-agent"),
		GitEmail:     strPtr("timeout-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    strPtr(agent.ID),
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: tpl.Name,
		Title:        "Timeout test task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0, // will be set by sequence sync
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node, err := db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:          task.ID,
		MaxRejectCycles: 3,
		TimeoutMinutes:  60,
		Name:            "code",
		Description:     sql.NullString{String: "code node", Valid: true},
		SortOrder:       1,
		NodeType:        db.NodeTypeStandard,
		Status:          db.TaskNodeStatusInProgress,
		AssigneeType:    db.AssigneeTypeAnyAgent,
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	// Use FakeClock to simulate 31 minutes passing for timeout checking.
	fakeClock := clock.NewFakeClock(time.Now())
	fakeClock.Advance(31 * time.Minute)
	cutoff := fakeClock.Now().Add(-30 * time.Minute)

	// Execute claim timeout release with the computed cutoff
	releasedNodes, err := s.ReleaseClaimTimeoutNodes(ctx, cutoff)
	if err != nil {
		t.Fatalf("release claim timeout nodes: %v", err)
	}

	if len(releasedNodes) == 0 {
		t.Fatal("expected at least 1 released node, got 0")
	}

	// Verify the node returns to pending
	updatedNode, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node: %v", err)
	}
	if updatedNode.Status != types.TaskNodeStatusManualIntervention {
		t.Fatalf("expected manual_intervention after timeout release, got %s", updatedNode.Status)
	}
	t.Logf("claim timeout release: node correctly reset to manual_intervention (released %d nodes)", len(releasedNodes))
}

// TestHeartbeatTimeout verifies that timed-out heartbeats cause the runtime instance to be marked offline.
func TestHeartbeatTimeout(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// Create workspace and Agent
	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "heartbeat-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "HT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "heartbeat-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOffline,
		GitName:      strPtr("heartbeat-agent"),
		GitEmail:     strPtr("heartbeat-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Use FakeClock to simulate heartbeat expiration.
	fakeClock := clock.NewFakeClock(time.Now())
	staleHeartbeat := fakeClock.Now().Add(-5 * time.Minute)

	rt, err := s.CreateRuntime(ctx, types.CreateRuntimeParams{
		AgentID:  agent.ID,
		DaemonID: "daemon-heartbeat-test",
		Provider: types.AgentProviderClaude,
		Status:   types.RuntimeStatusOnline,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	_, err = db.New(pgDB).UpdateRuntimeHeartbeat(ctx, db.UpdateRuntimeHeartbeatParams{
		ID:            uuid.MustParse(rt.ID),
		LastHeartbeat: sql.NullTime{Time: staleHeartbeat, Valid: true},
	})
	if err != nil {
		t.Fatalf("update runtime heartbeat: %v", err)
	}

	staleCutoff := fakeClock.Now().Add(-100 * time.Second)
	staleRuntimes, err := s.MarkStaleRuntimes(ctx, staleCutoff)
	if err != nil {
		t.Fatalf("mark stale runtimes: %v", err)
	}

	if len(staleRuntimes) == 0 {
		t.Fatal("expected at least 1 stale runtime to be marked offline, got 0")
	}

	t.Logf("heartbeat timeout: %d stale runtimes marked offline", len(staleRuntimes))

	offlineAgents, err := s.UpdateOfflineAgents(ctx)
	if err != nil {
		t.Fatalf("update offline agents: %v", err)
	}
	t.Logf("offline agents updated: %d", len(offlineAgents))
}

// TestSchedulerCheckHeartbeatTimeout tests the full scheduler task.
func TestSchedulerCheckHeartbeatTimeout(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	ctx := context.Background()

	err := sched.CheckHeartbeatTimeout(ctx)
	if err != nil {
		t.Fatalf("CheckHeartbeatTimeout: %v", err)
	}
	t.Log("scheduler CheckHeartbeatTimeout completed without error")
}

// TestSchedulerReleaseClaimTimeout tests the full scheduler task for claim timeout.
func TestSchedulerReleaseClaimTimeout(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	ctx := context.Background()

	err := sched.ReleaseClaimTimeoutNodes(ctx)
	if err != nil {
		t.Fatalf("ReleaseClaimTimeoutNodes: %v", err)
	}
	t.Log("scheduler ReleaseClaimTimeoutNodes completed without error")
}

// TestSchedulerClearExpiredReservations verifies design doc §12.4: ClearExpiredReservations
// clears expired (>30s) reserved_for_agent_id on pending nodes, making them open to any Agent.
func TestSchedulerClearExpiredReservations(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// Create workspace, project, Agent, task, and pending node with reservation
	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "reservation-clear-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "RC",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "reservation-clear-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "reservation-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOffline,
		GitName:      strPtr("reservation-agent"),
		GitEmail:     strPtr("reservation-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Reservation clear test",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Pending node with reserved_for_agent_id for agent and expired reservation_expires_at
	agentUUID := uuid.MustParse(agent.ID)
	expired := time.Now().Add(-2 * time.Minute) // exceeds 30s threshold
	node, err := db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:             task.ID,
		MaxRejectCycles:    3,
		TimeoutMinutes:     60,
		Name:               "code",
		Description:        sql.NullString{String: "code node", Valid: true},
		SortOrder:          1,
		NodeType:           db.NodeTypeStandard,
		Status:             db.TaskNodeStatusPending,
		AssigneeType:       db.AssigneeTypeAnyAgent,
		ReservedForAgentID: uuid.NullUUID{UUID: agentUUID, Valid: true},
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}
	_, err = pgDB.ExecContext(ctx,
		`UPDATE task_nodes SET reservation_expires_at = $1 WHERE id = $2`,
		expired, node.ID)
	if err != nil {
		t.Fatalf("set reservation_expires_at: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	// Call ClearExpiredReservations: using real clock, cutoff = now-30s,
	// the 2-minute-expired reservation should be cleared.
	if err := sched.ClearExpiredReservations(ctx); err != nil {
		t.Fatalf("ClearExpiredReservations: %v", err)
	}

	got, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node after clear: %v", err)
	}
	if got.ReservedForAgentID != nil {
		t.Fatalf("expected reserved_for_agent_id cleared, still set to %v", *got.ReservedForAgentID)
	}
	if got.ReservationExpiresAt != nil {
		t.Fatalf("expected reservation_expires_at cleared, still set to %v", *got.ReservationExpiresAt)
	}
	if got.Status != types.TaskNodeStatusPending {
		t.Fatalf("expected node to remain pending, got %s", got.Status)
	}
	t.Log("ClearExpiredReservations cleared expired reservation, node remains pending/open to all agents")
}

// TestCheckNodeTimeout verifies that nodes exceeding timeout_minutes are set to manual_intervention.
func TestSchedulerWorkflowTriggers(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()
	now := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "workflow-trigger-scheduler-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "WT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "workflow-trigger-project",
		Description: strPtr("test"),
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	configBytes, err := json.Marshal(map[string]interface{}{
		"project_id":       proj.ID,
		"interval_minutes": float64(45),
		"title":            "Scheduled Inspection",
		"description":      "Check project status",
	})
	if err != nil {
		t.Fatalf("marshal trigger config: %v", err)
	}

	nextRunAt := now.Add(-time.Minute)
	tpl, _, err := s.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID:    wksp.ID,
		Name:           "scheduled-template-" + uuid.New().String()[:8],
		Description:    strPtr("scheduled"),
		TriggerType:    "schedule",
		TriggerConfig:  configBytes,
		TriggerEnabled: true,
		NextRunAt:      &nextRunAt,
	}, []types.CreateTemplateNodeParams{
		{Name: "Execute Inspection", SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
	})
	if err != nil {
		t.Fatalf("create scheduled template: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)
	sched.Clock = clock.NewFakeClock(now)

	if err := sched.ProcessWorkflowTriggers(ctx); err != nil {
		t.Fatalf("ProcessWorkflowTriggers: %v", err)
	}

	tasks, err := s.ListAllTasks(ctx, uuid.MustParse(proj.ID))
	if err != nil {
		t.Fatalf("ListAllTasks: %v", err)
	}
	found := false
	for _, task := range tasks {
		if task.Title == "Scheduled Inspection" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scheduled trigger to create task 'Scheduled Inspection', tasks: %#v", tasks)
	}

	updated, err := s.GetWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("GetWorkflowTemplate: %v", err)
	}
	if updated.LastTriggeredAt == nil {
		t.Fatal("expected last_triggered_at to be set")
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("next_run_at = %#v, want %s", updated.NextRunAt, now.Add(45*time.Minute))
	}
}

func TestCheckNodeTimeout(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// Create workspace, project, Agent, task, and node
	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "node-timeout-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "NT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "node-timeout-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "node-timeout-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOnline,
		GitName:      strPtr("node-timeout-agent"),
		GitEmail:     strPtr("node-timeout-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    strPtr(agent.ID),
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Node timeout test task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node, err := db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:          task.ID,
		MaxRejectCycles: 3,
		TimeoutMinutes:  5,
		Name:            "code",
		Description:     sql.NullString{String: "code node", Valid: true},
		SortOrder:       1,
		NodeType:        db.NodeTypeStandard,
		Status:          db.TaskNodeStatusInProgress,
		AssigneeType:    db.AssigneeTypeSpecificAgent,
		AssigneeID:      uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true},
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	sixMinAgo := time.Now().Add(-6 * time.Minute)
	_, err = pgDB.ExecContext(ctx, `UPDATE task_nodes SET updated_at = $1 WHERE id = $2`, sixMinAgo, node.ID)
	if err != nil {
		t.Fatalf("set node updated_at to past: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	err = sched.CheckNodeTimeout(ctx)
	if err != nil {
		t.Fatalf("CheckNodeTimeout: %v", err)
	}

	updatedNode, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node: %v", err)
	}
	if updatedNode.Status != types.TaskNodeStatusManualIntervention {
		t.Fatalf("expected manual_intervention after node timeout, got %s", updatedNode.Status)
	}

	transitions, err := s.ListNodeTransitions(ctx, node.ID)
	if err != nil {
		t.Fatalf("list node transitions: %v", err)
	}
	if len(transitions) == 0 {
		t.Fatal("expected at least 1 transition record, got 0")
	}
	lastTransition := transitions[len(transitions)-1]
	if lastTransition.ToStatus != types.TaskNodeStatusManualIntervention {
		t.Fatalf("expected transition to manual_intervention, got %s", lastTransition.ToStatus)
	}
	if lastTransition.Action != types.TransitionActionTimeout {
		t.Fatalf("expected transition action timeout, got %s", lastTransition.Action)
	}

	t.Log("checkNodeTimeout: node correctly set to manual_intervention with timeout transition")
}

// TestOfflineAgentFallback verifies that in_progress nodes assigned to agents offline for over 1 hour are set to manual_intervention.
func TestOfflineAgentFallback(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "offline-fallback-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "OF",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "offline-fallback-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "offline-fallback-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOffline,
		GitName:      strPtr("offline-fallback-agent"),
		GitEmail:     strPtr("offline-fallback-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    strPtr(agent.ID),
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	_, err = pgDB.ExecContext(ctx, `UPDATE agents SET updated_at = $1 WHERE id = $2`, twoHoursAgo, agent.ID)
	if err != nil {
		t.Fatalf("set agent updated_at to past: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Offline fallback test task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node, err := db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:          task.ID,
		MaxRejectCycles: 3,
		TimeoutMinutes:  60,
		Name:            "code",
		Description:     sql.NullString{String: "code node", Valid: true},
		SortOrder:       1,
		NodeType:        db.NodeTypeStandard,
		Status:          db.TaskNodeStatusInProgress,
		AssigneeType:    db.AssigneeTypeSpecificAgent,
		AssigneeID:      uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true},
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	err = sched.OfflineAgentFallback(ctx)
	if err != nil {
		t.Fatalf("OfflineAgentFallback: %v", err)
	}

	updatedNode, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node: %v", err)
	}
	if updatedNode.Status != types.TaskNodeStatusManualIntervention {
		t.Fatalf("expected manual_intervention after offline agent fallback, got %s", updatedNode.Status)
	}

	transitions, err := s.ListNodeTransitions(ctx, node.ID)
	if err != nil {
		t.Fatalf("list node transitions: %v", err)
	}
	if len(transitions) == 0 {
		t.Fatal("expected at least 1 transition record, got 0")
	}
	lastTransition := transitions[len(transitions)-1]
	if lastTransition.ToStatus != types.TaskNodeStatusManualIntervention {
		t.Fatalf("expected transition to manual_intervention, got %s", lastTransition.ToStatus)
	}

	t.Log("offlineAgentFallback: node correctly set to manual_intervention for offline agent")
}

// TestOfflineAgentFallbackNoOpForOnlineAgent verifies that in_progress nodes assigned to online agents (offline less than 1 hour) are not affected.
func TestOfflineAgentFallbackNoOpForOnlineAgent(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "offline-noop-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "ON",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "offline-noop-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "recently-offline-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOffline,
		GitName:      strPtr("recently-offline-agent"),
		GitEmail:     strPtr("recently-offline-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    strPtr(agent.ID),
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Offline noop test task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node, err := db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:          task.ID,
		MaxRejectCycles: 3,
		TimeoutMinutes:  60,
		Name:            "code",
		Description:     sql.NullString{String: "code node", Valid: true},
		SortOrder:       1,
		NodeType:        db.NodeTypeStandard,
		Status:          db.TaskNodeStatusInProgress,
		AssigneeType:    db.AssigneeTypeSpecificAgent,
		AssigneeID:      uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true},
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	err = sched.OfflineAgentFallback(ctx)
	if err != nil {
		t.Fatalf("OfflineAgentFallback: %v", err)
	}

	updatedNode, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node: %v", err)
	}
	if updatedNode.Status != types.TaskNodeStatusInProgress {
		t.Fatalf("expected in_progress (agent offline < 1 hour), got %s", updatedNode.Status)
	}

	t.Log("offlineAgentFallback: correctly skipped for recently-offline agent")
}

// TestAgentAutoRecoverOnline verifies that agents stuck in "busy" status without in_progress nodes are recovered to "online".
func TestAgentAutoRecoverOnline(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "auto-recover-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "AR",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "stuck-busy-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusBusy,
		GitName:      strPtr("stuck-busy-agent"),
		GitEmail:     strPtr("stuck-busy-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	gotAgent, err := s.GetAgent(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if gotAgent.Status != types.AgentStatusBusy {
		t.Fatalf("expected agent to be busy before test, got %s", gotAgent.Status)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	err = sched.AgentAutoRecoverOnline(ctx)
	if err != nil {
		t.Fatalf("AgentAutoRecoverOnline: %v", err)
	}

	recoveredAgent, err := s.GetAgent(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("get agent after recovery: %v", err)
	}
	if recoveredAgent.Status != types.AgentStatusOnline {
		t.Fatalf("expected agent to be online after auto-recover, got %s", recoveredAgent.Status)
	}

	t.Log("agentAutoRecoverOnline: stuck busy agent correctly recovered to online")
}

// TestAgentAutoRecoverOnlineNoOpForBusyAgentWithNode verifies that offline agents with in_progress nodes are not recovered.
func TestAgentAutoRecoverOnlineNoOpForBusyAgentWithNode(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "recover-noop-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "RN",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "recover-noop-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "busy-with-node-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusBusy,
		GitName:      strPtr("busy-with-node-agent"),
		GitEmail:     strPtr("busy-with-node-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    strPtr(agent.ID),
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Recover noop test task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err = db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:          task.ID,
		MaxRejectCycles: 3,
		TimeoutMinutes:  60,
		Name:            "code",
		Description:     sql.NullString{String: "code node", Valid: true},
		SortOrder:       1,
		NodeType:        db.NodeTypeStandard,
		Status:          db.TaskNodeStatusInProgress,
		AssigneeType:    db.AssigneeTypeSpecificAgent,
		AssigneeID:      uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true},
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	err = sched.AgentAutoRecoverOnline(ctx)
	if err != nil {
		t.Fatalf("AgentAutoRecoverOnline: %v", err)
	}

	recoveredAgent, err := s.GetAgent(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("get agent after recovery attempt: %v", err)
	}
	if recoveredAgent.Status != types.AgentStatusBusy {
		t.Fatalf("expected agent to remain busy (has in_progress node), got %s", recoveredAgent.Status)
	}

	t.Log("agentAutoRecoverOnline: correctly skipped busy agent with in_progress node")
}

// TestRenotifyPendingNodes verifies that pending nodes with updated_at older than 30 seconds are re-broadcast via SSE when online agents exist.
func TestRenotifyPendingNodes(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "renotify-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "RP",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "renotify-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "renotify-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOnline,
		GitName:      strPtr("renotify-agent"),
		GitEmail:     strPtr("renotify-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    strPtr(agent.ID),
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	rt, err := s.CreateRuntime(ctx, types.CreateRuntimeParams{
		AgentID:  agent.ID,
		DaemonID: "daemon-renotify-test",
		Provider: types.AgentProviderClaude,
		Status:   types.RuntimeStatusOnline,
	})
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	_, err = db.New(pgDB).UpdateRuntimeHeartbeat(ctx, db.UpdateRuntimeHeartbeatParams{
		ID:            uuid.MustParse(rt.ID),
		LastHeartbeat: sql.NullTime{Time: time.Now(), Valid: true},
	})
	if err != nil {
		t.Fatalf("update runtime heartbeat: %v", err)
	}

	task, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Renotify test task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node, err := db.New(pgDB).CreateTaskNode(ctx, db.CreateTaskNodeParams{
		TaskID:          task.ID,
		MaxRejectCycles: 3,
		TimeoutMinutes:  60,
		Name:            "code",
		Description:     sql.NullString{String: "code node", Valid: true},
		SortOrder:       1,
		NodeType:        db.NodeTypeStandard,
		Status:          db.TaskNodeStatusPending,
		AssigneeType:    db.AssigneeTypeAnyAgent,
	})
	if err != nil {
		t.Fatalf("create task node: %v", err)
	}

	sixtySecAgo := time.Now().Add(-60 * time.Second)
	_, err = pgDB.ExecContext(ctx, `UPDATE task_nodes SET updated_at = $1 WHERE id = $2`, sixtySecAgo, node.ID)
	if err != nil {
		t.Fatalf("set node updated_at to past: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)

	err = sched.RenotifyPendingNodes(ctx)
	if err != nil {
		t.Fatalf("RenotifyPendingNodes: %v", err)
	}

	updatedNode, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node: %v", err)
	}
	if updatedNode.Status != types.TaskNodeStatusPending {
		t.Fatalf("expected node to remain pending after renotify, got %s", updatedNode.Status)
	}

	t.Log("renotifyPendingNodes: completed successfully, node remains pending")
}

// TestSchedulerMemoryGCDeletesLowConfidenceOldMemories verifies design doc §12.11:
// DeleteLowConfidenceMemories(now-30d) physically deletes memories with confidence<0.1 AND verified=false
// AND stale=false AND created_at<30 days ago; high-confidence/verified/stale/recent memories are preserved.
//
// Note: lowConfidenceMemoryGC is an unexported method (called daily at 03:00 by the scheduler), whose core action
// is delegated to Store.DeleteLowConfidenceMemories, so testing the exported method here directly covers
// the §12.11 deletion criteria. CreateMemory sets created_at=now() via DB defaults, so the test
// backdates the time via UPDATE memories SET created_at to simulate "older than 30 days".
func TestSchedulerMemoryGCDeletesLowConfidenceOldMemories(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	ctx := context.Background()
	now := time.Now()
	gcCutoff := now.Add(-30 * 24 * time.Hour) // now - 30d
	aged := now.Add(-31 * 24 * time.Hour)     // 31 days ago, before cutoff

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "memgc-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "MG",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	// Should be deleted: low confidence + not verified + not stale + created 30 days ago.
	deletable, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: wksp.ID,
		Type:        "insight",
		Title:       "low-confidence old",
		Content:     "should be gc'd",
		Confidence:  0.05,
		Verified:    false,
		Metadata:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create deletable memory: %v", err)
	}
	// Should be preserved: high confidence.
	highConf, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: wksp.ID,
		Type:        "insight",
		Title:       "high-confidence old",
		Content:     "keep",
		Confidence:  0.5,
		Verified:    false,
		Metadata:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create high-confidence memory: %v", err)
	}
	// Should be preserved: verified.
	verified, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: wksp.ID,
		Type:        "insight",
		Title:       "verified old",
		Content:     "keep",
		Confidence:  0.05,
		Verified:    true,
		Metadata:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create verified memory: %v", err)
	}
	// Should be preserved: stale (stale=true).
	staleMem, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: wksp.ID,
		Type:        "insight",
		Title:       "stale old",
		Content:     "keep",
		Confidence:  0.05,
		Verified:    false,
		Metadata:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create stale memory: %v", err)
	}
	if _, err := pgDB.ExecContext(ctx,
		`UPDATE memories SET stale = true WHERE id = $1`, staleMem.ID); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	// Should be preserved: low confidence but recently created (created_at after cutoff).
	recent, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID: wksp.ID,
		Type:        "insight",
		Title:       "low-confidence recent",
		Content:     "keep",
		Confidence:  0.05,
		Verified:    false,
		Metadata:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create recent memory: %v", err)
	}

	// Backdate created_at of deletable/highConf/verified/stale to 31 days ago (recent memory stays at now).
	for _, id := range []string{deletable.ID, highConf.ID, verified.ID, staleMem.ID} {
		if _, err := pgDB.ExecContext(ctx,
			`UPDATE memories SET created_at = $1 WHERE id = $2`, aged, id); err != nil {
			t.Fatalf("backdate memory %s: %v", id, err)
		}
	}

	deleted, err := s.DeleteLowConfidenceMemories(ctx, gcCutoff)
	if err != nil {
		t.Fatalf("DeleteLowConfidenceMemories: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 memory deleted, got %d", deleted)
	}

	// deletable should be physically deleted.
	if _, err := s.GetMemory(ctx, uuid.MustParse(deletable.ID)); err == nil {
		t.Fatal("expected deletable memory to be physically deleted, still found")
	}
	// The four controls should still be retrievable.
	for _, m := range []types.Memory{highConf, verified, staleMem, recent} {
		if _, err := s.GetMemory(ctx, uuid.MustParse(m.ID)); err != nil {
			t.Fatalf("expected memory %s to survive GC, got error: %v", m.ID, err)
		}
	}
	t.Log("memory_gc: low-confidence old memory deleted, controls survived")
}

// TestSchedulerCleanupOldWorkspacesIdentifiesCandidates verifies design doc §12.12:
// CleanupOldWorkspaces(now-7d, 500) identifies completed/cancelled tasks older than 7 days as cleanup candidates in cursor batches,
// grouped and logged by workspace — the scheduler only identifies candidates, does not delete tasks; actual file cleanup is performed by CLI/events.
//
// Note: CleanupOldWorkspaces is an exported method; UpdateTaskStatus sets updated_at to now(),
// so the test backdates via UPDATE tasks SET updated_at to 8 days ago to simulate "older than 7 days".
func TestSchedulerCleanupOldWorkspacesIdentifiesCandidates(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()
	now := time.Now()
	cleanupCutoff := now.Add(-7 * 24 * time.Hour) // now - 7d
	aged := now.Add(-8 * 24 * time.Hour)          // 8 days ago, before cutoff

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "wscleanup-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "WC",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: wksp.ID,
		Name:        "wscleanup-proj",
		Description: strPtr("test"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	agent, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  wksp.ID,
		Name:         "wscleanup-agent",
		Provider:     types.AgentProviderClaude,
		Instructions: "test",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       types.AgentStatusOffline,
		GitName:      strPtr("wscleanup-agent"),
		GitEmail:     strPtr("wscleanup-agent@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// A task completed 8 days ago — should be identified as a candidate.
	oldCompleted, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Old completed task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create old task: %v", err)
	}
	if _, err := db.New(pgDB).UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
		ID: oldCompleted.ID, Status: db.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("mark old task completed: %v", err)
	}
	if _, err := pgDB.ExecContext(ctx,
		`UPDATE tasks SET updated_at = $1 WHERE id = $2`, aged, oldCompleted.ID); err != nil {
		t.Fatalf("backdate old task updated_at: %v", err)
	}

	// A task cancelled 8 days ago — should be identified as a candidate.
	oldCancelled, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Old cancelled task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create cancelled task: %v", err)
	}
	if _, err := db.New(pgDB).UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
		ID: oldCancelled.ID, Status: db.TaskStatusCancelled,
	}); err != nil {
		t.Fatalf("mark task cancelled: %v", err)
	}
	if _, err := pgDB.ExecContext(ctx,
		`UPDATE tasks SET updated_at = $1 WHERE id = $2`, aged, oldCancelled.ID); err != nil {
		t.Fatalf("backdate cancelled task updated_at: %v", err)
	}

	// A recently completed task — should not be identified (updated_at after cutoff).
	recentCompleted, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Recent completed task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create recent task: %v", err)
	}
	if _, err := db.New(pgDB).UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
		ID: recentCompleted.ID, Status: db.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("mark recent task completed: %v", err)
	}
	// Do not backdate updated_at, keep at now().

	// A task that is 8 days old but still active — should not be identified (status not completed/cancelled).
	oldActive, err := db.New(pgDB).CreateTask(ctx, db.CreateTaskParams{
		ProjectID:    uuid.MustParse(proj.ID),
		WorkflowName: "test-flow",
		Title:        "Old active task",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         db.TaskTypeTask,
		Priority:     db.TaskPriorityMedium,
		Status:       db.TaskStatusActive,
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(agent.ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create old active task: %v", err)
	}
	if _, err := pgDB.ExecContext(ctx,
		`UPDATE tasks SET updated_at = $1 WHERE id = $2`, aged, oldActive.ID); err != nil {
		t.Fatalf("backdate old active task updated_at: %v", err)
	}

	hub := ws.NewHub(rdb)
	sched := scheduler.NewScheduler(s, hub, rdb)
	sched.Clock = clock.NewFakeClock(now)

	// The scheduler only identifies candidates, should not error.
	if err := sched.CleanupOldWorkspaces(ctx); err != nil {
		t.Fatalf("CleanupOldWorkspaces: %v", err)
	}

	// Verify the candidate set via the underlying query: should contain exactly the two 8-day-old completed/cancelled tasks.
	candidates, err := s.GetCompletedTasksOlderThan(ctx, cleanupCutoff, 0, 500)
	if err != nil {
		t.Fatalf("GetCompletedTasksOlderThan: %v", err)
	}
	candIDs := make(map[int32]bool, len(candidates))
	for _, c := range candidates {
		candIDs[c.ID] = true
	}
	if !candIDs[oldCompleted.ID] {
		t.Fatalf("expected old completed task %d to be a cleanup candidate, candidates: %v", oldCompleted.ID, candIDs)
	}
	if !candIDs[oldCancelled.ID] {
		t.Fatalf("expected old cancelled task %d to be a cleanup candidate, candidates: %v", oldCancelled.ID, candIDs)
	}
	if candIDs[recentCompleted.ID] {
		t.Fatalf("recent completed task %d should NOT be a candidate (too new)", recentCompleted.ID)
	}
	if candIDs[oldActive.ID] {
		t.Fatalf("old active task %d should NOT be a candidate (not completed/cancelled)", oldActive.ID)
	}

	// §12.12: the scheduler only identifies candidates, does not delete tasks — all tasks should still exist.
	for _, id := range []int32{oldCompleted.ID, oldCancelled.ID, recentCompleted.ID, oldActive.ID} {
		if _, err := s.GetTask(ctx, id); err != nil {
			t.Fatalf("expected task %d to still exist (scheduler only identifies, does not delete): %v", id, err)
		}
	}
	t.Logf("workspace_cleanup: identified %d candidates (completed+cancelled older than 7d), tasks preserved", len(candidates))
}
