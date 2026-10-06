// agent.go provides HTTP API endpoints for AI agent (Agent) CRUD management, skill binding, MCP server binding, Token rotation, and permission management.
//
// All endpoints require authentication; write operations require member or higher role permissions.
// The agent's custom_env field is only visible to users with write permissions (owner/admin/member) to prevent secret leakage.

package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// AgentHandler handles HTTP requests related to AI agents, including creating, querying, updating, and deleting agents, managing agent skill and MCP server bindings, and agent permission control.
type AgentHandler struct {
	Svc *service.Service
}

// NewAgentHandler creates an AgentHandler instance.
func NewAgentHandler(svc *service.Service) *AgentHandler {
	return &AgentHandler{Svc: svc}
}

// Routes returns the complete route table for Agents (including read and write operations).
func (h *AgentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListAgents)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetAgent)
		r.Get("/stats", h.GetAgentStats)
		r.Put("/", h.UpdateAgent)
		r.Delete("/", h.DeleteAgent)
		r.Get("/skills", h.ListSkills)
		r.Get("/mcp-servers", h.ListMcpServers)
		r.Post("/skills", h.AddSkill)
		r.Delete("/skills/{skillId}", h.RemoveSkill)
		r.Post("/mcp-servers", h.AddMcpServer)
		r.Delete("/mcp-servers/{serverId}", h.RemoveMcpServer)
		r.Get("/in-progress-nodes", h.GetInProgressNodes)
		r.Get("/executions", h.ListExecutions)
		r.Post("/grant-role", h.GrantRole)
		r.Get("/permissions", h.ListPermissions)
		r.Post("/permissions", h.GrantPermission)
		r.Delete("/permissions/{permission}", h.RevokePermission)

		// daemon-only MCP execution endpoint: returns decrypted env_vars, only accessible by the Agent itself
		r.Get("/execution/mcp-servers", h.GetExecutionMcpServers)
	})

	return r
}

// GetAgentStats handles the GET /workspaces/{workspaceId}/agents/{id}/stats endpoint,
// returning completed task count, token usage, and average completion time for the agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter id is the agent UUID
//
// Response:
//   - 200: successfully returns the agent statistics
//   - 400: invalid agent id
//   - 404: agent does not exist or belongs to another workspace
func (h *AgentHandler) GetAgentStats(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}
	if checkAgentWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	stats, err := service.NewStatsService(h.Svc).GetAgentStats(r.Context(), id)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, stats)
}

// ReadRoutes returns the read-only route table for Agents (query operations only).
func (h *AgentHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListAgents)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetAgent)
		r.Get("/skills", h.ListSkills)
		r.Get("/mcp-servers", h.ListMcpServers)
	})

	return r
}

// WriteRoutes returns the write route table for Agents (modification operations only).
func (h *AgentHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateAgent)
		r.Delete("/", h.DeleteAgent)
		r.Post("/skills", h.AddSkill)
		r.Delete("/skills/{skillId}", h.RemoveSkill)
		r.Post("/mcp-servers", h.AddMcpServer)
		r.Delete("/mcp-servers/{serverId}", h.RemoveMcpServer)
	})

	return r
}



// ListAgents handles the GET /workspaces/{workspaceId}/agents endpoint, listing all AI agents under the workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the agent list
//   - 400: invalid workspace ID
//   - 401: not authenticated
func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	agentSvc := service.NewAgentService(h.Svc)
	agents, err := agentSvc.List(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// list response does not include custom_env (to prevent secret leakage)
	result := make([]agentResponse, len(agents))
	agentIDs := make([]uuid.UUID, len(agents))
	for i, a := range agents {
		result[i] = agentToResponse(a)
		parsed, err := uuid.Parse(a.ID)
		if err != nil {
			response.InternalServerError(w, fmt.Errorf("parse agent id: %w", err))
			return
		}
		agentIDs[i] = parsed
	}

	// batch-fill Token usage (aggregated in real time from the token_usage table)
	tuSvc := service.NewTokenUsageService(h.Svc)
	if tokenMap, err := tuSvc.GetByAgents(r.Context(), agentIDs); err == nil {
		for i := range result {
			if tu, ok := tokenMap[result[i].ID]; ok {
				result[i].InputTokens = tu.InputTokens
				result[i].OutputTokens = tu.OutputTokens
			}
		}
	}

	response.JSON(w, r, result)
}

// GetAgent handles the GET /workspaces/{workspaceId}/agents/{id} endpoint, querying the detailed info of the specified AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns agent details
//   - 400: invalid agent ID
//   - 401: not authenticated
//   - 404: agent does not exist or is not in the current workspace
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	agent := checkAgentWorkspace(h.Svc, w, r, id)
	if agent == nil {
		return
	}

	// only users with write permission can view custom_env
	claims, _ := svcmw.GetAuthFromContext(r.Context())
	if requireWriteAccess(claims) == nil {
		response.JSON(w, r, agentToResponseWithEnv(*agent))
	} else {
		response.JSON(w, r, agentToResponse(*agent))
	}
}


// UpdateAgent handles the PUT /workspaces/{workspaceId}/agents/{id} endpoint, updating the AI agent's configuration info.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - instructions: string, agent execution instructions
//   - model: string, model to use
//   - status: string, agent status
//   - custom_env: object, custom environment variables
//   - extra_args: string[], extra command-line arguments
//   - git_name: string, Git commit username
//   - git_email: string, Git commit email
//
// Response:
//   - 200: successfully returns the updated agent info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
//   - 404: agent does not exist
func (h *AgentHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse agent ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	if checkAgentWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// parse request body
	var req updateAgentRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if req.Status != "" {
		switch req.Status {
		case "online", "offline", "busy", "paused", "pending":
		default:
			response.BadRequest(w, "invalid status: must be one of online/offline/busy/paused/pending")
			return
		}
	}

	// custom_env is passed through to the service directly as json.RawMessage
	customEnv := req.CustomEnv

	extraArgs := req.ExtraArgs
	if extraArgs == nil {
		extraArgs = []string{}
	}

	// call service to update the agent
	agentSvc := service.NewAgentService(h.Svc)
	agent, err := agentSvc.Update(r.Context(), buildUpdateAgentParams(
		id, req.Instructions, req.Model, req.Status, customEnv, extraArgs, req.GitName, req.GitEmail,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "agent not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, agentToResponseWithEnv(agent))
}

// DeleteAgent handles the DELETE /workspaces/{workspaceId}/agents/{id} endpoint, deleting the specified AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: deleted successfully
//   - 400: invalid agent ID
//   - 401: not authenticated
//   - 403: no permission
//   - 404: agent does not exist
func (h *AgentHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse agent ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	agent := checkAgentWorkspace(h.Svc, w, r, id)
	if agent == nil {
		return
	}

	// call service to delete the agent
	agentSvc := service.NewAgentService(h.Svc)
	if err := agentSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	// record audit log
	workspaceID, _ := uuid.Parse(chi.URLParam(r, "workspaceId"))
	auditSvc := service.NewAuditService(h.Svc)
	if err := auditSvc.Log(r.Context(), service.AuditLogEntry{
		WorkspaceID:  workspaceID,
		ActorType:    claims.UserType,
		ActorID:      claims.UserID,
		Action:       "agent.delete",
		ResourceType: "agent",
		ResourceID:   agent.ID,
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	}); err != nil {
		slog.Warn("audit log write failed", "err", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListSkills handles the GET /workspaces/{workspaceId}/agents/{id}/skills endpoint, listing the skills bound to the AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the skill list
//   - 400: invalid agent ID
//   - 401: not authenticated
//   - 404: agent does not exist
func (h *AgentHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	agentSvc := service.NewAgentService(h.Svc)
	skills, err := agentSvc.ListSkills(r.Context(), agentID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, skills)
}

// ListMcpServers handles the GET /workspaces/{workspaceId}/agents/{id}/mcp-servers endpoint, listing the MCP servers bound to the AI agent.
func (h *AgentHandler) ListMcpServers(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	agentSvc := service.NewAgentService(h.Svc)
	servers, err := agentSvc.ListMcpServers(r.Context(), agentID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok || claims.UserType != "agent" {
		for i := range servers {
			servers[i].EnvVars = maskAgentMcpEnvVarsForDisplay(servers[i].EnvVars)
		}
	}

	response.JSON(w, r, servers)
}

// GetExecutionMcpServers handles the GET /workspaces/{workspaceId}/agents/{id}/execution/mcp-servers endpoint,
// returning the list of MCP servers needed for Agent execution (env_vars decrypted).
//
// Authentication: only the Agent itself can access (user_type=agent and user_id matches the path {id}),
// human users and other Agents get 403.
// This endpoint is dedicated to Agent daemon execution and returns the fully decrypted environment variables.
func (h *AgentHandler) GetExecutionMcpServers(w http.ResponseWriter, r *http.Request) {
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

	// strong authentication: only the Agent itself can access
	if claims.UserType != "agent" || claims.UserID != agentID {
		response.Forbidden(w, "only the agent itself can access execution MCP servers")
		return
	}

	agentSvc := service.NewAgentService(h.Svc)
	servers, err := agentSvc.ListExecutionMcpServers(r.Context(), agentID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, servers)
}



// AddSkill handles the POST /workspaces/{workspaceId}/agents/{id}/skills endpoint, binding a skill to the AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - skill_id: UUID, skill ID (required)
//   - enabled: bool, whether to enable
//
// Response:
//   - 201: skill bound successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
//   - 404: agent does not exist
func (h *AgentHandler) AddSkill(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse agent ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// parse request body
	var req addSkillRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if checkSkillWorkspace(h.Svc, w, r, req.SkillID) == nil {
		return
	}

	// enabled defaults to true: a bind action implies enable, callers may
	// explicitly pass false to bind disabled
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// call service to bind the skill
	agentSvc := service.NewAgentService(h.Svc)
	skill, err := agentSvc.AddSkill(r.Context(), buildAddAgentSkillParams(agentID, req.SkillID, enabled))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, skill)
}

// RemoveSkill handles the DELETE /workspaces/{workspaceId}/agents/{id}/skills/{skillId} endpoint, removing the specified skill from the AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: removed successfully
//   - 400: invalid agent ID or skill ID
//   - 401: not authenticated
//   - 403: no permission
//   - 404: agent or skill does not exist
func (h *AgentHandler) RemoveSkill(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse agent ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// parse skill ID
	skillID, err := uuid.Parse(chi.URLParam(r, "skillId"))
	if err != nil {
		response.BadRequest(w, "invalid skill id")
		return
	}

	// call service to remove the skill
	agentSvc := service.NewAgentService(h.Svc)
	if err := agentSvc.RemoveSkill(r.Context(), buildRemoveAgentSkillParams(agentID, skillID)); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


// AddMcpServer handles the POST /workspaces/{workspaceId}/agents/{id}/mcp-servers endpoint, binding an MCP server to the AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - mcp_server_id: UUID, MCP server ID (required)
//   - enabled: bool, whether to enable
//
// Response:
//   - 201: MCP server bound successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
//   - 404: agent does not exist
func (h *AgentHandler) AddMcpServer(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse agent ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// parse request body
	var req addMcpServerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if checkMcpServerWorkspace(h.Svc, w, r, req.McpServerID) == nil {
		return
	}

	// enabled defaults to true: a bind action implies enable, callers may
	// explicitly pass false to bind disabled
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	// call service to bind the MCP server
	agentSvc := service.NewAgentService(h.Svc)
	server, err := agentSvc.AddMcpServer(r.Context(), buildAddAgentMcpServerParams(agentID, req.McpServerID, enabled))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, server)
}

// RemoveMcpServer handles the DELETE /workspaces/{workspaceId}/agents/{id}/mcp-servers/{serverId} endpoint, removing the MCP server bound to the AI agent.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: removed successfully
//   - 400: invalid agent ID or server ID
//   - 401: not authenticated
//   - 403: no permission
//   - 404: agent or MCP server does not exist
func (h *AgentHandler) RemoveMcpServer(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse agent ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// verify the agent belongs to the current workspace
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// parse server ID
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		response.BadRequest(w, "invalid server id")
		return
	}

	// call service to remove the MCP server
	agentSvc := service.NewAgentService(h.Svc)
	if err := agentSvc.RemoveMcpServer(r.Context(), buildRemoveAgentMcpServerParams(agentID, serverID)); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


// --- Agent permission management ---

// GetInProgressNodes handles the GET /agents/{id}/in-progress-nodes endpoint,
// querying nodes claimed by the specified Agent but not yet completed (in_progress), used to resume execution after an Agent restart.
//
// Authentication: only the Agent itself can query (user_type=agent and user_id matches the path {id}),
// human users are not allowed to query this endpoint.
func (h *AgentHandler) GetInProgressNodes(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	agentIDStr := chi.URLParam(r, "id")
	if agentIDStr == "" {
		response.BadRequest(w, "missing agent id")
		return
	}
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// authentication: only the Agent itself can query
	if claims.UserType != "agent" || claims.UserID != agentID {
		response.Forbidden(w, "only the agent itself can query its in-progress nodes")
		return
	}

	nodes, err := service.NewAgentService(h.Svc).GetInProgressNodesByAgent(r.Context(), agentID, claims.WorkspaceID)
	if err != nil {
		response.InternalServerError(w, fmt.Errorf("get in-progress nodes: %w", err))
		return
	}

	response.JSON(w, r, nodes)
}

// ListExecutions handles GET /workspaces/{workspaceId}/agents/{id}/executions?limit=10,
// returning the agent's recent execution sessions with task context.
//
// Response:
//   - 200: [{task_id, task_title, node_name, status, branch, started_at, completed_at}]
//   - 400: invalid agent id
func (h *AgentHandler) ListExecutions(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}
	limit := int32(10)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			response.BadRequest(w, "limit must be an integer between 1 and 100")
			return
		}
		limit = int32(n)
	}

	executions, err := service.NewAgentService(h.Svc).ListExecutions(r.Context(), agentID, limit)
	if err != nil {
		response.InternalServerError(w, fmt.Errorf("list agent executions: %w", err))
		return
	}
	if executions == nil {
		executions = []types.AgentExecution{}
	}
	response.JSON(w, r, executions)
}

// grantRoleRequest grant role request body.
type grantRoleRequest struct {
	Role string `json:"role"` // predefined role name (developer/reviewer/ops)
}

// GrantRole handles POST /workspaces/{workspaceId}/agents/{id}/grant-role,
// granting all permissions of a predefined role to the agent.
//
// Request body:
//   - role: string, predefined role name (required)
//
// Response:
//   - 200: {"message": "role granted"}
//   - 400: missing/unknown role
func (h *AgentHandler) GrantRole(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireAdminAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	var req grantRoleRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if req.Role == "" {
		response.BadRequest(w, "role is required")
		return
	}

	permSvc := service.NewAgentPermissionService(h.Svc)
	if _, ok := types.AgentRoles[req.Role]; !ok {
		response.BadRequest(w, "unknown role: " + req.Role)
		return
	}
	if err := permSvc.GrantRolePermissions(r.Context(), agentID, req.Role, claims.UserID); err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{"message": "role granted"})
}

// ListPermissions handles GET /workspaces/{workspaceId}/agents/{id}/permissions,
// listing the agent's granted permissions.
//
// Response:
//   - 200: permission list
//   - 400: invalid agent id
func (h *AgentHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	perms, err := service.NewAgentPermissionService(h.Svc).ListPermissions(r.Context(), agentID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, perms)
}

// grantPermissionRequest grant permission request body.
type grantPermissionRequest struct {
	Permission string `json:"permission"` // permission string (e.g. task:approve)
}

// GrantPermission handles POST /workspaces/{workspaceId}/agents/{id}/permissions,
// granting one permission to the agent at the workspace-wide resource scope.
//
// Request body:
//   - permission: string, permission name (required)
//
// Response:
//   - 201: the granted permission record
//   - 400: missing/unknown permission
func (h *AgentHandler) GrantPermission(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireAdminAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	var req grantPermissionRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if req.Permission == "" {
		response.BadRequest(w, "permission is required")
		return
	}
	if !types.IsValidAgentPermission(req.Permission) {
		response.BadRequest(w, "unknown permission: "+req.Permission)
		return
	}

	permSvc := service.NewAgentPermissionService(h.Svc)
	perm, err := permSvc.Grant(r.Context(), agentID, req.Permission, "*", nil, claims.UserID)
	if err != nil {
		if isUniqueViolation(err) {
			response.Conflict(w, "the agent already holds this permission at the workspace scope")
			return
		}
		response.InternalServerError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, perm)
}

// RevokePermission handles DELETE /workspaces/{workspaceId}/agents/{id}/permissions/{permission},
// revoking every workspace-wide grant of the named permission from the agent.
//
// Response:
//   - 204: revoked (or nothing to revoke)
//   - 400: invalid ids
func (h *AgentHandler) RevokePermission(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireAdminAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}
	permission := chi.URLParam(r, "permission")
	if permission == "" {
		response.BadRequest(w, "permission is required")
		return
	}

	permSvc := service.NewAgentPermissionService(h.Svc)
	perms, err := permSvc.ListPermissions(r.Context(), agentID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	for _, p := range perms {
		if p.Permission == permission {
			permID, err := uuid.Parse(p.ID)
			if err != nil {
				continue
			}
			if err := permSvc.Revoke(r.Context(), permID); err != nil {
				response.InternalServerError(w, err)
				return
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
