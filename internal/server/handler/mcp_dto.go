// mcp_dto.go 为 mcp.go 提供 types 类型别名、参数构建器和响应转换函数。
package handler

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	apitypes "github.com/teammate/server/internal/types"
)

// ---- 请求结构体 ----

// createMcpServerRequest 创建 MCP 服务器请求体。
type createMcpServerRequest struct {
	Name     string          `json:"name"`      // 服务器名称
	Url      string          `json:"url"`       // 服务器 URL
	Type     string          `json:"type"`      // 服务器类型
	AuthType string          `json:"auth_type"` // 认证类型
	EnvVars  json.RawMessage `json:"env_vars"`  // 环境变量（JSON）
	Status   string          `json:"status"`    // 初始状态
}

// updateMcpServerRequest 更新 MCP 服务器请求体。
type updateMcpServerRequest struct {
	Name     *string         `json:"name,omitempty"`      // 服务器名称（nil=保持）
	Url      *string         `json:"url,omitempty"`       // 服务器 URL（nil=保持）
	Type     *string         `json:"type,omitempty"`      // 服务器类型（nil=保持）
	AuthType *string         `json:"auth_type,omitempty"` // 认证类型（nil=保持）
	EnvVars  json.RawMessage `json:"env_vars,omitempty"`  // 环境变量（nil=保持，{} = 清空）
	Status   *string         `json:"status,omitempty"`    // 服务器状态（nil=保持）
}

// ---- 参数构建器 ----

// buildCreateMcpServerParams 从请求字段构建 types.CreateMcpServerParams。
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

// ---- 响应转换函数 ----

// mcpServerResponse 将 types.McpServer 转换为 API 响应。
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

// rawObject 将 JSON RawMessage 转换为 map。
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
