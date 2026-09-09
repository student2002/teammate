// mcp_dto.go provides types aliases, parameter builders, and response conversion functions for mcp.go.
package handler

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	apitypes "github.com/teammate/server/internal/types"
)

// ---- request structs ----

// createMcpServerRequest create MCP server request body.
type createMcpServerRequest struct {
	Name     string          `json:"name"`      // server name
	Url      string          `json:"url"`       // server URL
	Type     string          `json:"type"`      // server type
	AuthType string          `json:"auth_type"` // authentication type
	EnvVars  json.RawMessage `json:"env_vars"`  // environment variables (JSON)
	Status   string          `json:"status"`    // initial status
}

// updateMcpServerRequest update MCP server request body.
type updateMcpServerRequest struct {
	Name     *string         `json:"name,omitempty"`      // server name (nil=keep)
	Url      *string         `json:"url,omitempty"`       // server URL (nil=keep)
	Type     *string         `json:"type,omitempty"`      // server type (nil=keep)
	AuthType *string         `json:"auth_type,omitempty"` // authentication type (nil=keep)
	EnvVars  json.RawMessage `json:"env_vars,omitempty"`  // environment variables (nil=keep, {} = clear)
	Status   *string         `json:"status,omitempty"`    // server status (nil=keep)
}

// ---- parameter builders ----

// buildCreateMcpServerParams builds types.CreateMcpServerParams from request fields.
func buildCreateMcpServerParams(
	workspaceID uuid.UUID,
	name string,
	url string,
	serverType string,
	authType string,
	envVars pqtype.NullRawMessage,
	status string,
) apitypes.CreateMcpServerParams {
	typeStr := serverType
	return apitypes.CreateMcpServerParams{
		WorkspaceID: workspaceID.String(),
		Name:        name,
		URL:         url,
		Type:        &typeStr,
		AuthType:    authType,
		EnvVars:     envVars.RawMessage,
	}
}

// ---- response conversion functions ----

// mcpServerResponse converts types.McpServer to an API response.
func mcpServerResponse(server apitypes.McpServer) apitypes.McpServerResponse {
	idUUID, _ := uuid.Parse(server.ID)
	wsUUID, _ := uuid.Parse(server.WorkspaceID)
	return apitypes.McpServerResponse{
		ID:          idUUID,
		WorkspaceID: wsUUID,
		Name:        server.Name,
		Url:         server.URL,
		Type:        server.Type,
		AuthType:    server.AuthType,
		EnvVars:     rawObject(server.EnvVars, server.EnvVars != nil),
		Status:      server.Status,
		CreatedAt:   server.CreatedAt,
	}
}

// rawObject converts a JSON RawMessage to a map.
func rawObject(raw json.RawMessage, valid bool) map[string]interface{} {
	if !valid || len(raw) == 0 || string(raw) == "null" {
		return map[string]interface{}{}
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return map[string]interface{}{}
	}
	return obj
}
