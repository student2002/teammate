// node.go provides data access operations for workflow nodes.
//
// This file contains CRUD operations and core transactional operations for nodes:
//   - ApproveNodeInTx: approve a node completion (including continuation right passing, task completion detection)
//   - RejectNodeInTx: reject a node (including roll back to target node, max reject cycles check)
//   - ClaimTaskNode: optimistic lock claim (version field prevents concurrency)
//   - UpdateTaskNodeStatus: optimistic lock status update
//
// Node state machine (5 real states):
//
//	pending → in_progress → completed
//	               ↓              ↓
//	      manual_intervention   rejected → (roll back to target node)
//
// Transactional operations use database transactions to guarantee atomicity and
// automatically roll back on failure.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// GetTaskNode queries a single workflow node record by ID.
//
// Parameters:
//   - ctx: request context, supports timeout and cancellation
//   - id: UUID identifier of the node
//
// Returns:
//   - db.TaskNode: node record, including status, assignment info, version number, etc.
//   - error: error returned when the query fails (e.g. node does not exist)
func (s *Store) GetTaskNode(ctx context.Context, id uuid.UUID) (types.TaskNode, error) {
	node, err := s.q.GetTaskNode(ctx, id)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ClaimTaskNode uses an optimistic lock (version field) to try to claim a pending node.
//
// Claim conditions (SQL WHERE clause):
//   - The node status must be pending
//   - reserved_for_agent_id is empty, or equals the current agent ID (continuation right)
//
// After a successful claim, the node status becomes in_progress and the version field increments.
// If another agent claims simultaneously, the version mismatch causes the SQL to affect 0 rows
// and returns an error.
//
// Parameters:
//   - ctx: request context
//   - params: claim params, including node ID, agent ID, and expected version
//
// Returns:
//   - db.TaskNode: the node record after a successful claim (version already incremented)
//   - error: error returned when the claim fails (e.g. already claimed, version conflict)
func (s *Store) ClaimTaskNode(ctx context.Context, params types.ClaimTaskNodeParams) (types.TaskNode, error) {
	dbParams, err := FromDomainClaimTaskNodeParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert claim task node params: %w", err)
	}
	node, err := s.q.ClaimTaskNode(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("claim task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ClaimTaskNodeByHuman uses an optimistic lock to let a human claim a pending node with assignee_type=human.
func (s *Store) ClaimTaskNodeByHuman(ctx context.Context, params types.ClaimTaskNodeByHumanParams) (types.TaskNode, error) {
	dbParams, err := FromDomainClaimTaskNodeByHumanParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert claim task node by human params: %w", err)
	}
	node, err := s.q.ClaimTaskNodeByHuman(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("claim task node by human: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ReclaimTaskNode uses an optimistic lock to re-claim a node already claimed by the same agent.
//
// Used to restore the claim state when an agent reconnects. Unlike ClaimTaskNode, ReclaimTaskNode
// requires the node status to be in_progress and reserved_for_agent_id to match.
//
// Parameters:
//   - ctx: request context
//   - params: re-claim params, including node ID, agent ID, and expected version
//
// Returns:
//   - db.TaskNode: the node record after a successful re-claim
//   - error: error returned when the re-claim fails
func (s *Store) ReclaimTaskNode(ctx context.Context, params types.ReclaimTaskNodeParams) (types.TaskNode, error) {
	dbParams, err := FromDomainReclaimTaskNodeParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert reclaim task node params: %w", err)
	}
	node, err := s.q.ReclaimTaskNode(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("reclaim task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// UpdateTaskNodeStatus uses an optimistic lock (version field) to update the node status.
//
// Update conditions (SQL WHERE clause):
//   - Node ID matches
//   - Current status matches the Status_2 parameter
//   - version field matches (prevents concurrency)
//
// On successful update the version increments. Commonly used for state machine transitions:
// pending → in_progress → completed/rejected/manual_intervention
//
// Parameters:
//   - ctx: request context
//   - params: update params, including node ID, target status, current status, version, etc.
//
// Returns:
//   - db.TaskNode: the node record after a successful update
//   - error: error returned when the update fails (e.g. version conflict, status mismatch)
func (s *Store) UpdateTaskNodeStatus(ctx context.Context, params types.UpdateTaskNodeStatusParams) (types.TaskNode, error) {
	dbParams, err := FromDomainUpdateTaskNodeStatusParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert update task node status params: %w", err)
	}
	node, err := s.q.UpdateTaskNodeStatus(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update task node status: %w", err)
	}
	return ToDomainTaskNode(node)
}

// CreateNodeTransition creates a node status transition audit record.
//
// A transition record is created on every node status change, used for auditing and debugging.
// The record includes: source status, target status, action type (approve/reject/manual, etc.),
// operator info, and comment content.
//
// Parameters:
//   - ctx: request context
//   - params: transition record params, including node ID, source status, target status, action type, etc.
//
// Returns:
//   - types.NodeTransition: the created transition record
//   - error: error returned when creation fails
func (s *Store) CreateNodeTransition(ctx context.Context, params types.CreateNodeTransitionParams) (types.NodeTransition, error) {
	dbParams, err := FromDomainCreateNodeTransitionParams(params)
	if err != nil {
		return types.NodeTransition{}, fmt.Errorf("convert create node transition params: %w", err)
	}
	transition, err := s.q.CreateNodeTransition(ctx, dbParams)
	if err != nil {
		return types.NodeTransition{}, fmt.Errorf("create node transition: %w", err)
	}
	return ToDomainNodeTransition(transition)
}

// GetNextTaskNode retrieves the next node after the specified node (ordered by sort_order).
//
// Used to determine the next node to process after approving a node. If the current node is the
// last one, sql.ErrNoRows is returned.
//
// Parameters:
//   - ctx: request context
//   - params: query params, including taskID and current node ID
//
// Returns:
//   - db.TaskNode: the next node record
//   - error: returns sql.ErrNoRows when there is no next node
func (s *Store) GetNextTaskNode(ctx context.Context, params types.GetNextTaskNodeParams) (types.TaskNode, error) {
	dbParams, err := FromDomainGetNextTaskNodeParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert get next task node params: %w", err)
	}
	node, err := s.q.GetNextTaskNode(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get next task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// GetPrevTaskNode retrieves the previous node before the specified node (ordered by sort_order).
//
// Used to determine the roll back target when rejecting a node. If the current node is the
// first one, sql.ErrNoRows is returned.
//
// Parameters:
//   - ctx: request context
//   - params: query params, including taskID and current node ID
//
// Returns:
//   - db.TaskNode: the previous node record
//   - error: returns sql.ErrNoRows when there is no previous node
func (s *Store) GetPrevTaskNode(ctx context.Context, params types.GetPrevTaskNodeParams) (types.TaskNode, error) {
	dbParams, err := FromDomainGetPrevTaskNodeParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert get prev task node params: %w", err)
	}
	node, err := s.q.GetPrevTaskNode(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get prev task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// GetPrevStandardNodeAssignee retrieves the assignee of the previous standard (non-review type) node.
//
// Used for self-review avoidance check: the reviewer of a review node cannot be the executor
// of the previous standard node.
//
// Parameters:
//   - ctx: request context
//   - params: query params, including taskID and current review node ID
//
// Returns:
//   - uuid.NullUUID: the assignee ID of the previous standard node (empty if none)
//   - error: error returned when the query fails
func (s *Store) GetPrevStandardNodeAssignee(ctx context.Context, params types.GetPrevStandardNodeAssigneeParams) (uuid.NullUUID, error) {
	dbParams, err := FromDomainGetPrevStandardNodeAssigneeParams(params)
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("convert get prev standard node assignee params: %w", err)
	}
	assignee, err := s.q.GetPrevStandardNodeAssignee(ctx, dbParams)
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("get prev standard node assignee: %w", err)
	}
	return assignee, nil
}

// IncrementRejectCount increments the reject count of a node.
//
// Called every time a node is rejected, used to check whether max reject cycles
// (max_reject_cycles) has been exceeded.
// When the threshold is exceeded, the node will be set to manual_intervention status.
//
// Parameters:
//   - ctx: request context
//   - nodeID: UUID identifier of the node
//
// Returns:
//   - db.TaskNode: the updated node record (reject_count already incremented)
//   - error: error returned when the update fails
func (s *Store) IncrementRejectCount(ctx context.Context, nodeID uuid.UUID) (types.TaskNode, error) {
	node, err := s.q.IncrementRejectCount(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("increment reject count: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ResetRejectCount resets the reject count and status of a node.
//
// Used for node resets in special cases (e.g. recovery after manual intervention).
//
// Parameters:
//   - ctx: request context
//   - params: reset params, including node ID and target status
//
// Returns:
//   - types.TaskNode: the reset node record
//   - error: error returned when the reset fails
func (s *Store) ResetRejectCount(ctx context.Context, params types.ResetRejectCountParams) (types.TaskNode, error) {
	dbParams, err := FromDomainResetRejectCountParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert reset reject count params: %w", err)
	}
	node, err := s.q.ResetRejectCount(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("reset reject count: %w", err)
	}
	return ToDomainTaskNode(node)
}

// UpdateNodeSummary updates the execution summary of a node.
//
// The summary is submitted by the Agent after execution completes, containing key information
// about task execution.
// Only the assigned Agent or a member with write permission can update it.
//
// Parameters:
//   - ctx: request context
//   - nodeID: UUID identifier of the node
//   - summary: execution summary text
//
// Returns:
//   - types.TaskNode: the updated node record
//   - error: error returned when the update fails
func (s *Store) UpdateNodeSummary(ctx context.Context, nodeID uuid.UUID, summary string) (types.TaskNode, error) {
	node, err := s.q.UpdateNodeSummary(ctx, db.UpdateNodeSummaryParams{
		ID:      nodeID,
		Summary: summary,
	})
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node summary: %w", err)
	}
	return ToDomainTaskNode(node)
}

// UpdateTaskStatus updates the overall status of a task.
//
// Task status change triggers:
//   - active → completed: when all nodes are completed
//   - active → cancelled: when the task is deleted (soft delete)
//
// Parameters:
//   - ctx: request context
//   - params: update params, including task ID and target status
//
// Returns:
//   - types.Task: the updated task record
//   - error: error returned when the update fails
func (s *Store) UpdateTaskStatus(ctx context.Context, params types.UpdateTaskStatusParams) (types.Task, error) {
	dbParams, err := FromDomainUpdateTaskStatusParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert update task status params: %w", err)
	}
	task, err := s.q.UpdateTaskStatus(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("update task status: %w", err)
	}
	return ToDomainTask(task)
}

// GetReadyNodes returns all pending nodes in the specified task that can be claimed.
//
// "Claimable" conditions:
//   - Status is pending
//   - All DAG dependency (depends_on) nodes are completed
//
// Used by the Agent to poll for the list of claimable nodes.
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//
// Returns:
//   - []db.TaskNode: list of claimable nodes
//   - error: error returned when the query fails
func (s *Store) GetReadyNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	nodes, err := s.q.GetReadyNodes(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get ready nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// GetInProgressNodesByAgent queries nodes claimed but not yet completed (in_progress) by the
// specified Agent in the specified workspace.
// Used to resume unfinished execution after an Agent restart.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.GetInProgressNodesByAgentRow: list of in_progress nodes (including project_id)
//   - error: error returned when the query fails
func (s *Store) GetInProgressNodesByAgent(ctx context.Context, agentID uuid.UUID, workspaceID uuid.UUID) ([]types.GetInProgressNodesByAgentRow, error) {
	rows, err := s.q.GetInProgressNodesByAgent(ctx, db.GetInProgressNodesByAgentParams{
		AssigneeID:  uuid.NullUUID{UUID: agentID, Valid: true},
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("get in-progress nodes by agent: %w", err)
	}
	return ToDomainGetInProgressNodesByAgentRowSlice(rows)
}

// ApproveNodeInTx performs the node approval operation within a transaction.
//
// This is the core transactional operation of the node state machine, including the following steps:
//  1. Update the current node status to completed
//  2. Create a status transition record
//  3. Find the next node:
//     - If there is no next node, check whether all nodes are completed; when completed, set the task to completed
//     - If the next node is already in_progress, pass the continuation right (reserved_for_agent_id)
//     - If the next node is pending, set its status and pass the continuation right
//
// Continuation right logic:
//   - After a standard node completes, the next non-review node gets a 5-minute continuation right
//   - If the next node's timeout is > 30 minutes, the continuation right is extended to 15 minutes
//   - A review node does not pass the continuation right to the executor of the previous standard node
//
// Parameters:
//   - ctx: request context
//   - nodeID: ID of the node to approve
//   - currentNode: full record of the current node (including version for the optimistic lock)
//   - completedBy: completer ID (Agent or Member)
//   - operatorID: operator ID
//   - operatorType: operator type ("agent" or "member")
//   - comment: approval comment (optional)
//
// Returns:
//   - db.TaskNode: the node record after approval (status = completed)
//   - error: error returned when the transaction fails (auto rollback)
func (s *Store) ApproveNodeInTx(ctx context.Context, nodeID uuid.UUID, currentNode types.TaskNode, completedBy uuid.NullUUID, operatorID uuid.NullUUID, operatorType string, comment string) (types.TaskNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	q := s.q.WithTx(tx)

	// Step 1: update the current node status to completed
	node, err := q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
	   ID:                   nodeID,
	   Status:               db.TaskNodeStatusCompleted,
	   AssigneeType:         db.AssigneeType(currentNode.AssigneeType),
	   AssigneeID:           stringToNullUUID(currentNode.AssigneeID),
	   ReservedForAgentID:   stringToNullUUID(currentNode.ReservedForAgentID),
	   RejectCount:          int32(currentNode.RejectCount),
	   CompletedAt:          sql.NullTime{Time: s.Clock.Now(), Valid: true},
	   CompletedBy:          completedBy,
	   ReservationExpiresAt: sql.NullTime{},
	   Version:              int32(currentNode.Version),
	   Status_2:             db.TaskNodeStatus(currentNode.Status), // use the current status, supports pending or in_progress
	  })
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node to completed: %w", err)
	}

	// Step 2: create a status transition record
	_, _ = q.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
		TaskNodeID:   nodeID,
		FromStatus:   db.TaskNodeStatus(currentNode.Status),
		ToStatus:     db.TaskNodeStatusCompleted,
		Action:       db.TransitionActionApprove,
		Comment:      sql.NullString{String: comment, Valid: comment != ""},
		OperatorID:   operatorID,
		OperatorType: operatorType,
	})

	// Step 3: find the next node
	nextNode, err := q.GetNextTaskNode(ctx, db.GetNextTaskNodeParams{
		TaskID: currentNode.TaskID,
		ID:     nodeID,
	})
	if err == sql.ErrNoRows {
		// No next node — check whether all nodes are completed
		nodeCount, err := q.GetTaskNodeCount(ctx, currentNode.TaskID)
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("get task node count: %w", err)
		}
		completedCount, err := q.GetCompletedNodeCount(ctx, currentNode.TaskID)
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("get completed node count: %w", err)
		}
		if completedCount < nodeCount {
			return types.TaskNode{}, fmt.Errorf("cannot complete task: %d/%d nodes completed", completedCount, nodeCount)
		}
		// All nodes completed — set the task status to completed
		_, err = q.UpdateTaskStatus(ctx, db.UpdateTaskStatusParams{
			ID:     currentNode.TaskID,
			Status: db.TaskStatusCompleted,
		})
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("update task status to completed: %w", err)
		}
	} else if err != nil {
		return types.TaskNode{}, fmt.Errorf("get next node: %w", err)
	} else {
		// There is a next node: set its status and continuation right
		if nextNode.Status == db.TaskNodeStatusInProgress {
			// The next node has already been claimed — do not overwrite the status, only pass the continuation right
			if stringToNullUUID(currentNode.AssigneeID).Valid && !(nextNode.NodeType == db.NodeTypeReview && db.NodeType(currentNode.NodeType) == db.NodeTypeStandard) {
				var continuationReservationExpiresAt sql.NullTime
				expiration := s.Clock.Now().Add(5 * time.Minute)
				if nextNode.TimeoutMinutes > 30 {
					expiration = s.Clock.Now().Add(15 * time.Minute)
				}
				continuationReservationExpiresAt = sql.NullTime{Time: expiration, Valid: true}

				_, err = q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
					ID:                   nextNode.ID,
					Status:               nextNode.Status, // keep in_progress
					AssigneeType:         nextNode.AssigneeType,
					AssigneeID:           nextNode.AssigneeID,
					ReservedForAgentID:   stringToNullUUID(currentNode.AssigneeID), // only pass the continuation right
					RejectCount:          nextNode.RejectCount,
					CompletedAt:          sql.NullTime{},
					CompletedBy:          uuid.NullUUID{},
					ReservationExpiresAt: continuationReservationExpiresAt,
					Version:              nextNode.Version,
					Status_2:             db.TaskNodeStatusInProgress,
				})
				if err != nil {
					return types.TaskNode{}, fmt.Errorf("update next node continuation: %w", err)
				}
			}
		} else {
			// The next node is pending — set the status and pass the continuation right
			nextStatus := db.TaskNodeStatusPending
			nextAssigneeType := nextNode.AssigneeType
			nextAssigneeID := nextNode.AssigneeID
			nextReservedForAgentID := nextNode.ReservedForAgentID

			// Set the continuation right: after a standard node completes, the next non-review node gets the continuation right
			if stringToNullUUID(currentNode.AssigneeID).Valid {
				if !(nextNode.NodeType == db.NodeTypeReview && db.NodeType(currentNode.NodeType) == db.NodeTypeStandard) {
					nextReservedForAgentID = stringToNullUUID(currentNode.AssigneeID)
				}
			}

			var nextReservationExpiresAt sql.NullTime
			if nextReservedForAgentID.Valid {
				expiration := s.Clock.Now().Add(5 * time.Minute)
				if nextNode.TimeoutMinutes > 30 {
					expiration = s.Clock.Now().Add(15 * time.Minute)
				}
				nextReservationExpiresAt = sql.NullTime{Time: expiration, Valid: true}
			}

			_, err = q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
				ID:                   nextNode.ID,
				Status:               nextStatus,
				AssigneeType:         nextAssigneeType,
				AssigneeID:           nextAssigneeID,
				ReservedForAgentID:   nextReservedForAgentID,
				RejectCount:          nextNode.RejectCount,
				CompletedAt:          sql.NullTime{},
				CompletedBy:          uuid.NullUUID{},
				ReservationExpiresAt: nextReservationExpiresAt,
				Version:              nextNode.Version,
				Status_2:             nextNode.Status,
			})
			if err != nil {
				return types.TaskNode{}, fmt.Errorf("update next node: %w", err)
			}

			// Create a transition record for the next node
			_, _ = q.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
				TaskNodeID:   nextNode.ID,
				FromStatus:   nextNode.Status,
				ToStatus:     nextStatus,
				Action:       db.TransitionActionReclaim,
				OperatorID:   operatorID,
				OperatorType: operatorType,
			})
		}

		if comment != "" {
			currentNodeID, _ := stringToUUID(currentNode.ID)
			_, err = q.CreateComment(ctx, db.CreateCommentParams{
				TaskID:       currentNode.TaskID,
				NodeID:       uuid.NullUUID{UUID: nextNode.ID, Valid: true},
				SourceNodeID: uuid.NullUUID{UUID: currentNodeID, Valid: true},
				AuthorType:   operatorType,
				AuthorID:     operatorID.UUID,
				Content:      comment,
				CommentType:  "handoff",
				Metadata:     pqtype.NullRawMessage{},
				Mentions:     []uuid.UUID{},
			})
			if err != nil {
				return types.TaskNode{}, fmt.Errorf("create handoff comment: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return types.TaskNode{}, fmt.Errorf("commit tx: %w", err)
	}

	return ToDomainTaskNode(node)
}

// RejectNodeInTx performs the node rejection operation within a transaction.
//
// Rejection flow:
//  1. Update the current node status to rejected, increment reject_count
//  2. Create a status transition record
//  3. Handle the target node:
//     - If the target node's reject_count >= max_review_cycles, set it to manual_intervention
//     - Otherwise set the target node to pending, preserving reserved_for_agent_id
//  4. Automatically create a rejection comment
//
// Target node selection logic:
//   - By default roll back to the previous node (when targetNodeID is empty)
//   - Can roll back to any node with a smaller sort_order
//   - Cannot roll back to manual or human type nodes
//
// Parameters:
//   - ctx: request context
//   - nodeID: ID of the node to reject
//   - currentNode: full record of the current node
//   - targetNode: full record of the roll back target node
//   - maxReviewCycles: max reject cycles configured for the project
//   - operatorID: operator ID
//   - operatorType: operator type
//   - targetNodeID: target node ID (optional, used to specify the roll back target)
//   - comment: rejection comment
//
// Returns:
//   - db.TaskNode: the node record after rejection (status = rejected)
//   - error: error returned when the transaction fails (auto rollback)
func (s *Store) RejectNodeInTx(ctx context.Context, nodeID uuid.UUID, currentNode types.TaskNode, targetNode types.TaskNode, maxReviewCycles int32, operatorID uuid.NullUUID, operatorType string, targetNodeID uuid.NullUUID, comment string) (types.TaskNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	q := s.q.WithTx(tx)

	// Step 1: update the current node status to rejected, increment reject_count
	node, err := q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
		ID:                   nodeID,
		Status:               db.TaskNodeStatusRejected,
		AssigneeType:         db.AssigneeType(currentNode.AssigneeType),
		AssigneeID:           stringToNullUUID(currentNode.AssigneeID),
		ReservedForAgentID:   stringToNullUUID(currentNode.ReservedForAgentID),
		RejectCount:          int32(currentNode.RejectCount) + 1,
		CompletedAt:          sql.NullTime{},
		CompletedBy:          uuid.NullUUID{},
		ReservationExpiresAt: sql.NullTime{},
		Version:              int32(currentNode.Version),
		Status_2:             db.TaskNodeStatusInProgress,
	})
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node to rejected: %w", err)
	}

	// Step 2: create a status transition record
	_, _ = q.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
		TaskNodeID:   nodeID,
		FromStatus:   db.TaskNodeStatus(currentNode.Status),
		ToStatus:     db.TaskNodeStatusRejected,
		Action:       db.TransitionActionReject,
		TargetNodeID: targetNodeID,
		Comment:      sql.NullString{String: comment, Valid: comment != ""},
		OperatorID:   operatorID,
		OperatorType: operatorType,
	})

	// Step 3: increment the target node's reject_count
	targetNodeUUID, _ := stringToUUID(targetNode.ID)
	updatedTargetNode, err := q.IncrementRejectCount(ctx, targetNodeUUID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("increment reject count: %w", err)
	}

	// Step 4: check whether max reject cycles has been exceeded
	if updatedTargetNode.RejectCount >= maxReviewCycles {
		// Threshold exceeded — target node transitions to manual_intervention
		_, err = q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
			ID:                   targetNodeUUID,
			Status:               db.TaskNodeStatusManualIntervention,
			AssigneeType:         db.AssigneeTypeHuman,
			AssigneeID:           updatedTargetNode.AssigneeID,
			ReservedForAgentID:   uuid.NullUUID{},
			RejectCount:          updatedTargetNode.RejectCount,
			CompletedAt:          sql.NullTime{},
			CompletedBy:          uuid.NullUUID{},
			ReservationExpiresAt: sql.NullTime{},
			Version:              updatedTargetNode.Version,
			Status_2:             updatedTargetNode.Status,
		})
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("set target node to manual_intervention: %w", err)
		}
		_, _ = q.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
			TaskNodeID:   targetNodeUUID,
			FromStatus:   updatedTargetNode.Status,
			ToStatus:     db.TaskNodeStatusManualIntervention,
			Action:       db.TransitionActionManual,
			OperatorID:   operatorID,
			OperatorType: operatorType,
		})
	} else {
		// Threshold not exceeded — target node returns to pending, preserving reserved_for_agent_id
		_, err = q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
			ID:                   targetNodeUUID,
			Status:               db.TaskNodeStatusPending,
			AssigneeType:         db.AssigneeTypeAnyAgent,
			AssigneeID:           uuid.NullUUID{},
			ReservedForAgentID:   uuid.NullUUID{UUID: updatedTargetNode.AssigneeID.UUID, Valid: updatedTargetNode.AssigneeID.Valid},
			RejectCount:          updatedTargetNode.RejectCount,
			CompletedAt:          sql.NullTime{},
			CompletedBy:          uuid.NullUUID{},
			ReservationExpiresAt: sql.NullTime{},
			Version:              updatedTargetNode.Version,
			Status_2:             updatedTargetNode.Status,
		})
		if err != nil {
			return types.TaskNode{}, fmt.Errorf("set target node to pending: %w", err)
		}
		_, _ = q.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
			TaskNodeID:   targetNodeUUID,
			FromStatus:   updatedTargetNode.Status,
			ToStatus:     db.TaskNodeStatusPending,
			Action:       db.TransitionActionReject,
			TargetNodeID: uuid.NullUUID{UUID: nodeID, Valid: true},
			OperatorID:   operatorID,
			OperatorType: operatorType,
		})
	}

	// Step 5: automatically create a rejection comment
	currentNodeUUID, _ := stringToUUID(currentNode.ID)
	commentContent := fmt.Sprintf("Node \"%s\" rejected, routing back to node \"%s\"", currentNode.Name, targetNode.Name)
	if comment != "" {
		commentContent = fmt.Sprintf("Node \"%s\" rejected: %s — routing back to node \"%s\"", currentNode.Name, comment, targetNode.Name)
	}
	_, _ = q.CreateComment(ctx, db.CreateCommentParams{
		TaskID:       currentNode.TaskID,
		NodeID:       uuid.NullUUID{UUID: targetNodeUUID, Valid: true},
		SourceNodeID: uuid.NullUUID{UUID: currentNodeUUID, Valid: true},
		AuthorType:   operatorType,
		AuthorID:     operatorID.UUID,
		Content:      commentContent,
		CommentType:  "decision",
		Metadata:     pqtype.NullRawMessage{},
		Mentions:     []uuid.UUID{},
	})

	if err := tx.Commit(); err != nil {
		return types.TaskNode{}, fmt.Errorf("commit tx: %w", err)
	}

	return ToDomainTaskNode(node)
}
