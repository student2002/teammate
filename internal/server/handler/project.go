// project.go 提供项目的 CRUD 管理、成员/审查者管理及 Git 凭据管理等 HTTP API 端点。
//
// 项目是任务的容器，支持 Git 仓库集成。
// 项目角色：lead（项目负责人）、developer（开发者）、reviewer（审查者）。

package handler

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/crypto"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// ProjectHandler 处理项目管理的 HTTP 请求，包括项目的 CRUD、成员和审查者管理，以及 Git 凭据管理。
type ProjectHandler struct {
	Svc *service.Service
}

// NewProjectHandler 创建 ProjectHandler 实例。
func NewProjectHandler(svc *service.Service) *ProjectHandler {
	return &ProjectHandler{Svc: svc}
}

// checkProjectRole 验证当前认证用户是否具有指定的项目级角色权限。
// 成功时返回项目 ID，失败时写入错误响应并返回 uuid.Nil。
func checkProjectRole(svc *service.Service, w http.ResponseWriter, r *http.Request, requiredRole string) uuid.UUID {
	return checkProjectRoleByParam(svc, w, r, "id", requiredRole)
}

// checkProjectRoleByParam 与 checkProjectRole 功能相同，但从指定的 URL 参数名读取项目 ID。
func checkProjectRoleByParam(svc *service.Service, w http.ResponseWriter, r *http.Request, param string, requiredRole string) uuid.UUID {
	// 解析项目 ID
	projectID, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return uuid.Nil
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return uuid.Nil
	}

	// Agent 不允许管理项目设置
	if claims.UserType == "agent" {
		response.Forbidden(w, "agents cannot manage project settings")
		return uuid.Nil
	}

	// 验证项目属于当前工作区
	if checkProjectWorkspace(svc, w, r, projectID) == nil {
		return uuid.Nil
	}

	// 检查项目角色权限
	projSvc := service.NewProjectService(svc)
	if err := projSvc.CheckMemberProjectAccess(r.Context(), claims.UserID, projectID, claims.Role, requiredRole); err != nil {
		response.Forbidden(w, err.Error())
		return uuid.Nil
	}

	return projectID
}

// Routes 返回项目的完整路由表（包含读写操作）。
func (h *ProjectHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateProject)
	r.Get("/", h.ListProjects)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetProject)
		r.Put("/", h.UpdateProject)
		r.Delete("/", h.DeleteProject)
		r.Post("/members", h.AddProjectMember)
		r.Get("/members", h.ListProjectMembers)
		r.Delete("/members/{memberId}", h.RemoveProjectMember)
		r.Post("/reviewers", h.AddProjectReviewer)
		r.Get("/reviewers", h.ListProjectReviewers)
		r.Delete("/reviewers/{reviewerId}", h.RemoveProjectReviewer)
	})

	return r
}

// ReadRoutes 返回项目的只读路由表。
func (h *ProjectHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListProjects)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetProject)
		r.Get("/members", h.ListProjectMembers)
		r.Get("/reviewers", h.ListProjectReviewers)
	})

	return r
}

// WriteRoutes 返回项目的写入路由表。
func (h *ProjectHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateProject)
	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateProject)
		r.Delete("/", h.DeleteProject)
		r.Post("/members", h.AddProjectMember)
		r.Delete("/members/{memberId}", h.RemoveProjectMember)
		r.Post("/reviewers", h.AddProjectReviewer)
		r.Delete("/reviewers/{reviewerId}", h.RemoveProjectReviewer)
	})

	return r
}

// CreateProject 处理 POST /workspaces/{workspaceId}/projects 端点，创建新项目并将创建者自动设为项目负责人。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，项目名称（必填）
//   - description: string，项目描述
//   - icon: string，项目图标
//   - status: string，项目状态，默认 "planned"
//   - repo_url: string，Git 仓库 URL
//   - context: string，项目上下文
//
// 响应：
//   - 201: 成功创建项目
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
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
	var req createProjectRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 项目名称为必填项
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(w, "name is required")
		return
	}

	// Git 仓库地址为必填项
	if req.RepoUrl == "" {
		response.BadRequest(w, "repo_url is required")
		return
	}

	// 设置默认状态
	status := req.Status
	if status == "" {
		status = ProjectStatusPlanned
	}

	// 调用 service 创建项目
	projSvc := service.NewProjectService(h.Svc)
	project, err := projSvc.Create(r.Context(), buildCreateProjectParams(
		workspaceID,
		req.Name,
		req.Description,
		req.Icon,
		status,
		req.RepoUrl,
		req.Context,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 自动将创建者添加为项目负责人
	if claims, ok := svcmw.GetAuthFromContext(r.Context()); ok && claims.UserType == "member" {
		projSvc2 := service.NewProjectService(h.Svc)
		projectUUID, _ := uuid.Parse(project.ID)
		_, _ = projSvc2.AddMember(r.Context(), buildCreateProjectMemberParams(
			projectUUID,
			"human",
			uuid.NullUUID{},
			uuid.NullUUID{UUID: claims.UserID, Valid: true},
			"lead",
		))
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, project)
}

// ListProjects 处理 GET /workspaces/{workspaceId}/projects 端点，列出工作区下的项目，Agent 仅可见自己参与的项目。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回项目列表
//   - 400: 工作区 ID 无效
//   - 401: 未认证
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	projSvc := service.NewProjectService(h.Svc)

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	var projects []Project
	if claims.UserType == "agent" {
		// Agent 仅可见自己参与的项目
		projects, err = projSvc.ListByAgentMembership(r.Context(), workspaceID, claims.UserID)
	} else {
		// 人类用户可见所有项目
		projects, err = projSvc.List(r.Context(), workspaceID)
	}
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, projects)
}

// GetProject 处理 GET /workspaces/{workspaceId}/projects/{id} 端点，查询指定项目的详细信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回项目详情
//   - 400: 项目 ID 无效
//   - 401: 未认证
//   - 404: 项目不存在
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// 验证项目属于当前工作区
	project := checkProjectWorkspace(h.Svc, w, r, id)
	if project == nil {
		return
	}

	response.JSON(w, r, project)
}

// UpdateProject 处理 PUT /workspaces/{workspaceId}/projects/{id} 端点，更新项目配置，需要 lead 角色权限。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，项目名称
//   - description: string，项目描述
//   - status: string，项目状态
//   - repo_url: string，Git 仓库 URL
//   - context: string，项目上下文
//
// 响应：
//   - 200: 成功返回更新后的项目信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无 lead 角色权限
//   - 404: 项目不存在
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	// 验证项目角色权限
	id := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if id == uuid.Nil {
		return
	}

	// 解析请求体
	var req updateProjectRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 项目名称为必填项
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(w, "name is required")
		return
	}

	// 如果未提供状态，获取当前项目以保留现有状态
	status := req.Status
	if status == "" {
		projSvc := service.NewProjectService(h.Svc)
		existing, err := projSvc.Get(r.Context(), id)
		if err != nil {
			response.NotFound(w, "project not found")
			return
		}
		status = existing.Status
	}

	// 调用 service 更新项目
	projSvc := service.NewProjectService(h.Svc)
	project, err := projSvc.Update(r.Context(), buildUpdateProjectParams(
		id,
		req.Name,
		req.Description,
		status,
		req.RepoUrl,
		req.Context,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "project not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, project)
}

// DeleteProject 处理 DELETE /workspaces/{workspaceId}/projects/{id} 端点，删除指定项目，需要 lead 角色权限。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功删除
//   - 401: 未认证
//   - 403: 无 lead 角色权限
//   - 404: 项目不存在
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	// 验证项目角色权限
	id := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if id == uuid.Nil {
		return
	}

	// 调用 service 删除项目
	projSvc := service.NewProjectService(h.Svc)
	if err := projSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddProjectMember 处理 POST /workspaces/{workspaceId}/projects/{id}/members 端点，为项目添加成员（人类或 Agent）。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - member_type: string，成员类型（"human" 或 "agent"）
//   - agent_id: UUID，Agent ID（agent 类型时必填）
//   - member_id: UUID，人类用户 ID（human 类型时必填）
//   - role: string，项目角色（lead/developer/reviewer）
//
// 响应：
//   - 201: 成功添加成员
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无 lead 角色权限
func (h *ProjectHandler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	// 验证项目角色权限
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// 解析请求体
	var req addProjectMemberRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 转换可选的 ID 字段
	var agentID uuid.NullUUID
	if req.AgentID != nil {
		agentID = uuid.NullUUID{UUID: *req.AgentID, Valid: true}
	}
	var memberID uuid.NullUUID
	if req.MemberID != nil {
		memberID = uuid.NullUUID{UUID: *req.MemberID, Valid: true}
	}

	// 调用 service 添加成员
	projSvc := service.NewProjectService(h.Svc)
	member, err := projSvc.AddMember(r.Context(), buildCreateProjectMemberParams(
		projectID,
		req.MemberType,
		agentID,
		memberID,
		req.Role,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, member)
}

// ListProjectMembers 处理 GET /workspaces/{workspaceId}/projects/{id}/members 端点，列出项目的成员列表。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回成员列表
//   - 400: 项目 ID 无效
//   - 401: 未认证
//   - 404: 项目不存在
func (h *ProjectHandler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// 验证项目属于当前工作区
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}

	// 调用 service 查询成员
	projSvc := service.NewProjectService(h.Svc)
	members, err := projSvc.ListMembers(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, members)
}

// RemoveProjectMember 处理 DELETE /workspaces/{workspaceId}/projects/{id}/members/{memberId} 端点，移除项目成员。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功移除
//   - 400: 成员 ID 无效
//   - 401: 未认证
//   - 403: 无 lead 角色权限
//   - 404: 成员不存在
func (h *ProjectHandler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	// 验证项目角色权限
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// 解析成员 ID
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		response.BadRequest(w, "invalid member id")
		return
	}

	// 验证成员记录属于该项目
	projSvc := service.NewProjectService(h.Svc)
	member, err := projSvc.GetProjectMember(r.Context(), memberID)
	if err != nil {
		response.NotFound(w, "project member not found")
		return
	}
	if member.ProjectID != projectID.String() {
		response.NotFound(w, "project member not found")
		return
	}

	// 调用 service 移除成员
	if err := projSvc.RemoveMember(r.Context(), memberID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddProjectReviewer 处理 POST /workspaces/{workspaceId}/projects/{id}/reviewers 端点，为项目添加审查者。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - member_type: string，成员类型（"human" 或 "agent"）
//   - agent_id: UUID，Agent ID（agent 类型时必填）
//   - member_id: UUID，人类用户 ID（human 类型时必填）
//
// 响应：
//   - 201: 成功添加审查者
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无 lead 角色权限
func (h *ProjectHandler) AddProjectReviewer(w http.ResponseWriter, r *http.Request) {
	// 验证项目角色权限
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// 解析请求体
	var req addProjectReviewerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 转换可选的 ID 字段
	var agentID uuid.NullUUID
	if req.AgentID != nil {
		agentID = uuid.NullUUID{UUID: *req.AgentID, Valid: true}
	}
	var memberID uuid.NullUUID
	if req.MemberID != nil {
		memberID = uuid.NullUUID{UUID: *req.MemberID, Valid: true}
	}

	// 调用 service 添加审查者
	projSvc := service.NewProjectService(h.Svc)
	reviewer, err := projSvc.AddReviewer(r.Context(), buildCreateProjectReviewerParams(
		projectID,
		req.MemberType,
		agentID,
		memberID,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, reviewer)
}

// ListProjectReviewers 处理 GET /workspaces/{workspaceId}/projects/{id}/reviewers 端点，列出项目的审查者列表。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回审查者列表
//   - 400: 项目 ID 无效
//   - 401: 未认证
//   - 404: 项目不存在
func (h *ProjectHandler) ListProjectReviewers(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// 验证项目属于当前工作区
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}

	// 调用 service 查询审查者
	projSvc := service.NewProjectService(h.Svc)
	reviewers, err := projSvc.ListReviewers(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, reviewers)
}

// RemoveProjectReviewer 处理 DELETE /workspaces/{workspaceId}/projects/{id}/reviewers/{reviewerId} 端点，移除项目审查者。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功移除
//   - 400: 审查者 ID 无效
//   - 401: 未认证
//   - 403: 无 lead 角色权限
//   - 404: 审查者不存在
func (h *ProjectHandler) RemoveProjectReviewer(w http.ResponseWriter, r *http.Request) {
	// 验证项目角色权限
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// 解析审查者 ID
	reviewerID, err := uuid.Parse(chi.URLParam(r, "reviewerId"))
	if err != nil {
		response.BadRequest(w, "invalid reviewer id")
		return
	}

	// 验证审查者记录属于该项目
	reviewer, err := service.NewProjectService(h.Svc).GetProjectReviewerByReviewerID(r.Context(), reviewerID)
	if err != nil {
		response.NotFound(w, "project reviewer not found")
		return
	}
	if reviewer.ProjectID != projectID.String() {
		response.NotFound(w, "project reviewer not found")
		return
	}

	// 调用 service 移除审查者
	projSvc := service.NewProjectService(h.Svc)
	if err := projSvc.RemoveReviewer(r.Context(), reviewerID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Git 凭据管理 ---

// maskPAT 返回脱敏的 PAT，仅显示最后 4 个字符。
func maskPAT(pat string) string {
	if len(pat) <= 4 {
		return "****"
	}
	return "****" + pat[len(pat)-4:]
}

// isFineGrainedPAT 检查 PAT 是否为细粒度/仓库范围的 Token。
// GitHub 细粒度 PAT 以 "github_pat_" 开头。
// GitHub 经典 PAT 以 "ghp_" 开头。
// GitLab 项目访问 Token 以 "glpts-" 开头（仓库范围）。
// GitLab 个人访问 Token 以 "glpat-" 开头（账户范围）。
func isFineGrainedPAT(pat string, patType string) bool {
	// 显式类型声明优先
	if patType == "fine_grained" {
		return true
	}
	if patType == "classic" {
		return false
	}
	// 从 Token 前缀自动检测
	prefix := strings.ToLower(pat)
	return strings.HasPrefix(prefix, "github_pat_") || strings.HasPrefix(prefix, "glpts-")
}

// GitCredentialsHandler 处理 GET /projects/{projectId}/git-credentials 端点，返回项目的 Git 凭据。
// Agent 获取加密版，人类获取脱敏版。
func GitCredentialsHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 解析项目 ID
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			response.BadRequest(w, "invalid project id")
			return
		}

		// 获取认证信息
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}

		// Agent 权限验证
		if claims.UserType == "agent" {
			// Agent 必须是项目的显式成员
			projSvc := service.NewProjectService(svc)
			if err := projSvc.CheckAgentProjectAccess(r.Context(), claims.UserID, projectID); err != nil {
				response.Forbidden(w, err.Error())
				return
			}
			// Agent 必须具有 git:push 权限才能读取凭据
			permSvc := service.NewAgentPermissionService(svc)
			has, err := permSvc.HasPermission(r.Context(), claims.UserID, types.PermGitPush)
			if err != nil || !has {
				response.Forbidden(w, "agent lacks git:push permission")
				return
			}
		}

		// 查询 Git 凭据
		authSvc := service.NewAuthService(svc, "")
		credentials, err := authSvc.GetGitCredentialsByProject(r.Context(), projectID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		// Agent 路径：解密 AES 加密的 PAT，然后用 Agent 的公钥重新加密
		if claims.UserType == "agent" {
			agentID := claims.UserID

			// 获取 Agent 的最新公钥
			publicKeyPEM, err := authSvc.GetLatestPublicKeyForAgent(r.Context(), agentID)
			if err != nil {
				response.NotFound(w, fmt.Sprintf("no public key found for agent: %v", err))
				return
			}

			// 解析公钥
			pubKey, err := crypto.ParsePublicKey([]byte(publicKeyPEM))
			if err != nil {
				response.InternalServerError(w, err)
				return
			}


			// 处理每个凭据
			result := make([]encryptedCredential, 0, len(credentials))
			for _, cred := range credentials {
				// 首先解密存储的 AES 加密 PAT
				plainPAT, err := crypto.DecryptPAT(cred.EncryptedPAT)
				if err != nil {
					response.InternalServerError(w, err)
					return
				}

				// 然后用 Agent 的 RSA 公钥重新加密
				encrypted, err := crypto.EncryptWithPublicKey(pubKey, []byte(plainPAT))
				if err != nil {
					response.InternalServerError(w, err)
					return
				}

				credID, _ := uuid.Parse(cred.ID)
				result = append(result, encryptedCredential{
					ID:           credID,
					RepoUrl:      cred.RepoURL,
					Username:     cred.Username,
					EncryptedPAT: base64.StdEncoding.EncodeToString(encrypted),
				})
			}

			// 返回加密凭据
			response.JSON(w, r, map[string]interface{}{
				"credentials": result,
			})
			return
		}

		// 人类用户路径：解密 PAT 用于脱敏显示

		// 处理每个凭据
		result := make([]maskedCredential, 0, len(credentials))
		for _, cred := range credentials {
			var createdBy *uuid.UUID
			if cred.CreatedBy != nil {
				if cb, err := uuid.Parse(*cred.CreatedBy); err == nil {
					createdBy = &cb
				}
			}
			// 解密 AES 加密的 PAT 用于脱敏
			plainPAT, err := crypto.DecryptPAT(cred.EncryptedPAT)
			if err != nil {
				response.InternalServerError(w, err)
				return
			}
			credID, _ := uuid.Parse(cred.ID)
			result = append(result, maskedCredential{
				ID:        credID,
				RepoUrl:   cred.RepoURL,
				Username:  cred.Username,
				MaskedPAT: maskPAT(plainPAT),
				CreatedBy: createdBy,
				CreatedAt: cred.CreatedAt,
				UpdatedAt: cred.UpdatedAt,
			})
		}

		// 返回脱敏凭据
		response.JSON(w, r, map[string]interface{}{
			"credentials": result,
		})
	}
}

// CreateGitCredentialHandler 处理 POST /projects/{projectId}/git-credentials 端点，创建项目的 Git 凭据，PAT 使用 AES-256-GCM 加密存储。
func CreateGitCredentialHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 验证项目角色权限
		projectID := checkProjectRoleByParam(svc, w, r, "projectId", types.ProjectRoleDeveloper)
		if projectID == uuid.Nil {
			return
		}

		// 获取认证信息
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}

		// Agent 不允许创建 Git 凭据
		if claims.UserType == "agent" {
			response.Forbidden(w, "agents cannot create git credentials")
			return
		}

		// 解析请求体
		var req createGitCredentialRequest
		if err := render.Decode(r, &req); err != nil {
			response.BadRequest(w, err.Error())
			return
		}

		// 验证必填字段
		if req.RepoUrl == "" {
			response.BadRequest(w, "repo_url is required")
			return
		}
		if req.PAT == "" {
			response.BadRequest(w, "pat is required")
			return
		}

		// 验证 PAT 范围：如果使用经典（账户范围）Token 则发出警告
		if !isFineGrainedPAT(req.PAT, req.PATType) {
			log.Printf("[git-credentials] WARNING: project %s is using a classic/broad-scope PAT for %s — "+
				"this token can access ALL repositories. Consider using a fine-grained/project-scoped token instead.",
				projectID, req.RepoUrl)
		}

		// 设置默认用户名
		username := req.Username
		if username == "" {
			username = "git"
		}

		// 使用 AES-256-GCM 加密 PAT
		encryptedPAT, err := crypto.EncryptPAT(req.PAT)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		// 调用 service 创建凭据
		authSvc := service.NewAuthService(svc, "")
		credential, err := authSvc.CreateGitCredential(r.Context(), buildCreateGitCredentialParams(
			projectID,
			req.RepoUrl,
			username,
			encryptedPAT,
			uuid.NullUUID{UUID: claims.UserID, Valid: true},
		))
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		// 准备范围警告
		scopeWarning := ""
		if !isFineGrainedPAT(req.PAT, req.PATType) {
			scopeWarning = "This token has account-wide access. For better security, use a fine-grained/project-scoped token that only has access to this repository."
		}


		var createdBy *uuid.UUID
		if credential.CreatedBy != nil {
			if cb, err := uuid.Parse(*credential.CreatedBy); err == nil {
				createdBy = &cb
			}
		}

		// 解密 PAT 用于脱敏显示
		plainPAT, _ := crypto.DecryptPAT(credential.EncryptedPAT)

		// 返回创建结果
		w.WriteHeader(http.StatusCreated)
		credID, _ := uuid.Parse(credential.ID)
		credPID, _ := uuid.Parse(credential.ProjectID)
		response.JSON(w, r, credentialResponse{
			ID:           credID,
			ProjectID:    credPID,
			RepoUrl:      credential.RepoURL,
			Username:     credential.Username,
			MaskedPAT:    maskPAT(plainPAT),
			ScopeWarning: scopeWarning,
			CreatedBy:    createdBy,
			CreatedAt:    credential.CreatedAt,
			UpdatedAt:    credential.UpdatedAt,
		})
	}
}

// UpdateGitCredentialHandler 处理 PUT /projects/{projectId}/git-credentials/{credentialId} 端点，更新项目的 Git 凭据。
func UpdateGitCredentialHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 验证项目角色权限
		projectID := checkProjectRoleByParam(svc, w, r, "projectId", types.ProjectRoleDeveloper)
		if projectID == uuid.Nil {
			return
		}

		// 获取认证信息
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}

		// Agent 不允许更新 Git 凭据
		if claims.UserType == "agent" {
			response.Forbidden(w, "agents cannot update git credentials")
			return
		}

		// 解析凭据 ID
		credentialID, err := uuid.Parse(chi.URLParam(r, "credentialId"))
		if err != nil {
			response.BadRequest(w, "invalid credential id")
			return
		}

		// 验证凭据属于该项目
		authSvc := service.NewAuthService(svc, "")
		existing, err := authSvc.GetGitCredential(r.Context(), credentialID)
		if err != nil {
			response.NotFound(w, "git credential not found")
			return
		}
		if existing.ProjectID != projectID.String() {
			response.NotFound(w, "git credential not found")
			return
		}

		// 解析请求体
		var req updateGitCredentialRequest
		if err := render.Decode(r, &req); err != nil {
			response.BadRequest(w, err.Error())
			return
		}

		// 使用现有值作为默认值
		repoUrl := existing.RepoURL
		if req.RepoUrl != "" {
			repoUrl = req.RepoUrl
		}
		username := existing.Username
		if req.Username != "" {
			username = req.Username
		}
		encryptedPAT := existing.EncryptedPAT
		if req.PAT != "" {
			// 使用 AES-256-GCM 加密新的 PAT
			var err error
			encryptedPAT, err = crypto.EncryptPAT(req.PAT)
			if err != nil {
				response.InternalServerError(w, err)
				return
			}
		}

		// 调用 service 更新凭据
		credential, err := authSvc.UpdateGitCredential(r.Context(), buildUpdateGitCredentialParams(
			credentialID,
			repoUrl,
			username,
			encryptedPAT,
		))
		if err != nil {
			response.InternalServerError(w, err)
			return
		}


		var createdBy *uuid.UUID
		if credential.CreatedBy != nil {
			if cb, err := uuid.Parse(*credential.CreatedBy); err == nil {
				createdBy = &cb
			}
		}

		// 解密 PAT 用于脱敏显示
		plainPAT, _ := crypto.DecryptPAT(credential.EncryptedPAT)

		// 返回更新结果
		updID, _ := uuid.Parse(credential.ID)
		updPID, _ := uuid.Parse(credential.ProjectID)
		response.JSON(w, r, credentialResponse{
			ID:        updID,
			ProjectID: updPID,
			RepoUrl:   credential.RepoURL,
			Username:  credential.Username,
			MaskedPAT: maskPAT(plainPAT),
			CreatedBy: createdBy,
			CreatedAt: credential.CreatedAt,
			UpdatedAt: credential.UpdatedAt,
		})
	}
}
