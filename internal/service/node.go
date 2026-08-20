// node.go 实现工作流节点的业务逻辑，是系统中最复杂的服务。
// 包含节点认领、批准、拒绝、人工干预、解决等状态机操作，
// 以及 DAG 依赖检查、自我审查回避、续约权管理等功能。
// 所有状态变更通过 SSE 事件通知相关代理，控制事件通过 Redis 缓冲确保不丢失。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// NodeService 提供节点管理相关的业务逻辑，是系统中最复杂的服务。
// 节点状态机：pending → in_progress → completed / rejected / manual_intervention
type NodeService struct {
	svc *Service
}

// NewNodeService 创建一个新的 NodeService 实例。
func NewNodeService(svc *Service) *NodeService {
	return &NodeService{svc: svc}
}

// projPtrFromString 将 domain 风格的 project ID 字符串解析为 *uuid.UUID。
// 解析失败时返回 nil（HasResourcePermission 会将 nil 当作"无特定资源"处理）。
// 未来若 HasResourcePermission 统一改为接受 string，本 helper 可移除。
func projPtrFromString(s string) *uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &u
}

// uuidFromStr 将 domain 风格的字符串解析为 uuid.UUID，解析失败返回 uuid.Nil。
// 用于把 types.Task.ProjectID (string) 传给接受 uuid.UUID 的 Store 方法（如 GetProject）。
// 未来若 Store 方法统一改为接受 string，本 helper 可移除。
func uuidFromStr(s string) uuid.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return u
}

// ClaimNodeResult 保存认领节点操作的结果。
type ClaimNodeResult struct {
	Node types.TaskNode // 认领后的节点信息
}

// Claim 验证并认领一个节点给代理。
// 认领流程包含多层权限和状态检查，确保节点分配的正确性。
//
// 步骤：
//  1. 获取节点信息
//  2. 获取任务信息以检查项目成员关系
//  3. 检查代理是否有权认领（项目成员关系）
//  4. 资源级权限检查：代理必须拥有该项目的 task:claim 权限
//  5. 检查前置节点是否已完成（线性工作流）
//  6. DAG 依赖检查：所有 depends_on 节点必须已完成
//  7. 自我审查回避检查：review 节点不能由前序节点的执行 Agent 认领
//  8. 续约权检查：当前节点已保留给其他代理时，其他代理不能认领
//  9. 执行认领（乐观锁，version 字段）
//  10. 创建状态转换记录
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 要认领的节点 ID
//   - agentID: 认领的代理 ID
//
// 返回：
//   - *ClaimNodeResult: 认领后的节点信息
//   - error: 可能的错误（节点不存在、权限不足、前置节点未完成、已被认领等）
func (s *NodeService) Claim(ctx context.Context, nodeID, operatorID uuid.UUID, operatorType string) (*ClaimNodeResult, error) {
	node, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	// 检查任务状态，已取消或已完成的任务不允许认领
	if task.Status == types.TaskStatusCancelled || task.Status == types.TaskStatusCompleted {
		return nil, fmt.Errorf("task is %s, cannot claim node", task.Status)
	}

	// Agent 认领：检查项目访问权限和 claim 权限
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

	// Agent 认领 review 节点时检查 self-review
	if operatorType == "agent" && node.NodeType == types.NodeTypeReview {
		prevAssigneeID, err := s.svc.Store.GetPrevStandardNodeAssignee(ctx, types.GetPrevStandardNodeAssigneeParams{
			TaskID: node.TaskID,
			NodeID: nodeID.String(),
		})
		if err == nil && prevAssigneeID.Valid && prevAssigneeID.UUID == operatorID {
			return nil, fmt.Errorf("self-review is not allowed: you wrote the code for this task")
		}
	}

	// Agent 认领时检查续约权
	if operatorType == "agent" && node.ReservedForAgentID != nil && *node.ReservedForAgentID != operatorID.String() {
		if node.ReservationExpiresAt != nil && node.ReservationExpiresAt.After(s.svc.Store.Clock.Now()) {
			return nil, fmt.Errorf("node is reserved for another agent (continuation right)")
		}
	}

	var claimedNode types.TaskNode
	if operatorType != "agent" {
		// 人类认领
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

// ApproveNodeResult 保存批准节点操作的结果。
type ApproveNodeResult struct {
	Node types.TaskNode // 批准后的节点信息
}

// CompleteStandardNode 完成一个标准节点，不需要 task:approve 权限。
// 用于 /complete 端点，仅限被分配的代理调用。
// 与 Approve() 的关键区别：不需要审批权限，代理只能完成自己被分配的标准节点。
//
// 步骤：
//  1. 获取节点信息
//  2. 验证节点状态为 in_progress
//  3. 调用 Store 在事务中完成节点（更新状态、记录完成时间、创建转换记录）
//  4. 通过 SSE 发布事件通知下一个节点可被认领
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（agent/member）
//   - comment: 完成备注（可选）
//
// 返回：
//   - *ApproveNodeResult: 完成后的节点信息
//   - error: 可能的错误（节点不存在、状态不是 in_progress）
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

// Approve 批准一个节点并级联到下一个节点或完成任务。
// 需要检查代理的 task:approve 权限。
//
// 步骤：
//  1. 获取节点信息
//  2. 验证节点状态为 in_progress
//  3. 代理操作时进行资源级权限检查（task:approve）
//  4. 调用 Store 在事务中批准节点
//  5. 通过 SSE 发布事件通知下一个节点可被认领
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（agent/member）
//   - comment: 批准备注（可选）
//
// 返回：
//   - *ApproveNodeResult: 批准后的节点信息
//   - error: 可能的错误（节点不存在、权限不足、状态不是 in_progress）
func (s *NodeService) Approve(ctx context.Context, nodeID uuid.UUID, operatorID uuid.UUID, operatorType, comment string) (*ApproveNodeResult, error) {
	currentNode, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	// 节点必须是 in_progress 状态才能审批（需先认领）
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

	// completed_by 引用 agents 表，人类操作时应为 NULL
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

// RejectNodeResult 保存拒绝节点操作的结果。
type RejectNodeResult struct {
	Node types.TaskNode // 拒绝后的节点信息
}

// Reject 拒绝一个节点并回退到目标节点。
// 只有目标节点会被重置为 pending，中间节点保留原有状态。
// 拒绝后通过 SSE 发送 node:reject_rollback 事件通知目标节点的代理执行 git 回退。
//
// 步骤：
//  1. 获取节点信息
//  2. 验证节点状态为 in_progress
//  3. 代理操作时进行资源级权限检查（task:reject）
//  4. 确定回退目标节点（默认为前序节点）
//  5. 验证目标节点（属于同一任务、排序在前、非手动节点）
//  6. 获取最大拒绝循环次数（防止无限拒绝）
//  7. 调用 Store 在事务中执行拒绝和回退
//  8. 将该任务的记忆标记为过时
//  9. 通过 SSE 发布 node:pending 事件通知目标节点可被重新认领
//  10. 通过 SSE 发送 node:reject_rollback 控制事件到目标节点的代理（Redis 缓冲）
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 被拒绝的节点 ID
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（agent/member）
//   - targetNodeID: 可选，指定回退目标节点 ID（nil 则自动选择前序节点）
//   - comment: 拒绝备注（可选）
//
// 返回：
//   - *RejectNodeResult: 拒绝后的节点信息
//   - error: 可能的错误（节点不存在、权限不足、目标节点无效等）
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

// ManualIntervention 将节点设置为 manual_intervention 状态。
// 用于超时、系统错误或代理主动报告需要人类干预的场景。
//
// 步骤：
//  1. 获取节点信息
//  2. 验证节点状态为 in_progress
//  3. 代理操作时进行资源级权限检查（task:execute）
//  4. 更新节点状态为 manual_intervention，分配者类型改为 human
//  5. 创建状态转换记录
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（agent/system）
//   - comment: 备注（可选）
//
// 返回：
//   - types.TaskNode: 更新后的节点信息
//   - error: 可能的错误（节点不存在、状态不是 in_progress）
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

	// Interrupt→ManualIntervention：AssigneeType 改为 human，清空 ReservedForAgentID 和 ReservationExpiresAt
	// （对齐 store/node_interrupt.go 的语义，让人类介入接管）
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

// ResolveAction 定义节点恢复的操作方式。
type ResolveAction string

const (
	ResolveActionReExecute ResolveAction = "re_execute" // 重置为 pending，Agent 重新执行
	ResolveActionComplete  ResolveAction = "complete"   // 直接标记为 completed，跳过重新执行
)

// Resolve 将节点从 manual_intervention 状态恢复。
// 如果提供了 newAgentID，则将节点重新分配给该代理。
// action 参数决定恢复方式：re_execute（默认）重置为 pending 让 Agent 重新执行，
// complete 直接标记为 completed 使用已有的 summary。
//
// 步骤：
//  1. 获取节点信息
//  2. 验证节点状态为 manual_intervention
//  3. 确定新的分配者（保持原分配者或使用 newAgentID）
//  4. 更新节点状态为 in_progress，重置拒绝计数
//  5. 创建状态转换记录
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（member）
//   - comment: 备注（可选）
//   - newAgentID: 可选，新的代理 ID（nil 则保持原分配者）
//
// 返回：
//   - types.TaskNode: 更新后的节点信息
//   - error: 可能的错误（节点不在 manual_intervention 状态、数据库更新失败）
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

	// complete 模式：直接标记为 completed，跳过重新执行
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

		// 完成后触发后续节点
		s.publishNodeEventAfterApprove(ctx, currentNode.TaskID)

		return node, nil
	}

	// 默认 re_execute 模式：重置为 pending，Agent 重新执行
	assigneeID := currentNode.AssigneeID
	if newAgentID != nil {
		newID := newAgentID.String()
		assigneeID = &newID
	}

	// resolve→pending：AssigneeType 重置为 any_agent（让任意 Agent 可认领），清空续约权
	node, err := s.svc.Store.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
		ID:          nodeID.String(),
		Status:      types.TaskNodeStatusPending,
		AssigneeType: types.AssigneeTypeAnyAgent,
		AssigneeID:  assigneeID,
		ReservedForAgentID: nil,
		RejectCount: 0, // re_execute 重置 reject count
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

	// 发布 node:pending 事件，使 Agent 可以重新认领该节点
	task, err := s.svc.Store.GetTask(ctx, currentNode.TaskID)
	if err == nil {
		s.publishNodePendingEvent(ctx, currentNode.TaskID)
		_ = task // 获取 task 以备将来可能使用
	}

	return node, nil
}

// SkipClaim 允许代理主动放弃续约权，使节点对其他代理开放认领。
// 续约权是节点 N 完成后默认保留 30 秒给当前 Agent 认领节点 N+1 的机制。
//
// 步骤：
//  1. 获取节点信息
//  2. 验证代理是否持有续约权
//  3. 资源级权限检查（task:claim）
//  4. 清除保留的代理 ID 和过期时间
//  5. 创建状态转换记录
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//   - agentID: 代理 ID
//
// 返回：
//   - error: 可能的错误（代理未持有续约权、权限不足）
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

	// SkipClaim 语义：释放续约权，ReservedForAgentID 清空让其他 Agent 可认领
	_, err = s.svc.Store.UpdateTaskNodeStatus(ctx, types.UpdateTaskNodeStatusParams{
		ID:          nodeID.String(),
		Status:      node.Status,
		AssigneeType: node.AssigneeType,
		AssigneeID:  node.AssigneeID,
		ReservedForAgentID: nil, // 释放续约权
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

// GetTaskNode 根据 ID 查询单个工作流节点。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//
// 返回：
//   - types.TaskNode: 节点信息
//   - error: 可能的错误（节点不存在）
func (s *NodeService) GetTaskNode(ctx context.Context, nodeID uuid.UUID) (types.TaskNode, error) {
	node, err := s.svc.Store.GetTaskNode(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node: %w", err)
	}
	return node, nil
}

// CreateNodeTransition 创建节点状态流转审计记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 流转记录参数，包含节点 ID、源状态、目标状态、操作类型等
//
// 返回：
//   - types.NodeTransition: 创建的流转记录
//   - error: 可能的错误（数据库写入失败）
func (s *NodeService) CreateNodeTransition(ctx context.Context, params types.CreateNodeTransitionParams) (types.NodeTransition, error) {
	transition, err := s.svc.Store.CreateNodeTransition(ctx, params)
	if err != nil {
		return types.NodeTransition{}, fmt.Errorf("create node transition: %w", err)
	}
	return transition, nil
}

// UpdateNodeSummary 更新节点的执行摘要。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//   - summary: 执行摘要文本
//
// 返回：
//   - types.TaskNode: 更新后的节点记录
//   - error: 可能的错误（节点不存在、数据库更新失败）
func (s *NodeService) UpdateNodeSummary(ctx context.Context, nodeID uuid.UUID, summary string) (types.TaskNode, error) {
	node, err := s.svc.Store.UpdateNodeSummary(ctx, nodeID, summary)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("update node summary: %w", err)
	}
	return node, nil
}

// ListNodes 列出指定任务的所有工作流节点。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//
// 返回：
//   - []types.TaskNode: 节点列表
//   - error: 可能的错误（数据库查询失败）
func (s *NodeService) ListNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	return s.svc.Store.ListTaskNodes(ctx, taskID)
}

// InterruptTaskResult 保存中断任务操作的结果。
type InterruptTaskResult struct {
	TaskID           int32 // 被中断的任务 ID
	InterruptedNodes int   // 被中断的节点数量
}

// InterruptTask 将任务中所有 in_progress 状态的节点设为 manual_intervention。
// 通过事务保证原子性，并通过 SSE 发送 task:interrupt 控制事件通知正在执行的代理。
//
// 步骤：
//  1. 代理操作时进行资源级权限检查（task:execute）
//  2. 查询任务的所有节点
//  3. 收集正在执行中的代理信息（用于后续发送中断事件）
//  4. 在事务中将所有 in_progress 节点设为 manual_intervention
//  5. 提交事务
//  6. 通过 SSE 发布 node:pending 事件
//  7. 向正在执行中的代理发送 task:interrupt 控制事件（Redis 缓冲）
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//   - operatorID: 操作者 ID
//   - operatorType: 操作者类型（member/agent）
//   - comment: 中断备注（可选）
//
// 返回：
//   - *InterruptTaskResult: 包含任务 ID 和被中断的节点数量
//   - error: 可能的错误（数据库操作失败）
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

// publishNodeEventAfterApprove 在节点批准后检查新解除阻塞的节点，
// 通过 SSE 发布 node:pending 或 node:continuation_invite 事件。
// 同时检查 DAG 依赖以发现可能刚变为可用的节点。
//
// 步骤：
//  1. 查询任务和项目信息
//  2. 查询所有就绪节点（DAG 依赖全部完成的 pending 节点）
//  3. 向工作区广播 node:pending 事件
//  4. 检查有续约权的 in_progress 节点，发送 node:continuation_invite 事件
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
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

// publishNodePendingEvent 通过 SSE 向工作区发布 node:pending 事件，
// 通知代理某个节点可被认领。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
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
