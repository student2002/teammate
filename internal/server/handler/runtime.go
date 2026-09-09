// runtime.go provides HTTP API endpoints for Agent daemon runtime registration, heartbeat, sync, and public key upload.
//
// A runtime is an instance of an Agent daemon that stays online via heartbeats.
// An Agent can only operate its own runtime; human users can operate all runtimes in the workspace.

package handler

import (
	"database/sql"
	"errors"
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

// RuntimeHandler handles HTTP requests for Agent daemon runtime management, including registration, heartbeat, sync, and public key upload.
type RuntimeHandler struct {
	Svc *service.Service
}

// NewRuntimeHandler creates a RuntimeHandler instance.
func NewRuntimeHandler(svc *service.Service) *RuntimeHandler {
	return &RuntimeHandler{Svc: svc}
}

// Routes returns the complete route table for runtimes (including read and write operations).
func (h *RuntimeHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.RegisterRuntime)
	r.Post("/{id}/heartbeat", h.Heartbeat)

	return r
}

// ReadRoutes returns the read-only routes for runtimes (accessible to viewer+).
func (h *RuntimeHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	return r
}

// WriteRoutes returns the write routes for runtimes (accessible to member+ or the Agent itself).
func (h *RuntimeHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.RegisterRuntime)
	r.Post("/{id}/heartbeat", h.Heartbeat)

	return r
}


// RegisterRuntime handles the POST /workspaces/{workspaceId}/runtimes endpoint, registering an Agent daemon runtime instance.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - agent_id: string, Agent ID (required)
//   - daemon_id: string, daemon ID
//   - provider: string, Agent provider
//   - version: string, daemon version
//   - status: string, initial status, default "online"
//   - session_token_hash: string, session token hash
//   - session_expires_at: string, session expiration time
//   - public_key: string, RSA public key
//
// Response:
//   - 201: runtime registered successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: Agents can only register their own runtime; humans need admin+ role
func (h *RuntimeHandler) RegisterRuntime(w http.ResponseWriter, r *http.Request) {
	// parse request body
	var req registerRuntimeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// agent_id is already a uuid.UUID (the registerRuntimeRequest.AgentID field type)
	agentID := req.AgentID
	if agentID == uuid.Nil {
		response.BadRequest(w, "invalid agent_id: must be a valid UUID")
		return
	}

	// verify the Agent belongs to the workspace in the URL
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// permission check
	if claims.UserType == "agent" {
		// Agents can only register runtimes for themselves
		if claims.UserID != agentID {
			response.Forbidden(w, "agents can only register runtimes for themselves")
			return
		}
	}

	if claims.UserType == "member" {
		// only admin+ role can register runtimes on behalf of an Agent
		if types.MemberRoleLevel(claims.Role) < 3 {
			response.Forbidden(w, "only admins can register runtimes on behalf of agents")
			return
		}
	}

	// verify the Agent exists and belongs to the workspace
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "agent not found")
		return
	}

	// set default status
	status := req.Status
	if status == "" {
		status = RuntimeStatusOnline
	}

	// call service to register the runtime
	rtSvc := service.NewRuntimeService(h.Svc)
	runtime, err := rtSvc.Register(r.Context(), buildCreateRuntimeParams(
		agentID, req.DaemonID, req.Provider, req.Version, status, req.SessionTokenHash, req.SessionExpiresAt, req.PublicKey,
	))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.BadRequest(w, "agent not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, runtime)
}

// Heartbeat handles the POST /workspaces/{workspaceId}/runtimes/{id}/heartbeat endpoint; an Agent periodically sends heartbeats to stay online.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the runtime info
//   - 400: invalid runtime ID
//   - 401: not authenticated
//   - 403: Agents can only operate their own runtime
//   - 404: runtime does not exist
func (h *RuntimeHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	// parse runtime ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid runtime id")
		return
	}

	// verify runtime ownership (Agents can only operate their own runtime)
	if h.checkRuntimeOwnership(w, r, id) == nil {
		return
	}

	// call service to send the heartbeat
	rtSvc := service.NewRuntimeService(h.Svc)
	runtime, err := rtSvc.Heartbeat(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "runtime not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, runtime)
}

// --- public key upload ---
func (h *RuntimeHandler) checkRuntimeWorkspace(w http.ResponseWriter, r *http.Request, runtimeID uuid.UUID) bool {
	// parse workspace ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return false
	}

	// query the runtime
	runtime, err := service.NewRuntimeService(h.Svc).GetRuntimeByID(r.Context(), runtimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "runtime not found")
			return false
		}
		response.InternalServerError(w, err)
		return false
	}

	// verify the Agent belongs to the workspace
	rtAgentID, _ := uuid.Parse(runtime.AgentID)
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), rtAgentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "runtime not found")
		return false
	}

	return true
}

// checkRuntimeOwnership verifies that the runtime belongs to the currently authenticated Agent's own runtime.
// For Agents: the runtime must belong to the authenticated Agent (to prevent cross-Agent interference).
// For human users: the runtime's Agent must belong to the URL workspace.
func (h *RuntimeHandler) checkRuntimeOwnership(w http.ResponseWriter, r *http.Request, runtimeID uuid.UUID) *Runtime {
	// parse workspace ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return nil
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return nil
	}

	// query the runtime
	runtime, err := service.NewRuntimeService(h.Svc).GetRuntimeByID(r.Context(), runtimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "runtime not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// verify the Agent belongs to the workspace
	rAgentID, _ := uuid.Parse(runtime.AgentID)
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), rAgentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "runtime not found")
		return nil
	}

	// Agents can only operate their own runtime
	if claims.UserType == "agent" && rAgentID != claims.UserID {
		response.Forbidden(w, "agents can only operate on their own runtimes")
		return nil
	}

	return &runtime
}
