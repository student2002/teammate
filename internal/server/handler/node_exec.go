// node_exec.go provides the agentd node execution protocol endpoints (S4,
// plan §12.6): node discovery (GET /tasks/{taskId}/nodes), claim, and the
// complete/fail/manual/interrupt-ack receipts, plus the workspace-scoped
// in-progress recovery view. The claim response is the opaque execution unit;
// every receipt validates the execution id against the node's in-flight
// attempt and maps superseded executions to 409.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// NodeExecutionHandler handles the agentd node execution protocol requests.
type NodeExecutionHandler struct {
	Svc *service.Service
}

// NewNodeExecutionHandler creates a NodeExecutionHandler instance.
func NewNodeExecutionHandler(svc *service.Service) *NodeExecutionHandler {
	return &NodeExecutionHandler{Svc: svc}
}

// Routes returns the task-scoped node execution routes, mounted under
// /tasks/{taskId} after the task access middleware.
func (h *NodeExecutionHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.ListTaskNodes)
	r.Post("/{nodeId}/claim", h.ClaimNode)
	r.Post("/{nodeId}/complete", h.CompleteNode)
	r.Post("/{nodeId}/fail", h.FailNode)
	r.Post("/{nodeId}/manual", h.ManualNode)
	r.Post("/{nodeId}/interrupt-ack", h.AckInterrupt)
	r.Post("/{nodeId}/submit", h.SubmitHumanInput)
	return r
}

// claimRequest is the body of the claim endpoint.
type claimRequest struct {
	AgentID string `json:"agent_id"`
}

// completeRequest is the body of the completion receipt.
type completeRequest struct {
	ExecutionID string          `json:"execution_id"`
	Summary     string          `json:"summary"`
	Output      json.RawMessage `json:"output"`
}

// failRequest is the body of the failure receipt. The code is stored
// verbatim; retry and routing stay server decisions.
type failRequest struct {
	ExecutionID  string `json:"execution_id"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

// manualRequest is the body of the manual intervention receipt.
type manualRequest struct {
	OperatorID   string `json:"operator_id"`
	OperatorType string `json:"operator_type"`
	ExecutionID  string `json:"execution_id"`
	Comment      string `json:"comment"`
}

// interruptAckRequest is the body of the interrupt acknowledgement.
type interruptAckRequest struct {
	Comment string `json:"comment"`
}

// ListTaskNodes handles GET /tasks/{taskId}/nodes: every node of the task in
// the execution-unit shape (status is the node status, unclaimed nodes carry
// an empty execution id). agentd filters the ready ones for claiming.
func (h *NodeExecutionHandler) ListTaskNodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := svcmw.GetAuthFromContext(r.Context()); !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}
	if !h.checkAgentProjectAccess(w, r, task) {
		return
	}

	nodes, err := service.NewTaskService(h.Svc).ListTaskNodes(r.Context(), task.ID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	units := make([]types.ExecutionUnit, 0, len(nodes))
	for _, node := range nodes {
		units = append(units, service.BuildExecutionUnit(node, ""))
	}
	response.JSON(w, r, units)
}

// ClaimNode handles POST /tasks/{taskId}/nodes/{nodeId}/claim: a ready agentd
// node becomes running for the requesting agent and the response is the flat
// execution unit. Not-ready, already-claimed or server-executed nodes return
// 409; agents outside the task's project return 403.
func (h *NodeExecutionHandler) ClaimNode(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}

	var req claimRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	agentID, ok := h.resolveAgentIdentity(w, claims, req.AgentID)
	if !ok {
		return
	}
	if !h.checkAgentProjectAccess(w, r, task) {
		return
	}

	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	unit, err := service.NewWorkflowRunService(h.Svc).ClaimForAgent(r.Context(), task.ID, nodeID, agentID)
	if err != nil {
		writeNodeExecError(w, err)
		return
	}
	response.JSON(w, r, unit)
}

// CompleteNode handles POST /tasks/{taskId}/nodes/{nodeId}/complete: the
// receipt closes the claimed execution as succeeded, stores summary/output
// and advances the graph.
func (h *NodeExecutionHandler) CompleteNode(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}
	if claims.UserType != "agent" {
		if err := requireWriteAccess(claims); err != nil {
			response.Forbidden(w, err.Error())
			return
		}
	}

	var req completeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if !h.checkAgentProjectAccess(w, r, task) {
		return
	}

	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	err = service.NewWorkflowRunService(h.Svc).CompleteForAgent(r.Context(), task.ID, nodeID, req.ExecutionID, req.Summary, req.Output)
	if err != nil {
		writeNodeExecError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{"status": "ok"})
}

// FailNode handles POST /tasks/{taskId}/nodes/{nodeId}/fail: the receipt
// closes the claimed execution as failed and consumes one attempt against the
// node's retry policy.
func (h *NodeExecutionHandler) FailNode(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}
	if claims.UserType != "agent" {
		if err := requireWriteAccess(claims); err != nil {
			response.Forbidden(w, err.Error())
			return
		}
	}

	var req failRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if !h.checkAgentProjectAccess(w, r, task) {
		return
	}

	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	err = service.NewWorkflowRunService(h.Svc).FailForAgent(r.Context(), task.ID, nodeID, req.ExecutionID, req.ErrorCode, req.ErrorMessage)
	if err != nil {
		writeNodeExecError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{"status": "ok"})
}

// ManualNode handles POST /tasks/{taskId}/nodes/{nodeId}/manual: the daemon
// reports the execution needs a human; the node lands in manual_intervention.
func (h *NodeExecutionHandler) ManualNode(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}

	var req manualRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	// The operator identity defaults to the caller; an agent principal cannot
	// report on behalf of another agent.
	operatorID := req.OperatorID
	if operatorID == "" {
		operatorID = claims.UserID.String()
	}
	if claims.UserType == "agent" && operatorID != claims.UserID.String() {
		response.Forbidden(w, "cannot report on behalf of another agent")
		return
	}
	operatorType := req.OperatorType
	if operatorType == "" {
		operatorType = claims.UserType
	}
	if !h.checkAgentProjectAccess(w, r, task) {
		return
	}

	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	err = service.NewWorkflowRunService(h.Svc).MarkNodeManualIntervention(r.Context(), task.ID, nodeID, req.ExecutionID, operatorID, operatorType, req.Comment)
	if err != nil {
		writeNodeExecError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{"status": "ok"})
}

// AckInterrupt handles POST /tasks/{taskId}/nodes/{nodeId}/interrupt-ack: the
// daemon confirms the interrupt was processed; the node lands in
// manual_intervention with an interrupt_ack transition.
func (h *NodeExecutionHandler) AckInterrupt(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	task, ok := taskFromContext(r)
	if !ok {
		response.NotFound(w, "task not found")
		return
	}
	if claims.UserType != "agent" {
		if err := requireWriteAccess(claims); err != nil {
			response.Forbidden(w, err.Error())
			return
		}
	}

	var req interruptAckRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if !h.checkAgentProjectAccess(w, r, task) {
		return
	}

	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	err = service.NewWorkflowRunService(h.Svc).AckNodeInterrupt(r.Context(), task.ID, nodeID, claims.UserID.String(), claims.UserType, req.Comment)
	if err != nil {
		writeNodeExecError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{"status": "ok"})
}

// resolveAgentIdentity resolves the claiming agent: agent principals claim
// for themselves (the body's agent_id must match), human callers must name
// the agent explicitly.
func (h *NodeExecutionHandler) resolveAgentIdentity(w http.ResponseWriter, claims svcmw.AuthClaims, agentID string) (uuid.UUID, bool) {
	if agentID == "" {
		if claims.UserType == "agent" {
			agentID = claims.UserID.String()
		} else {
			response.BadRequest(w, "agent_id is required")
			return uuid.Nil, false
		}
	}
	id, err := uuid.Parse(agentID)
	if err != nil {
		response.BadRequest(w, "invalid agent_id")
		return uuid.Nil, false
	}
	if claims.UserType == "agent" && id != claims.UserID {
		response.Forbidden(w, "cannot claim on behalf of another agent")
		return uuid.Nil, false
	}
	if claims.UserType != "agent" {
		if err := requireWriteAccess(claims); err != nil {
			response.Forbidden(w, err.Error())
			return uuid.Nil, false
		}
	}
	return id, true
}

// checkAgentProjectAccess verifies an agent caller is a member of the task's
// project; human callers pass through (workspace access was already checked).
// It writes the 403 response and returns false on failure.
func (h *NodeExecutionHandler) checkAgentProjectAccess(w http.ResponseWriter, r *http.Request, task types.Task) bool {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok || claims.UserType != "agent" {
		return true
	}
	projectID, err := uuid.Parse(task.ProjectID)
	if err != nil {
		response.InternalServerError(w, err)
		return false
	}
	if err := service.NewProjectService(h.Svc).CheckAgentProjectAccess(r.Context(), claims.UserID, projectID); err != nil {
		response.Forbidden(w, "agent is not a member of this project")
		return false
	}
	return true
}

// taskFromContext extracts the task injected by the task access middleware.
func taskFromContext(r *http.Request) (types.Task, bool) {
	raw, ok := svcmw.GetTaskFromContext(r.Context())
	if !ok {
		return types.Task{}, false
	}
	task, ok := raw.(types.Task)
	return task, ok
}

// writeNodeExecError maps a node-execution failure to its protocol status:
// 404 unknown node, 409 superseded/conflicting executions, 500 the rest.
func writeNodeExecError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNodeNotFound):
		response.NotFound(w, "node not found")
	case errors.Is(err, service.ErrExecutionSuperseded):
		response.Conflict(w, "execution superseded")
	case errors.Is(err, service.ErrNodeNotClaimable):
		response.Conflict(w, "node is not claimable")
	case errors.Is(err, service.ErrNodeNotRunning):
		response.Conflict(w, "node is not running")
	case errors.Is(err, service.ErrNodeConflict):
		response.Conflict(w, "node state changed")
	default:
		response.InternalServerError(w, err)
	}
}

// NewInProgressNodesHandler serves GET /workspaces/{workspaceId}/agents/{agentId}/in-progress-nodes:
// the daemon recovery view listing the agent's claimed-but-unfinished
// executions with their original execution ids. Agent principals may only
// list their own nodes.
func NewInProgressNodesHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "authentication required")
			return
		}
		agentID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			response.BadRequest(w, "invalid agent id")
			return
		}
		if claims.UserType == "agent" && claims.UserID != agentID {
			response.Forbidden(w, "agents can only list their own in-progress nodes")
			return
		}

		units, err := service.NewWorkflowRunService(svc).ListAgentInProgressNodes(r.Context(), agentID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		response.JSON(w, r, units)
	}
}
