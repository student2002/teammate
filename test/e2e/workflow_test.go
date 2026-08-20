// Package test 包含覆盖完整 agent 守护进程生命周期的端到端测试：
// agent 注册、认证流程、会话令牌交换以及从开始到完成的工作流执行。
package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

// getTestDSN 返回测试数据库连接字符串。
func getTestDSN() string {
	return testdb.GetTestDSN()
}

// connectTestDB 打开测试数据库连接。
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

// TestWorkflowSmoke 测试完整的端到端工作流周期：
// 1. 创建工作空间
// 2. 创建项目
// 3. 创建包含 [implement → review] 节点的工作流模板
// 4. 从模板创建任务
// 5. 验证任务节点以 pending 状态创建
// 6. 创建 agent 并注册为项目成员
// 7. Agent 认领节点
// 8. 验证节点转换为 in_progress
// 9. 完成节点
// 10. 验证节点转换为 completed
// 11. 验证下一个节点变为可用（带延续邀请的 pending 状态）
// 12. 将审查节点拒绝回第一个节点
// 13. 验证回滚：第一个节点回到 pending，审查节点变为 rejected
// 14. 解决 manual_intervention 节点
func TestWorkflowSmoke(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	svc := service.New(pgDB, nil, nil) // Hub 传 nil 没问题 — SSE 是无操作的
	ctx := context.Background()

	// 步骤 1：创建工作空间
	desc1 := "e2e smoke test"
	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "e2e-smoke-" + uuid.New().String()[:8],
		Description: &desc1,
		IssuePrefix: "ES",
	})
	if err != nil {
		t.Fatalf("step 1 - create workspace: %v", err)
	}
	t.Logf("step 1: workspace created id=%s", ws.ID)
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })

	// 步骤 2：创建项目
	desc2 := "e2e smoke project"
	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "e2e-smoke-proj",
		Description: &desc2,
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("step 2 - create project: %v", err)
	}
	t.Logf("step 2: project created id=%s", proj.ID)

	// 第 3 步：创建包含 [implement → review] 节点的工作流模板
	desc3 := "implement then review"
	desc4 := "implement the feature"
	desc5 := "review the implementation"
	tpl, tplNodes, err := s.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: ws.ID,
		Name:        "e2e-implement-review",
		Description: &desc3,
	}, []types.CreateTemplateNodeParams{
		{
			Name:            "implement",
			Description:     &desc4,
			SortOrder:       1,
			NodeType:        "standard",
			AssigneeType:    "any_agent",
			TimeoutMinutes:  60,
			MaxRejectCycles: 3,
		},
		{
			Name:           "review",
			Description:    &desc5,
			SortOrder:      2,
			NodeType:       "review",
			AssigneeType:   "any_agent",
			TimeoutMinutes: 30,
		},
	})
	if err != nil {
		t.Fatalf("step 3 - create workflow template: %v", err)
	}
	if len(tplNodes) != 2 {
		t.Fatalf("step 3 - expected 2 template nodes, got %d", len(tplNodes))
	}
	t.Logf("step 3: workflow template created id=%s with %d nodes", tpl.ID, len(tplNodes))

	// 创建一个人工成员用于授予权限（granted_by 外键约束）
	// 以及将该成员用作 task 的 author_id（NOT NULL）
	member, err := s.CreateMember(ctx, types.CreateMemberParams{
		Email: "e2e-test@example.com",
		Name:  "E2E Tester",
	})
	if err != nil {
		t.Fatalf("step 5b - create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	// 以 admin 角色将成员添加到工作区
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("step 5b - create workspace member: %v", err)
	}

	// 第 4 步：根据模板创建任务
	taskSvc := service.NewTaskService(svc)
	desc6 := "end-to-end workflow smoke test"
	taskResult, err := taskSvc.Create(ctx, uuid.MustParse(proj.ID), types.CreateTaskParams{
		ProjectID:    proj.ID,
		WorkflowName: tpl.Name,
		Title:        "E2E smoke test task",
		Description:  &desc6,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "member",
		AuthorID:     member.ID,
	}, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("step 4 - create task: %v", err)
	}
	task := taskResult.Task
	nodes := taskResult.Nodes
	t.Logf("step 4: task created id=%d with %d nodes", task.ID, len(nodes))

	// 第 5 步：验证任务节点以 pending 状态创建
	if len(nodes) != 2 {
		t.Fatalf("step 5 - expected 2 task nodes, got %d", len(nodes))
	}
	node1 := nodes[0] // implement
	node2 := nodes[1] // review
	if node1.Status != "pending" {
		t.Fatalf("step 5 - node1 (implement): expected pending, got %s", node1.Status)
	}
	if node2.Status != "pending" {
		t.Fatalf("step 5 - node2 (review): expected pending, got %s", node2.Status)
	}
	t.Logf("step 5: both nodes in pending state")

	// 第 6 步：创建两个 Agent 并将它们注册为项目成员
	model1 := "claude-3.5-sonnet"
	gitName1 := "e2e-implementer"
	gitEmail1 := "e2e-implementer@teammate.local"
	agent1, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "e2e-implementer",
		Provider:     "claude",
		Instructions: "implement agent",
		Model:        &model1,
		Status:       "online",
		GitName:      &gitName1,
		GitEmail:     &gitEmail1,
	})
	if err != nil {
		t.Fatalf("step 6 - create agent1: %v", err)
	}

	model2 := "claude-3.5-sonnet"
	gitName2 := "e2e-reviewer"
	gitEmail2 := "e2e-reviewer@teammate.local"
	agent2, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "e2e-reviewer",
		Provider:     "claude",
		Instructions: "review agent",
		Model:        &model2,
		Status:       "online",
		GitName:      &gitName2,
		GitEmail:     &gitEmail2,
	})
	if err != nil {
		t.Fatalf("step 6 - create agent2: %v", err)
	}

	// 将两个 Agent 添加到项目
	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    &agent1.ID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("step 6 - add agent1 to project: %v", err)
	}
	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    &agent2.ID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("step 6 - add agent2 to project: %v", err)
	}

	// 向两个 Agent 授予默认权限
	agent1ID := uuid.MustParse(agent1.ID)
	agent2ID := uuid.MustParse(agent2.ID)
	memberID := uuid.MustParse(member.ID)
	if err := s.GrantDefaultPermissions(ctx, agent1ID, memberID); err != nil {
		t.Fatalf("step 6 - grant default permissions to agent1: %v", err)
	}
	if err := s.GrantDefaultPermissions(ctx, agent2ID, memberID); err != nil {
		t.Fatalf("step 6 - grant default permissions to agent2: %v", err)
	}
	// 为 review Agent 授予额外权限（task:approve、task:reject 等）
	for _, perm := range types.DeniedByDefaultAgentPermissions {
		_, _ = s.GrantAgentPermission(ctx, agent1ID, perm, "*", nil, memberID)
		_, _ = s.GrantAgentPermission(ctx, agent2ID, perm, "*", nil, memberID)
	}

	t.Logf("step 6: agents created and added to project (agent1=%s, agent2=%s)", agent1.ID, agent2.ID)

	// 第 7 步：Agent1 认领 implement 节点
	nodeSvc := service.NewNodeService(svc)
	node1ID := uuid.MustParse(node1.ID)
	node2ID := uuid.MustParse(node2.ID)
	claimResult, err := nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("step 7 - agent1 claim node1: %v", err)
	}
	t.Logf("step 7: agent1 claimed node1 (implement)")

	// 第 8 步：验证节点流转为 in_progress
	claimedNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 8 - get node1: %v", err)
	}
	if claimedNode1.Status != "in_progress" {
		t.Fatalf("step 8 - expected in_progress, got %s", claimedNode1.Status)
	}
	if claimedNode1.AssigneeID == nil || *claimedNode1.AssigneeID != agent1.ID {
		t.Fatalf("step 8 - expected assignee_id=%s, got %v", agent1.ID, claimedNode1.AssigneeID)
	}
	t.Logf("step 8: node1 is in_progress, assigned to agent1")
	_ = claimResult // 抑制未使用警告

	// 第 9 步：完成 implement 节点
	approveResult, err := nodeSvc.CompleteStandardNode(ctx, node1ID, agent1ID, "agent", "implementation done")
	if err != nil {
		t.Fatalf("step 9 - complete node1: %v", err)
	}
	t.Logf("step 9: node1 (implement) completed")

	// 第 10 步：验证 node1 流转为 completed
	completedNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 10 - get node1: %v", err)
	}
	if completedNode1.Status != "completed" {
		t.Fatalf("step 10 - expected completed, got %s", completedNode1.Status)
	}
	t.Logf("step 10: node1 is completed")

	// 第 11 步：验证下一个节点（review）变为可用（pending 并带续行邀请）
	reviewNode, err := s.GetTaskNode(ctx, node2ID)
	if err != nil {
		t.Fatalf("step 11 - get node2: %v", err)
	}
	if reviewNode.Status != "pending" {
		t.Fatalf("step 11 - expected review node to be pending, got %s", reviewNode.Status)
	}
	// review 节点应为完成 node1 的 Agent 保留续行权
	// 然而，不允许自我审查，因此续行权不应被设置
	// 因为 node1 是 standard 而 node2 是 review——代码会跳过
	// standard→review 的续行权
	t.Logf("step 11: node2 (review) is pending, reserved_for_agent_id=%v", reviewNode.ReservedForAgentID)
	_ = approveResult

	// 第 12 步：Agent2 认领 review 节点并拒绝使其退回 implement 节点
	claimResult2, err := nodeSvc.Claim(ctx, node2ID, agent2ID, "agent")
	if err != nil {
		t.Fatalf("step 12 - agent2 claim node2: %v", err)
	}
	_ = claimResult2

	// 验证 node2 现在为 in_progress
	reviewInProgress, err := s.GetTaskNode(ctx, node2ID)
	if err != nil {
		t.Fatalf("step 12 - get node2 after claim: %v", err)
	}
	if reviewInProgress.Status != "in_progress" {
		t.Fatalf("step 12 - expected node2 in_progress after claim, got %s", reviewInProgress.Status)
	}

	// 拒绝 node2 并指向 node1
	rejectResult, err := nodeSvc.Reject(ctx, node2ID, agent2ID, "agent", &node1ID, "needs rework")
	if err != nil {
		t.Fatalf("step 12 - reject node2: %v", err)
	}
	t.Logf("step 12: node2 (review) rejected back to node1 (implement)")

	// 第 13 步：验证回滚
	// node2 应被拒绝
	if rejectResult.Node.Status != "rejected" {
		t.Fatalf("step 13 - expected node2 status rejected, got %s", rejectResult.Node.Status)
	}
	t.Logf("step 13a: node2 is rejected")

	// node1 应回到 pending（需要重新认领）
	rolledBackNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 13 - get node1 after reject: %v", err)
	}
	if rolledBackNode1.Status != "pending" {
		t.Fatalf("step 13 - expected node1 status pending after rollback, got %s", rolledBackNode1.Status)
	}
	// node1 的 reject_count 应已递增
	if rolledBackNode1.RejectCount < 1 {
		t.Fatalf("step 13 - expected node1 reject_count >= 1, got %d", rolledBackNode1.RejectCount)
	}
	t.Logf("step 13b: node1 is back to pending with reject_count=%d", rolledBackNode1.RejectCount)

	// 验证状态流转已被记录
	transitions1, err := s.ListNodeTransitions(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 13 - list node1 transitions: %v", err)
	}
	if len(transitions1) == 0 {
		t.Fatal("step 13 - expected transitions for node1, got 0")
	}
	transitions2, err := s.ListNodeTransitions(ctx, node2ID)
	if err != nil {
		t.Fatalf("step 13 - list node2 transitions: %v", err)
	}
	if len(transitions2) == 0 {
		t.Fatal("step 13 - expected transitions for node2, got 0")
	}
	t.Logf("step 13c: transitions recorded (node1=%d, node2=%d)", len(transitions1), len(transitions2))

	// 第 14 步：解决 manual_intervention 节点
	// 首先，通过超过最大拒绝周期将 node1 设置为 manual_intervention。
	// 我们需要将拒绝次数提升到 max_reject_cycles。
	// node1 当前的 max_reject_cycles 来自项目（默认 3）。
	// node1 在之前的拒绝中已有 reject_count=1。
	// 我们再多执行几次拒绝周期以触发 manual_intervention。

	// 重新认领 node1 并再次完成它
	_, err = nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("step 14 - re-claim node1: %v", err)
	}
	_, err = nodeSvc.CompleteStandardNode(ctx, node1ID, agent1ID, "agent", "second implementation")
	if err != nil {
		t.Fatalf("step 14 - complete node1 (2nd time): %v", err)
	}

	// Agent2 再次认领 review 节点
	_, err = nodeSvc.Claim(ctx, node2ID, agent2ID, "agent")
	if err != nil {
		t.Fatalf("step 14 - agent2 re-claim node2: %v", err)
	}

	// 再次拒绝（第 2 个拒绝周期）
	_, err = nodeSvc.Reject(ctx, node2ID, agent2ID, "agent", &node1ID, "still needs work")
	if err != nil {
		t.Fatalf("step 14 - reject node2 (2nd time): %v", err)
	}

	// 重复：认领 → 完成 → 认领 review → 拒绝（第 3 次）
	_, err = nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("step 14 - re-claim node1 (3rd time): %v", err)
	}
	_, err = nodeSvc.CompleteStandardNode(ctx, node1ID, agent1ID, "agent", "third implementation")
	if err != nil {
		t.Fatalf("step 14 - complete node1 (3rd time): %v", err)
	}

	_, err = nodeSvc.Claim(ctx, node2ID, agent2ID, "agent")
	if err != nil {
		t.Fatalf("step 14 - agent2 re-claim node2 (3rd time): %v", err)
	}

	// 第 3 次拒绝——这应触发 node1 上的 manual_intervention
	// （reject_count 将达到 max_reject_cycles=3）
	_, err = nodeSvc.Reject(ctx, node2ID, agent2ID, "agent", &node1ID, "still not good enough")
	if err != nil {
		t.Fatalf("step 14 - reject node2 (3rd time): %v", err)
	}

	// 验证 node1 现在处于 manual_intervention
	manualNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 14 - get node1 after 3rd reject: %v", err)
	}
	if manualNode1.Status != "manual_intervention" {
		t.Fatalf("step 14 - expected node1 manual_intervention, got %s", manualNode1.Status)
	}
	t.Logf("step 14a: node1 is now in manual_intervention after %d reject cycles", manualNode1.RejectCount)

	// 解决 manual_intervention 节点——将其重新分配给 agent1
	resolvedNode, err := nodeSvc.Resolve(ctx, node1ID, memberID, "member", "human resolved, reassigning to agent1", &agent1ID, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("step 14 - resolve node1: %v", err)
	}
	if resolvedNode.Status != "pending" {
		t.Fatalf("step 14 - expected node1 pending after resolve, got %s", resolvedNode.Status)
	}
	if resolvedNode.AssigneeID == nil || *resolvedNode.AssigneeID != agent1.ID {
		t.Fatalf("step 14 - expected node1 assigned to agent1 after resolve, got %v", resolvedNode.AssigneeID)
	}
	t.Logf("step 14b: node1 resolved back to pending, assigned to agent1")

	// 验证完整生命周期：所有状态变更都有对应的流转记录
	allTransitions, err := s.ListNodeTransitions(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 14 - list all node1 transitions: %v", err)
	}
	// node1 的预期状态流转：
	// 1. pending → in_progress（认领）
	// 2. in_progress → completed（批准）
	// 3.（来自拒绝）completed → pending（拒绝路由）
	// 4. pending → in_progress（重新认领）
	// 5. in_progress → completed（批准）
	// 6.（来自拒绝）completed → pending（拒绝路由）
	// 7. pending → in_progress（重新认领）
	// 8. in_progress → completed（批准）
	// 9.（来自拒绝）completed → manual_intervention（拒绝路由，超过最大周期）
	// 10. manual_intervention → in_progress (resolve)
	if len(allTransitions) < 8 {
		t.Fatalf("step 14 - expected at least 8 transitions for node1, got %d", len(allTransitions))
	}
	t.Logf("step 14c: node1 has %d transitions recorded (full lifecycle)", len(allTransitions))

	t.Log("E2E workflow smoke test PASSED: complete lifecycle verified")
}

// TestWorkflowSelfReviewPrevention 验证完成工作流第一个（实现）节点的代理不能认领第二个（审核）节点来审核自己的工作。
func TestWorkflowSelfReviewPrevention(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// 准备：工作区、项目、包含 [implement → review] 的工作流
	desc1 := "self-review prevention test"
	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "self-review-test-" + uuid.New().String()[:8],
		Description: &desc1,
		IssuePrefix: "SR",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })
	desc2 := "test"
	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "self-review-proj",
		Description: &desc2,
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	desc3 := "test self-review"
	desc4 := "implement"
	desc5 := "review"
	tpl, _, err := s.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: ws.ID,
		Name:        "self-review-flow",
		Description: &desc3,
	}, []types.CreateTemplateNodeParams{
		{
			Name:           "implement",
			Description:    &desc4,
			SortOrder:      1,
			NodeType:       "standard",
			AssigneeType:   "any_agent",
			TimeoutMinutes: 60,
		},
		{
			Name:           "review",
			Description:    &desc5,
			SortOrder:      2,
			NodeType:       "review",
			AssigneeType:   "any_agent",
			TimeoutMinutes: 30,
		},
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	// 创建一个人工成员用于授予权限
	member, err := s.CreateMember(ctx, types.CreateMemberParams{
		Email: "self-review-test@example.com",
		Name:  "Self-Review Tester",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	// 以 admin 角色将成员添加到工作区
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	// 创建单个 Agent
	model1 := "claude-3.5-sonnet"
	gitName1 := "self-review-agent"
	gitEmail1 := "self-review-agent@teammate.local"
	agent1, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "self-review-agent",
		Provider:     "claude",
		Instructions: "test",
		Model:        &model1,
		Status:       "online",
		GitName:      &gitName1,
		GitEmail:     &gitEmail1,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    &agent1.ID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	agent1ID := uuid.MustParse(agent1.ID)
	memberID := uuid.MustParse(member.ID)
	if err := s.GrantDefaultPermissions(ctx, agent1ID, memberID); err != nil {
		t.Fatalf("grant default permissions to agent1: %v", err)
	}
	for _, perm := range types.DeniedByDefaultAgentPermissions {
		_, _ = s.GrantAgentPermission(ctx, agent1ID, perm, "*", nil, memberID)
	}

	// 创建任务
	taskSvc := service.NewTaskService(svc)
	desc6 := "test"
	taskResult, err := taskSvc.Create(ctx, uuid.MustParse(proj.ID), types.CreateTaskParams{
		ProjectID:    proj.ID,
		WorkflowName: tpl.Name,
		Title:        "Self-review test task",
		Description:  &desc6,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "member",
		AuthorID:     member.ID,
	}, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node1 := taskResult.Nodes[0]
	node2 := taskResult.Nodes[1]

	// Agent1 认领并完成 implement 节点
	nodeSvc := service.NewNodeService(svc)
	node1ID := uuid.MustParse(node1.ID)
	_, err = nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.CompleteStandardNode(ctx, node1ID, agent1ID, "agent", "done")
	if err != nil {
		t.Fatalf("complete node1: %v", err)
	}

	// Agent1 尝试认领 review 节点——应被拒绝（自我审查）
	node2ID := uuid.MustParse(node2.ID)
	_, err = nodeSvc.Claim(ctx, node2ID, agent1ID, "agent")
	if err == nil {
		t.Fatal("expected self-review prevention error, but claim succeeded")
	}
	t.Logf("self-review correctly prevented: %v", err)
}

// TestWorkflowInterruptAndManualIntervention 验证终端流程：任务可以被终端，将所有 in_progress 节点设为 manual_intervention，
// 并且节点可以后续解决回 in_progress 继续工作。
func TestWorkflowInterruptAndManualIntervention(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// 准备
	desc1 := "interrupt test"
	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "interrupt-test-" + uuid.New().String()[:8],
		Description: &desc1,
		IssuePrefix: "IN",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })
	desc2 := "test"
	proj, err := s.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "interrupt-proj",
		Description: &desc2,
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	desc3 := "test interrupt"
	desc4 := "write code"
	desc5 := "review code"
	tpl, _, err := s.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: ws.ID,
		Name:        "interrupt-flow",
		Description: &desc3,
	}, []types.CreateTemplateNodeParams{
		{
			Name:           "code",
			Description:    &desc4,
			SortOrder:      1,
			NodeType:       "standard",
			AssigneeType:   "any_agent",
			TimeoutMinutes: 60,
		},
		{
			Name:           "review",
			Description:    &desc5,
			SortOrder:      2,
			NodeType:       "review",
			AssigneeType:   "any_agent",
			TimeoutMinutes: 30,
		},
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}

	// 创建一个人工成员用于授予权限
	member, err := s.CreateMember(ctx, types.CreateMemberParams{
		Email: "interrupt-test@example.com",
		Name:  "Interrupt Tester",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	// 以 admin 角色将成员添加到工作区
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	model1 := "claude-3.5-sonnet"
	gitName1 := "interrupt-agent"
	gitEmail1 := "interrupt-agent@teammate.local"
	agent1, _, err := s.CreateAgent(ctx, types.CreateAgentParams{
		WorkspaceID:  ws.ID,
		Name:         "interrupt-agent",
		Provider:     "claude",
		Instructions: "test",
		Model:        &model1,
		Status:       "online",
		GitName:      &gitName1,
		GitEmail:     &gitEmail1,
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	_, err = s.CreateProjectMember(ctx, types.CreateProjectMemberParams{
		ProjectID:  proj.ID,
		MemberType: "agent",
		AgentID:    &agent1.ID,
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("add agent to project: %v", err)
	}

	agent1ID := uuid.MustParse(agent1.ID)
	memberID := uuid.MustParse(member.ID)
	if err := s.GrantDefaultPermissions(ctx, agent1ID, memberID); err != nil {
		t.Fatalf("grant default permissions to agent1: %v", err)
	}
	for _, perm := range types.DeniedByDefaultAgentPermissions {
		_, _ = s.GrantAgentPermission(ctx, agent1ID, perm, "*", nil, memberID)
	}

	taskSvc := service.NewTaskService(svc)
	desc6 := "test"
	taskResult, err := taskSvc.Create(ctx, uuid.MustParse(proj.ID), types.CreateTaskParams{
		ProjectID:    proj.ID,
		WorkflowName: tpl.Name,
		Title:        "Interrupt test task",
		Description:  &desc6,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "member",
		AuthorID:     member.ID,
	}, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	node1 := taskResult.Nodes[0]

	// Agent 认领 node1
	nodeSvc := service.NewNodeService(svc)
	node1ID := uuid.MustParse(node1.ID)
	_, err = nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}

	// 验证 node1 为 in_progress
	inProgressNode, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if inProgressNode.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", inProgressNode.Status)
	}

	// 中断任务
	interruptResult, err := nodeSvc.InterruptTask(ctx, taskResult.Task.ID, memberID, "member", "human interrupted")
	if err != nil {
		t.Fatalf("interrupt task: %v", err)
	}
	if interruptResult.InterruptedNodes != 1 {
		t.Fatalf("expected 1 interrupted node, got %d", interruptResult.InterruptedNodes)
	}
	t.Logf("task interrupted: %d nodes set to manual_intervention", interruptResult.InterruptedNodes)

	// 验证 node1 现在为 manual_intervention
	manualNode, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("get node1 after interrupt: %v", err)
	}
	if manualNode.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after interrupt, got %s", manualNode.Status)
	}

	// 解决 manual_intervention 节点
	resolvedNode, err := nodeSvc.Resolve(ctx, node1ID, memberID, "member", "resolved, reassigning", &agent1ID, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("resolve node1: %v", err)
	}
	if resolvedNode.Status != "pending" {
		t.Fatalf("expected pending after resolve, got %s", resolvedNode.Status)
	}
	t.Logf("manual_intervention node resolved back to pending")
}
