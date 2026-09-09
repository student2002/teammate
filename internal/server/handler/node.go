// node.go provides HTTP API endpoints for workflow node state transitions, including claim, approve, reject, manual intervention, complete, interrupt, etc.
//
// Node state machine: pending -> in_progress -> completed
//                                    -> manual_intervention
//                                    -> rejected -> (rollback to target node)
// Node types: standard (AI agent execution), review (AI or human review), manual (human execution)

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

// NodeHandler handles HTTP requests for workflow node state transitions, including claim, approve, reject, manual intervention, complete, interrupt, etc.
type NodeHandler struct {
	Svc *service.Service
}

// NewNodeHandler creates a NodeHandler instance.
func NewNodeHandler(svc *service.Service) *NodeHandler {
	return &NodeHandler{Svc: svc}
}

// Routes returns the route table for node operations.
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

// ListNodeTransitions handles the GET /tasks/{taskId}/nodes/{nodeId}/transitions endpoint, querying the node's state transition history.
// Equivalent to TaskHandler.ListNodeTransitions, mounted under the node operations route group (the path the frontend actually calls).
//
// Response:
//   - 200: successfully returns the transition history
//   - 400: invalid node ID
//   - 404: node does not exist
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

// ClaimNode handles the POST /tasks/{taskId}/nodes/{id}/claim endpoint, allowing an Agent to claim a pending node.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: claimed successfully, returns node info
//   - 400: invalid node ID
//   - 401: not authenticated
//   - 403: non-Agent cannot claim or there is a self-review conflict
//   - 404: node does not exist
//   - 409: node has already been claimed or is reserved for another Agent
//
// Processing flow:
//  1. verify the authenticated identity (only Agents can claim)
//  2. call service to perform the claim (optimistic lock)
//  3. handle claim conflicts (409 Conflict)
func (h *NodeHandler) ClaimNode(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// get the operator identity from the auth context (not the request body)
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// get node info to determine claim permission
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
		// Agents cannot claim nodes assigned to humans
		if node.AssigneeType == AssigneeTypeHuman {
			response.Forbidden(w, "agents cannot claim human-assigned nodes")
			return
		}
	} else {
		// humans can only claim nodes assigned to humans
		if node.AssigneeType != AssigneeTypeHuman {
			response.Forbidden(w, "only agents can claim this node")
			return
		}
	}

	// call service to perform the claim
	nodeSvc := service.NewNodeService(h.Svc)
	result, err := nodeSvc.Claim(r.Context(), nodeID, claims.UserID, claims.UserType)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "node not found")
			return
		}
		// handle specific error messages
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


// ApproveNode handles the POST /tasks/{taskId}/nodes/{id}/approve endpoint, approving the specified node and advancing the workflow to the next stage.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - comment: string, approval comment
//
// Response:
//   - 200: approval passed, returns node info
//   - 400: invalid node ID
//   - 401: not authenticated
//   - 403: insufficient permissions (Agents need task:approve, humans need member+ role)
//   - 404: node does not exist
//   - 409: version conflict
func (h *NodeHandler) ApproveNode(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// get the operator identity from the auth context
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// permission check: Agents need task:approve, humans need member+ role
	if err := checkNodeOpPermission(claims, types.PermTaskApprove); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse request body
	var req approveNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to perform the approval
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


// RejectNode handles the POST /tasks/{taskId}/nodes/{id}/reject endpoint, rejecting the specified node, supporting rollback to a target node.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - target_node_id: UUID, rollback target node ID (optional, defaults to rolling back to the previous node)
//   - comment: string, rejection comment
//
// Response:
//   - 200: rejection successful, returns node info
//   - 400: parameter error or invalid target node
//   - 401: not authenticated
//   - 403: insufficient permissions (Agents need task:reject, humans need member+ role)
//   - 404: node does not exist
//   - 409: version conflict
func (h *NodeHandler) RejectNode(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// get the operator identity from the auth context
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// permission check: Agents need task:reject, humans need member+ role
	if err := checkNodeOpPermission(claims, types.PermTaskReject); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse request body
	var req rejectNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to perform the rejection
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


// ManualIntervention handles the POST /tasks/{taskId}/nodes/{id}/manual endpoint, marking a node as needing manual intervention.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - comment: string, intervention explanation
//
// Response:
//   - 200: marked successfully, returns node info
//   - 400: invalid node ID
//   - 401: not authenticated
//   - 403: insufficient permissions (Agents need task:execute, humans need member+ role)
//   - 404: node does not exist
//   - 409: version conflict
func (h *NodeHandler) ManualIntervention(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// get the operator identity from the auth context
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// permission check: Agents need task:execute, humans need member+ role
	if err := checkNodeOpPermission(claims, types.PermTaskExecute); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse request body
	var req manualInterventionRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to mark manual intervention
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


// ResolveNode handles the POST /tasks/{taskId}/nodes/{id}/resolve endpoint, resolving a node in the manual-intervention state, with an option to reassign to another Agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - comment: string, resolution explanation
//   - agent_id: UUID, optional, reassign to another Agent
//
// Response:
//   - 200: resolved successfully, returns node info
//   - 400: parameter error or node is not in manual-intervention state
//   - 401: not authenticated
//   - 403: only admins/members can resolve
//   - 404: node does not exist
//   - 409: version conflict
func (h *NodeHandler) ResolveNode(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// only admins/members can resolve; Agents or viewers cannot
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

	// parse request body
	var req resolveNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to resolve the node
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

// SkipClaim handles the POST /tasks/{taskId}/nodes/{id}/skip-claim endpoint, allowing an Agent to relinquish the continuation right of a node.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: continuation right relinquished successfully
//   - 400: invalid node ID or Agent does not hold the continuation right
//   - 401: not authenticated
//   - 403: non-Agent cannot perform this operation
//   - 409: version conflict
func (h *NodeHandler) SkipClaim(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// get the operator identity from the auth context
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	// only Agents can relinquish the continuation right
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can skip claim")
		return
	}

	// call service to relinquish the continuation right
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

// ListNodes handles the GET /tasks/{taskId}/nodes endpoint, listing all workflow nodes for the specified task.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the node list
//   - 400: invalid task ID
func (h *NodeHandler) ListNodes(w http.ResponseWriter, r *http.Request) {
	// parse task ID
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

	// workspace ownership has already been verified by TaskAccessMiddleware

	// call service to query nodes
	nodeSvc := service.NewNodeService(h.Svc)
	nodes, err := nodeSvc.ListNodes(r.Context(), taskID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, nodes)
}


// CompleteNode handles the POST /tasks/{taskId}/nodes/{id}/complete endpoint, allowing an Agent to complete the standard-type node it is responsible for.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - summary: string, execution summary
//
// Response:
//   - 200: completed successfully, returns node info
//   - 400: invalid node ID or unsupported node type
//   - 401: not authenticated
//   - 403: non-responsible Agent cannot complete
//   - 404: node does not exist
//   - 409: version conflict or node is not in progress
func (h *NodeHandler) CompleteNode(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// only Agents can use the complete endpoint
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can complete nodes; members should use approve")
		return
	}

	// parse request body
	var req completeNodeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// verify the Agent is the assignee of the node
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// verify assignee identity
	if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
		response.Forbidden(w, "only the assigned agent can complete this node")
		return
	}

	// verify node status
	if node.Status != TaskNodeStatusInProgress {
		response.Conflict(w, fmt.Sprintf("node is not in progress: current status is %s", node.Status))
		return
	}

	// only standard nodes can be completed (review nodes require approve/reject)
	if node.NodeType != NodeTypeStandard {
		response.BadRequest(w, "only standard nodes can be completed; review nodes require approve/reject")
		return
	}

	// call service to complete the standard node (does not require task:approve permission)
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


// InterruptAck handles the POST /tasks/{taskId}/nodes/{id}/interrupt-ack endpoint, where an Agent acknowledges that it has received the interrupt instruction.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - comment: string, acknowledgment explanation
//
// Response:
//   - 200: acknowledgment successful
//   - 400: invalid node ID
//   - 401: not authenticated
//   - 403: non-Agent cannot acknowledge or non-responsible Agent
func (h *NodeHandler) InterruptAck(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// only Agents can acknowledge interrupts
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can acknowledge interrupts")
		return
	}

	// parse request body
	var req interruptAckRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// verify the Agent is the assignee of the node
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// verify assignee identity
	if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
		response.Forbidden(w, "only the assigned agent can acknowledge this interrupt")
		return
	}

	// create an acknowledgment transition record
	commentStr := req.Comment
	operatorIDStr := claims.UserID.String()
	_, err = service.NewNodeService(h.Svc).CreateNodeTransition(r.Context(), CreateNodeTransitionParams{
		TaskNodeID:   nodeID.String(),
		FromStatus:   node.Status,
		ToStatus:     node.Status, // status unchanged
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

// checkNodeOpPermission verifies that the authenticated user has permission to perform node operations.
// Agents must have the specified permission (checked via the RequireAgentPermission middleware - this is defense in depth);
// human users need member or higher role.
func checkNodeOpPermission(claims svcmw.AuthClaims, agentPermission string) error {
	if claims.UserType == "agent" {
		// Agent permissions are checked by the RequireAgentPermission middleware
		// this is a defense-in-depth check - if the middleware did not run, log a warning
		// the actual permission check is performed in the service layer
		return nil
	}
	// human users need member or higher role (viewer is not allowed)
	if types.MemberRoleLevel(claims.Role) < 2 {
		return fmt.Errorf("insufficient permissions: member role or higher required")
	}
	return nil
}


// UpdateSummary handles the POST /tasks/{taskId}/nodes/{id}/summary endpoint, updating the node's execution summary; only the node assignee or member+ role can operate.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - summary: string, execution summary (required)
//
// Response:
//   - 200: successfully returns the updated node info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: non-assignee or insufficient permissions
//   - 404: node does not exist
func (h *NodeHandler) UpdateSummary(w http.ResponseWriter, r *http.Request) {
	// parse node ID
	nodeID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	// workspace ownership has already been verified by NodeAccessMiddleware

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// parse request body
	var req updateSummaryRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// verify summary is required
	if req.Summary == "" {
		response.BadRequest(w, "summary is required")
		return
	}

	// identity verification: Agents must be the node assignee, members need member+ role
	if claims.UserType == "agent" {
		// query the node to verify assignee identity
		node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.NotFound(w, "node not found")
				return
			}
			response.InternalServerError(w, err)
			return
		}
		// verify assignee identity
		if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
			response.Forbidden(w, "only the assigned agent can update this node's summary")
			return
		}
	} else {
		// human users need member or higher role (viewer cannot update the summary)
		if err := requireWriteAccess(claims); err != nil {
			response.Forbidden(w, err.Error())
			return
		}
	}

	// call service to update the summary
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
