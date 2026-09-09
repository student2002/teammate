// helpers.go provides common helper functions for the handler layer, used for workspace ownership validation, permission checks, and other cross-resource operations.
//
// All check*Workspace functions write an HTTP error response directly and return nil on validation failure;
// callers can use the return value to decide whether to continue processing.

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

// checkProjectWorkspace verifies that the project belongs to the current authenticated user's workspace.
// On success returns the project record; on failure writes an error response and returns nil.
func checkProjectWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, projectID uuid.UUID) *types.Project {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the project
	project, err := service.NewProjectService(svc).Get(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "project not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// verify the project belongs to the current workspace
	if project.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "project not found")
		return nil
	}

	return &project
}

// checkTaskWorkspace verifies that the task belongs to a project within the current authenticated user's workspace.
// On success returns the task record; on failure writes an error response and returns nil.
func checkTaskWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, taskID int32) *Task {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the task
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

	// verify the task's project belongs to the current workspace
	taskProjectID, _ := uuid.Parse(task.ProjectID)
	project, err := service.NewProjectService(svc).Get(r.Context(), taskProjectID)
	if err != nil || project.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "task not found")
		return nil
	}

	return &task
}

// checkNodeWorkspace verifies that the node belongs to a task within the current authenticated user's workspace, and also validates that the node's TaskID matches the URL parameter.
// On success returns the node record; on failure writes an error response and returns nil.
func checkNodeWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) *types.TaskNode {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the node
	node, err := service.NewNodeService(svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// verify the node's TaskID matches the taskId in the URL
	taskIDStr := chi.URLParam(r, "taskId")
	if taskIDStr != "" {
		var urlTaskID int32
		if _, err := fmt.Sscanf(taskIDStr, "%d", &urlTaskID); err == nil && node.TaskID != urlTaskID {
			response.NotFound(w, "node not found")
			return nil
		}
	}

	// verify the node -> task -> project -> workspace ownership chain
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

// checkWorkflowTemplateWorkspace verifies that the workflow template belongs to the current authenticated user's workspace.
// On success returns the template record; on failure writes an error response and returns nil.
func checkWorkflowTemplateWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, templateID uuid.UUID) *WorkflowTemplate {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the workflow template
	template, err := service.NewWorkflowService(svc).GetTemplate(r.Context(), templateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "workflow template not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// verify the template belongs to the current workspace
	if template.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "workflow template not found")
		return nil
	}

	return &template
}

// requireWriteAccess checks whether the current authenticated user has write permission.
// Only allows human users with role member or higher; Agents must use dedicated permission routes.
func requireWriteAccess(claims svcmw.AuthClaims) error {
	// Agents are not allowed to use write routes directly; they must go through dedicated permission routes
	if claims.UserType == "agent" {
		return fmt.Errorf("agents must use agent-specific permission routes")
	}
	// human users need a role of member or higher in the current workspace
	if types.MemberRoleLevel(claims.Role) < 2 {
		return fmt.Errorf("insufficient permissions: member role or higher required")
	}
	return nil
}

// checkAgentWorkspace verifies that the Agent belongs to the current authenticated user's workspace.
// On success returns the Agent record; on failure writes an error response and returns nil.
func checkAgentWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, agentID uuid.UUID) *types.Agent {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the Agent
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

	// verify the Agent belongs to the current workspace
	if agent.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "agent not found")
		return nil
	}

	return &agent
}

// checkSkillWorkspace verifies that the skill belongs to the current authenticated user's workspace.
// On success returns the skill record; on failure writes an error response and returns nil.
func checkSkillWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, skillID uuid.UUID) *Skill {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the skill
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

	// verify the skill belongs to the current workspace
	if skill.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "skill not found")
		return nil
	}

	return &skill
}

// checkMcpServerWorkspace verifies that the MCP server belongs to the current authenticated user's workspace.
// On success returns the MCP server record; on failure writes an error response and returns nil.
func checkMcpServerWorkspace(svc *service.Service, w http.ResponseWriter, r *http.Request, serverID uuid.UUID) *types.McpServer {
	// get the workspace authorization context for the current resource.
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return nil
	}

	// query the MCP server
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

	// verify the MCP server belongs to the current workspace
	if server.WorkspaceID != ws.WorkspaceID.String() {
		response.NotFound(w, "mcp server not found")
		return nil
	}

	return &server
}

// checkTaskBelongsToProject verifies that the task's ProjectID matches the projectId parameter in the URL.
// On success returns nil; on failure writes an error response and returns an error.
func checkTaskBelongsToProject(svc *service.Service, w http.ResponseWriter, r *http.Request, task *Task) error {
	// if not under a project route, skip validation
	projectIDStr := chi.URLParam(r, "projectId")
	if projectIDStr == "" {
		return nil
	}

	// parse and validate the project ID
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return fmt.Errorf("invalid project id")
	}

	// verify the task belongs to that project
	if task.ProjectID != projectID.String() {
		response.NotFound(w, "task not found")
		return fmt.Errorf("task does not belong to this project")
	}
	return nil
}
