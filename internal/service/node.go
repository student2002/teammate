// node.go implements the business logic for workflow nodes; it is the most complex service in
// the system. It covers node state-machine operations such as claim, approve, reject,
// manual intervention, and resolve, plus DAG dependency checks, self-review avoidance, and
// continuation-right management. All state changes notify the relevant agents via SSE events;
// control events are buffered through Redis to ensure none are lost.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// NodeService provides the business logic for node management; it is the most complex service
// in the system.
// Node state machine: pending → in_progress → completed / rejected / manual_intervention
type NodeService struct {
	svc *Service
}

// NewNodeService creates a new NodeService instance.
func NewNodeService(svc *Service) *NodeService {
	return &NodeService{svc: svc}
}

// projPtrFromString parses a domain-style project ID string into a *uuid.UUID.
// On failure it returns nil (HasResourcePermission treats nil as "no specific resource").
// If HasResourcePermission is later unified to accept string, this helper can be removed.
func projPtrFromString(s string) *uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &u
}

// uuidFromStr parses a domain-style string into a uuid.UUID, returning uuid.Nil on failure.
// Used to pass types.Task.ProjectID (string) to Store methods that accept uuid.UUID (e.g. GetProject).
// If Store methods are later unified to accept string, this helper can be removed.
func uuidFromStr(s string) uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return u
}

// ClaimNodeResult saves the result of a node-claim operation.
type ClaimNodeResult struct {
	Node types.TaskNode // node info after being claimed
}

// Claim verifies and claims a node for an agent.
// The claim flow includes multiple layers of permission and state checks to ensure correct
// node assignment.
//
// Steps:
//  1. Fetch node info
//  2. Fetch task info to check project membership
//  3. Check whether the agent has the right to claim (project membership)
//  4. Resource-level permission check: the agent must hold the task:claim permission for the project
//  5. Check that the previous node is completed (linear workflow)
//  6. DAG dependency check: all depends_on nodes must be completed
//  7. Self-review avoidance check: a review node cannot be claimed by the agent that executed
//     a preceding node
//  8. Continuation-right check: when the current node is reserved for another agent, that other
//     agent may not claim it
//  9. Perform the claim (optimistic lock via the version field)
//  10. Create a state-transition record
//
// Parameters:
//   - ctx: request context
//   - nodeID: ID of the node to claim
//   - agentID: ID of the claiming agent
//
// Returns:
//   - *ClaimNodeResult: node info after being claimed
//   - error: possible error (node does not exist, insufficient permissions, previous node not
//     completed, already claimed, etc.)
func (s *NodeService) Claim(ctx context.Context, nodeID, operatorID uuid.UUID, operatorType string) (*ClaimNodeResult, error) {
	node, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	// Check task status; cancelled or completed tasks cannot be claimed
	if task.Status == types.TaskStatusCancelled || task.Status == types.TaskStatusCompleted {
		return nil, fmt.Errorf("task is %s, cannot claim node", task.Status)
	}

	// Agent claim: check project access and claim permission
	if operatorType == "agent" {
		projectSvc := NewProjectService(s.svc)
		projectID, _ := uuid.Parse(task.ProjectID)
		if err := projectSvc.CheckAgentProjectAccess(ctx, operatorID, projectID); err != nil {
			return nil, err
		}

		permSvc := NewAgentPermissionService(s.svc)
		hasPerm, err := permSvc.HasResourcePermission(ctx, operatorID, types.PermTaskClaim, "project", projPtrFromString(task.ProjectID))
		if err != nil {
			return nil, fmt.Errorf("check claim permission: %w", err)
		}
		if !hasPerm {
			return nil, fmt.Errorf("agent does not have task:claim permission for this project")
		}
	}

	prevNode, err := s.svc.Store.GetPrevTaskNode(ctx, types.GetPrevTaskNodeParams{
		TaskID: node.TaskID,
		NodeID: nodeID.String(),
	})
	if err == nil {
		if prevNode.Status != types.TaskNodeStatusCompleted {
			return nil, fmt.Errorf("node not available for claiming: previous node must be completed first (current status: %s)", prevNode.Status)
		}
	}

	if len(node.DependsOn) > 0 {
		for _, depID := range node.DependsOn {
			depUUID, _ := uuid.Parse(depID)
			depNode, err := s.svc.Store.GetTaskNode(ctx, depUUID)
			if err != nil {
				return nil, fmt.Errorf("node not available for claiming: dependency node %s not found", depID)
			}
			if depNode.Status != types.TaskNodeStatusCompleted {
				return nil, fmt.Errorf("node not available for claiming: dependency node %q must be completed first (current status: %s)", depNode.Name, depNode.Status)
			}
		}
	}

	// When an agent claims a review node, check for self-review
	if operatorType == "agent" && node.NodeType == types.NodeTypeReview {
		prevAssigneeID, err := s.svc.Store.GetPrevStandardNodeAssignee(ctx, types.GetPrevStandardNodeAssigneeParams{
			TaskID: node.TaskID,
			NodeID: nodeID.String(),
		})
		if err == nil && prevAssigneeID.Valid && prevAssigneeID.UUID == operatorID {
			return nil, fmt.Errorf("self-review is not allowed: you wrote the code for this task")
		}
	}

	// Agent claim: check continuation right
	if operatorType == "agent" && node.ReservedForAgentID != nil && *node.ReservedForAgentID != operatorID.String() {
		if node.ReservationExpiresAt != nil && node.ReservationExpiresAt.After(s.svc.Store.Clock.Now()) {
			return nil, fmt.Errorf("node is reserved for another agent (continuation right)")
		}
	}

	var claimedNode types.TaskNode
	if operatorType != "agent" {
		// Human claim
		opID := operatorID.String()
		claimedNode, err = s.svc.Store.ClaimTaskNodeByHuman(ctx, types.ClaimTaskNodeByHumanParams{
			ID:         nodeID.String(),
			AssigneeID: &opID,
			Version:    int32(node.Version),
		})
		if err != nil {
			return nil, fmt.Errorf("node not available for claiming: %w", err)
		}
	} else if node.Status == types.TaskNodeStatusInProgress {
		if node.AssigneeID != nil && *node.AssigneeID == operatorID.String() {
			return &ClaimNodeResult{Node: node}, nil
		}
		opID := operatorID.String()
		claimedNode, err = s.svc.Store.ReclaimTaskNode(ctx, types.ReclaimTaskNodeParams{
			ID:      nodeID.String(),
			Version: int32(node.Version),
		})
		if err != nil {
			return nil, fmt.Errorf("node not available for re-claiming: %w", err)
		}
		_ = opID
	} else {
		opID := operatorID.String()
		claimedNode, err = s.svc.Store.ClaimTaskNode(ctx, types.ClaimTaskNodeParams{
			ID:         nodeID.String(),
			AssigneeID: &opID,
			Version:    int32(node.Version),
		})
		if err != nil {
			return nil, fmt.Errorf("node not available for claiming: %w", err)
		}
	}

	fromStatus := types.TaskNodeStatusPending
	if node.Status == types.TaskNodeStatusInProgress {
		fromStatus = types.TaskNodeStatusInProgress
	}
	opID := operatorID.String()
	if _, err := s.svc.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
		TaskNodeID:   nodeID.String(),
		FromStatus:   fromStatus,
		ToStatus:     types.TaskNodeStatusInProgress,
		Action:       types.TransitionActionReclaim,
		OperatorID:   &opID,
		OperatorType: operatorType,
	}); err != nil {
		slog.Warn("failed to create node transition", "err", err)
	}

	return &ClaimNodeResult{Node: claimedNode}, nil
}

// ApproveNodeResult saves the result of a node-approve operation.
type ApproveNodeResult struct {
	Node types.TaskNode // node info after being approved
}

// CompleteStandardNode completes a standard node without requiring the task:approve permission.
// Used by the /complete endpoint; only callable by the assigned agent.
// Key difference from Approve(): no approval permission is required, and an agent can only
// complete the standard node it has been assigned.
//
// Steps:
//  1. Fetch node info
//  2. Verify the node status is in_progress
//  3. Call the Store to complete the node within a transaction (update status, record completion
//     time, create a transition record)
//  4. Publish an event via SSE to notify that the next node can be claimed
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//   - operatorID: operator ID
//   - operatorType: operator type (agent/member)
//   - comment: completion comment (optional)
//
// Returns:
//   - *ApproveNodeResult: node info after completion
//   - error: possible error (node does not exist, status is not in_progress)
func (s *NodeService) CompleteStandardNode(ctx context.Context, nodeID uuid.UUID, operatorID uuid.UUID, operatorType, comment string) (*ApproveNodeResult, error) {
	currentNode, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	if currentNode.Status != types.TaskNodeStatusInProgress {
		return nil, fmt.Errorf("node cannot be completed: current status is %s, expected in_progress", currentNode.Status)
	}

	if operatorType == "" {
		operatorType = "agent"
	}

	completedBy := uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil}
	operatorIDNull := uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil}

	node, err := s.svc.Store.ApproveNodeInTx(ctx, nodeID, currentNode, completedBy, operatorIDNull, operatorType, comment)
	if err != nil {
		return nil, err
	}

	s.publishNodeEventAfterApprove(ctx, currentNode.TaskID)

	return &ApproveNodeResult{Node: node}, nil
}

// Approve approves a node and cascades to the next node or completes the task.
// Requires checking the agent's task:approve permission.
//
// Steps:
//  1. Fetch node info
//  2. Verify the node status is in_progress
//  3. For agent operations, perform a resource-level permission check (task:approve)
//  4. Call the Store to approve the node within a transaction
//  5. Publish an event via SSE to notify that the next node can be claimed
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//   - operatorID: operator ID
//   - operatorType: operator type (agent/member)
//   - comment: approval comment (optional)
//
// Returns:
//   - *ApproveNodeResult: node info after being approved
//   - error: possible error (node does not exist, insufficient permissions, status is not in_progress)
func (s *NodeService) Approve(ctx context.Context, nodeID uuid.UUID, operatorID uuid.UUID, operatorType, comment string) (*ApproveNodeResult, error) {
	currentNode, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	// The node must be in_progress to be approved (it must be claimed first)
	if currentNode.Status != types.TaskNodeStatusInProgress {
		return nil, fmt.Errorf("node cannot be approved: current status is %s, expected in_progress: %w",
			currentNode.Status, types.ErrNodeStateConflict)
	}

	if operatorType == "" {
		operatorType = "agent"
	}

	if operatorType == "agent" {
		task, err := s.svc.Store.GetTask(ctx, currentNode.TaskID)
		if err != nil {
			return nil, fmt.Errorf("get task: %w", err)
		}
		permSvc := NewAgentPermissionService(s.svc)
		hasPerm, err := permSvc.HasResourcePermission(ctx, operatorID, types.PermTaskApprove, "project", projPtrFromString(task.ProjectID))
		if err != nil {
			return nil, fmt.Errorf("check approve permission: %w", err)
		}
		if !hasPerm {
			return nil, fmt.Errorf("agent does not have task:approve permission for this project")
		}
	}

	// completed_by references the agents table; it should be NULL for human operations
	var completedBy uuid.NullUUID
	if operatorType == "agent" {
		completedBy = uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil}
	}
	operatorIDNull := uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil}

	node, err := s.svc.Store.ApproveNodeInTx(ctx, nodeID, currentNode, completedBy, operatorIDNull, operatorType, comment)
	if err != nil {
		return nil, err
	}

	s.publishNodeEventAfterApprove(ctx, currentNode.TaskID)

	return &ApproveNodeResult{Node: node}, nil
}

// RejectNodeResult saves the result of a node-reject operation.
type RejectNodeResult struct {
	Node types.TaskNode // node info after being rejected
}

// Reject rejects a node and rolls back to the target node.
// Only the target node is reset to pending; intermediate nodes retain their original status.
// After rejection, a node:reject_rollback event is sent via SSE to notify the target node's
// agent to perform a git rollback.
//
// Steps:
//  1. Fetch node info
//  2. Verify the node status is in_progress
//  3. For agent operations, perform a resource-level permission check (task:reject)
//  4. Determine the rollback target node (defaults to the previous node)
//  5. Validate the target node (belongs to the same task, sorts before, is not a manual node)
//  6. Get the maximum reject-cycle count (to prevent infinite rejection)
//  7. Call the Store to perform the reject and rollback within a transaction
//  8. Mark the task's memories as stale
//  9. Publish a node:pending event via SSE to notify that the target node can be re-claimed
//  10. Send a node:reject_rollback control event via SSE to the target node's agent (Redis buffered)
//
// Parameters:
//   - ctx: request context
//   - nodeID: ID of the node being rejected
//   - operatorID: operator ID
//   - operatorType: operator type (agent/member)
//   - targetNodeID: optional, the rollback target node ID (nil means auto-select the previous node)
//   - comment: reject comment (optional)
//
// Returns:
//   - *RejectNodeResult: node info after being rejected
//   - error: possible error (node does not exist, insufficient permissions, invalid target node, etc.)
func (s *NodeService) Reject(ctx context.Context, nodeID uuid.UUID, operatorID uuid.UUID, operatorType string, targetNodeID *uuid.UUID, comment string) (*RejectNodeResult, error) {
	currentNode, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	if currentNode.Status != types.TaskNodeStatusInProgress {
		return nil, fmt.Errorf("node cannot be rejected: current status is %s, expected in_progress", currentNode.Status)
	}

	if operatorType == "" {
		operatorType = "agent"
	}

	if operatorType == "agent" {
		task, err := s.svc.Store.GetTask(ctx, currentNode.TaskID)
		if err != nil {
			return nil, fmt.Errorf("get task: %w", err)
		}
		permSvc := NewAgentPermissionService(s.svc)
		hasPerm, err := permSvc.HasResourcePermission(ctx, operatorID, types.PermTaskReject, "project", projPtrFromString(task.ProjectID))
		if err != nil {
			return nil, fmt.Errorf("check reject permission: %w", err)
		}
		if !hasPerm {
			return nil, fmt.Errorf("agent does not have task:reject permission for this project")
		}
	}

	var targetID uuid.UUID
	if targetNodeID != nil {
		targetID = *targetNodeID
	} else {
		prevNode, err := s.svc.Store.GetPrevTaskNode(ctx, types.GetPrevTaskNodeParams{
			TaskID: currentNode.TaskID,
			NodeID: nodeID.String(),
		})
		if err != nil {
			return nil, fmt.Errorf("no previous node to reject to: %w", err)
		}
		targetID, _ = uuid.Parse(prevNode.ID)
	}

	targetNode, err := s.svc.Store.GetTaskNode(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("target node not found: %w", err)
	}

	if targetNode.TaskID != currentNode.TaskID {
		return nil, fmt.Errorf("target node must belong to the same task")
	}

	if targetNode.SortOrder >= currentNode.SortOrder {
		return nil, fmt.Errorf("target node must have a sort_order less than the current node")
	}

	if targetNode.NodeType == types.NodeTypeManual || targetNode.AssigneeType == types.AssigneeTypeHuman {
		return nil, fmt.Errorf("cannot reject to a manual node or a node assigned to a human")
	}

	var maxRejectCycles int32 = 5
	if targetNode.MaxRejectCycles > 0 {
		maxRejectCycles = int32(targetNode.MaxRejectCycles)
	}

	var targetNodeIDNull uuid.NullUUID
	if targetNodeID != nil {
		targetNodeIDNull = uuid.NullUUID{UUID: *targetNodeID, Valid: true}
	}
	operatorIDNull := uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil}

	node, err := s.svc.Store.RejectNodeInTx(ctx, nodeID, currentNode, targetNode, maxRejectCycles, operatorIDNull, operatorType, targetNodeIDNull, comment)
	if err != nil {
		return nil, err
	}

	if err := s.svc.Store.MarkMemoriesStaleByTask(ctx, currentNode.TaskID); err != nil {
		slog.Warn("failed to mark memories stale", "task_id", currentNode.TaskID, "err", err)
	}

	s.publishNodePendingEvent(ctx, currentNode.TaskID)

	if targetNode.AssigneeID != nil {
		task, err := s.svc.Store.GetTask(ctx, currentNode.TaskID)
		if err != nil {
			slog.Error("failed to get task for rollback event", "err", err)
		}
		projectIDStr := ""
		if err == nil {
			projectIDStr = task.ProjectID
		}
		targetAttempt := int(targetNode.RejectCount) + 1
		assigneeUUID, _ := uuid.Parse(*targetNode.AssigneeID)
		s.svc.PublishControlEvent(ctx, assigneeUUID, types.EventNodeRejectRollback, map[string]interface{}{
			"task_id":        fmt.Sprintf("%d", currentNode.TaskID),
			"target_node_id": targetID.String(),
			"target_order":   targetNode.SortOrder,
			"rejected_node":  nodeID.String(),
			"project_id":     projectIDStr,
			"target_attempt": targetAttempt,
		})
	}

	return &RejectNodeResult{Node: node}, nil
}

// ManualIntervention sets a node to the manual_intervention status.
// Used for timeouts, system errors, or scenarios where an agent actively reports the need for
// human intervention.
//
// Steps:
//  1. Fetch node info
//  2. Verify the node status is in_progress
//  3. For agent operations, perform a resource-level permission check (task:execute)
//  4. Update the node status to manual_intervention; change the assignee type to human
//  5. Create a state-transition record
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//   - operatorID: operator ID
//   - operatorType: operator type (agent/system)
//   - comment: comment (optional)
//
// Returns:
//   - types.TaskNode: updated node info
//   - error: possible error (node does not exist, status is not in_progress)
func (s *NodeService) ManualIntervention(ctx context.Context, nodeID uuid.UUID, operatorID uuid.UUID, operatorType, comment string) (types.TaskNode, error) {
	currentNode, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("node not found: %w", err)
	}

	if currentNode.Status != types.TaskNodeStatusInProgress {
		return types.TaskNode{}, fmt.Errorf("node cannot be set to manual_intervention: current status is %s, expected in_progress", currentNode.Status)
	}

	if operatorType == "" {
		operatorType = "system"
	}

	if operatorType == "agent" {
		task, err := s.svc.Store.GetTask(ctx, currentNode.TaskID)
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("get task: %w", err)
		}
		permSvc := NewAgentPermissionService(s.svc)
		hasPerm, err := permSvc.HasResourcePermission(ctx, operatorID, types.PermTaskExecute, "project", projPtrFromString(task.ProjectID))
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("check execute permission: %w", err)
		}
		if !hasPerm {
			return types.TaskNode{}, fmt.Errorf("agent does not have task:execute permission for this project")
		}
	}

	// Interrupt→ManualIntervention: change AssigneeType to human, clear ReservedForAgentID and
	// ReservationExpiresAt (aligns with the semantics of store/node_interrupt.go, letting a human
	// take over)
	node, err := s.svc.Store.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
		ID:          nodeID.String(),
		Status:      types.TaskNodeStatusManualIntervention,
		AssigneeType: types.AssigneeTypeHuman,
		AssigneeID:  currentNode.AssigneeID,
		ReservedForAgentID: nil,
		RejectCount: int32(currentNode.RejectCount),
		CompletedBy: currentNode.AssigneeID,
		Version:     int32(currentNode.Version),
		ExpectedCurrentStatus: currentNode.Status,
	})
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node to manual_intervention: %w", err)
	}

	commentStr := comment
	operatorIDStr := operatorID.String()
	if _, err := s.svc.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
		TaskNodeID:   nodeID.String(),
		FromStatus:   currentNode.Status,
		ToStatus:     types.TaskNodeStatusManualIntervention,
		Action:       types.TransitionActionManual,
		Comment:      &commentStr,
		OperatorID:   &operatorIDStr,
		OperatorType: operatorType,
	}); err != nil {
		slog.Warn("failed to create node transition", "err", err)
	}

	return node, nil
}

// ResolveAction defines the way a node is restored.
type ResolveAction string

const (
	ResolveActionReExecute ResolveAction = "re_execute" // Reset to pending; the agent re-executes
	ResolveActionComplete  ResolveAction = "complete"   // Mark directly as completed, skipping re-execution
)

// Resolve restores a node from the manual_intervention status.
// If newAgentID is provided, the node is reassigned to that agent.
// The action parameter determines the restoration mode: re_execute (default) resets to pending
// so the agent re-executes; complete marks it directly as completed using the existing summary.
//
// Steps:
//  1. Fetch node info
//  2. Verify the node status is manual_intervention
//  3. Determine the new assignee (keep the original assignee or use newAgentID)
//  4. Update the node status to in_progress; reset the reject count
//  5. Create a state-transition record
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//   - operatorID: operator ID
//   - operatorType: operator type (member)
//   - comment: comment (optional)
//   - newAgentID: optional, the new agent ID (nil means keep the original assignee)
//
// Returns:
//   - types.TaskNode: updated node info
//   - error: possible error (node is not in manual_intervention, database update failure)
func (s *NodeService) Resolve(ctx context.Context, nodeID uuid.UUID, operatorID uuid.UUID, operatorType, comment string, newAgentID *uuid.UUID, action ResolveAction) (types.TaskNode, error) {
	currentNode, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("node not found: %w", err)
	}

	if currentNode.Status != types.TaskNodeStatusManualIntervention {
		return types.TaskNode{}, fmt.Errorf("node is not in manual_intervention status")
	}

	if operatorType == "" {
		operatorType = "member"
	}

	// complete mode: mark directly as completed, skipping re-execution
	if action == ResolveActionComplete {
		completedBy := uuid.NullUUID{UUID: operatorID, Valid: operatorID != uuid.Nil}
		var completedByStr *string
		if completedBy.Valid {
			s := completedBy.UUID.String()
			completedByStr = &s
		}
		now := time.Now()
		node, err := s.svc.Store.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
			ID:          nodeID.String(),
			Status:      types.TaskNodeStatusCompleted,
			AssigneeType: currentNode.AssigneeType,
			AssigneeID:  currentNode.AssigneeID,
			ReservedForAgentID: currentNode.ReservedForAgentID,
			RejectCount: int32(currentNode.RejectCount),
			CompletedAt: &now,
			CompletedBy: completedByStr,
			Version:     int32(currentNode.Version),
			ExpectedCurrentStatus: currentNode.Status,
		})
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("complete node: %w", err)
		}

		commentStr := comment
		operatorIDStr := operatorID.String()
		if _, err := s.svc.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
			TaskNodeID:   nodeID.String(),
			FromStatus:   types.TaskNodeStatusManualIntervention,
			ToStatus:     types.TaskNodeStatusCompleted,
			Action:       types.TransitionActionManual,
			Comment:      &commentStr,
			OperatorID:   &operatorIDStr,
			OperatorType: operatorType,
		}); err != nil {
			slog.Warn("failed to create node transition", "err", err)
		}

		// After completion, trigger the next node
		s.publishNodeEventAfterApprove(ctx, currentNode.TaskID)

		return node, nil
	}

	// Default re_execute mode: reset to pending; the agent re-executes
	assigneeID := currentNode.AssigneeID
	if newAgentID != nil {
		newID := newAgentID.String()
		assigneeID = &newID
	}

	// resolve→pending: reset AssigneeType to any_agent (so any agent can claim) and clear the
	// continuation right
	node, err := s.svc.Store.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
		ID:          nodeID.String(),
		Status:      types.TaskNodeStatusPending,
		AssigneeType: types.AssigneeTypeAnyAgent,
		AssigneeID:  assigneeID,
		ReservedForAgentID: nil,
		RejectCount: 0, // re_execute resets the reject count
		CompletedBy: assigneeID,
		Version:     int32(currentNode.Version),
		ExpectedCurrentStatus: currentNode.Status,
	})
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("reset reject count: %w", err)
	}

	commentStr := comment
	operatorIDStr := operatorID.String()
	if _, err := s.svc.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
		TaskNodeID:   nodeID.String(),
		FromStatus:   types.TaskNodeStatusManualIntervention,
		ToStatus:     types.TaskNodeStatusPending,
		Action:       types.TransitionActionManual,
		Comment:      &commentStr,
		OperatorID:   &operatorIDStr,
		OperatorType: operatorType,
	}); err != nil {
		slog.Warn("failed to create node transition", "err", err)
	}

	// Publish a node:pending event so the agent can re-claim the node
	task, err := s.svc.Store.GetTask(ctx, currentNode.TaskID)
	if err == nil {
		s.publishNodePendingEvent(ctx, currentNode.TaskID)
		_ = task // fetched for possible future use
	}

	return node, nil
}

// SkipClaim allows an agent to actively give up its continuation right, opening the node for
// claim by other agents. The continuation right is the mechanism that, after node N is
// completed, retains the right to claim node N+1 for the current agent for 30 seconds by default.
//
// Steps:
//  1. Fetch node info
//  2. Verify the agent holds the continuation right
//  3. Resource-level permission check (task:claim)
//  4. Clear the reserved agent ID and expiration time
//  5. Create a state-transition record
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//   - agentID: agent ID
//
// Returns:
//   - error: possible error (agent does not hold the continuation right, insufficient permissions)
func (s *NodeService) SkipClaim(ctx context.Context, nodeID, agentID uuid.UUID) error {
	node, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("node not found: %w", err)
	}

	if node.ReservedForAgentID == nil || *node.ReservedForAgentID != agentID.String() {
		return fmt.Errorf("agent does not hold the continuation right for this node")
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	permSvc := NewAgentPermissionService(s.svc)
	hasPerm, err := permSvc.HasResourcePermission(ctx, agentID, types.PermTaskClaim, "project", projPtrFromString(task.ProjectID))
	if err != nil {
		return fmt.Errorf("check claim permission: %w", err)
	}
	if !hasPerm {
		return fmt.Errorf("agent does not have task:claim permission for this project")
	}

	// SkipClaim semantics: release the continuation right; clear ReservedForAgentID so other
	// agents can claim
	_, err = s.svc.Store.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
		ID:          nodeID.String(),
		Status:      node.Status,
		AssigneeType: node.AssigneeType,
		AssigneeID:  node.AssigneeID,
		ReservedForAgentID: nil, // release the continuation right
		RejectCount: int32(node.RejectCount),
		CompletedBy: node.AssigneeID,
		Version:     int32(node.Version),
		ExpectedCurrentStatus: node.Status,
	})
	if err != nil {
		return fmt.Errorf("node version conflict: %w", err)
	}

	skipComment := "skip-claim: agent gave up continuation right"
	agentIDStr := agentID.String()
	if _, err := s.svc.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
		TaskNodeID:   nodeID.String(),
		FromStatus:   node.Status,
		ToStatus:     node.Status,
		Action:       types.TransitionActionReclaim,
		Comment:      &skipComment,
		OperatorID:   &agentIDStr,
		OperatorType: "agent",
	}); err != nil {
		slog.Warn("failed to create node transition", "err", err)
	}

	return nil
}

// GetTaskNode queries a single workflow node by ID.
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//
// Returns:
//   - types.TaskNode: node info
//   - error: possible error (node does not exist)
func (s *NodeService) GetTaskNode(ctx context.Context, nodeID uuid.UUID) (types.TaskNode, error) {
	node, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node: %w", err)
	}
	return node, nil
}

// CreateNodeTransition creates a node state-transition audit record.
//
// Parameters:
//   - ctx: request context
//   - params: transition-record parameters, including node ID, source status, target status,
//     action type, etc.
//
// Returns:
//   - types.NodeTransition: the created transition record
//   - error: possible error (database write failure)
func (s *NodeService) CreateNodeTransition(ctx context.Context, params types.CreateNodeTransitionParams) (types.NodeTransition, error) {
	transition, err := s.svc.Store.CreateNodeTransition(ctx, params)
	if err != nil {
		return types.NodeTransition{}, fmt.Errorf("create node transition: %w", err)
	}
	return transition, nil
}

// UpdateNodeSummary updates a node's execution summary.
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//   - summary: execution summary text
//
// Returns:
//   - types.TaskNode: updated node record
//   - error: possible error (node does not exist, database update failure)
func (s *NodeService) UpdateNodeSummary(ctx context.Context, nodeID uuid.UUID, summary string) (types.TaskNode, error) {
	node, err := s.svc.Store.UpdateNodeSummary(ctx, nodeID, summary)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node summary: %w", err)
	}
	return node, nil
}

// ListNodes lists all workflow nodes for the given task.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//
// Returns:
//   - []types.TaskNode: node list
//   - error: possible error (database query failure)
func (s *NodeService) ListNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	return s.svc.Store.ListTaskNodes(ctx, taskID)
}

// InterruptTaskResult saves the result of an interrupt-task operation.
type InterruptTaskResult struct {
	TaskID           int32 // ID of the interrupted task
	InterruptedNodes int   // number of interrupted nodes
}

// InterruptTask sets all in_progress nodes of a task to manual_intervention.
// Atomicity is guaranteed via a transaction, and a task:interrupt control event is sent via SSE
// to notify the agents currently executing.
//
// Steps:
//  1. For agent operations, perform a resource-level permission check (task:execute)
//  2. Query all nodes of the task
//  3. Collect the currently-executing agents (used later to send interrupt events)
//  4. Within a transaction, set all in_progress nodes to manual_intervention
//  5. Commit the transaction
//  6. Publish a node:pending event via SSE
//  7. Send a task:interrupt control event to the currently-executing agents (Redis buffered)
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//   - operatorID: operator ID
//   - operatorType: operator type (member/agent)
//   - comment: interrupt comment (optional)
//
// Returns:
//   - *InterruptTaskResult: contains the task ID and the number of interrupted nodes
//   - error: possible error (database operation failure)
func (s *NodeService) InterruptTask(ctx context.Context, taskID int32, operatorID uuid.UUID, operatorType, comment string) (*InterruptTaskResult, error) {
	if operatorType == "" {
		operatorType = "member"
	}

	if operatorType == "agent" {
		task, err := s.svc.Store.GetTask(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("get task: %w", err)
		}
		permSvc := NewAgentPermissionService(s.svc)
		hasPerm, err := permSvc.HasResourcePermission(ctx, operatorID, types.PermTaskExecute, "project", projPtrFromString(task.ProjectID))
		if err != nil {
			return nil, fmt.Errorf("check execute permission: %w", err)
		}
		if !hasPerm {
			return nil, fmt.Errorf("agent does not have task:execute permission for this project")
		}
	}

	targets, interruptedCount, err := s.svc.Store.InterruptInProgressNodes(ctx, taskID, operatorID, operatorType, comment)
	if err != nil {
		return nil, err
	}

	s.publishNodePendingEvent(ctx, taskID)

	for _, target := range targets {
		s.svc.PublishControlEvent(ctx, target.AssigneeID, types.EventTaskInterrupt, map[string]interface{}{
			"task_id": fmt.Sprintf("%d", taskID),
			"node_id": target.NodeID.String(),
		})
	}

	return &InterruptTaskResult{
		TaskID:           taskID,
		InterruptedNodes: interruptedCount,
	}, nil
}

// publishNodeEventAfterApprove checks newly unblocked nodes after a node is approved, and
// publishes node:pending or node:continuation_invite events via SSE.
// It also checks DAG dependencies to discover nodes that may have just become available.
//
// Steps:
//  1. Query task and project info
//  2. Query all ready nodes (pending nodes whose DAG dependencies are all completed)
//  3. Broadcast a node:pending event to the workspace
//  4. Check in_progress nodes that hold a continuation right and send node:continuation_invite events
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
func (s *NodeService) publishNodeEventAfterApprove(ctx context.Context, taskID int32) {
	task, err := s.svc.Store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	projectID := uuidFromStr(task.ProjectID)

	readyNodes, err := s.svc.Store.GetReadyNodes(ctx, taskID)
	if err != nil {
		return
	}

	for _, node := range readyNodes {
		s.svc.publishToProject(ctx, projectID, types.EventNodePending, map[string]interface{}{
			"task_id":    fmt.Sprintf("%d", taskID),
			"node_id":    node.ID,
			"project_id": task.ProjectID,
		})
	}

	nodes, err := s.svc.Store.ListTaskNodes(ctx, taskID)
	if err != nil {
		return
	}
	for _, node := range nodes {
		if node.Status == types.TaskNodeStatusInProgress && node.ReservedForAgentID != nil {
			reservedID, _ := uuid.Parse(*node.ReservedForAgentID)
			s.svc.PublishControlEvent(ctx, reservedID, types.EventNodeContinuationInvite, map[string]interface{}{
				"task_id":    fmt.Sprintf("%d", taskID),
				"node_id":    node.ID,
				"project_id": task.ProjectID,
			})
		}
	}
}

// publishNodePendingEvent publishes a node:pending event to the workspace via SSE,
// notifying agents that a node can be claimed.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
func (s *NodeService) publishNodePendingEvent(ctx context.Context, taskID int32) {
	task, err := s.svc.Store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	s.svc.publishToProject(ctx, uuidFromStr(task.ProjectID), types.EventNodePending, map[string]interface{}{
		"task_id":    fmt.Sprintf("%d", taskID),
		"project_id": task.ProjectID,
	})
}
