// node_dto.go 定义 Node 相关的请求/响应结构体和领域类型别名。
package handler

import (
	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// ---- 领域类型别名，消除 handler 对 db/generated 的直接依赖 ----
// Note: AssigneeType, AssigneeTypeHuman, NodeType are defined in workflow_dto.go.

type TaskNodeStatus = string

const TaskNodeStatusInProgress = apitypes.TaskNodeStatusInProgress

const NodeTypeStandard = apitypes.NodeTypeStandard

type TransitionAction = string

const TransitionActionInterruptAck = apitypes.TransitionActionInterruptAck

type CreateNodeTransitionParams = apitypes.CreateNodeTransitionParams

// ---- 请求结构体 ----

// approveNodeRequest 审批请求体。
type approveNodeRequest struct {
	Comment string `json:"comment"` // 审批意见
}

// rejectNodeRequest �驳回请求体。
type rejectNodeRequest struct {
	TargetNodeID *uuid.UUID `json:"target_node_id"` // 回退目标节点 ID（可选）
	Comment      string     `json:"comment"`        // 驳回意见
}

// manualInterventionRequest 人工干预请求体。
type manualInterventionRequest struct {
	Comment string `json:"comment"` // 干预说明
}

// resolveNodeRequest 解决人工干预请求体。
type resolveNodeRequest struct {
	Comment string     `json:"comment"`  // 解决说明
	AgentID *uuid.UUID `json:"agent_id"` // 可选：重新分配给其他 Agent
	Action  string     `json:"action"`   // 恢复方式：re_execute（默认）或 complete
}

// completeNodeRequest 完成节点请求体。
type completeNodeRequest struct {
	Summary string `json:"summary"` // 执行摘要
}

// interruptAckRequest 中断确认请求体。
type interruptAckRequest struct {
	Comment string `json:"comment"` // 确认说明
}

// updateSummaryRequest 更新摘要请求体。
type updateSummaryRequest struct {
	Summary string `json:"summary"` // 执行摘要
}
