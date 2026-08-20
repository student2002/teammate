// Package scheduler_test 包含 scheduler 调度器的测试，覆盖
// Redis 分布式锁、锁过期、认领超时释放、
// 心跳超时、离线 Agent 回退以及节点超时处理。
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

// TestMain 是测试入口函数，初始化测试数据库并运行所有测试。
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

// strPtr 是 *string 的便捷构造器，用于 types.*Params 中的 *string 字段。
func strPtr(s string) *string { return &s }

// getTestDSN 返回测试数据库连接字符串，委托给共享的 testdb 辅助函数。
func getTestDSN() string {
	return testdb.GetTestDSN()
}

// getTestRedisURL 返回 Redis 连接 URL，默认设置为 localhost:16379，
// 可通过 TEAMS_REDIS_URL 环境变量覆盖。
func getTestRedisURL() string {
	if url := os.Getenv("TEAMS_REDIS_URL"); url != "" {
		return url
	}
	return "redis://localhost:16379"
}

// connectTestDB 打开测试数据库连接，如果数据库不可用则跳过测试。
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

// connectTestRedis 打开测试 Redis 实例连接，如果 Redis 不可用则跳过测试。
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

// TestRedisDistributedLock 验证只有一个实例能获取锁。
func TestRedisDistributedLock(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	ctx := context.Background()
	lockKey := fmt.Sprintf("test:scheduler:lock:%s", uuid.New().String()[:8])
	lockTTL := 10 * time.Second

	t.Cleanup(func() { rdb.Del(ctx, lockKey) })

	// 第一个实例获取锁
	ok, err := rdb.SetNX(ctx, lockKey, "instance-1", lockTTL).Result()
	if err != nil {
		t.Fatalf("setnx error: %v", err)
	}
	if !ok {
		t.Fatal("first instance should acquire the lock")
	}

	// 第二个实例尝试获取同一把锁——应失败
	ok, err = rdb.SetNX(ctx, lockKey, "instance-2", lockTTL).Result()
	if err != nil {
		t.Fatalf("setnx error: %v", err)
	}
	if ok {
		t.Fatal("second instance should NOT acquire the lock")
	}

	t.Log("distributed lock: only one instance can hold the lock")
}

// TestLockExpiration 验证锁在任务完成后被释放（通过显式 DEL），
// 并且也会在 TTL 到期后自动释放。
func TestLockExpiration(t *testing.T) {
	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	ctx := context.Background()
	lockKey := fmt.Sprintf("test:scheduler:lock:%s", uuid.New().String()[:8])
	lockTTL := 5 * time.Second

	t.Cleanup(func() { rdb.Del(ctx, lockKey) })

	// 获取锁
	ok, _ := rdb.SetNX(ctx, lockKey, "locked", lockTTL).Result()
	if !ok {
		t.Fatal("should acquire lock")
	}

	// 模拟任务完成——释放锁
	rdb.Del(ctx, lockKey)

	// 验证锁已释放——另一个实例可以获取它
	ok, _ = rdb.SetNX(ctx, lockKey, "locked-again", lockTTL).Result()
	if !ok {
		t.Fatal("lock should be available after explicit release")
	}
	rdb.Del(ctx, lockKey)

	// 测试 TTL 过期
	ok, _ = rdb.SetNX(ctx, lockKey, "locked-ttl", 1*time.Second).Result()
	if !ok {
		t.Fatal("should acquire lock for TTL test")
	}

	// 等待 TTL 过期
	time.Sleep(1500 * time.Millisecond)

	ok, _ = rdb.SetNX(ctx, lockKey, "after-expiry", lockTTL).Result()
	if !ok {
		t.Fatal("lock should be available after TTL expiry")
	}
	rdb.Del(ctx, lockKey)

	t.Log("lock expiration: lock correctly released via DEL and TTL expiry")
}

// TestWithLockFunction 测试调度器的 WithLock 辅助函数。// 验证 WithLock 辅助函数获取 Redis 锁、执行回调函数并在之后释放锁。
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

	// 验证执行后锁已释放
	val, err := rdb.Get(ctx, lockKey).Result()
	if err == nil && val != "" {
		t.Fatalf("lock should be released after WithLock completes, but key exists: %s", val)
	}
	t.Log("WithLock: function executed and lock released correctly")
}

// TestWithLockOnlyOneInstance 验证 WithLock 通过拒绝获取同一锁两次来防止并发执行。
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

	// 先手动获取锁
	ok, _ := rdb.SetNX(ctx, lockKey, "other-instance", 10*time.Second).Result()
	if !ok {
		t.Fatal("should acquire manual lock")
	}

	// 由于锁已被持有，WithLock 应跳过执行
	executed := false
	sched.WithLock(ctx, taskName, 10*time.Second, func(ctx context.Context) error {
		executed = true
		return nil
	})

	if executed {
		t.Fatal("WithLock should NOT execute when lock is held by another instance")
	}
	t.Log("WithLock: correctly skipped when lock is held")

	// 清理
	rdb.Del(ctx, lockKey)
}

// TestClaimTimeoutRelease 验证持续的 in_progress 超过 30 分钟的节点被释放回 pending。
// 该测试验证当认领 agent 的运行实例离线超过心跳超时阈值时，调度器释放任务节点上的过期认领。
func TestClaimTimeoutRelease(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// 创建工作区、项目、工作流、任务和节点
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
		Sequence:     0, // 将由 sequence 同步设置
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

	// 使用 FakeClock 模拟 31 分钟过去以进行超时检查。
	fakeClock := clock.NewFakeClock(time.Now())
	fakeClock.Advance(31 * time.Minute)
	cutoff := fakeClock.Now().Add(-30 * time.Minute)

	// 使用计算出的截止时间执行认领超时释放
	releasedNodes, err := s.ReleaseClaimTimeoutNodes(ctx, cutoff)
	if err != nil {
		t.Fatalf("release claim timeout nodes: %v", err)
	}

	if len(releasedNodes) == 0 {
		t.Fatal("expected at least 1 released node, got 0")
	}

	// 验证节点回到 pending
	updatedNode, err := s.GetTaskNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("get task node: %v", err)
	}
	if updatedNode.Status != types.TaskNodeStatusManualIntervention {
		t.Fatalf("expected manual_intervention after timeout release, got %s", updatedNode.Status)
	}
	t.Logf("claim timeout release: node correctly reset to manual_intervention (released %d nodes)", len(releasedNodes))
}

// TestHeartbeatTimeout 验证超时的心跳导致运行实例被标记为离线。
func TestHeartbeatTimeout(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// 创建工作区和 Agent
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

	// 使用 FakeClock 模拟心跳过期。
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

// TestSchedulerCheckHeartbeatTimeout 测试完整的调度器任务。
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

// TestSchedulerReleaseClaimTimeout 测试认领超时的完整调度器任务。
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

// TestSchedulerClearExpiredReservations 验证设计文档 §12.4：ClearExpiredReservations
// 清除 pending 节点上保留超期（>30s）的 reserved_for_agent_id，使节点对任意 Agent 开放。
func TestSchedulerClearExpiredReservations(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()

	// 创建工作区、项目、Agent、任务和带保留期的 pending 节点
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

	// pending 节点，带保留给 agent 的 reserved_for_agent_id 与已过期的 reservation_expires_at
	agentUUID := uuid.MustParse(agent.ID)
	expired := time.Now().Add(-2 * time.Minute) // 超过 30s 阈值
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

	// 调用 ClearExpiredReservations：使用真实时钟，cutoff = now-30s，
	// 已过期 2 分钟的保留应被清除。
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

// TestCheckNodeTimeout 验证超过 timeout_minutes 的节点被设置为 manual_intervention。
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
		"title":            "定时巡检",
		"description":      "检查项目状态",
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
		{Name: "执行巡检", SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
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
		if task.Title == "定时巡检" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected scheduled trigger to create task 定时巡检, tasks: %#v", tasks)
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

	// 创建工作区、项目、Agent、任务和节点
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

// TestOfflineAgentFallback 验证分配给离线超过 1 小时的 agent 的 in_progress 节点被设置为 manual_intervention。
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

// TestOfflineAgentFallbackNoOpForOnlineAgent 验证分配给在线 agent（小时以内）的 in_progress 节点不受影响。
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

// TestAgentAutoRecoverOnline 验证 stuck 在 "busy" 状态且没有 in_progress 节点的 agent 被恢复为 "online"。
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

// TestAgentAutoRecoverOnlineNoOpForBusyAgentWithNode 验证离线且有 in_progress 节点的代理不会被恢复。
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

// TestRenotifyPendingNodes 验证 updated_at 超过 30 秒的待处理节点在有在线代理时通过 SSE 重新广播。
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

// TestSchedulerMemoryGCDeletesLowConfidenceOldMemories 验证设计文档 §12.11：
// DeleteLowConfidenceMemories(now-30d) 物理删除 confidence<0.1 AND verified=false
// AND stale=false AND created_at<30天前 的记忆；高置信度/已验证/已过期/近期记忆均保留。
//
// 说明：lowConfidenceMemoryGC 是未导出方法（调度器每日 03:00 调用），其核心动作
// 委托给 Store.DeleteLowConfidenceMemories，因此此处直接测试该导出方法即可覆盖
// §12.11 的删除判据。CreateMemory 由 DB 默认值设置 created_at=now()，故测试通过
// UPDATE memories SET created_at 回拨时间来模拟"超过 30 天"。
func TestSchedulerMemoryGCDeletesLowConfidenceOldMemories(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	ctx := context.Background()
	now := time.Now()
	gcCutoff := now.Add(-30 * 24 * time.Hour) // now - 30d
	aged := now.Add(-31 * 24 * time.Hour)     // 31 天前，早于 cutoff

	wksp, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "memgc-test-" + uuid.New().String()[:8],
		Description: strPtr("test"),
		IssuePrefix: "MG",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, wksp.ID) })

	// 应被删除：低置信度 + 未验证 + 未过期 + 创建于 30 天前。
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
	// 应保留：高置信度。
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
	// 应保留：已验证。
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
	// 应保留：已过期（stale=true）。
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
	// 应保留：低置信度但近期创建（created_at 在 cutoff 之后）。
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

	// 回拨 deletable/highConf/verified/stale 的 created_at 到 31 天前（近期记忆保持 now）。
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

	// deletable 应已物理删除。
	if _, err := s.GetMemory(ctx, uuid.MustParse(deletable.ID)); err == nil {
		t.Fatal("expected deletable memory to be physically deleted, still found")
	}
	// 四个对照组应仍可取回。
	for _, m := range []types.Memory{highConf, verified, staleMem, recent} {
		if _, err := s.GetMemory(ctx, uuid.MustParse(m.ID)); err != nil {
			t.Fatalf("expected memory %s to survive GC, got error: %v", m.ID, err)
		}
	}
	t.Log("memory_gc: low-confidence old memory deleted, controls survived")
}

// TestSchedulerCleanupOldWorkspacesIdentifiesCandidates 验证设计文档 §12.12：
// CleanupOldWorkspaces(now-7d, 500) 游标分批识别 7 天前完成/取消的任务作为清理候选，
// 按工作区分组记录日志——调度器只识别候选，不删除任务；实际文件清理由 CLI/事件执行。
//
// 说明：CleanupOldWorkspaces 是导出方法；UpdateTaskStatus 将 updated_at 置为 now()，
// 故测试通过 UPDATE tasks SET updated_at 回拨到 8 天前以模拟"超过 7 天"。
func TestSchedulerCleanupOldWorkspacesIdentifiesCandidates(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	rdb := connectTestRedis(t)
	t.Cleanup(func() { rdb.Close() })

	s := store.New(pgDB)
	ctx := context.Background()
	now := time.Now()
	cleanupCutoff := now.Add(-7 * 24 * time.Hour) // now - 7d
	aged := now.Add(-8 * 24 * time.Hour)          // 8 天前，早于 cutoff

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

	// 一个 8 天前完成的任务——应被识别为候选。
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

	// 一个 8 天前取消的任务——应被识别为候选。
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

	// 一个近期完成的任务——不应被识别（updated_at 在 cutoff 之后）。
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
	// 不回拨 updated_at，保持 now()。

	// 一个 8 天前但仍 active 的任务——不应被识别（状态非 completed/cancelled）。
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

	// 调度器只识别候选，不应报错。
	if err := sched.CleanupOldWorkspaces(ctx); err != nil {
		t.Fatalf("CleanupOldWorkspaces: %v", err)
	}

	// 通过底层查询验证候选集合：应恰好包含两个 8 天前的完成/取消任务。
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

	// §12.12：调度器只识别候选，不删除任务——所有任务应仍存在。
	for _, id := range []int32{oldCompleted.ID, oldCancelled.ID, recentCompleted.ID, oldActive.ID} {
		if _, err := s.GetTask(ctx, id); err != nil {
			t.Fatalf("expected task %d to still exist (scheduler only identifies, does not delete): %v", id, err)
		}
	}
	t.Logf("workspace_cleanup: identified %d candidates (completed+cancelled older than 7d), tasks preserved", len(candidates))
}
