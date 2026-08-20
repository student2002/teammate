// helpers.go 提供 handler 层的通用辅助函数，用于工作区归属校验、权限检查等跨资源操作。
//
// 所有 check*Workspace 函数在验证失败时会直接写入 HTTP 错误响应并返回 nil，
// 调用方可通过返回值判断是否继续处理。

package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// checkProjectWorkspace 验证项目是否属于当前认证用户的工作区。
// 成功时返回项目记录，失败时写入错误响应并返回 nil。
func checkProjectWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, projectID uuid.UUID) *types.Project {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询项目
	project, err := service.NewProjectService(svc).Get(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "project not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证项目属于当前工作区
	if project.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "project not found")
		return nil
	}

	return &project
}

// checkTaskWorkspace 验证任务是否属于当前认证用户工作区内的某个项目。
// 成功时返回任务记录，失败时写入错误响应并返回 nil。
func checkTaskWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, taskID int32) *Task {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询任务
	taskSvc := service.NewTaskService(svc)
	task, err := taskSvc.Get(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "task not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证任务所属项目属于当前工作区
	taskProjectID, _ := uuid.Parse(task.ProjectID)
	project, err := service.NewProjectService(svc).Get(r.Context(), taskProjectID)
	if err != nil || project.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "task not found")
		return nil
	}

	return &task
}

// checkNodeWorkspace 验证节点是否属于当前认证用户工作区内的任务，同时校验节点的 TaskID 与 URL 参数一致。
// 成功时返回节点记录，失败时写入错误响应并返回 nil。
func checkNodeWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) *types.TaskNode {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询节点
	node, err := service.NewNodeService(svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证节点的 TaskID 与 URL 中的 taskId 匹配
	taskIDStr := chi.URLParam(r, "taskId")
	if taskIDStr != "" {
		var urlTaskID int32
		if _, err := fmt.Sscanf(taskIDStr, "%d", &urlTaskID); err == nil && node.TaskID != urlTaskID {
			response.NotFound(w, "node not found")
			return nil
		}
	}

	// 验证节点 → 任务 → 项目 → 工作区 归属链
	taskSvc := service.NewTaskService(svc)
	task, err := taskSvc.Get(r.Context(), node.TaskID)
	if err != nil {
		response.NotFound(w, "node not found")
		return nil
	}

	taskProjectID, _ := uuid.Parse(task.ProjectID)
	project, err := service.NewProjectService(svc).Get(r.Context(), taskProjectID)
	if err != nil || project.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "node not found")
		return nil
	}

	return &node
}

// checkWorkflowTemplateWorkspace 验证工作流模板是否属于当前认证用户的工作区。
// 成功时返回模板记录，失败时写入错误响应并返回 nil。
func checkWorkflowTemplateWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, templateID uuid.UUID) *WorkflowTemplate {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询工作流模板
	template, err := service.NewWorkflowService(svc).GetTemplate(r.Context(), templateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "workflow template not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证模板属于当前工作区
	if template.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "workflow template not found")
		return nil
	}

	return &template
}

// requireWriteAccess 检查当前认证用户是否具有写入权限。
// 仅允许角色为 member 及以上的人类用户，Agent 必须使用专门的权限路由。
func requireWriteAccess(claims svcmw.AuthClaims) error {
	// Agent 不允许直接使用写入路由，需通过专门的权限路由
	if claims.UserType == "agent" {
		return fmt.Errorf("agents must use agent-specific permission routes")
	}
	// 人类用户需要当前工作区 member 及以上角色
	if types.MemberRoleLevel(claims.Role) < 2 {
		return fmt.Errorf("insufficient permissions: member role or higher required")
	}
	return nil
}

// checkAgentWorkspace 验证 Agent 是否属于当前认证用户的工作区。
// 成功时返回 Agent 记录，失败时写入错误响应并返回 nil。
func checkAgentWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, agentID uuid.UUID) *types.Agent {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询 Agent
	agentSvc := service.NewAgentService(svc)
	agent, err := agentSvc.Get(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "agent not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证 Agent 属于当前工作区
	if agent.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "agent not found")
		return nil
	}

	return &agent
}

// checkSkillWorkspace 验证技能是否属于当前认证用户的工作区。
// 成功时返回技能记录，失败时写入错误响应并返回 nil。
func checkSkillWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, skillID uuid.UUID) *Skill {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询技能
	skillSvc := service.NewSkillService(svc)
	skill, err := skillSvc.Get(r.Context(), skillID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "skill not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证技能属于当前工作区
	if skill.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "skill not found")
		return nil
	}

	return &skill
}

// checkMcpServerWorkspace 验证 MCP 服务器是否属于当前认证用户的工作区。
// 成功时返回 MCP 服务器记录，失败时写入错误响应并返回 nil。
func checkMcpServerWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, serverID uuid.UUID) *types.McpServer {
	// 获取当前资源的工作区授权上下文。
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// 查询 MCP 服务器
	mcpSvc := service.NewMcpService(svc)
	server, err := mcpSvc.Get(r.Context(), serverID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "mcp server not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证 MCP 服务器属于当前工作区
	if server.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "mcp server not found")
		return nil
	}

	return &server
}

// checkTaskBelongsToProject 验证任务的 ProjectID 是否与 URL 中的 projectId 参数匹配。
// 成功时返回 nil，失败时写入错误响应并返回错误。
func checkTaskBelongsToProject(svc *service.Service, w http.ResponseWriter, r *http.Request, task *Task) error {
	// 如果不在项目路由下，跳过验证
	projectIDStr := chi.URLParam(r, "projectId")
	if projectIDStr == "" {
		return nil
	}

	// 解析并验证项目 ID
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return fmt.Errorf("invalid project id")
	}

	// 验证任务属于该项目
	if task.ProjectID != projectID.String() {
		response.NotFound(w, "task not found")
		return fmt.Errorf("task does not belong to this project")
	}
	return nil
}
