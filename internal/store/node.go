// node.go 提供工作流节点的数据访问操作。
//
// 本文件包含节点的 CRUD 操作和核心事务性操作：
//   - ApproveNodeInTx: 批准节点完成（含续约权传递、任务完成检测）
//   - RejectNodeInTx: 驳回节点（含回退目标节点、最大驳回轮次检查）
//   - ClaimTaskNode: 乐观锁认领（version 字段防并发）
//   - UpdateTaskNodeStatus: 乐观锁状态更新
//
// 节点状态机（5 种真实状态）：
//
//	pending → in_progress → completed
//	               ↓              ↓
//	      manual_intervention   rejected → (回退到目标节点)
//
// 事务操作使用数据库事务保证原子性，失败时自动回滚。
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

// GetTaskNode 根据 ID 查询单个工作流节点记录。
//
// 参数：
//   - ctx: 请求上下文，支持超时和取消
//   - id: 节点的 UUID 标识符
//
// 返回：
//   - db.TaskNode: 节点记录，包含状态、分配信息、版本号等
//   - error: 查询失败时返回错误（如节点不存在）
func (s *Store) GetTaskNode(ctx context.Context, id uuid.UUID) (types.TaskNode, error) {
	node, err := s.q.GetTaskNode(ctx, id)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ClaimTaskNode 使用乐观锁（version 字段）尝试认领一个待处理节点。
//
// 认领条件（SQL WHERE 子句）：
//   - 节点状态必须为 pending
//   - reserved_for_agent_id 为空，或等于当前代理 ID（续约权）
//
// 成功认领后，节点状态变为 in_progress，version 字段递增。
// 如果其他代理同时认领，version 不匹配导致 SQL 影响 0 行，返回错误。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 认领参数，包含节点 ID、代理 ID、期望的 version
//
// 返回：
//   - db.TaskNode: 认领成功后的节点记录（version 已递增）
//   - error: 认领失败时返回错误（如已被认领、version 冲突）
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

// ClaimTaskNodeByHuman 使用乐观锁让人类认领一个 assignee_type=human 的待处理节点。
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

// ReclaimTaskNode 使用乐观锁重新认领一个已被同一代理认领的节点。
//
// 用于代理重连时恢复认领状态。与 ClaimTaskNode 不同，ReclaimTaskNode
// 要求节点状态为 in_progress 且 reserved_for_agent_id 匹配。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 重新认领参数，包含节点 ID、代理 ID、期望的 version
//
// 返回：
//   - db.TaskNode: 重新认领成功后的节点记录
//   - error: 重新认领失败时返回错误
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

// UpdateTaskNodeStatus 使用乐观锁（version 字段）更新节点状态。
//
// 更新条件（SQL WHERE 子句）：
//   - 节点 ID 匹配
//   - 当前状态匹配 Status_2 参数
//   - version 字段匹配（防并发）
//
// 成功更新后 version 递增。常用于状态机转换：
// pending → in_progress → completed/rejected/manual_intervention
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含节点 ID、目标状态、当前状态、version 等
//
// 返回：
//   - db.TaskNode: 更新成功后的节点记录
//   - error: 更新失败时返回错误（如 version 冲突、状态不匹配）
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

// CreateNodeTransition 创建节点状态流转审计记录。
//
// 每次节点状态变更都会创建一条流转记录，用于审计和调试。
// 记录包含：源状态、目标状态、操作类型（approve/reject/manual 等）、
// 操作者信息、评论内容。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 流转记录参数，包含节点 ID、源状态、目标状态、操作类型等
//
// 返回：
//   - types.NodeTransition: 创建的流转记录
//   - error: 创建失败时返回错误
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

// GetNextTaskNode 获取指定节点的下一个节点（按 sort_order 排序）。
//
// 用于批准节点后确定下一个需要处理的节点。如果当前节点是最后一个，
// 返回 sql.ErrNoRows。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含 taskID 和当前节点 ID
//
// 返回：
//   - db.TaskNode: 下一个节点记录
//   - error: 无下一个节点时返回 sql.ErrNoRows
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

// GetPrevTaskNode 获取指定节点的上一个节点（按 sort_order 排序）。
//
// 用于驳回节点时确定回退目标。如果当前节点是第一个节点，
// 返回 sql.ErrNoRows。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含 taskID 和当前节点 ID
//
// 返回：
//   - db.TaskNode: 上一个节点记录
//   - error: 无上一个节点时返回 sql.ErrNoRows
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

// GetPrevStandardNodeAssignee 获取前一个标准节点（非 review 类型）的分配者。
//
// 用于自我审查回避检查：review 节点的审查者不能是前一个 standard 节点的执行者。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含 taskID 和当前 review 节点 ID
//
// 返回：
//   - uuid.NullUUID: 前一个标准节点的分配者 ID（如果没有则为空）
//   - error: 查询失败时返回错误
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

// IncrementRejectCount 增加节点的驳回次数。
//
// 每次节点被驳回时调用，用于检查是否超过最大驳回轮次（max_reject_cycles）。
// 超过阈值时，节点将被设为 manual_intervention 状态。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点的 UUID 标识符
//
// 返回：
//   - db.TaskNode: 更新后的节点记录（reject_count 已递增）
//   - error: 更新失败时返回错误
func (s *Store) IncrementRejectCount(ctx context.Context, nodeID uuid.UUID) (types.TaskNode, error) {
	node, err := s.q.IncrementRejectCount(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("increment reject count: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ResetRejectCount 重置节点的驳回次数和状态。
//
// 用于特殊情况下的节点重置（如人工干预后恢复）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 重置参数，包含节点 ID 和目标状态
//
// 返回：
//   - types.TaskNode: 重置后的节点记录
//   - error: 重置失败时返回错误
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

// UpdateNodeSummary 更新节点的执行摘要。
//
// 摘要由 Agent 在执行完成后提交，包含任务执行的关键信息。
// 仅分配的 Agent 或具有写权限的成员可以更新。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点的 UUID 标识符
//   - summary: 执行摘要文本
//
// 返回：
//   - types.TaskNode: 更新后的节点记录
//   - error: 更新失败时返回错误
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

// UpdateTaskStatus 更新任务的整体状态。
//
// 任务状态变更触发条件：
//   - active → completed: 所有节点完成时
//   - active → cancelled: 任务被删除（软删除）时
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含任务 ID 和目标状态
//
// 返回：
//   - types.Task: 更新后的任务记录
//   - error: 更新失败时返回错误
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

// GetReadyNodes 返回指定任务中所有可以认领的待处理节点。
//
// "可认领"的条件：
//   - 状态为 pending
//   - 所有 DAG 依赖（depends_on）节点已完成
//
// 用于 Agent 轮询时获取可认领的节点列表。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的整数 ID
//
// 返回：
//   - []db.TaskNode: 可认领的节点列表
//   - error: 查询失败时返回错误
func (s *Store) GetReadyNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	nodes, err := s.q.GetReadyNodes(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get ready nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// GetInProgressNodesByAgent 查询指定 Agent 在指定工作区中认领但未完成（in_progress）的节点。
// 用于 Agent 重启后恢复未完成的执行。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.GetInProgressNodesByAgentRow: in_progress 节点列表（含 project_id）
//   - error: 查询失败时返回错误
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

// ApproveNodeInTx 在事务中执行节点批准操作。
//
// 这是节点状态机的核心事务操作，包含以下步骤：
//  1. 将当前节点状态更新为 completed
//  2. 创建状态流转记录
//  3. 查找下一个节点：
//     - 如果没有下一个节点，检查所有节点是否完成，完成后将任务设为 completed
//     - 如果下一个节点已在 in_progress 状态，传递续约权（reserved_for_agent_id）
//     - 如果下一个节点是 pending 状态，设置其状态并传递续约权
//
// 续约权逻辑：
//   - 标准节点完成后，下一个非 review 节点获得 5 分钟续约权
//   - 如果下一个节点超时时间 > 30 分钟，续约权延长至 15 分钟
//   - review 节点不传递续约权给前一个 standard 节点的执行者
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 要批准的节点 ID
//   - currentNode: 当前节点的完整记录（包含 version 用于乐观锁）
//   - completedBy: 完成者 ID（Agent 或 Member）
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（"agent" 或 "member"）
//   - comment: 批准评论（可选）
//
// 返回：
//   - db.TaskNode: 批准后的节点记录（status = completed）
//   - error: 事务失败时返回错误（自动回滚）
func (s *Store) ApproveNodeInTx(ctx context.Context, nodeID uuid.UUID, currentNode types.TaskNode, completedBy uuid.NullUUID, operatorID uuid.NullUUID, operatorType string, comment string) (types.TaskNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	q := s.q.WithTx(tx)

	// 步骤 1：将当前节点状态更新为 completed
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
	   Status_2:             db.TaskNodeStatus(currentNode.Status), // 使用当前状态，支持 pending 或 in_progress
	  })
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node to completed: %w", err)
	}

	// 步骤 2：创建状态流转记录
	_, _ = q.CreateNodeTransition(ctx, db.CreateNodeTransitionParams{
		TaskNodeID:   nodeID,
		FromStatus:   db.TaskNodeStatus(currentNode.Status),
		ToStatus:     db.TaskNodeStatusCompleted,
		Action:       db.TransitionActionApprove,
		Comment:      sql.NullString{String: comment, Valid: comment != ""},
		OperatorID:   operatorID,
		OperatorType: operatorType,
	})

	// 步骤 3：查找下一个节点
	nextNode, err := q.GetNextTaskNode(ctx, db.GetNextTaskNodeParams{
		TaskID: currentNode.TaskID,
		ID:     nodeID,
	})
	if err == sql.ErrNoRows {
		// 没有下一个节点——检查所有节点是否完成
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
		// 所有节点完成——将任务状态设为 completed
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
		// 有下一个节点：设置其状态和续约权
		if nextNode.Status == db.TaskNodeStatusInProgress {
			// 下一个节点已被认领——不覆盖状态，仅传递续约权
			if stringToNullUUID(currentNode.AssigneeID).Valid && !(nextNode.NodeType == db.NodeTypeReview && db.NodeType(currentNode.NodeType) == db.NodeTypeStandard) {
				var continuationReservationExpiresAt sql.NullTime
				expiration := s.Clock.Now().Add(5 * time.Minute)
				if nextNode.TimeoutMinutes > 30 {
					expiration = s.Clock.Now().Add(15 * time.Minute)
				}
				continuationReservationExpiresAt = sql.NullTime{Time: expiration, Valid: true}

				_, err = q.UpdateTaskNodeStatus(ctx, db.UpdateTaskNodeStatusParams{
					ID:                   nextNode.ID,
					Status:               nextNode.Status, // 保持 in_progress
					AssigneeType:         nextNode.AssigneeType,
					AssigneeID:           nextNode.AssigneeID,
					ReservedForAgentID:   stringToNullUUID(currentNode.AssigneeID), // 仅传递续约权
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
			// 下一个节点是 pending 状态——设置状态并传递续约权
			nextStatus := db.TaskNodeStatusPending
			nextAssigneeType := nextNode.AssigneeType
			nextAssigneeID := nextNode.AssigneeID
			nextReservedForAgentID := nextNode.ReservedForAgentID

			// 设置续约权：标准节点完成后，下一个非 review 节点获得续约权
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

			// 为下一个节点创建流转记录
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

// RejectNodeInTx 在事务中执行节点驳回操作。
//
// 驳回流程：
//  1. 将当前节点状态更新为 rejected，reject_count 递增
//  2. 创建状态流转记录
//  3. 处理目标节点：
//     - 如果目标节点的 reject_count >= max_review_cycles，设为 manual_intervention
//     - 否则将目标节点设为 pending，保留 reserved_for_agent_id
//  4. 自动创建驳回评论
//
// 目标节点选择逻辑：
//   - 默认回退到上一个节点（targetNodeID 为空时）
//   - 可以指定回退到任意 sort_order 更小的节点
//   - 不能回退到 manual 或 human 类型的节点
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 要驳回的节点 ID
//   - currentNode: 当前节点的完整记录
//   - targetNode: 回退目标节点的完整记录
//   - maxReviewCycles: 项目配置的最大驳回轮次
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型
//   - targetNodeID: 目标节点 ID（可选，用于指定回退目标）
//   - comment: 驳回评论
//
// 返回：
//   - db.TaskNode: 驳回后的节点记录（status = rejected）
//   - error: 事务失败时返回错误（自动回滚）
func (s *Store) RejectNodeInTx(ctx context.Context, nodeID uuid.UUID, currentNode types.TaskNode, targetNode types.TaskNode, maxReviewCycles int32, operatorID uuid.NullUUID, operatorType string, targetNodeID uuid.NullUUID, comment string) (types.TaskNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	q := s.q.WithTx(tx)

	// 步骤 1：将当前节点状态更新为 rejected，reject_count 递增
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

	// 步骤 2：创建状态流转记录
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

	// 步骤 3：增加目标节点的 reject_count
	targetNodeUUID, _ := stringToUUID(targetNode.ID)
	updatedTargetNode, err := q.IncrementRejectCount(ctx, targetNodeUUID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("increment reject count: %w", err)
	}

	// 步骤 4：检查是否超过最大驳回轮次
	if updatedTargetNode.RejectCount >= maxReviewCycles {
		// 超过阈值——目标节点转为 manual_intervention
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
		// 未超过阈值——目标节点回到 pending，保留 reserved_for_agent_id
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

	// 步骤 5：自动创建驳回评论
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
