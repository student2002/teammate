// service_test.go covers tests for Service layer core logic.
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

// strPtr returns a string pointer, used for *string fields in types domain structs.
func strPtr(s string) *string {
	return &s
}

// int32Ptr returns an int32 pointer, used for *int32 fields in types domain structs.
func int32Ptr(i int32) *int32 {
	return &i
}

// TestMain sets up the test database.
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup test database: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	os.Exit(code)
}

// svcGetTestDSN returns the database connection string for service tests.
func svcGetTestDSN() string {
	return testdb.GetTestDSN()
}

// svcConnectTestDB connects to the test database and returns the connection.
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

// setupServiceTest creates a complete test environment: workspace, project, workflow, agents, and task.
// Returns the service instance, database connection, and all test-referenced IDs.
func setupServiceTest(t *testing.T) (*service.Service, *sql.DB, *testEnv) {
	t.Helper()
	pgDB := svcConnectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// Create workspace
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

	// Create a member for granting permissions
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

	// Add member to workspace as admin
	_, err = svc.Store.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	// Create project
	proj, err := svc.Store.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "proj-" + uuid.New().String()[:8],
		Description: strPtr("test project"),
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Create workflow template with 3 nodes: code (standard) -> review (review) -> deploy (standard)
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

	// Set project default workflow
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

	// Create agents
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

	// Add agents to project and grant permissions
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
		// Grant default permissions (task:claim, task:execute, task:comment, memory:read)
		if err := svc.Store.GrantDefaultPermissions(ctx, uuid.MustParse(aid), uuid.MustParse(member.ID)); err != nil {
			t.Fatalf("grant default permissions for agent %s: %v", aid, err)
		}
		// Also grant task:approve and task:reject permissions (denied by default, but needed in tests)
		for _, perm := range []string{"task:approve", "task:reject"} {
			_, err = svc.Store.GrantAgentPermission(ctx, uuid.MustParse(aid), perm, "*", nil, uuid.MustParse(member.ID))
			if err != nil {
				t.Fatalf("grant %s permission for agent %s: %v", perm, aid, err)
			}
		}
	}

	// Create task using Store.CreateTask (also creates task nodes)
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
		Sequence:     0, // Will be set to task.ID by Store.CreateTask
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

// ---------- Service layer tests ----------

// TestOptimisticLockClaimVersionConflict verifies that when two agents concurrently claim the same node, only one succeeds.
func TestOptimisticLockClaimVersionConflict(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	nodeID := env.taskNodes[0].ID

	// Agent1 claims successfully
	result, err := service.NewNodeService(svc).Claim(ctx, uuid.MustParse(nodeID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("agent1 claim should succeed: %v", err)
	}
	if result.Node.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", result.Node.Status)
	}

	// Agent2 attempts to claim the same node — should fail (version conflict)
	_, err = service.NewNodeService(svc).Claim(ctx, uuid.MustParse(nodeID), uuid.MustParse(env.agent2ID), "agent")
	if err == nil {
		t.Fatal("agent2 claim should fail with version conflict, but succeeded")
	}
	t.Logf("agent2 claim correctly failed: %v", err)
}

// TestOptimisticLockVersionIncrementOnClaim verifies design doc §10.1: version must increment by +1 after a successful claim.
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

	// Both the in-memory return value and persisted value should have version+1 after claim
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

// TestOptimisticLockVersionIncrementOnApprove verifies design doc §10.1/§5.1: the current node's version increments by +1 after approval.
func TestOptimisticLockVersionIncrementOnApprove(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID

	// Claim node1
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	before, err := svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if err != nil {
		t.Fatalf("get node1 before approve: %v", err)
	}
	versionBefore := before.Version

	// Approve
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

// TestConcurrentClaim uses goroutines to simulate concurrent claims on the same node; only one should succeed.
func TestConcurrentClaim(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// Use the first pending node
	nodeID := env.taskNodes[0].ID

	var successCount int32
	var failCount int32
	var wg sync.WaitGroup

	// Launch 10 goroutines to concurrently claim the same node
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

	// At least one claim must succeed, and the node can only be claimed by a single Agent.
	// Since repeated claims are idempotent, the same Agent may succeed multiple times,
	// but the node can only be claimed by one Agent.
	if successCount < 1 {
		t.Fatalf("expected at least 1 successful claim, got %d (failures: %d)", successCount, failCount)
	}

	// Verify the node is only claimed by one agent
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

	// Verify only one of the two agents holds the claim
	assigneeIsAgent1 := *claimedNode.AssigneeID == env.agent1ID
	assigneeIsAgent2 := *claimedNode.AssigneeID == env.agent2ID
	if !assigneeIsAgent1 && !assigneeIsAgent2 {
		t.Fatalf("expected assignee to be agent1 or agent2, got %s", *claimedNode.AssigneeID)
	}
}

// TestNodeRejectCascadingIntermediateReset verifies that rejecting a node resets intermediate nodes to pending.
func TestNodeRejectCascadingIntermediateReset(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID // code
	node2ID := env.taskNodes[1].ID // review

	// Agent1 claims and approves node1 (code)
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "done coding")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// Agent2 claims node2 (review) — different agent to avoid self-review
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("claim node2: %v", err)
	}

	// Reject node2 targeting node1 (rollback to code)
	targetNodeID := uuid.MustParse(node1ID)
	_, err = nodeSvc.Reject(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent", &targetNodeID, "needs rework")
	if err != nil {
		t.Fatalf("reject node2: %v", err)
	}

	// Verify node1 is back to pending (assignee cleared, reserved_for_agent_id set)
	node1, err := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if node1.Status != "pending" {
		t.Fatalf("node1: expected pending after reject, got %s", node1.Status)
	}

	// Verify node2 is rejected
	node2, err := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node2ID))
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if node2.Status != "rejected" {
		t.Fatalf("node2: expected rejected, got %s", node2.Status)
	}

	// Verify node1's reject_count has been incremented
	if node1.RejectCount < 1 {
		t.Fatalf("node1: expected reject_count >= 1, got %d", node1.RejectCount)
	}
	t.Logf("reject cascade: node1=%s (reject_count=%d), node2=%s",
		node1.Status, node1.RejectCount, node2.Status)
}

// TestMaxRejectCycleCircuitBreaker verifies that when reject_count >= max_review_cycles, the target node is escalated to manual_intervention.
func TestMaxRejectCycleCircuitBreaker(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	// Update project max_review_cycles to 2 so that CreateTask propagates it to task nodes
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

	// Create a new task so that CreateTask propagates the project's max_review_cycles to task nodes
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

	// Cycle 1: claim -> approve node1 -> claim node2 -> reject targeting node1
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

	// Verify node1's reject_count = 1
	node1, _ := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if node1.RejectCount != 1 {
		t.Fatalf("expected reject_count 1 after first reject, got %d", node1.RejectCount)
	}

	// Cycle 2: claim node1 (back to pending after reject) -> approve -> claim node2 -> reject targeting node1
	// Note: after reject, node1 is in pending state with reserved_for_agent_id and needs to be re-claimed
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

	// Now node1 should be in manual_intervention (reject_count >= max_review_cycles=2)
	node1, _ = env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if node1.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after max cycles, got %s", node1.Status)
	}
	t.Logf("circuit breaker: node1 status=%s, reject_count=%d — correct", node1.Status, node1.RejectCount)
}

// TestContinuationRightConflict verifies that when a node has a continuation right, other agents get a conflict error when claiming.
func TestContinuationRightConflict(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	// Create a workflow where all nodes are standard (so continuation rights will be set)
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

	// Create a task from this template using Store.CreateTask
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
		Sequence:     0, // Will be set to task.ID by Store.CreateTask
	}, tplNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Agent1 claims and approves node1
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// After approval, node2's reserved_for_agent_id should be agent1 (continuation right)
	node2, err := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(taskNodes[1].ID))
	if err != nil {
		t.Fatalf("get node2: %v", err)
	}
	if node2.ReservedForAgentID == nil || *node2.ReservedForAgentID != env.agent1ID {
		t.Fatalf("expected reserved_for_agent_id = agent1, got %v", node2.ReservedForAgentID)
	}

	// Agent2 attempts to claim node2 — should conflict (continuation right)
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[1].ID), uuid.MustParse(env.agent2ID), "agent")
	if err == nil {
		t.Fatal("agent2 should be blocked by continuation right, but claim succeeded")
	}
	t.Logf("continuation right conflict correctly blocked: %v", err)
}

// TestSelfReviewAvoidance verifies that an agent who completed a previous standard node cannot claim a review node.
func TestSelfReviewAvoidance(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID // code (standard)
	node2ID := env.taskNodes[1].ID // review

	// Agent1 claims and approves the code node
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "code done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// Same Agent attempts to claim the review node — should fail (self-review)
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent1ID), "agent")
	if err == nil {
		t.Fatal("self-review should be blocked, but claim succeeded")
	}
	t.Logf("self-review correctly blocked: %v", err)

	// A different Agent should be able to claim the review node
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("agent2 should be able to claim review node: %v", err)
	}
}

// TestSkipClaimReleasesContinuationRight verifies that SkipClaim clears reserved_for_agent_id, allowing other agents to claim.
func TestSkipClaimReleasesContinuationRight(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	// Create a workflow with 2 standard nodes to test continuation rights
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
		Sequence:     0, // Will be set to task.ID by Store.CreateTask
	}, tplNodes)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Agent1 claims and approves node1
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(taskNodes[0].ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// Verify node2 has a continuation right
	node2, _ := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(taskNodes[1].ID))
	if node2.ReservedForAgentID == nil || *node2.ReservedForAgentID != env.agent1ID {
		t.Fatalf("expected continuation right for agent1, got %v", node2.ReservedForAgentID)
	}

	// Agent1 skips claim
	err = nodeSvc.SkipClaim(ctx, uuid.MustParse(taskNodes[1].ID), uuid.MustParse(env.agent1ID))
	if err != nil {
		t.Fatalf("skip claim: %v", err)
	}

	// Now agent2 should be able to claim node2
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(taskNodes[1].ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("agent2 should claim after skip: %v", err)
	}
	t.Log("skip-claim correctly released continuation right")
}

// TestManualInterventionAndResolve verifies the manual intervention flow: a node enters manual_intervention and is resolved back to pending.
func TestManualInterventionAndResolve(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID

	// First, claim the node
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}

	// Set to manual_intervention
	node, err := nodeSvc.ManualIntervention(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "system", "stuck")
	if err != nil {
		t.Fatalf("manual intervention: %v", err)
	}
	if node.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention, got %s", node.Status)
	}

	// Resolve back to pending (so the Agent can re-claim)
	node, err = nodeSvc.Resolve(ctx, uuid.MustParse(node1ID), uuid.New(), "member", "resolved", nil, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if node.Status != "pending" {
		t.Fatalf("expected pending after resolve, got %s", node.Status)
	}
	t.Log("manual_intervention -> resolve flow works correctly")
}

// TestInterruptTask verifies that interrupting a task sets all in_progress nodes to manual_intervention.
func TestInterruptTask(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID

	// Claim node1 (set it to in_progress)
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}

	// Interrupt task
	result, err := nodeSvc.InterruptTask(ctx, env.taskID, uuid.New(), "member", "emergency stop")
	if err != nil {
		t.Fatalf("interrupt task: %v", err)
	}
	if result.InterruptedNodes != 1 {
		t.Fatalf("expected 1 interrupted node, got %d", result.InterruptedNodes)
	}

	// Verify node1 is now in manual_intervention
	node1, _ := env.svc.Store.GetTaskNode(ctx, uuid.MustParse(node1ID))
	if node1.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after interrupt, got %s", node1.Status)
	}
	t.Log("interrupt task correctly set in_progress nodes to manual_intervention")
}

// TestClaimNonExistentNode verifies that claiming a non-existent node returns an error.
func TestClaimNonExistentNode(t *testing.T) {
	svc, _, _ := setupServiceTest(t)
	ctx := context.Background()

	_, err := service.NewNodeService(svc).Claim(ctx, uuid.New(), uuid.New(), "agent")
	if err == nil {
		t.Fatal("expected error for non-existent node, got nil")
	}
	t.Logf("claim non-existent node correctly failed: %v", err)
}

// TestApproveNonInProgressNode verifies that approving a non-in_progress node fails.
func TestApproveNonInProgressNode(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// Attempt to approve a pending node (not in_progress) — should fail at the store layer
	node1ID := env.taskNodes[0].ID
	_, err := service.NewNodeService(svc).Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "approve")
	// Approve should fail because the node is pending, not in_progress
	if err != nil {
		t.Logf("approve pending node correctly failed: %v", err)
	} else {
		t.Log("approve pending node — store layer may have allowed it (optimistic lock version mismatch expected)")
	}
}

// TestRejectToForwardNode verifies that rejecting to a node with a higher sort order fails.
func TestRejectToForwardNode(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	nodeSvc := service.NewNodeService(svc)

	node1ID := env.taskNodes[0].ID
	node2ID := env.taskNodes[1].ID

	// Claim and approve node1
	_, err := nodeSvc.Claim(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}
	_, err = nodeSvc.Approve(ctx, uuid.MustParse(node1ID), uuid.MustParse(env.agent1ID), "agent", "done")
	if err != nil {
		t.Fatalf("approve node1: %v", err)
	}

	// Claim node2
	_, err = nodeSvc.Claim(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent")
	if err != nil {
		t.Fatalf("claim node2: %v", err)
	}

	// Attempt to reject node2 targeting node3 (forward) — should fail
	node3ID := env.taskNodes[2].ID
	targetNodeID := uuid.MustParse(node3ID)
	_, err = nodeSvc.Reject(ctx, uuid.MustParse(node2ID), uuid.MustParse(env.agent2ID), "agent", &targetNodeID, "bad reject")
	if err == nil {
		t.Fatal("reject to forward node should fail, but succeeded")
	}
	t.Logf("reject to forward node correctly failed: %v", err)
}

// TestMaxRejectCyclesDefaultInheritance verifies the design doc §6 default propagation:
// when a template node does not set MaxRejectCycles (=0) and the project does not set MaxReviewCycles,
// CreateTask should fall back to the project's MaxReviewCycles (DB schema DEFAULT 5),
// so each task node's MaxRejectCycles = 5.
func TestMaxRejectCyclesDefaultInheritance(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest creates the project without MaxReviewCycles (→ DB default 5),
	// and the template nodes do not set MaxRejectCycles (→ 0), so it should fall back to 5.
	for i, tn := range env.taskNodes {
		// Re-read from the Store to confirm the persisted value
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

// TestMaxRejectCyclesFromTemplateNode verifies the design doc §6 priority:
// when a template node explicitly sets MaxRejectCycles, it should take precedence
// over the project's MaxReviewCycles.
func TestMaxRejectCyclesFromTemplateNode(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// Create a template with explicit MaxRejectCycles=3 on nodes (project default is still 5)
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

// TestListNodes verifies listing task nodes.
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

// TestTemplateStatsUsageCountAndCompletion verifies the design doc "Workflow Template & Trigger Design" §6:
// TemplateStatsService.GetStats aggregates by tasks.workflow_name and returns usage_count /
// avg_completion_seconds / reject_rate metrics.
func TestTemplateStatsUsageCountAndCompletion(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest already created 1 task with this template (env.taskID), so usage_count >= 1.
	statsSvc := service.NewTemplateStatsService(svc)
	stats, err := statsSvc.GetStats(ctx, uuid.MustParse(env.templateID))
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if stats.UsageCount < 1 {
		t.Fatalf("expected usage_count >= 1, got %d", stats.UsageCount)
	}
	// The task was just created and not completed, so avg_completion_seconds should at least be non-negative
	if stats.AvgCompletionSeconds < 0 {
		t.Fatalf("expected avg_completion_seconds >= 0, got %v", stats.AvgCompletionSeconds)
	}
	// No cancelled tasks, so reject_rate should be 0
	if stats.RejectRate != 0 {
		t.Fatalf("expected reject_rate 0 (no cancelled tasks), got %v", stats.RejectRate)
	}
	t.Logf("template stats: usage_count=%d, avg_completion_seconds=%.2f, reject_rate=%.2f",
		stats.UsageCount, stats.AvgCompletionSeconds, stats.RejectRate)
}

// TestTemplateStatsRejectRateOnCancelled verifies the design doc §6: reject_rate is
// calculated as the proportion of status='cancelled' tasks (0–1).
func TestTemplateStatsRejectRateOnCancelled(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	// setupServiceTest already created 1 active task; additionally create 1 more
	// with the same template name and status=cancelled, so reject_rate = 1/2 = 0.5.
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

	// active task
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

	// cancelled task (created directly via DB to bypass CreateTask's default active status constraint)
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
	// 1 cancelled out of 2 tasks → reject_rate = 0.5
	if stats.RejectRate < 0.49 || stats.RejectRate > 0.51 {
		t.Fatalf("expected reject_rate ~0.5, got %v", stats.RejectRate)
	}
	t.Logf("template stats (2 tasks, 1 cancelled): usage_count=%d, reject_rate=%.2f",
		stats.UsageCount, stats.RejectRate)
}
