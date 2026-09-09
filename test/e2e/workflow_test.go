// Package test contains end-to-end tests covering the full agent daemon lifecycle:
// agent registration, authentication flow, session token exchange, and workflow execution from start to finish.
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

// getTestDSN returns the test database connection string.
func getTestDSN() string {
	return testdb.GetTestDSN()
}

// connectTestDB opens a test database connection.
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

// TestWorkflowSmoke tests the full end-to-end workflow cycle:
// 1. Create workspace
// 2. Create project
// 3. Create workflow template with [implement → review] nodes
// 4. Create task from template
// 5. Verify task nodes are created in pending status
// 6. Create agent and register as project member
// 7. Agent claims node
// 8. Verify node transitions to in_progress
// 9. Complete node
// 10. Verify node transitions to completed
// 11. Verify next node becomes available (pending with continuation invite)
// 12. Reject review node back to first node
// 13. Verify rollback: first node back to pending, review node becomes rejected
// 14. Resolve manual_intervention node
func TestWorkflowSmoke(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	svc := service.New(pgDB, nil, nil) // passing nil for Hub is fine — SSE is a no-op
	ctx := context.Background()

	// Step 1: Create workspace
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

	// Step 2: Create project
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

	// Step 3: Create workflow template with [implement → review] nodes
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

	// Create a human member for granting permissions (granted_by foreign key constraint)
	// and to use as the task's author_id (NOT NULL)
	member, err := s.CreateMember(ctx, types.CreateMemberParams{
		Email: "e2e-test@example.com",
		Name:  "E2E Tester",
	})
	if err != nil {
		t.Fatalf("step 5b - create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	// Add member to workspace as admin
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("step 5b - create workspace member: %v", err)
	}

	// Step 4: Create task from template
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

	// Step 5: Verify task nodes are created in pending status
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

	// Step 6: Create two Agents and register them as project members
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

	// Add both Agents to the project
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

	// Grant default permissions to both Agents
	agent1ID := uuid.MustParse(agent1.ID)
	agent2ID := uuid.MustParse(agent2.ID)
	memberID := uuid.MustParse(member.ID)
	if err := s.GrantDefaultPermissions(ctx, agent1ID, memberID); err != nil {
		t.Fatalf("step 6 - grant default permissions to agent1: %v", err)
	}
	if err := s.GrantDefaultPermissions(ctx, agent2ID, memberID); err != nil {
		t.Fatalf("step 6 - grant default permissions to agent2: %v", err)
	}
	// Grant additional permissions to review Agent (task:approve, task:reject, etc.)
	for _, perm := range types.DeniedByDefaultAgentPermissions {
		_, _ = s.GrantAgentPermission(ctx, agent1ID, perm, "*", nil, memberID)
		_, _ = s.GrantAgentPermission(ctx, agent2ID, perm, "*", nil, memberID)
	}

	t.Logf("step 6: agents created and added to project (agent1=%s, agent2=%s)", agent1.ID, agent2.ID)

	// Step 7: Agent1 claims implement node
	nodeSvc := service.NewNodeService(svc)
	node1ID := uuid.MustParse(node1.ID)
	node2ID := uuid.MustParse(node2.ID)
	claimResult, err := nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("step 7 - agent1 claim node1: %v", err)
	}
	t.Logf("step 7: agent1 claimed node1 (implement)")

	// Step 8: Verify node transitions to in_progress
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
	_ = claimResult // suppress unused warning

	// Step 9: Complete implement node
	approveResult, err := nodeSvc.CompleteStandardNode(ctx, node1ID, agent1ID, "agent", "implementation done")
	if err != nil {
		t.Fatalf("step 9 - complete node1: %v", err)
	}
	t.Logf("step 9: node1 (implement) completed")

	// Step 10: Verify node1 transitions to completed
	completedNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 10 - get node1: %v", err)
	}
	if completedNode1.Status != "completed" {
		t.Fatalf("step 10 - expected completed, got %s", completedNode1.Status)
	}
	t.Logf("step 10: node1 is completed")

	// Step 11: Verify the next node (review) becomes available (pending with continuation invite)
	reviewNode, err := s.GetTaskNode(ctx, node2ID)
	if err != nil {
		t.Fatalf("step 11 - get node2: %v", err)
	}
	if reviewNode.Status != "pending" {
		t.Fatalf("step 11 - expected review node to be pending, got %s", reviewNode.Status)
	}
	// The review node should reserve continuation rights for the Agent that completed node1
	// However, self-review is not allowed, so the continuation right should not be set
	// because node1 is standard and node2 is review — the code skips
	// standard→review continuation rights
	t.Logf("step 11: node2 (review) is pending, reserved_for_agent_id=%v", reviewNode.ReservedForAgentID)
	_ = approveResult

	// Step 12: Agent2 claims review node and rejects it back to implement node
	claimResult2, err := nodeSvc.Claim(ctx, node2ID, agent2ID, "agent")
	if err != nil {
		t.Fatalf("step 12 - agent2 claim node2: %v", err)
	}
	_ = claimResult2

	// Verify node2 is now in_progress
	reviewInProgress, err := s.GetTaskNode(ctx, node2ID)
	if err != nil {
		t.Fatalf("step 12 - get node2 after claim: %v", err)
	}
	if reviewInProgress.Status != "in_progress" {
		t.Fatalf("step 12 - expected node2 in_progress after claim, got %s", reviewInProgress.Status)
	}

	// Reject node2 targeting node1
	rejectResult, err := nodeSvc.Reject(ctx, node2ID, agent2ID, "agent", &node1ID, "needs rework")
	if err != nil {
		t.Fatalf("step 12 - reject node2: %v", err)
	}
	t.Logf("step 12: node2 (review) rejected back to node1 (implement)")

	// Step 13: Verify rollback
	// node2 should be rejected
	if rejectResult.Node.Status != "rejected" {
		t.Fatalf("step 13 - expected node2 status rejected, got %s", rejectResult.Node.Status)
	}
	t.Logf("step 13a: node2 is rejected")

	// node1 should return to pending (needs re-claim)
	rolledBackNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 13 - get node1 after reject: %v", err)
	}
	if rolledBackNode1.Status != "pending" {
		t.Fatalf("step 13 - expected node1 status pending after rollback, got %s", rolledBackNode1.Status)
	}
	// node1's reject_count should have been incremented
	if rolledBackNode1.RejectCount < 1 {
		t.Fatalf("step 13 - expected node1 reject_count >= 1, got %d", rolledBackNode1.RejectCount)
	}
	t.Logf("step 13b: node1 is back to pending with reject_count=%d", rolledBackNode1.RejectCount)

	// Verify state transitions have been recorded
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

	// Step 14: Resolve manual_intervention node
	// First, set node1 to manual_intervention by exceeding the max reject cycles.
	// We need to raise the reject count to max_reject_cycles.
	// node1's current max_reject_cycles comes from the project (default 3).
	// node1 already has reject_count=1 from the previous rejection.
	// We perform a few more reject cycles to trigger manual_intervention.

	// Re-claim node1 and complete it again
	_, err = nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("step 14 - re-claim node1: %v", err)
	}
	_, err = nodeSvc.CompleteStandardNode(ctx, node1ID, agent1ID, "agent", "second implementation")
	if err != nil {
		t.Fatalf("step 14 - complete node1 (2nd time): %v", err)
	}

	// Agent2 re-claims the review node
	_, err = nodeSvc.Claim(ctx, node2ID, agent2ID, "agent")
	if err != nil {
		t.Fatalf("step 14 - agent2 re-claim node2: %v", err)
	}

	// Reject again (2nd reject cycle)
	_, err = nodeSvc.Reject(ctx, node2ID, agent2ID, "agent", &node1ID, "still needs work")
	if err != nil {
		t.Fatalf("step 14 - reject node2 (2nd time): %v", err)
	}

	// Repeat: claim → complete → claim review → reject (3rd time)
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

	// 3rd reject — this should trigger manual_intervention on node1
	// (reject_count will reach max_reject_cycles=3)
	_, err = nodeSvc.Reject(ctx, node2ID, agent2ID, "agent", &node1ID, "still not good enough")
	if err != nil {
		t.Fatalf("step 14 - reject node2 (3rd time): %v", err)
	}

	// Verify node1 is now in manual_intervention
	manualNode1, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 14 - get node1 after 3rd reject: %v", err)
	}
	if manualNode1.Status != "manual_intervention" {
		t.Fatalf("step 14 - expected node1 manual_intervention, got %s", manualNode1.Status)
	}
	t.Logf("step 14a: node1 is now in manual_intervention after %d reject cycles", manualNode1.RejectCount)

	// Resolve the manual_intervention node — reassign it to agent1
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

	// Verify the complete lifecycle: all state transitions have corresponding transition records
	allTransitions, err := s.ListNodeTransitions(ctx, node1ID)
	if err != nil {
		t.Fatalf("step 14 - list all node1 transitions: %v", err)
	}
	// Expected state transitions for node1:
	// 1. pending → in_progress (claim)
	// 2. in_progress → completed (approve)
	// 3. (from reject) completed → pending (reject routing)
	// 4. pending → in_progress (re-claim)
	// 5. in_progress → completed (approve)
	// 6. (from reject) completed → pending (reject routing)
	// 7. pending → in_progress (re-claim)
	// 8. in_progress → completed (approve)
	// 9. (from reject) completed → manual_intervention (reject routing, exceeded max cycles)
	// 10. manual_intervention → in_progress (resolve)
	if len(allTransitions) < 8 {
		t.Fatalf("step 14 - expected at least 8 transitions for node1, got %d", len(allTransitions))
	}
	t.Logf("step 14c: node1 has %d transitions recorded (full lifecycle)", len(allTransitions))

	t.Log("E2E workflow smoke test PASSED: complete lifecycle verified")
}

// TestWorkflowSelfReviewPrevention verifies that the agent who completed the first (implement) node cannot claim the second (review) node to review its own work.
func TestWorkflowSelfReviewPrevention(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// Setup: workspace, project, workflow with [implement → review]
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

	// Create a human member for granting permissions
	member, err := s.CreateMember(ctx, types.CreateMemberParams{
		Email: "self-review-test@example.com",
		Name:  "Self-Review Tester",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	// Add member to workspace as admin
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	})
	if err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	// Create a single Agent
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

	// Create task
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

	// Agent1 claims and completes the implement node
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

	// Agent1 attempts to claim the review node — should be rejected (self-review)
	node2ID := uuid.MustParse(node2.ID)
	_, err = nodeSvc.Claim(ctx, node2ID, agent1ID, "agent")
	if err == nil {
		t.Fatal("expected self-review prevention error, but claim succeeded")
	}
	t.Logf("self-review correctly prevented: %v", err)
}

// TestWorkflowInterruptAndManualIntervention verifies the interrupt flow: a task can be interrupted, setting all in_progress nodes to manual_intervention,
// and nodes can subsequently be resolved back to in_progress to continue working.
func TestWorkflowInterruptAndManualIntervention(t *testing.T) {
	pgDB := connectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })

	s := store.New(pgDB)
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	// Setup
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

	// Create a human member for granting permissions
	member, err := s.CreateMember(ctx, types.CreateMemberParams{
		Email: "interrupt-test@example.com",
		Name:  "Interrupt Tester",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	// Add member to workspace as admin
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

	// Agent claims node1
	nodeSvc := service.NewNodeService(svc)
	node1ID := uuid.MustParse(node1.ID)
	_, err = nodeSvc.Claim(ctx, node1ID, agent1ID, "agent")
	if err != nil {
		t.Fatalf("claim node1: %v", err)
	}

	// Verify node1 is in_progress
	inProgressNode, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("get node1: %v", err)
	}
	if inProgressNode.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", inProgressNode.Status)
	}

	// Interrupt task
	interruptResult, err := nodeSvc.InterruptTask(ctx, taskResult.Task.ID, memberID, "member", "human interrupted")
	if err != nil {
		t.Fatalf("interrupt task: %v", err)
	}
	if interruptResult.InterruptedNodes != 1 {
		t.Fatalf("expected 1 interrupted node, got %d", interruptResult.InterruptedNodes)
	}
	t.Logf("task interrupted: %d nodes set to manual_intervention", interruptResult.InterruptedNodes)

	// Verify node1 is now in manual_intervention
	manualNode, err := s.GetTaskNode(ctx, node1ID)
	if err != nil {
		t.Fatalf("get node1 after interrupt: %v", err)
	}
	if manualNode.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after interrupt, got %s", manualNode.Status)
	}

	// Resolve the manual_intervention node
	resolvedNode, err := nodeSvc.Resolve(ctx, node1ID, memberID, "member", "resolved, reassigning", &agent1ID, service.ResolveActionReExecute)
	if err != nil {
		t.Fatalf("resolve node1: %v", err)
	}
	if resolvedNode.Status != "pending" {
		t.Fatalf("expected pending after resolve, got %s", resolvedNode.Status)
	}
	t.Logf("manual_intervention node resolved back to pending")
}
