// mcp.go provides HTTP API endpoints for creating, querying, updating, deleting, health-checking, and managing the status of MCP (Model Context Protocol) servers.
//
// MCP servers are external tool services that AI agents can call, supporting multiple authentication methods (API Key, OAuth, etc.).

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	apitypes "github.com/teammate/server/internal/types"
)

// McpHandler handles HTTP requests for MCP server management, including creating, querying, updating, deleting, health-checking, and status management.
type McpHandler struct {
	Svc *service.Service
}

// NewMcpHandler creates an McpHandler instance.
func NewMcpHandler(svc *service.Service) *McpHandler {
	return &McpHandler{Svc: svc}
}

// Routes returns the complete route table for MCP servers (including read and write operations).
func (h *McpHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateMcpServer)
	r.Get("/", h.ListMcpServers)
	r.Put("/{id}", h.UpdateMcpServer)
	r.Delete("/{id}", h.DeleteMcpServer)

	return r
}

// ReadRoutes returns the read-only route table for MCP servers.
func (h *McpHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListMcpServers)

	return r
}

// WriteRoutes returns the write route table for MCP servers.
func (h *McpHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateMcpServer)
	r.Put("/{id}", h.UpdateMcpServer)
	r.Delete("/{id}", h.DeleteMcpServer)

	return r
}

// CreateMcpServer handles the POST /workspaces/{workspaceId}/mcp-servers endpoint, creating a new MCP server configuration.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, server name (required)
//   - url: string, server URL
//   - type: string, server type
//   - auth_type: string, authentication type
//   - env_vars: object, environment variables
//   - status: string, initial status, default "active"
//
// Response:
//   - 201: MCP server created successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
func (h *McpHandler) CreateMcpServer(w http.ResponseWriter, r *http.Request) {
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

	// parse workspace ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// parse request body
	var req createMcpServerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// input validation
	if err := validateCreateMcpServer(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// set default status
	status := req.Status
	if status == "" {
		status = "active"
	}

	// convert environment variables
	var envVars pqtype.NullRawMessage
	if req.EnvVars != nil {
		envVars = pqtype.NullRawMessage{RawMessage: req.EnvVars, Valid: true}
		// validate that env_vars is a valid JSON object
		if err := validateEnvVarsObject(req.EnvVars); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
	}

	// call service to create the MCP server
	mcpSvc := service.NewMcpService(h.Svc)
	server, err := mcpSvc.Create(r.Context(), buildCreateMcpServerParams(
		workspaceID, req.Name, req.Url, req.Type, req.AuthType, envVars, status,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, mcpServerResponse(server))
}

// ListMcpServers handles the GET /workspaces/{workspaceId}/mcp-servers endpoint, listing all MCP servers under the workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the MCP server list
//   - 400: invalid workspace ID
func (h *McpHandler) ListMcpServers(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// call service to query MCP servers
	mcpSvc := service.NewMcpService(h.Svc)
	servers, err := mcpSvc.List(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	items := make([]apitypes.McpServerResponse, 0, len(servers))
	for _, server := range servers {
		items = append(items, mcpServerResponse(server))
	}
	response.JSON(w, r, items)
}

// UpdateMcpServerStatus handles the PUT /workspaces/{workspaceId}/mcp-servers/{id}/status endpoint, updating the MCP server's running status.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - status: string, new status (required)
//
// Response:
// DeleteMcpServer handles the DELETE /workspaces/{workspaceId}/mcp-servers/{id} endpoint, deleting the specified MCP server.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: deleted successfully
//   - 400: invalid server ID
//   - 401: not authenticated
//   - 403: no permission
//   - 404: MCP server does not exist
func (h *McpHandler) DeleteMcpServer(w http.ResponseWriter, r *http.Request) {
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

	// parse server ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid server id")
		return
	}

	// verify the MCP server belongs to the current workspace
	if checkMcpServerWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// call service to delete the MCP server
	mcpSvc := service.NewMcpService(h.Svc)
	if err := mcpSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateMcpServer handles the PUT /workspaces/{workspaceId}/mcp-servers/{id} endpoint, updating the MCP server's configuration info.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, server name
//   - url: string, server URL
//   - type: string, server type
//   - auth_type: string, authentication type
//   - env_vars: object, environment variables
//   - status: string, server status, default "active"
//
// Response:
//   - 200: successfully returns the updated server info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
//   - 404: MCP server does not exist
func (h *McpHandler) UpdateMcpServer(w http.ResponseWriter, r *http.Request) {
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

	// parse server ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid server id")
		return
	}

	// verify the MCP server belongs to the current workspace
	if checkMcpServerWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// parse request body
	var req updateMcpServerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// input validation (all pointer fields: nil=keep, non-nil=replace)
	if err := validateUpdateMcpServer(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if req.EnvVars != nil {
		if err := validateEnvVarsObject(req.EnvVars); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
	}

	// call service to update the MCP server (pointer fields nil=keep existing value)
	mcpSvc := service.NewMcpService(h.Svc)
	server, err := mcpSvc.Update(r.Context(), id, req.Name, req.Url, req.Type, req.AuthType, req.EnvVars, req.Status)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, mcpServerResponse(server))
}

// ---- input validation functions ----

// validateCreateMcpServer validates the input legality of the create MCP server request.
func validateCreateMcpServer(req createMcpServerRequest) error {
	if len(req.Name) == 0 {
		return fmt.Errorf("name is required")
	}
	if len(req.Name) > 200 {
		return fmt.Errorf("name must be at most 200 characters")
	}
	return validateMcpFields(req.Name, req.Url, req.Type, string(req.AuthType))
}

// validateUpdateMcpServer validates the input legality of the update MCP server request (shares field validation with create).
// Pointer fields that are non-nil and empty string are rejected (explicitly writing empty values is forbidden).
func validateUpdateMcpServer(req updateMcpServerRequest) error {
	name := ""
	if req.Name != nil {
		name = *req.Name
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("name must not be empty")
		}
	}
	url := ""
	if req.Url != nil {
		url = *req.Url
	}
	mcpType := ""
	if req.Type != nil {
		mcpType = *req.Type
	}
	authType := ""
	if req.AuthType != nil {
		authType = string(*req.AuthType)
	}
	return validateMcpFields(name, url, mcpType, authType)
}

// validateMcpFields validates the common MCP server fields (name, URL, type, auth type).
// When name is empty, length is not validated (in the update scenario it can be empty, meaning keep the existing value).
func validateMcpFields(name, url, mcpType, authType string) error {
	if len(name) > 200 {
		return fmt.Errorf("name must be at most 200 characters")
	}
	if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
		return fmt.Errorf("url must start with http://, https://, ws://, or wss://")
	}
	if url != "" && len(url) > 2048 {
		return fmt.Errorf("url must be at most 2048 characters")
	}
	if mcpType != "" && mcpType != "sse" && mcpType != "http" && mcpType != "streamable_http" {
		return fmt.Errorf("type must be one of: sse, http, streamable_http")
	}
	if authType != "" && authType != "none" && authType != "basic" && authType != "bearer" && authType != "api_key" {
		return fmt.Errorf("auth_type must be one of: none, basic, bearer, api_key")
	}
	return nil
}

// validateEnvVarsObject validates that env_vars is a valid JSON object.
func validateEnvVarsObject(raw json.RawMessage) error {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return fmt.Errorf("env_vars must be a valid JSON object")
	}
	return nil
}
