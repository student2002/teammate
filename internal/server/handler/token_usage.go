// token_usage.go provides HTTP API endpoints for AI agent token usage reporting and querying.
//
// This file provides the following HTTP API endpoints:
//   - POST /tasks/{taskId}/token-usage: an Agent reports token usage data for the executing node (Agent identity only)
//   - GET /tasks/{taskId}/token-usage: query the token usage summary for the specified task
//
// The reporting endpoint strictly requires the requester to be an Agent and to be the assignee of the target node.
// The Agent ID is extracted from the auth claims to prevent forgery. The ownership relationship between the node and the task is verified through URL parameters and database checks to ensure workspace isolation.

package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// TokenUsageHandler handles HTTP requests for token usage reporting and querying.
type TokenUsageHandler struct {
	Svc *service.Service
}

// NewTokenUsageHandler creates a TokenUsageHandler instance.
//
// Parameters:
//   - svc: business logic service instance, provides token usage management capabilities
//
// Returns:
//   - *TokenUsageHandler: token usage handler instance
func NewTokenUsageHandler(svc *service.Service) *TokenUsageHandler {
	return &TokenUsageHandler{Svc: svc}
}

// Routes returns the route table for token usage.
//
// Returns:
//   - chi.Router: routes containing token usage reporting and querying endpoints
func (h *TokenUsageHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.ReportTokenUsage)

	return r
}

// ReportTokenUsage handles the POST /tasks/{taskId}/token-usage endpoint, where an Agent reports token usage data for the executing node.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter taskId is the task ID, request body contains token usage data
//
// Returns:
//   - no return value, writes a JSON response via w (201 Created), containing the created usage record or an error message
func (h *TokenUsageHandler) ReportTokenUsage(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// Only Agents can report token usage - a Member UUID would violate the
	// token_usage.agent_id foreign key constraint (REFERENCES agents(id)).
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can report token usage")
		return
	}

	var req types.ReportTokenUsageReq
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// derive agent_id from auth claims (not the request body) to prevent forgery
	agentID := claims.UserID

	// verify that this Agent is the assignee of the node
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), req.TaskNodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}
	if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
		response.Forbidden(w, "only the assigned agent can report token usage for this node")
		return
	}

	// verify the node belongs to the task in the URL (workspace isolation)
	urlTaskIDStr := chi.URLParam(r, "taskId")
	var urlTaskID int32
	if _, err := fmt.Sscanf(urlTaskIDStr, "%d", &urlTaskID); err != nil || node.TaskID != urlTaskID {
		response.BadRequest(w, "node does not belong to the specified task")
		return
	}

	tuSvc := service.NewTokenUsageService(h.Svc)
	usage, err := tuSvc.Create(r.Context(), buildCreateTokenUsageParams(
		req.TaskNodeID, agentID, req.InputTokens, req.OutputTokens, req.TotalTokens, req.CostEstimate,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, usage)
}
