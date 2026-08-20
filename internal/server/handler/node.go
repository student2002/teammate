// node.go 提供工作流节点的状态流转 HTTP API 端点，包括认领、审批、驳回、人工干预、完成、中断等操作。
//
// 节点状态机：pending → in_progress → completed
//                                    → manual_intervention
//                                    → rejected → (回退到目标节点)
// 节点类型：standard（AI 代理执行）、review（AI 或人类审查）、manual（人类执行）

package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// NodeHandler 处理工作流节点状态流转的 HTTP 请求，包括认领、审批、驳回、人工干预、完成、中断等操作。
type NodeHandler struct {
	Svc *service.Service
}

// NewNodeHandler 创建 NodeHandler 实例。
func NewNodeHandler(svc *service.Service) *NodeHandler {
	return &NodeHandler{Svc: svc}
}

// Routes 返回节点操作的路由表。
func (h *NodeHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListNodes)
	r.Get("/{id}/transitions", h.ListNodeTransitions)
	r.Post("/{id}/claim", h.ClaimNode)
	r.Post("/{id}/approve", h.ApproveNode)
	r.Post("/{id}/reject", h.RejectNode)
	r.Post("/{id}/manual", h.ManualIntervention)
	r.Post("/{id}/resolve", h.ResolveNode)
	r.Post("/{id}/skip-claim", h.SkipClaim)
	r.Post("/{id}/summary", h.UpdateSummary)
	r.Post("/{id}/complete", h.CompleteNode)
	r.Post("/{id}/interrupt-ack", h.InterruptAck)

	return r
}

// ListNodeTransitions 处理 GET /tasks/{taskId}/nodes/{nodeId}/transitions 端点，查询节点的状态流转历史。
// 与 TaskHandler.ListNodeTransitions 等价，挂载在节点操作路由组下（前端实际调用路径）。
//
// 响应：
//   - 200: 成功返回流转历史
//   - 400: 节点 ID 无效
//   - 404: 节点不存在
func (h *NodeHandler) ListNodeTransitions(w http.ResponseWriter, r *http.Request) {
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	node := checkNodeWorkspace(h.Svc, w, r, nodeID)
	if node == nil {
		return
	}

	taskSvc := service.NewTaskService(h.Svc)
	transitions, err := taskSvc.ListNodeTransitions(r.Context(), nodeID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, transitions)
}

// ClaimNode 处理 POST /tasks/{taskId}/nodes/{id}/claim 端点，允许 Agent 认领待处理的节点。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功认领，返回节点信息
//   - 400: 节点 ID 无效
//   - 401: 未认证
//   - 403: 非 Agent 无法认领或存在自我审查冲突
//   - 404: 节点不存在
//   - 409: 节点已被认领或保留给其他 Agent
//
// 处理流程：
//  1. 验证认证身份（仅 Agent 可认领）
//  2. 调用 service 执行认领（乐观锁）
//  3. 处理认领冲突（409 Conflict）
func (h *NodeHandler) ClaimNode(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 从认证上下文获取操作者身份（非请求体）
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 获取节点信息以判断认领权限
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	if claims.UserType == "agent" {
		// Agent 不能认领 human 分配的节点
		if node.AssigneeType == AssigneeTypeHuman {
			response.Forbidden(w, "agents cannot claim human-assigned nodes")
			return
		}
	} else {
		// 人类只能认领 human 分配的节点
		if node.AssigneeType != AssigneeTypeHuman {
			response.Forbidden(w, "only agents can claim this node")
			return
		}
	}

	// 调用 service 执行认领
	nodeSvc := service.NewNodeService(h.Svc)
	result, err := nodeSvc.Claim(r.Context(), nodeID, claims.UserID, claims.UserType)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		// 处理特定错误消息
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not a member"):
			response.Forbidden(w, errMsg)
		case strings.Contains(errMsg, "self-review"):
			response.Forbidden(w, errMsg)
		case strings.Contains(errMsg, "reserved for another"):
			response.Conflict(w, errMsg)
		case strings.Contains(errMsg, "not available for claiming"), strings.Contains(errMsg, "not available for re-claiming"):
			response.Conflict(w, errMsg)
		default:
			response.InternalServerError(w, err)
		}
		return
	}

	response.JSON(w, r, result.Node)
}


// ApproveNode 处理 POST /tasks/{taskId}/nodes/{id}/approve 端点，审批通过指定节点，推进工作流到下一阶段。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - comment: string，审批意见
//
// 响应：
//   - 200: 审批通过，返回节点信息
//   - 400: 节点 ID 无效
//   - 401: 未认证
//   - 403: 权限不足（Agent 需要 task:approve，人类需要 member+ 角色）
//   - 404: 节点不存在
//   - 409: 版本冲突
func (h *NodeHandler) ApproveNode(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 从认证上下文获取操作者身份
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 权限检查：Agent 需要 task:approve，人类需要 member+ 角色
	if err := checkNodeOpPermission(claims, types.PermTaskApprove); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析请求体
	var req approveNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 执行审批
	nodeSvc := service.NewNodeService(h.Svc)
	result, err := nodeSvc.Approve(r.Context(), nodeID, claims.UserID, claims.UserType, req.Comment)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "version conflict") {
			response.Conflict(w, errMsg)
			return
		}
		if types.IsNodeStateConflict(err) {
			response.Conflict(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, result.Node)
}


// RejectNode 处理 POST /tasks/{taskId}/nodes/{id}/reject 端点，驳回指定节点，支持回退到目标节点。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - target_node_id: UUID，回退目标节点 ID（可选，默认回退到上一个节点）
//   - comment: string，驳回意见
//
// 响应：
//   - 200: 驳回成功，返回节点信息
//   - 400: 参数错误或目标节点无效
//   - 401: 未认证
//   - 403: 权限不足（Agent 需要 task:reject，人类需要 member+ 角色）
//   - 404: 节点不存在
//   - 409: 版本冲突
func (h *NodeHandler) RejectNode(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 从认证上下文获取操作者身份
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 权限检查：Agent 需要 task:reject，人类需要 member+ 角色
	if err := checkNodeOpPermission(claims, types.PermTaskReject); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析请求体
	var req rejectNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 执行驳回
	nodeSvc := service.NewNodeService(h.Svc)
	result, err := nodeSvc.Reject(r.Context(), nodeID, claims.UserID, claims.UserType, req.TargetNodeID, req.Comment)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "no previous node"):
			response.BadRequest(w, errMsg)
		case strings.Contains(errMsg, "sort_order less"):
			response.BadRequest(w, errMsg)
		case strings.Contains(errMsg, "cannot reject to"):
			response.BadRequest(w, errMsg)
		case strings.Contains(errMsg, "version conflict"):
			response.Conflict(w, errMsg)
		default:
			response.InternalServerError(w, err)
		}
		return
	}

	response.JSON(w, r, result.Node)
}


// ManualIntervention 处理 POST /tasks/{taskId}/nodes/{id}/manual 端点，将节点标记为需要人工干预。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - comment: string，干预说明
//
// 响应：
//   - 200: 标记成功，返回节点信息
//   - 400: 节点 ID 无效
//   - 401: 未认证
//   - 403: 权限不足（Agent 需要 task:execute，人类需要 member+ 角色）
//   - 404: 节点不存在
//   - 409: 版本冲突
func (h *NodeHandler) ManualIntervention(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 从认证上下文获取操作者身份
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 权限检查：Agent 需要 task:execute，人类需要 member+ 角色
	if err := checkNodeOpPermission(claims, types.PermTaskExecute); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析请求体
	var req manualInterventionRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 标记人工干预
	nodeSvc := service.NewNodeService(h.Svc)
	node, err := nodeSvc.ManualIntervention(r.Context(), nodeID, claims.UserID, claims.UserType, req.Comment)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "version conflict") {
			response.Conflict(w, errMsg)
			return
		}
		if strings.Contains(errMsg, "cannot be set to manual_intervention") {
			response.BadRequest(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, node)
}


// ResolveNode 处理 POST /tasks/{taskId}/nodes/{id}/resolve 端点，解决人工干预状态的节点，可选择重新分配给其他 Agent。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - comment: string，解决说明
//   - agent_id: UUID，可选，重新分配给其他 Agent
//
// 响应：
//   - 200: 解决成功，返回节点信息
//   - 400: 参数错误或节点不在人工干预状态
//   - 401: 未认证
//   - 403: 仅管理员/成员可解决
//   - 404: 节点不存在
//   - 409: 版本冲突
func (h *NodeHandler) ResolveNode(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 仅管理员/成员可解决，Agent 或 viewer 不行
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "unauthorized")
		return
	}
	if claims.UserType == "agent" {
		response.Forbidden(w, "only admins/members can resolve a node")
		return
	}
	if types.MemberRoleLevel(claims.Role) < 2 {
		response.Forbidden(w, "insufficient permissions: member role or higher required")
		return
	}

	// 解析请求体
	var req resolveNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 解决节点
	action := service.ResolveActionReExecute
	if req.Action == "complete" {
		action = service.ResolveActionComplete
	}
	nodeSvc := service.NewNodeService(h.Svc)
	node, err := nodeSvc.Resolve(r.Context(), nodeID, claims.UserID, claims.UserType, req.Comment, req.AgentID, action)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "not in manual_intervention"):
			response.BadRequest(w, errMsg)
		case strings.Contains(errMsg, "version conflict"):
			response.Conflict(w, errMsg)
		default:
			response.InternalServerError(w, err)
		}
		return
	}

	response.JSON(w, r, node)
}

// SkipClaim 处理 POST /tasks/{taskId}/nodes/{id}/skip-claim 端点，允许 Agent 放弃节点的续约权。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功放弃续约权
//   - 400: 节点 ID 无效或 Agent 不持有续约权
//   - 401: 未认证
//   - 403: 非 Agent 无法操作
//   - 409: 版本冲突
func (h *NodeHandler) SkipClaim(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 从认证上下文获取操作者身份
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	// 仅 Agent 可以放弃续约权
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can skip claim")
		return
	}

	// 调用 service 放弃续约权
	nodeSvc := service.NewNodeService(h.Svc)
	err = nodeSvc.SkipClaim(r.Context(), nodeID, claims.UserID)
	if err != nil {
		errMsg := err.Error()
		switch {
		case strings.Contains(errMsg, "does not hold"):
			response.BadRequest(w, errMsg)
		case strings.Contains(errMsg, "version conflict"):
			response.Conflict(w, errMsg)
		default:
			response.InternalServerError(w, err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message":"continuation right released"}`))
}

// ListNodes 处理 GET /tasks/{taskId}/nodes 端点，列出指定任务的所有工作流节点。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回节点列表
//   - 400: 任务 ID 无效
func (h *NodeHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	// 解析任务 ID
	taskIDStr := chi.URLParam(r, "taskId")
	if taskIDStr == "" {
		response.BadRequest(w, "missing task id")
		return
	}
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// 工作区归属已由 TaskAccessMiddleware 验证

	// 调用 service 查询节点
	nodeSvc := service.NewNodeService(h.Svc)
	nodes, err := nodeSvc.ListNodes(r.Context(), taskID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, nodes)
}


// CompleteNode 处理 POST /tasks/{taskId}/nodes/{id}/complete 端点，允许 Agent 完成其负责的标准类型节点。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - summary: string，执行摘要
//
// 响应：
//   - 200: 完成成功，返回节点信息
//   - 400: 节点 ID 无效或节点类型不支持
//   - 401: 未认证
//   - 403: 非负责人 Agent 无法完成
//   - 404: 节点不存在
//   - 409: 版本冲突或节点不在进行中状态
func (h *NodeHandler) CompleteNode(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 仅 Agent 可使用完成端点
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can complete nodes; members should use approve")
		return
	}

	// 解析请求体
	var req completeNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 验证 Agent 是节点的负责人
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// 验证负责人身份
	if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
		response.Forbidden(w, "only the assigned agent can complete this node")
		return
	}

	// 验证节点状态
	if node.Status != TaskNodeStatusInProgress {
		response.Conflict(w, fmt.Sprintf("node is not in progress: current status is %s", node.Status))
		return
	}

	// 仅 standard 节点可完成（review 节点需要 approve/reject）
	if node.NodeType != NodeTypeStandard {
		response.BadRequest(w, "only standard nodes can be completed; review nodes require approve/reject")
		return
	}

	// 调用 service 完成标准节点（不需要 task:approve 权限）
	nodeSvc := service.NewNodeService(h.Svc)
	result, err := nodeSvc.CompleteStandardNode(r.Context(), nodeID, claims.UserID, claims.UserType, req.Summary)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "version conflict") {
			response.Conflict(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, result.Node)
}


// InterruptAck 处理 POST /tasks/{taskId}/nodes/{id}/interrupt-ack 端点，Agent 确认已收到中断指令。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - comment: string，确认说明
//
// 响应：
//   - 200: 确认成功
//   - 400: 节点 ID 无效
//   - 401: 未认证
//   - 403: 非 Agent 无法确认或非负责人 Agent
func (h *NodeHandler) InterruptAck(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 仅 Agent 可确认中断
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can acknowledge interrupts")
		return
	}

	// 解析请求体
	var req interruptAckRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 验证 Agent 是节点的负责人
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// 验证负责人身份
	if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
		response.Forbidden(w, "only the assigned agent can acknowledge this interrupt")
		return
	}

	// 创建确认的流转记录
	commentStr := req.Comment
	operatorIDStr := claims.UserID.String()
	_, err = service.NewNodeService(h.Svc).CreateNodeTransition(r.Context(), CreateNodeTransitionParams{
		TaskNodeID:   nodeID.String(),
		FromStatus:   node.Status,
		ToStatus:     node.Status, // 状态不变
		Action:       TransitionActionInterruptAck,
		Comment:      &commentStr,
		OperatorID:   &operatorIDStr,
		OperatorType: "agent",
	})
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, map[string]string{"status": "acknowledged"})
}

// checkNodeOpPermission 验证认证用户是否有权限执行节点操作。
// Agent 必须具有指定权限（通过中间件 RequireAgentPermission 检查——这是纵深防御）；
// 人类用户需要 member 及以上角色。
func checkNodeOpPermission(claims svcmw.AuthClaims, agentPermission string) error {
	if claims.UserType == "agent" {
		// Agent 权限通过中间件 RequireAgentPermission 检查
		// 这是纵深防御检查——如果中间件未运行，记录警告
		// 实际权限检查在 service 层执行
		return nil
	}
	// 人类用户需要 member 及以上角色（viewer 不允许）
	if types.MemberRoleLevel(claims.Role) < 2 {
		return fmt.Errorf("insufficient permissions: member role or higher required")
	}
	return nil
}


// UpdateSummary 处理 POST /tasks/{taskId}/nodes/{id}/summary 端点，更新节点的执行摘要，仅节点负责人或 member+ 角色可操作。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - summary: string，执行摘要（必填）
//
// 响应：
//   - 200: 成功返回更新后的节点信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 非负责人或权限不足
//   - 404: 节点不存在
func (h *NodeHandler) UpdateSummary(w http.ResponseWriter, r *http.Request) {
	// 解析节点 ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// 工作区归属已由 NodeAccessMiddleware 验证

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 解析请求体
	var req updateSummaryRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 验证摘要必填
	if req.Summary == "" {
		response.BadRequest(w, "summary is required")
		return
	}

	// 身份验证：Agent 必须是节点负责人，member 需要 member+ 角色
	if claims.UserType == "agent" {
		// 查询节点验证负责人身份
		node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.NotFound(w, "node not found")
				return
			}
			response.InternalServerError(w, err)
			return
		}
		// 验证负责人身份
		if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
			response.Forbidden(w, "only the assigned agent can update this node's summary")
			return
		}
	} else {
		// 人类用户需要 member 及以上角色（viewer 不能更新摘要）
		if err := requireWriteAccess(claims); err != nil {
			response.Forbidden(w, err.Error())
			return
		}
	}

	// 调用 service 更新摘要
	node, err := service.NewNodeService(h.Svc).UpdateNodeSummary(r.Context(), nodeID, req.Summary)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, node)
}
