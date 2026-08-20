// mcp.go 提供 MCP（Model Context Protocol）服务器的创建、查询、更新、删除、健康检查及状态管理等 HTTP API 端点。
//
// MCP 服务器是 AI 代理可调用的外部工具服务，支持多种认证方式（API Key、OAuth 等）。

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

// McpHandler 处理 MCP 服务器管理的 HTTP 请求，包括创建、查询、更新、删除、健康检查及状态管理。
type McpHandler struct {
	Svc *service.Service
}

// NewMcpHandler 创建 McpHandler 实例。
func NewMcpHandler(svc *service.Service) *McpHandler {
	return &McpHandler{Svc: svc}
}

// Routes 返回 MCP 服务器的完整路由表（包含读写操作）。
func (h *McpHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateMcpServer)
	r.Get("/", h.ListMcpServers)
	r.Put("/{id}", h.UpdateMcpServer)
	r.Delete("/{id}", h.DeleteMcpServer)

	return r
}

// ReadRoutes 返回 MCP 服务器的只读路由表。
func (h *McpHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListMcpServers)

	return r
}

// WriteRoutes 返回 MCP 服务器的写入路由表。
func (h *McpHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateMcpServer)
	r.Put("/{id}", h.UpdateMcpServer)
	r.Delete("/{id}", h.DeleteMcpServer)

	return r
}

// CreateMcpServer 处理 POST /workspaces/{workspaceId}/mcp-servers 端点，创建新的 MCP 服务器配置。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，服务器名称（必填）
//   - url: string，服务器 URL
//   - type: string，服务器类型
//   - auth_type: string，认证类型
//   - env_vars: object，环境变量
//   - status: string，初始状态，默认 "active"
//
// 响应：
//   - 201: 成功创建 MCP 服务器
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
func (h *McpHandler) CreateMcpServer(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态和写入权限
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析工作区 ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// 解析请求体
	var req createMcpServerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 输入校验
	if err := validateCreateMcpServer(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 设置默认状态
	status := req.Status
	if status == "" {
		status = "active"
	}

	// 转换环境变量
	var envVars pqtype.NullRawMessage
	if req.EnvVars != nil {
		envVars = pqtype.NullRawMessage{RawMessage: req.EnvVars, Valid: true}
		// 验证 env_vars 是有效 JSON 对象
		if err := validateEnvVarsObject(req.EnvVars); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
	}

	// 调用 service 创建 MCP 服务器
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

// ListMcpServers 处理 GET /workspaces/{workspaceId}/mcp-servers 端点，列出工作区下的所有 MCP 服务器。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回 MCP 服务器列表
//   - 400: 工作区 ID 无效
func (h *McpHandler) ListMcpServers(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// 调用 service 查询 MCP 服务器
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

// UpdateMcpServerStatus 处理 PUT /workspaces/{workspaceId}/mcp-servers/{id}/status 端点，更新 MCP 服务器的运行状态。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - status: string，新状态（必填）
//
// 响应：
// DeleteMcpServer 处理 DELETE /workspaces/{workspaceId}/mcp-servers/{id} 端点，删除指定的 MCP 服务器。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功删除
//   - 400: 服务器 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: MCP 服务器不存在
func (h *McpHandler) DeleteMcpServer(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态和写入权限
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析服务器 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid server id")
		return
	}

	// 验证 MCP 服务器属于当前工作区
	if checkMcpServerWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// 调用 service 删除 MCP 服务器
	mcpSvc := service.NewMcpService(h.Svc)
	if err := mcpSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateMcpServer 处理 PUT /workspaces/{workspaceId}/mcp-servers/{id} 端点，更新 MCP 服务器的配置信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，服务器名称
//   - url: string，服务器 URL
//   - type: string，服务器类型
//   - auth_type: string，认证类型
//   - env_vars: object，环境变量
//   - status: string，服务器状态，默认 "active"
//
// 响应：
//   - 200: 成功返回更新后的服务器信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
//   - 404: MCP 服务器不存在
func (h *McpHandler) UpdateMcpServer(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态和写入权限
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析服务器 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid server id")
		return
	}

	// 验证 MCP 服务器属于当前工作区
	if checkMcpServerWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// 解析请求体
	var req updateMcpServerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 输入校验（所有指针字段：nil=保持，非 nil=替换）
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

	// 调用 service 更新 MCP 服务器（指针字段 nil=保持现有值）
	mcpSvc := service.NewMcpService(h.Svc)
	server, err := mcpSvc.Update(r.Context(), id, req.Name, req.Url, req.Type, req.AuthType, req.EnvVars, req.Status)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, mcpServerResponse(server))
}

// ---- 输入校验函数 ----

// validateCreateMcpServer 验证创建 MCP 服务器请求的输入合法性。
func validateCreateMcpServer(req createMcpServerRequest) error {
	if len(req.Name) == 0 {
		return fmt.Errorf("name is required")
	}
	if len(req.Name) > 200 {
		return fmt.Errorf("name must be at most 200 characters")
	}
	return validateMcpFields(req.Name, req.Url, req.Type, string(req.AuthType))
}

// validateUpdateMcpServer 验证更新 MCP 服务器请求的输入合法性（与 create 共用字段校验）。
// 指针字段 non-nil 且为空字符串时拒绝（禁止显式写空值）。
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

// validateMcpFields 校验 MCP 服务器公共字段（名称、URL、类型、认证类型）。
// name 为空时不校验长度（更新场景可为空，表示保持现有值）。
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

// validateEnvVarsObject 验证 env_vars 是合法的 JSON 对象。
func validateEnvVarsObject(raw json.RawMessage) error {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return fmt.Errorf("env_vars must be a valid JSON object")
	}
	return nil
}
