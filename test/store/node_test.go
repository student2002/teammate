// node_test.go 覆盖任务节点数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestApproveNodeInTx_BasicApproval 验证在事务中审批节点的基本流程。
func TestApproveNodeInTx_BasicApproval(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 3)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Approve test",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent.ID,
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	nodes, _ := s.ListTaskNodes(ctx, task.ID)
	firstNode := nodes[0]

	agentID := uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true}
	claimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID:         firstNode.ID,
		AssigneeID: &agent.ID,
		Version:    int32(firstNode.Version),
	})
	if err != nil {
		t.Fatalf("ClaimTaskNode: %v", err)
	}

	completed, err := s.ApproveNodeInTx(ctx, uuid.MustParse(firstNode.ID), claimed, agentID, agentID, "agent", "looks good")
	if err != nil {
		t.Fatalf("ApproveNodeInTx: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("expected completed, got %s", completed.Status)
	}

	nextNode := nodes[1]
	refreshed, err := s.GetTaskNode(ctx, uuid.MustParse(nextNode.ID))
	if err != nil {
		t.Fatalf("GetTaskNode: %v", err)
	}
	if refreshed.Status != "pending" {
		t.Fatalf("next node: expected pending, got %s", refreshed.Status)
	}

	comments, err := s.ListNodeComments(ctx, task.ID, uuid.MustParse(nextNode.ID))
	if err != nil {
		t.Fatalf("ListNodeComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 handoff comment on next node, got %d", len(comments))
	}
	if comments[0].NodeID == nil || *comments[0].NodeID != nextNode.ID {
		t.Fatalf("handoff comment should belong to next node, got %+v", comments[0].NodeID)
	}
	if comments[0].SourceNodeID == nil || *comments[0].SourceNodeID != firstNode.ID {
		t.Fatalf("handoff comment should source current node, got %+v", comments[0].SourceNodeID)
	}
	if comments[0].CommentType != "handoff" {
		t.Fatalf("expected handoff comment type, got %s", comments[0].CommentType)
	}
}

func TestApproveNodeInTx_AllNodesComplete(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)

	agentID := uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true}
	singleNode := []types.WorkflowTemplateNode{
		{Name: "only-node", SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
	}

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Single node task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent.ID,
	}, singleNode)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	nodes, _ := s.ListTaskNodes(ctx, task.ID)
	node := nodes[0]

	claimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID:         node.ID,
		AssigneeID: &agent.ID,
		Version:    int32(node.Version),
	})
	if err != nil {
		t.Fatalf("ClaimTaskNode: %v", err)
	}

	_, err = s.ApproveNodeInTx(ctx, uuid.MustParse(node.ID), claimed, agentID, agentID, "agent", "")
	if err != nil {
		t.Fatalf("ApproveNodeInTx: %v", err)
	}

	updatedTask, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if updatedTask.Status != "completed" {
		t.Fatalf("expected task completed, got %s", updatedTask.Status)
	}
}

func TestRejectNodeInTx_BasicReject(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 3)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Reject test",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent.ID,
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	nodes, _ := s.ListTaskNodes(ctx, task.ID)

	agentID := uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true}
	claimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID:         nodes[0].ID,
		AssigneeID: &agent.ID,
		Version:    int32(nodes[0].Version),
	})
	if err != nil {
		t.Fatalf("ClaimTaskNode: %v", err)
	}

	_, err = s.ApproveNodeInTx(ctx, uuid.MustParse(nodes[0].ID), claimed, agentID, agentID, "agent", "")
	if err != nil {
		t.Fatalf("ApproveNodeInTx: %v", err)
	}

	reviewNode, err := s.GetTaskNode(ctx, uuid.MustParse(nodes[1].ID))
	if err != nil {
		t.Fatalf("GetTaskNode: %v", err)
	}
	reviewClaimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID:         nodes[1].ID,
		AssigneeID: &agent.ID,
		Version:    int32(reviewNode.Version),
	})
	if err != nil {
		t.Fatalf("ClaimTaskNode review: %v", err)
	}

	targetNode, err := s.GetTaskNode(ctx, uuid.MustParse(nodes[0].ID))
	if err != nil {
		t.Fatalf("GetTaskNode target: %v", err)
	}

	rejected, err := s.RejectNodeInTx(ctx, uuid.MustParse(nodes[1].ID), reviewClaimed, targetNode, 3, agentID, "agent", uuid.NullUUID{UUID: uuid.MustParse(nodes[0].ID), Valid: true}, "needs fix")
	if err != nil {
		t.Fatalf("RejectNodeInTx: %v", err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("expected rejected, got %s", rejected.Status)
	}

	refreshedTarget, err := s.GetTaskNode(ctx, uuid.MustParse(nodes[0].ID))
	if err != nil {
		t.Fatalf("GetTaskNode: %v", err)
	}
	if refreshedTarget.Status != "pending" {
		t.Fatalf("target: expected pending, got %s", refreshedTarget.Status)
	}
	if refreshedTarget.RejectCount != 1 {
		t.Fatalf("expected reject_count=1, got %d", refreshedTarget.RejectCount)
	}
}

func TestRejectNodeInTx_MaxReviewCycles(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 3)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Max reject test",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent.ID,
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	nodes, _ := s.ListTaskNodes(ctx, task.ID)
	agentID := uuid.NullUUID{UUID: uuid.MustParse(agent.ID), Valid: true}
	maxCycles := int32(2)

	claimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID: nodes[0].ID, AssigneeID: &agent.ID, Version: int32(nodes[0].Version),
	})
	if err != nil {
		t.Fatalf("claim node 0: %v", err)
	}
	_, err = s.ApproveNodeInTx(ctx, uuid.MustParse(nodes[0].ID), claimed, agentID, agentID, "agent", "")
	if err != nil {
		t.Fatalf("approve node 0: %v", err)
	}

	reviewNode, _ := s.GetTaskNode(ctx, uuid.MustParse(nodes[1].ID))
	reviewClaimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID: nodes[1].ID, AssigneeID: &agent.ID, Version: int32(reviewNode.Version),
	})
	if err != nil {
		t.Fatalf("claim review node: %v", err)
	}

	targetNode, _ := s.GetTaskNode(ctx, uuid.MustParse(nodes[0].ID))
	_, err = s.RejectNodeInTx(ctx, uuid.MustParse(nodes[1].ID), reviewClaimed, targetNode, maxCycles, agentID, "agent", uuid.NullUUID{}, "first reject")
	if err != nil {
		t.Fatalf("first reject: %v", err)
	}

	codeNode, _ := s.GetTaskNode(ctx, uuid.MustParse(nodes[0].ID))
	codeClaimed, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID: codeNode.ID, AssigneeID: &agent.ID, Version: int32(codeNode.Version),
	})
	if err != nil {
		t.Fatalf("re-claim code node: %v", err)
	}
	_, err = s.ApproveNodeInTx(ctx, uuid.MustParse(nodes[0].ID), codeClaimed, agentID, agentID, "agent", "")
	if err != nil {
		t.Fatalf("re-approve code node: %v", err)
	}

	reviewNode2, _ := s.GetTaskNode(ctx, uuid.MustParse(nodes[1].ID))
	reviewClaimed2, err := s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID: nodes[1].ID, AssigneeID: &agent.ID, Version: int32(reviewNode2.Version),
	})
	if err != nil {
		t.Fatalf("re-claim review node: %v", err)
	}

	targetNode2, _ := s.GetTaskNode(ctx, uuid.MustParse(nodes[0].ID))
	_, err = s.RejectNodeInTx(ctx, uuid.MustParse(nodes[1].ID), reviewClaimed2, targetNode2, maxCycles, agentID, "agent", uuid.NullUUID{}, "second reject")
	if err != nil {
		t.Fatalf("second reject: %v", err)
	}

	finalTarget, _ := s.GetTaskNode(ctx, uuid.MustParse(nodes[0].ID))
	if finalTarget.Status != "manual_intervention" {
		t.Fatalf("expected manual_intervention after max rejects, got %s", finalTarget.Status)
	}
}

func TestClaimTaskNode_OptimisticLock(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)
	agent1, _ := createTestAgent(t, s, ws.ID)
	agent2, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent1.ID)
	addAgentToProject(t, s, proj.ID, agent2.ID)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Concurrent claim",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "agent",
		AuthorID:   agent1.ID,
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	nodes, _ := s.ListTaskNodes(ctx, task.ID)
	agentID1 := &agent1.ID
	agentID2 := &agent2.ID

	_, err = s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID: nodes[0].ID, AssigneeID: agentID1, Version: int32(nodes[0].Version),
	})
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	_, err = s.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
		ID: nodes[0].ID, AssigneeID: agentID2, Version: int32(nodes[0].Version),
	})
	if err == nil {
		t.Fatal("expected error for already claimed node")
	}
}
