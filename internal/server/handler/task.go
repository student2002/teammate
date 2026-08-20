// task.go 提供任务（Task）的 CRUD 管理、节点列表查询及 Git 分支更新等 HTTP API 端点。
//
// 任务由工作流模板定义，包含有序节点。创建任务时自动生成节点。

package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// TaskHandler 处理任务管理的 HTTP 请求，包括任务的 CRUD、节点查询及 Git 分支更新。
type TaskHandler struct {
	Svc *service.Service
}

// NewTaskHandler 创建 TaskHandler 实例。
func NewTaskHandler(svc *service.Service) *TaskHandler {
	return &TaskHandler{Svc: svc}
}

// Routes 返回任务的路由表。
func (h *TaskHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateTask)
	r.Get("/", h.ListTasks)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetTask)
		r.Put("/", h.UpdateTask)
		r.Delete("/", h.DeleteTask)
	})

	return r
}

// CreateTask 处理 POST /projects/{projectId}/tasks 端点，创建新任务并根据工作流模板生成有序节点。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - title: string，任务标题（必填）
//   - description: string，任务描述
//   - constraints: string，约束条件
//   - type: string，任务类型
//   - priority: string，优先级
//   - due_date: string，截止日期（RFC3339 或 YYYY-MM-DD 格式）
//   - labels: string[]，标签列表
//   - workflow_template_id: UUID，工作流模板 ID（必填）
//
// 响应：
//   - 201: 成功创建任务，返回任务和节点信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 项目不存在
//
// 处理流程：
//  1. 验证认证状态和写入权限
//  2. 验证项目和工作流模板属于当前工作区
//  3. 调用 service 创建任务并生成节点
//  4. 返回任务和节点信息
func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
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

	// 解析项目 ID
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// 解析请求体
	var req createTaskRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 验证工作流模板 ID 必填
	if req.WorkflowTemplateID == uuid.Nil {
		response.BadRequest(w, "workflow_template_id is required")
		return
	}

	taskSvc := service.NewTaskService(h.Svc)

	// 验证项目属于当前工作区
	project := checkProjectWorkspace(h.Svc, w, r, projectID)
	if project == nil {
		return
	}

	// 验证工作流模板属于当前工作区并获取模板名称
	template := checkWorkflowTemplateWorkspace(h.Svc, w, r, req.WorkflowTemplateID)
	if template == nil {
		return
	}

	// 设置默认优先级
	priority := req.Priority
	if priority == "" {
		priority = TaskPriorityMedium
	}

	// 设置默认任务类型
	taskType := req.Type
	if taskType == "" {
		taskType = TaskTypeTask
	}

	// 从认证信息派生作者身份（非请求体），防止伪造
	authorType := claims.UserType
	if authorType == "" {
		authorType = "human"
	}
	authorID := claims.UserID

	// 解析截止日期
	dueDate := service.ParseDueDate(req.DueDate)

	// 设置默认标签
	labels := req.Labels
	if labels == nil {
		labels = []string{}
	}

	// 调用 service 创建任务
	result, err := taskSvc.Create(r.Context(), projectID, buildCreateTaskParams(
		projectID, req.Title, req.Description, req.Constraints, taskType, priority,
		TaskStatusActive, authorType, authorID, dueDate, labels, template.Name,
	), req.WorkflowTemplateID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 返回任务和节点信息
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"task":  result.Task,
		"nodes": result.Nodes,
	})
}

// ListTasks 处理 GET /projects/{projectId}/tasks 端点，列出项目的任务，支持按状态过滤、分页和搜索。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 查询参数：
//   - status: string，按状态过滤（active/completed/cancelled/all）
//   - q: string，搜索关键词（匹配标题或描述）
//   - limit: int，每页数量（1-100，默认 50）
//   - offset: int，偏移量（默认 0）
//
// 行为：
//   - 当不传 limit/offset/q 参数时，保持向后兼容（ListTasks/ListAllTasks 已排除历史任务）
//   - 当传入 limit/offset/q 任一参数时，使用分页查询（不过滤历史任务），返回 PaginatedTaskResult
//
// 响应：
//   - 200: 成功返回任务列表（数组或分页结果）
//   - 400: 项目 ID 无效
//   - 401: 未认证
//   - 404: 项目不存在
func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	// 解析项目 ID
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// 验证项目属于当前工作区
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}

	// 获取查询参数
	statusFilter := r.URL.Query().Get("status")
	searchQuery := r.URL.Query().Get("q")
	limit := int32(50)
	offset := int32(0)

	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = int32(l)
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = int32(o)
	}

	taskSvc := service.NewTaskService(h.Svc)

	// 如果请求包含分页参数或搜索参数，使用分页查询（不过滤历史任务）
	hasPagination := r.URL.Query().Get("limit") != "" || r.URL.Query().Get("offset") != "" || searchQuery != ""

	if hasPagination {
		// 分页模式：空字符串表示不过滤状态
		var status string
		if statusFilter != "" && statusFilter != "all" {
			status = statusFilter
		}
		result, err := taskSvc.ListTasksPaginatedWithNodes(r.Context(), projectID, status, searchQuery, limit, offset)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		response.JSON(w, r, result)
		return
	}

	// 原有逻辑：无分页参数时保持向后兼容（ListTasks/ListAllTasks 已排除历史任务）
	if statusFilter == "all" {
		result, err := taskSvc.ListAllWithNodes(r.Context(), projectID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		response.JSON(w, r, result)
		return
	}

	// 构建查询参数
	status := TaskStatusActive
	if statusFilter != "" {
		status = TaskStatus(statusFilter)
	}
	params := buildListTasksParams(projectID, status)

	// 调用 service 查询任务
	result, err := taskSvc.ListWithNodes(r.Context(), params)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, result)
}

// GetTask 处理 GET /projects/{projectId}/tasks/{id} 端点，查询指定任务的详细信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回任务详情
//   - 400: 任务 ID 无效
//   - 401: 未认证
//   - 404: 任务不存在
func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	// 解析任务 ID
	taskIDStr := chi.URLParam(r, "id")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// 验证任务属于当前工作区
	task := checkTaskWorkspace(h.Svc, w, r, taskID)
	if task == nil {
		return
	}

	// 验证任务属于 URL 中的项目
	if err := checkTaskBelongsToProject(h.Svc, w, r, task); err != nil {
		return
	}

	// 返回任务详情
	response.JSON(w, r, taskToResponse(*task))
}

// UpdateTask 处理 PUT /projects/{projectId}/tasks/{id} 端点，更新任务信息，取消任务需要 admin/owner 权限。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - title: string，任务标题（最多 200 字符）
//   - description: string，任务描述（最多 50000 字符）
//   - priority: string，优先级
//   - labels: string[]，标签列表（最多 10 个）
//   - due_date: string，截止日期
//   - constraints: string，约束条件（最多 2000 字符）
//   - status: string，任务状态
//
// 响应：
//   - 200: 成功返回更新后的任务信息
//   - 400: 参数错误或任务状态不允许编辑
//   - 401: 未认证
//   - 403: 无权限（取消任务需要 admin/owner）
//   - 404: 任务不存在
func (h *TaskHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
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

	// 解析任务 ID
	taskIDStr := chi.URLParam(r, "id")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// 解析请求体
	var req updateTaskRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 验证字段长度
	if len(req.Title) > 200 {
		response.BadRequest(w, "title must be at most 200 characters")
		return
	}
	if len(req.Description) > 50000 {
		response.BadRequest(w, "description must be at most 50000 characters")
		return
	}
	if len(req.Constraints) > 2000 {
		response.BadRequest(w, "constraints must be at most 2000 characters")
		return
	}
	if len(req.Labels) > 10 {
		response.BadRequest(w, "at most 10 labels allowed")
		return
	}

	taskSvc := service.NewTaskService(h.Svc)

	// 验证任务属于当前工作区并检查状态
	existingTask := checkTaskWorkspace(h.Svc, w, r, taskID)
	if existingTask == nil {
		return
	}

	// 验证任务属于 URL 中的项目
	if err := checkTaskBelongsToProject(h.Svc, w, r, existingTask); err != nil {
		return
	}

	// 已完成或已取消的任务不允许编辑
	if existingTask.Status == TaskStatusCompleted || existingTask.Status == TaskStatusCancelled {
		response.BadRequest(w, fmt.Sprintf("cannot edit task in '%s' status", existingTask.Status))
		return
	}

	// 对于未提供的字段，使用现有值
	title := req.Title
	if title == "" {
		title = existingTask.Title
	}
	description := req.Description
	if description == "" {
		description = existingTask.Description
	}
	priority := req.Priority
	if priority == "" {
		priority = existingTask.Priority
	}
	labels := req.Labels
	if labels == nil {
		labels = existingTask.Labels
	}
	constraints := req.Constraints
	if constraints == "" {
		constraints = existingTask.Constraints
	}
	status := req.Status
	if status == "" {
		status = existingTask.Status
	}

	// 解析截止日期
	var dueDate sql.NullTime
	if req.DueDate != nil && *req.DueDate != "" {
		dueDate = service.ParseDueDate(req.DueDate)
	} else if existingTask.DueDate != nil {
		dueDate = sql.NullTime{Time: *existingTask.DueDate, Valid: true}
	}

	// 如果任务被取消，需要验证 admin/owner 权限
	if status == TaskStatusCancelled {
		userInfo, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok || (userInfo.Role != "owner" && userInfo.Role != "admin") {
			response.Forbidden(w, "only admin or owner can cancel tasks")
			return
		}
	}

	// 调用 service 更新任务
	task, err := taskSvc.Update(r.Context(), buildUpdateTaskParams(
		taskID, title, description, priority, labels, dueDate, constraints, status,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "task not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// 如果任务被取消，取消所有进行中的节点并通知 Agent
	if status == TaskStatusCancelled {
		// 先获取节点列表用于发送中断事件
		nodes, _ := taskSvc.ListTaskNodes(r.Context(), taskID)

		if err := taskSvc.CancelTaskNodes(r.Context(), taskID); err != nil {
			response.InternalServerError(w, err)
			return
		}

		// 向正在执行的 Agent 发送 task:interrupt 控制事件
		for _, node := range nodes {
			if node.Status == types.TaskNodeStatusInProgress && node.AssigneeID != nil {
				assigneeID, _ := uuid.Parse(*node.AssigneeID)
				h.Svc.PublishControlEvent(r.Context(), assigneeID, "task:interrupt", map[string]interface{}{
					"task_id": fmt.Sprintf("%d", taskID),
					"node_id": node.ID,
				})
			}
		}
	}

	// 返回更新后的任务
	response.JSON(w, r, taskToResponse(task))
}

// DeleteTask 处理 DELETE /projects/{projectId}/tasks/{id} 端点，删除指定任务及其关联的节点。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功删除
//   - 400: 任务 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 任务不存在
func (h *TaskHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
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

	// 解析任务 ID
	taskIDStr := chi.URLParam(r, "id")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// 验证任务属于当前工作区
	task := checkTaskWorkspace(h.Svc, w, r, taskID)
	if task == nil {
		return
	}

	// 验证任务属于 URL 中的项目
	if err := checkTaskBelongsToProject(h.Svc, w, r, task); err != nil {
		return
	}

	// 调用 service 删除任务
	taskSvc := service.NewTaskService(h.Svc)
	if err := taskSvc.Delete(r.Context(), taskID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// NewUpdateGitBranchHandler 处理 PUT /tasks/{taskId}/git-branch 端点，Agent 初始化 Git 工作目录后更新任务的 Git 分支信息。
func NewUpdateGitBranchHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 获取认证信息
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "authentication required")
			return
		}
		// 仅 Agent 可更新 Git 分支
		if claims.UserType != "agent" {
			response.Forbidden(w, "only agents can update git branch")
			return
		}

		// 解析任务 ID
		taskIDStr := chi.URLParam(r, "taskId")
		taskID, err := strconv.Atoi(taskIDStr)
		if err != nil {
			response.BadRequest(w, "invalid task id")
			return
		}

		// 解析请求体
		var req struct {
			GitBranch string `json:"git_branch"` // Git 分支名称
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "invalid request body")
			return
		}
		// 验证分支名称
		if req.GitBranch == "" {
			response.BadRequest(w, "git_branch is required")
			return
		}
		if len(req.GitBranch) > 200 {
			response.BadRequest(w, "git_branch too long (max 200 characters)")
			return
		}

		// 调用 service 更新 Git 分支
		taskSvc := service.NewTaskService(svc)
		if err := taskSvc.UpdateTaskGitBranch(r.Context(), int32(taskID), req.GitBranch); err != nil {
			response.InternalServerError(w, err)
			return
		}

		// 返回成功
		response.JSON(w, r, map[string]string{"status": "ok"})
	}
}
