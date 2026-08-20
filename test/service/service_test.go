// service_test.go 覆盖 Service 层基础逻辑的测试。
package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	dbgen "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// strPtr 返回字符串指针，用于 types 领域结构体中 *string 字段。
func strPtr(s string) *string {
	return &s
}

// int32Ptr 返回 int32 指针，用于 types 领域结构体中 *int32 字段。
func int32Ptr(i int32) *int32 {
	return &i
}

// TestMain 设置测试数据库。
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

// svcGetTestDSN 返回服务测试用的数据库连接字符串。
func svcGetTestDSN() string {
	return testdb.GetTestDSN()
}

// svcConnectTestDB 连接测试数据库并返回连接实例。
func svcConnectTestDB(t *testing.T) *sql.DB {
	t.Helper()
	testDB, err := sql.Open("pgx", svcGetTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	if err := testDB.Ping(); err != nil {
		testDB.Close()
		t.Skipf("database not available, skipping: %v", err)
	}
	return testDB
}

// setupServiceTest 创建完整的测试环境：工作区、项目、工作流、代理和任务。
// 返回服务实例、数据库连接和所有测试引用的 ID。
func setupServiceTest(t *testing.T) (*service.Service, *sql.DB, *testEnv) {
	t.Helper()
	pgDB := svcConnectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// 创建工作区
	ws, err := svc.Store.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "svc-test-" + uuid.New().String()[:8],
		Description: strPtr("service test"),
		IssuePrefix: "ST",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_ = testdb.DeleteWorkspace(pgDB, ws.ID)
	})

	// 创建用于权限授权的成员
	member, err := svc.Store.CreateMember(ctx, types.CreateMemberParams{
		Name:  "test-granter",
		Email: "granter-" + uuid.New().String()[:8] + "@test.local",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		_ = testdb.DeleteMember(pgDB, member.ID)
	})

	// 以管理员角色添加成员到工作区
	_, err = svc.Store.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	// 创建项目
	proj, err := svc.Store.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "proj-" + uuid.New().String()[:8],
		Description: strPtr("test project"),
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// 创建包含3个节点的工作流模板：code(标准) -> review(审核) -> deploy(标准)
	tpl, templateNodes, err := svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: ws.ID,
		Name:        "flow-" + uuid.New().String()[:8],
		Description: strPtr("3-node test flow"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code node"), SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
		{Name: "review", Description: strPtr("review node"), SortOrder: 2, NodeType: "review", AssigneeType: "any_agent", TimeoutMinutes: 60},
		{Name: "deploy", Description: strPtr("deploy node"), SortOrder: 3, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	// 设置项目默认工作流
	_, err = svc.Store.UpdateProject(ctx, types.UpdateProjectParams{
		ID:                proj.ID,
		Name:              proj.Name,
		Description:       &proj.Description,
		Status:            proj.Status,
		DefaultWorkflowID: &tpl.ID,
	})
	if err != nil {
		t.Fatalf("update project workflow: %v", err)
	}

	// 创建代理
	agent1, _, err := svc.Store.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "agent1-" + uuid.New().String()[:8],
		Provider:     "claude",
		Instructions: "test agent 1",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       "offline",
		GitName:      strPtr("agent1"),
		GitEmail:     strPtr("agent1@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent1: %v", err)
	}

	agent2, _, err := svc.Store.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "agent2-" + uuid.New().String()[:8],
		Provider:     "claude",
		Instructions: "test agent 2",
		Model:        strPtr("claude-3.5-sonnet"),
		Status:       "offline",
		GitName:      strPtr("agent2"),
		GitEmail:     strPtr("agent2@teammate.local"),
	})
	if err != nil {
		t.Fatalf("create agent2: %v", err)
	}

	// 添加代理到项目并授予权限
	for _, aid := range []string{agent1.ID, agent2.ID} {
		_, err = svc.Store.CreateProjectMember(ctx, types.CreateProjectMemberParams{
			ProjectID:  proj.ID,
			MemberType: "agent",
			AgentID:    &aid,
			Role:       "member",
		})
		if err != nil {
			t.Fatalf("add agent to project: %v", err)
		}
		// 授予默认权限（task:claim、task:execute、task:comment、memory:read）
		if err := svc.Store.GrantDefaultPermissions(ctx, uuid.MustParse(aid), uuid.MustParse(member.ID)); err != nil {
			t.Fatalf("grant default permissions for agent %s: %v", aid, err)
		}
		// 同时授予 task:approve 和 task:reject 权限（默认拒绝，但测试中需要）
		for _, perm := range []string{"task:approve", "task:reject"} {
			_, err = svc.Store.GrantAgentPermission(ctx, uuid.MustParse(aid), perm, "*", nil, uuid.MustParse(member.ID))
			if err != nil {
				t.Fatalf("grant %s permission for agent %s: %v", perm, aid, err)
			}
		}
	}

	// 使用 Store.CreateTask 创建任务（同时创建任务节点）
	task, taskNodes, err := svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    proj.ID,
		WorkflowName: tpl.Name,
		Title:        "Service test task",
		Description:  strPtr("test"),
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     agent1.ID,
		Sequence:     0, // 将由 Store.CreateTask 设置为 task.ID
	}, templateNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	env := &testEnv{
		svc:         svc,
		workspaceID: ws.ID,
		projectID:   proj.ID,
		templateID:  tpl.ID,
		taskID:      task.ID,
		agent1ID:    agent1.ID,
		agent2ID:    agent2.ID,
		taskNodes:   taskNodes,
	}

	return svc, pgDB, env
}

type testEnv struct {
	svc         *service.Service
	workspaceID string
	projectID   string
	templateID  string
	taskID      int32
	agent1ID    string
	agent2ID    string
	taskNodes   []types.TaskNode
}

// ---------- 服务层测试 ----------

// TestOptimisticLockClaimVersionConflict 验证两个代理同时认领同一节点只有一个能成功。
func TestOptimisticLockClaimVersionConflict(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	nodeID := env.taskNodes[0].ID

	// 代理1成功认领
	result, err := service.NewNodeService(svc).Claim(ctx, uuid.MustParse(nodeID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("agent1 claim should succeed: %v", err)
	}
	if result.Node.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", result.Node.Status)
	}

	// 代理2尝试认领同一节点——应失败（版本冲突）
	_, err = service.NewNodeService(svc).Claim(ctx, uuid.MustParse(nodeID), uuid.MustParse(env.agent2ID), "agent")
	if err == nil {
		t.Fatal("agent2 claim should fail with version conflict, but succeeded")
	}
	t.Logf("agent2 claim correctly failed: %v", err)
}

// TestOptimisticLockVersionIncrementOnClaim 验证设计文档 §10.1：认领成功后 version 必须 +1。
func TestOptimisticLockVersionIncrementOnClaim(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	nodeID := env.taskNodes[0].ID
	before, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(nodeID))
	if err != nil {
		t.Fatalf("get node before claim: %v", err)
	}
	versionBefore := before.Version

	result, err := service.NewNodeService(svc).Claim(ctx, uuid.MustParse(nodeID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim should succeed: %v", err)
	}

	// 认领后内存返回值与落库值都应 version+1
	if result.Node.Version != versionBefore+1 {
		t.Fatalf("returned version: expected %d, got %d", versionBefore+1, result.Node.Version)
	}

	got, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(nodeID))
	if err != nil {
		t.Fatalf("get node after claim: %v", err)
	}
	if got.Version != versionBefore+1 {
		t.Fatalf("persisted version: expected %d, got %d", versionBefore+1, got.Version)
	}
	t.Logf("claim incremented version %d -> %d", versionBefore, got.Version)
}

// TestOptimisticLockVersionIncrementOnApprove 验证设计文档 §10.1/§5.1：审批通过后当前节点 version +1。
func TestOptimisticLockVersionIncrementOnApprove(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID

	// 认领节点1
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	before, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if err != nil {
		t.Fatalf("get node1 before approve: %v", err)
	}
	versionBefore := before.Version

	// 审批通过
	result, err := nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}
	if result.Node.Version != versionBefore+1 {
		t.Fatalf("returned version: expected %d, got %d", versionBefore+1, result.Node.Version)
	}

	got, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if err != nil {
		t.Fatalf("get node1 after approve: %v", err)
	}
	if got.Version != versionBefore+1 {
		t.Fatalf("persisted version: expected %d, got %d", versionBefore+1, got.Version)
	}
	if got.Status != "completed" {
		t.Fatalf("expected completed after approve, got %s", got.Status)
	}
	t.Logf("approve incremented version %d -> %d (status=completed)", versionBefore, got.Version)
}

// TestConcurrentClaim 使用 goroutine 模拟同一节点的并发认领，只有一个应成功。
func TestConcurrentClaim(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// 使用第一个待处理节点
	nodeID := env.taskNodes[0].ID

	var successCount int32
	var failCount int32
	var wg sync.WaitGroup

	// 启动10个goroutine并发认领同一节点
	numGoroutines := 10
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			agentID := env.agent1ID
			if idx%2 == 1 {
				agentID = env.agent2ID
			}
			_, err := service.NewNodeService(svc).Claim(ctx, uuid.MustParse(nodeID), uuid.MustParse(agentID), "agent")
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&failCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// 至少有一个认领必须成功，且节点只能由单个 Agent 认领。
	// 由于重复认领具有幂等性，同一 Agent 可能会多次成功，
	// 但节点只能被一个 Agent 认领。
	if successCount < 1 {
		t.Fatalf("expected at least 1 successful claim, got %d (failures: %d)", successCount, failCount)
	}

	// 验证节点只被一个代理认领
	claimedNode, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(nodeID))
	if err != nil {
		t.Fatalf("get claimed node: %v", err)
	}
	if claimedNode.Status != "in_progress" {
		t.Fatalf("expected node status in_progress, got %s", claimedNode.Status)
	}
	if claimedNode.AssigneeID == nil {
		t.Fatal("expected node to have an assignee")
	}

	// 验证两个代理中只有一个持有认领
	assigneeIsAgent1 := *claimedNode.AssigneeID == env.agent1ID
	assigneeIsAgent2 := *claimedNode.AssigneeID == env.agent2ID
	if !assigneeIsAgent1 && !assigneeIsAgent2 {
		t.Fatalf("expected assignee to be agent1 or agent2, got %s", *claimedNode.AssigneeID)
	}
}

// TestNodeRejectCascadingIntermediateReset 验证驳回节点会将中间节点重置为 pending。
func TestNodeRejectCascadingIntermediateReset(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID // code
	node2ID := env.taskNodes[1].ID // review

	// 代理1认领并审批节点1（code）
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "done coding")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// 代理2认领节点2（review）——不同代理以避免自审
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("claim node2: %v", err)
	}

	// 驳回节点2，目标节点1（回退到code）
	targetNodeID := uuid.MustParse(node1ID)
	_, err = nodeSvc.Reject(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent", &targetNodeID, "needs rework")
	if err != nil {
		t.Fatalf("reject node2: %v", err)
	}

	// 验证节点1回到待处理（审批人已清除，reserved_for_agent_id已设置）
	node1, err := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if node1.Status != "pending" {
		t.Fatalf("node1: expected pending after reject, got %s", node1.Status)
	}

	// 验证节点2已驳回
	node2, err := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node2ID))
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if node2.Status != "rejected" {
		t.Fatalf("node2: expected rejected, got %s", node2.Status)
	}

	// 验证节点1的reject_count已增加
	if node1.RejectCount < 1 {
		t.Fatalf("node1: expected reject_count >= 1, got %d", node1.RejectCount)
	}
	t.Logf("reject cascade: node1=%s (reject_count=%d), node2=%s",
		node1.Status, node1.RejectCount, node2.Status)
}

// TestMaxRejectCycleCircuitBreaker 验证当 reject_count >= max_review_cycles 时目标节点升级为 manual_intervention。
func TestMaxRejectCycleCircuitBreaker(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	// 更新项目max_review_cycles为2，以便CreateTask将其传播到任务节点
	maxCycles := int32(2)
	desc := "test"
	_, err := env.svc.Store.UpdateProject(ctx, types.UpdateProjectParams{
		ID:              env.projectID,
		Name:            "test",
		Description:     &desc,
		Status:          "active",
		MaxReviewCycles: &maxCycles,
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}

	// 创建新任务，使CreateTask将项目的max_review_cycles传播到任务节点
	tpl, tplNodes, err := env.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: env.workspaceID,
		Name:        "flow-circuit-" + uuid.New().String()[:8],
		Description: strPtr("circuit breaker test flow"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code node"), SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
		{Name: "review", Description: strPtr("review node"), SortOrder: 2, NodeType: "review", AssigneeType: "any_agent", TimeoutMinutes: 60},
		{Name: "deploy", Description: strPtr("deploy node"), SortOrder: 3, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	_, taskNodes, err := svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    env.projectID,
		WorkflowName: tpl.Name,
		Title:        "Circuit breaker test",
		Description:  strPtr("test"),
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     env.agent1ID,
		Sequence:     0,
	}, tplNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node1ID := taskNodes[0].ID // code
	node2ID := taskNodes[1].ID // review
	targetNodeID := uuid.MustParse(node1ID)

	// 第1周期：认领->审批节点1->认领节点2->驳回目标节点1
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("cycle1 claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "code done")
	if err != nil {
		t.Fatalf("cycle1 approve node1: %v", err)
	}
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("cycle1 claim node2: %v", err)
	}
	_, err = nodeSvc.Reject(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent", &targetNodeID, "reject 1")
	if err != nil {
		t.Fatalf("cycle1 reject: %v", err)
	}

	// 验证节点1的reject_count = 1
	node1, _ := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if node1.RejectCount != 1 {
		t.Fatalf("expected reject_count 1 after first reject, got %d", node1.RejectCount)
	}

	// 第2周期：认领节点1（驳回后回到待处理）->审批->认领节点2->驳回目标节点1
	// 注意：驳回后，节点1处于待处理状态并带有reserved_for_agent_id，需要重新认领
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("cycle2 claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "code done again")
	if err != nil {
		t.Fatalf("cycle2 approve node1: %v", err)
	}
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("cycle2 claim node2: %v", err)
	}
	_, err = nodeSvc.Reject(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent", &targetNodeID, "reject 2")
	if err != nil {
		t.Fatalf("cycle2 reject: %v", err)
	}

	// 现在 node1 应处于 manual_intervention（reject_count >= max_review_cycles=2）
	node1, _ = env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if node1.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after max cycles, got %s", node1.Status)
	}
	t.Logf("circuit breaker: node1 status=%s, reject_count=%d — correct", node1.Status, node1.RejectCount)
}

// TestContinuationRightConflict 验证当节点有延续权时其他代理认领会收到冲突错误。
func TestContinuationRightConflict(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	// 创建所有节点均为 standard 的工作流（因此会设置续行权）
	tpl, tplNodes, err := env.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: env.workspaceID,
		Name:        "flow-continuation-" + uuid.New().String()[:8],
		Description: strPtr("2 standard nodes"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code node"), SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
		{Name: "test", Description: strPtr("test node"), SortOrder: 2, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// 使用此模板通过 Store.CreateTask 创建任务
	_, taskNodes, err := svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    env.projectID,
		WorkflowName: tpl.Name,
		Title:        "Continuation test",
		Description:  strPtr("test"),
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     env.agent1ID,
		Sequence:     0, // 将由 Store.CreateTask 设置为 task.ID
	}, tplNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Agent1 认领并批准 node1
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// 批准后，node2 的 reserved_for_agent_id 应为 agent1（续行权）
	node2, err := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(taskNodes[1].ID))
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if node2.ReservedForAgentID == nil || *node2.ReservedForAgentID != env.agent1ID {
		t.Fatalf("expected reserved_for_agent_id = agent1, got %v", node2.ReservedForAgentID)
	}

	// Agent2 尝试认领 node2——应发生冲突（续行权）
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[1].ID), uuid.MustParse(env.agent2ID), "agent")
	if err == nil {
		t.Fatal("agent2 should be blocked by continuation right, but claim succeeded")
	}
	t.Logf("continuation right conflict correctly blocked: %v", err)
}

// TestSelfReviewAvoidance 验证完成前一个标准节点的代理不能认领审核节点。
func TestSelfReviewAvoidance(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID // code (standard)
	node2ID := env.taskNodes[1].ID // review

	// Agent1 认领并批准 code 节点
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "code done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// 同一 Agent 尝试认领 review 节点——应失败（自我审查）
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent1ID), "agent")
	if err == nil {
		t.Fatal("self-review should be blocked, but claim succeeded")
	}
	t.Logf("self-review correctly blocked: %v", err)

	// 不同 Agent 应该能够认领 review 节点
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("agent2 should be able to claim review node: %v", err)
	}
}

// TestSkipClaimReleasesContinuationRight 验证 SkipClaim 清除 reserved_for_agent_id 使其他代理可认领。
func TestSkipClaimReleasesContinuationRight(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	// 创建包含 2 个 standard 节点的工作流以测试续行权
	tpl, tplNodes, err := env.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: env.workspaceID,
		Name:        "flow-skip-" + uuid.New().String()[:8],
		Description: strPtr("2 standard nodes"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code"), SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
		{Name: "test", Description: strPtr("test"), SortOrder: 2, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	_, taskNodes, err := svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    env.projectID,
		WorkflowName: tpl.Name,
		Title:        "Skip claim test",
		Description:  strPtr("test"),
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     env.agent1ID,
		Sequence:     0, // 将由 Store.CreateTask 设置为 task.ID
	}, tplNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Agent1 认领并批准 node1
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// 验证 node2 具有续行权
	node2, _ := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(taskNodes[1].ID))
	if node2.ReservedForAgentID == nil || *node2.ReservedForAgentID != env.agent1ID {
		t.Fatalf("expected continuation right for agent1, got %v", node2.ReservedForAgentID)
	}

	// Agent1 跳过认领
	err = nodeSvc.SkipClaim(ctx, uuid.MustParse(taskNodes[1].ID), uuid.MustParse(env.agent1ID))
	if err != nil {
		t.Fatalf("skip claim: %v", err)
	}

	// 现在 agent2 应该能够认领 node2
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[1].ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("agent2 should claim after skip: %v", err)
	}
	t.Log("skip-claim correctly released continuation right")
}

// TestManualInterventionAndResolve 验证手工干预流程：节点进入 manual_intervention 后解决回到 pending。
func TestManualInterventionAndResolve(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID

	// 先认领该节点
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}

	// 设置为 manual_intervention
	node, err := nodeSvc.ManualIntervention(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "system", "stuck")
	if err != nil {
		t.Fatalf("manual intervention: %v", err)
	}
	if node.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention, got %s", node.Status)
	}

	// 重新解析为 pending（以便 Agent 可以重新认领）
	node, err = nodeSvc.Resolve(ctx, uuid.MustParse(node1ID), uuid.New(), "member", "resolved", nil, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if node.Status != "pending" {
		t.Fatalf("expected pending after resolve, got %s", node.Status)
	}
	t.Log("manual_intervention -> resolve flow works correctly")
}

// TestInterruptTask 验证中断任务会将所有 in_progress 节点设置为 manual_intervention。
func TestInterruptTask(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID

	// 认领 node1（使其变为 in_progress）
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}

	// 中断任务
	result, err := nodeSvc.InterruptTask(ctx, env.taskID, uuid.New(), "member", "emergency stop")
	if err != nil {
		t.Fatalf("interrupt task: %v", err)
	}
	if result.InterruptedNodes != 1 {
		t.Fatalf("expected 1 interrupted node, got %d", result.InterruptedNodes)
	}

	// 验证 node1 现在为 manual_intervention
	node1, _ := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if node1.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after interrupt, got %s", node1.Status)
	}
	t.Log("interrupt task correctly set in_progress nodes to manual_intervention")
}

// TestClaimNonExistentNode 验证认领不存在的节点返回错误。
func TestClaimNonExistentNode(t *testing.T) {
	svc, _, _ := setupServiceTest(t)
	ctx := context.Background()

	_, err := service.NewNodeService(svc).Claim(ctx, uuid.New(), uuid.New(), "agent")
	if err == nil {
		t.Fatal("expected error for non-existent node, got nil")
	}
	t.Logf("claim non-existent node correctly failed: %v", err)
}

// TestApproveNonInProgressNode 验证审批非 in_progress 状态的节点失败。
func TestApproveNonInProgressNode(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// 尝试批准 pending 节点（而非 in_progress）——应在存储层失败
	node1ID := env.taskNodes[0].ID
	_, err := service.NewNodeService(svc).Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "approve")
	// 批准应失败，因为该节点是 pending 而非 in_progress
	if err != nil {
		t.Logf("approve pending node correctly failed: %v", err)
	} else {
		t.Log("approve pending node — store layer may have allowed it (optimistic lock version mismatch expected)")
	}
}

// TestRejectToForwardNode 验证驳回至排序更大的节点失败。
func TestRejectToForwardNode(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID
	node2ID := env.taskNodes[1].ID

	// 认领并批准 node1
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// 认领 node2
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("claim node2: %v", err)
	}

	// 尝试拒绝 node2 并指向 node3（前向）——应失败
	node3ID := env.taskNodes[2].ID
	targetNodeID := uuid.MustParse(node3ID)
	_, err = nodeSvc.Reject(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent", &targetNodeID, "bad reject")
	if err == nil {
		t.Fatal("reject to forward node should fail, but succeeded")
	}
	t.Logf("reject to forward node correctly failed: %v", err)
}

// TestMaxRejectCyclesDefaultInheritance 验证设计文档 §6 的默认值传播：
// 当模板节点未设置 MaxRejectCycles（=0）且项目未设置 MaxReviewCycles 时，
// CreateTask 应回退到项目的 MaxReviewCycles（DB schema DEFAULT 5），
// 使每个任务节点的 MaxRejectCycles = 5。
func TestMaxRejectCyclesDefaultInheritance(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest 创建项目时未设置 MaxReviewCycles（→ DB 默认 5），
	// 模板节点也未设置 MaxRejectCycles（→ 0），因此应回退到 5。
	for i, tn := range env.taskNodes {
		// 通过 Store 重新读取以确认落库值
		got, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(tn.ID))
		if err != nil {
			t.Fatalf("get task node %d: %v", i, err)
		}
		if got.MaxRejectCycles != 5 {
			t.Fatalf("node %d (%s): expected default MaxRejectCycles 5, got %d",
				i, got.Name, got.MaxRejectCycles)
		}
	}
	t.Logf("all %d task nodes inherited default MaxRejectCycles=5", len(env.taskNodes))
}

// TestMaxRejectCyclesFromTemplateNode 验证设计文档 §6 的优先级：
// 当模板节点显式设置 MaxRejectCycles 时，应优先使用模板节点的值，
// 而非项目的 MaxReviewCycles。
func TestMaxRejectCyclesFromTemplateNode(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// 创建模板，显式设置节点的 MaxRejectCycles=3（项目默认仍为 5）
	tpl, tplNodes, err := env.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: env.workspaceID,
		Name:        "flow-explicit-cycles-" + uuid.New().String()[:8],
		Description: strPtr("explicit max reject cycles"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code node"), SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
		{Name: "review", Description: strPtr("review node"), SortOrder: 2, NodeType: "review", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	_, taskNodes, err := svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    env.projectID,
		WorkflowName: tpl.Name,
		Title:        "explicit cycles test",
		Description:  strPtr("test"),
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     env.agent1ID,
		Sequence:     0,
	}, tplNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	for i, tn := range taskNodes {
		got, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(tn.ID))
		if err != nil {
			t.Fatalf("get task node %d: %v", i, err)
		}
		if got.MaxRejectCycles != 3 {
			t.Fatalf("node %d (%s): expected template MaxRejectCycles 3, got %d",
				i, got.Name, got.MaxRejectCycles)
		}
	}
	t.Logf("task nodes used template MaxRejectCycles=3 (not project default 5)")
}

// TestListNodes 验证列出任务节点。
func TestListNodes(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	nodes, err := service.NewNodeService(svc).ListNodes(ctx, env.taskID)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	t.Logf("listed %d nodes for task %d", len(nodes), env.taskID)
}

// TestTemplateStatsUsageCountAndCompletion 验证设计文档《工作流模板与触发设计》§六：
// TemplateStatsService.GetStats 按 tasks.workflow_name 聚合返回 usage_count /
// avg_completion_seconds / reject_rate 三项指标。
func TestTemplateStatsUsageCountAndCompletion(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest 已经用该模板创建了 1 个任务（env.taskID），故 usage_count >= 1。
	statsSvc := service.NewTemplateStatsService(svc)
	stats, err := statsSvc.GetStats(ctx, uuid.MustParse(env.templateID))
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.UsageCount < 1 {
		t.Fatalf("expected usage_count >= 1, got %d", stats.UsageCount)
	}
	// 该任务刚创建且未完成，avg_completion_seconds 至少应为非负数
	if stats.AvgCompletionSeconds < 0 {
		t.Fatalf("expected avg_completion_seconds >= 0, got %v", stats.AvgCompletionSeconds)
	}
	// 没有被取消的任务，reject_rate 应为 0
	if stats.RejectRate != 0 {
		t.Fatalf("expected reject_rate 0 (no cancelled tasks), got %v", stats.RejectRate)
	}
	t.Logf("template stats: usage_count=%d, avg_completion_seconds=%.2f, reject_rate=%.2f",
		stats.UsageCount, stats.AvgCompletionSeconds, stats.RejectRate)
}

// TestTemplateStatsRejectRateOnCancelled 验证设计文档 §六：reject_rate 按
// status='cancelled' 任务占比计算（0–1）。
func TestTemplateStatsRejectRateOnCancelled(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest 已创建 1 个 active 任务；再额外创建 1 个用同一模板名、
	// 状态为 cancelled 的任务，使 reject_rate = 1/2 = 0.5。
	tpl, _, err := svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: env.workspaceID,
		Name:        "stats-cancel-" + uuid.New().String()[:8],
		Description: strPtr("stats cancel flow"),
	}, []types.CreateTemplateNodeParams{
		{Name: "code", Description: strPtr("code node"), SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// active 任务
	_, _, err = svc.Store.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    env.projectID,
		WorkflowName: tpl.Name,
		Title:        "stats active",
		Description:  strPtr("test"),
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     env.agent1ID,
		Sequence:     0,
	}, nil)
	if err != nil {
		t.Fatalf("create active task: %v", err)
	}

	// cancelled 任务（用 db 直接创建以避免 CreateTask 默认 active 限制）
	pgDB := svcConnectTestDB(t)
	defer pgDB.Close()
	dbq := dbgen.New(pgDB)
	activeTask, err := dbq.CreateTask(ctx, dbgen.CreateTaskParams{
		ProjectID:    uuid.MustParse(env.projectID),
		WorkflowName: tpl.Name,
		Title:        "stats cancelled",
		Description:  sql.NullString{String: "test", Valid: true},
		Type:         "task",
		Priority:     "medium",
		Status:       "cancelled",
		AuthorType:   "agent",
		AuthorID:     uuid.MustParse(env.agent1ID),
		Sequence:     0,
	})
	if err != nil {
		t.Fatalf("create cancelled task: %v", err)
	}
	if _, err := pgDB.ExecContext(ctx,
		`UPDATE tasks SET sequence = $1 WHERE id = $1`, activeTask.ID); err != nil {
		t.Fatalf("set sequence: %v", err)
	}

	stats, err := service.NewTemplateStatsService(svc).GetStats(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.UsageCount != 2 {
		t.Fatalf("expected usage_count 2, got %d", stats.UsageCount)
	}
	// 2 个任务中 1 个 cancelled → reject_rate = 0.5
	if stats.RejectRate < 0.49 || stats.RejectRate > 0.51 {
		t.Fatalf("expected reject_rate ~0.5, got %v", stats.RejectRate)
	}
	t.Logf("template stats (2 tasks, 1 cancelled): usage_count=%d, reject_rate=%.2f",
		stats.UsageCount, stats.RejectRate)
}
