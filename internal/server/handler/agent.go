// agent.go 提供 AI 代理（Agent）的 CRUD 管理、技能绑定、MCP 服务器绑定、Token 轮换及权限管理等 HTTP API 端点。
//
// 所有端点均需认证，写操作需要 member 及以上角色权限。
// Agent 的 custom_env 字段仅对写权限用户（owner/admin/member）可见，防止密钥泄露。

package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// AgentHandler 处理 AI 代理相关的 HTTP 请求，包括创建、查询、更新、删除代理，管理代理的技能和 MCP 服务器绑定，以及代理权限控制。
type AgentHandler struct {
	Svc *service.Service
}

// NewAgentHandler 创建 AgentHandler 实例。
func NewAgentHandler(svc *service.Service) *AgentHandler {
	return &AgentHandler{Svc: svc}
}

// Routes 返回 Agent 的完整路由表（包含读写操作）。
func (h *AgentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateAgent)
	r.Get("/", h.ListAgents)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetAgent)
		r.Put("/", h.UpdateAgent)
		r.Delete("/", h.DeleteAgent)
		r.Get("/skills", h.ListSkills)
		r.Get("/mcp-servers", h.ListMcpServers)
		r.Post("/skills", h.AddSkill)
		r.Delete("/skills/{skillId}", h.RemoveSkill)
		r.Post("/mcp-servers", h.AddMcpServer)
		r.Delete("/mcp-servers/{serverId}", h.RemoveMcpServer)
		r.Get("/in-progress-nodes", h.GetInProgressNodes)

		// daemon-only MCP 执行端点：返回解密后的 env_vars，仅 Agent 自身可访问
		r.Get("/execution/mcp-servers", h.GetExecutionMcpServers)
	})

	return r
}

// ReadRoutes 返回 Agent 的只读路由表（仅查询操作）。
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

// WriteRoutes 返回 Agent 的写入路由表（仅修改操作）。
func (h *AgentHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateAgent)
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


// CreateAgent 处理 POST /workspaces/{workspaceId}/agents 端点，创建新的 AI 代理并生成 API Token。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，代理名称（必填）
//   - provider: string，代理提供者，默认 "claude"
//   - instructions: string，代理执行指令
//   - model: string，使用模型
//   - status: string，初始状态，默认 "offline"
//   - custom_env: object，自定义环境变量
//   - extra_args: string[]，额外命令行参数
//   - git_name: string，Git 提交用户名
//   - git_email: string，Git 提交邮箱
//
// 响应：
//   - 201: 成功创建，返回代理信息和 API Token
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限（需要 member 及以上角色）
//
// 处理流程：
//  1. 验证认证状态和写入权限
//  2. 解析工作区 ID 和请求体
//  3. 设置默认值（provider=claude, status=offline）
//  4. 调用 service 创建代理并生成 API Token
//  5. 记录审计日志
func (h *AgentHandler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	// 验证写入权限（member 及以上）
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
	var req createAgentRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 设置默认值
	provider := req.Provider
	if provider == "" {
		provider = AgentProviderClaude
	}

	status := req.Status
	if status == "" {
		status = AgentStatusOffline
	}

	// custom_env 直接以 json.RawMessage 透传给 service
	customEnv := req.CustomEnv

	extraArgs := req.ExtraArgs
	if extraArgs == nil {
		extraArgs = []string{}
	}

	gitName := req.GitName
	gitEmail := req.GitEmail

	if gitName == "" || gitEmail == "" {
		response.BadRequest(w, "git_name and git_email are required")
		return
	}

	agentSvc := service.NewAgentService(h.Svc)

	// 获取创建者的成员 ID，用于权限授予
	var grantedBy uuid.UUID
	if claims.UserID != uuid.Nil {
		grantedBy = claims.UserID
	}

	// 调用 service 创建代理
	result, err := agentSvc.Create(r.Context(), buildCreateAgentParams(
		workspaceID, req.Name, provider, req.Instructions, req.Model, status, customEnv, extraArgs, gitName, gitEmail,
	), grantedBy)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 返回创建结果（包含 API Token）
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, createAgentResponse{
		agentResponse: agentToResponseWithEnv(result.Agent),
		APIToken:      result.APIToken,
	})

	// 记录审计日志
	auditSvc := service.NewAuditService(h.Svc)
	if err := auditSvc.Log(r.Context(), service.AuditLogEntry{
		WorkspaceID:  workspaceID,
		ActorType:    claims.UserType,
		ActorID:      claims.UserID,
		Action:       "agent.create",
		ResourceType: "agent",
		ResourceID:   result.Agent.ID,
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	}); err != nil {
		slog.Warn("audit log write failed", "err", err)
	}
}

// ListAgents 处理 GET /workspaces/{workspaceId}/agents 端点，列出工作区下的所有 AI 代理。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回代理列表
//   - 400: 工作区 ID 无效
//   - 401: 未认证
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

	// 列表响应不包含 custom_env（防止密钥泄露）
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

	// 批量填充 Token 用量（从 token_usage 表实时聚合）
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

// GetAgent 处理 GET /workspaces/{workspaceId}/agents/{id} 端点，查询指定 AI 代理的详细信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回代理详情
//   - 400: 代理 ID 无效
//   - 401: 未认证
//   - 404: 代理不存在或不在当前工作区
func (h *AgentHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	agent := checkAgentWorkspace(h.Svc, w, r, id)
	if agent == nil {
		return
	}

	// 仅写权限用户可查看 custom_env
	claims, _ := svcmw.GetAuthFromContext(r.Context())
	if requireWriteAccess(claims) == nil {
		response.JSON(w, r, agentToResponseWithEnv(*agent))
	} else {
		response.JSON(w, r, agentToResponse(*agent))
	}
}


// UpdateAgent 处理 PUT /workspaces/{workspaceId}/agents/{id} 端点，更新 AI 代理的配置信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - instructions: string，代理执行指令
//   - model: string，使用模型
//   - status: string，代理状态
//   - custom_env: object，自定义环境变量
//   - extra_args: string[]，额外命令行参数
//   - git_name: string，Git 提交用户名
//   - git_email: string，Git 提交邮箱
//
// 响应：
//   - 200: 成功返回更新后的代理信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理不存在
func (h *AgentHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
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

	// 解析代理 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	if checkAgentWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// 解析请求体
	var req updateAgentRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// custom_env 直接以 json.RawMessage 透传给 service
	customEnv := req.CustomEnv

	extraArgs := req.ExtraArgs
	if extraArgs == nil {
		extraArgs = []string{}
	}

	// 调用 service 更新代理
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

// DeleteAgent 处理 DELETE /workspaces/{workspaceId}/agents/{id} 端点，删除指定的 AI 代理。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功删除
//   - 400: 代理 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理不存在
func (h *AgentHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
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

	// 解析代理 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	agent := checkAgentWorkspace(h.Svc, w, r, id)
	if agent == nil {
		return
	}

	// 调用 service 删除代理
	agentSvc := service.NewAgentService(h.Svc)
	if err := agentSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 记录审计日志
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

// ListSkills 处理 GET /workspaces/{workspaceId}/agents/{id}/skills 端点，列出 AI 代理已绑定的技能列表。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回技能列表
//   - 400: 代理 ID 无效
//   - 401: 未认证
//   - 404: 代理不存在
func (h *AgentHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
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

// ListMcpServers 处理 GET /workspaces/{workspaceId}/agents/{id}/mcp-servers 端点，列出 AI 代理已绑定的 MCP 服务器。
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

// GetExecutionMcpServers 处理 GET /workspaces/{workspaceId}/agents/{id}/execution/mcp-servers 端点，
// 返回 Agent 执行所需的 MCP 服务器列表（env_vars 已解密）。
//
// 鉴权：仅 Agent 自身可访问（user_type=agent 且 user_id 匹配路径 {id}），
// 人类用户和其他 Agent 访问返回 403。
// 该端点专供 Agent daemon 执行时使用，返回完整解密后的环境变量。
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

	// 强鉴权：仅 Agent 自身可访问
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



// AddSkill 处理 POST /workspaces/{workspaceId}/agents/{id}/skills 端点，为 AI 代理绑定一个技能。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - skill_id: UUID，技能 ID（必填）
//   - enabled: bool，是否启用
//
// 响应：
//   - 201: 成功绑定技能
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理不存在
func (h *AgentHandler) AddSkill(w http.ResponseWriter, r *http.Request) {
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

	// 解析代理 ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// 解析请求体
	var req addSkillRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if checkSkillWorkspace(h.Svc, w, r, req.SkillID) == nil {
		return
	}

	// 调用 service 绑定技能
	agentSvc := service.NewAgentService(h.Svc)
	skill, err := agentSvc.AddSkill(r.Context(), buildAddAgentSkillParams(agentID, req.SkillID, req.Enabled))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, skill)
}

// RemoveSkill 处理 DELETE /workspaces/{workspaceId}/agents/{id}/skills/{skillId} 端点，移除 AI 代理的指定技能。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功移除
//   - 400: 代理 ID 或技能 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理或技能不存在
func (h *AgentHandler) RemoveSkill(w http.ResponseWriter, r *http.Request) {
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

	// 解析代理 ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// 解析技能 ID
	skillID, err := uuid.Parse(chi.URLParam(r, "skillId"))
	if err != nil {
		response.BadRequest(w, "invalid skill id")
		return
	}

	// 调用 service 移除技能
	agentSvc := service.NewAgentService(h.Svc)
	if err := agentSvc.RemoveSkill(r.Context(), buildRemoveAgentSkillParams(agentID, skillID)); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


// AddMcpServer 处理 POST /workspaces/{workspaceId}/agents/{id}/mcp-servers 端点，为 AI 代理绑定 MCP 服务器。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - mcp_server_id: UUID，MCP 服务器 ID（必填）
//   - enabled: bool，是否启用
//
// 响应：
//   - 201: 成功绑定 MCP 服务器
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理不存在
func (h *AgentHandler) AddMcpServer(w http.ResponseWriter, r *http.Request) {
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

	// 解析代理 ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// 解析请求体
	var req addMcpServerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if checkMcpServerWorkspace(h.Svc, w, r, req.McpServerID) == nil {
		return
	}

	// 调用 service 绑定 MCP 服务器
	agentSvc := service.NewAgentService(h.Svc)
	server, err := agentSvc.AddMcpServer(r.Context(), buildAddAgentMcpServerParams(agentID, req.McpServerID, req.Enabled))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, server)
}

// RemoveMcpServer 处理 DELETE /workspaces/{workspaceId}/agents/{id}/mcp-servers/{serverId} 端点，移除 AI 代理绑定的 MCP 服务器。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功移除
//   - 400: 代理 ID 或服务器 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理或 MCP 服务器不存在
func (h *AgentHandler) RemoveMcpServer(w http.ResponseWriter, r *http.Request) {
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

	// 解析代理 ID
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid agent id")
		return
	}

	// 验证代理属于当前工作区
	if checkAgentWorkspace(h.Svc, w, r, agentID) == nil {
		return
	}

	// 解析服务器 ID
	serverID, err := uuid.Parse(chi.URLParam(r, "serverId"))
	if err != nil {
		response.BadRequest(w, "invalid server id")
		return
	}

	// 调用 service 移除 MCP 服务器
	agentSvc := service.NewAgentService(h.Svc)
	if err := agentSvc.RemoveMcpServer(r.Context(), buildRemoveAgentMcpServerParams(agentID, serverID)); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


// RotateAgentToken 处理 POST /workspaces/{workspaceId}/agents/{id}/rotate-token 端点，轮换 AI 代理的 API Token。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回新的 API Token
//   - 400: 代理 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 代理不存在
// --- Agent 权限管理 ---

// GetInProgressNodes 处理 GET /agents/{id}/in-progress-nodes 端点，
// 查询指定 Agent 认领但未完成（in_progress）的节点，用于 Agent 重启后恢复执行。
//
// 鉴权：仅 Agent 自身可查询（user_type=agent 且 user_id 匹配路径 {id}），
// 人类用户无权查询此端点。
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

	// 鉴权：仅 Agent 自身可查询
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
