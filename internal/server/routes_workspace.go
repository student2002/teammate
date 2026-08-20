// routes_workspace.go 注册工作区级路由：工作区 CRUD、成员管理、工作流、技能、MCP、运行时、通知。
//
// 读写边界在路由层显式声明：viewer+ 可访问读路由，member+ 可访问写路由。
// handler 层仍有二次校验作为兜底。
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/types"
)

// registerWorkspaceRoutes 注册工作区作用域的路由。
func (reg *routeRegistrar) registerWorkspaceRoutes(r chi.Router) {
	svc := reg.svc

	wsHandler := handler.NewWorkspaceHandler(svc)
	r.Get("/workspaces", wsHandler.ListWorkspaces)
	r.Post("/workspaces", wsHandler.CreateWorkspace)
	r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
		r.Use(svcmw.WorkspaceAuthMiddlewareWithChecker(reg.wsChk))

		r.Get("/", wsHandler.GetWorkspace)

		// 写操作需要 Owner/Admin（仅限人类用户，Agent 被拒绝）
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin"}, "", reg.agentPerm))
			r.Put("/", wsHandler.UpdateWorkspace)
			r.Delete("/", wsHandler.DeleteWorkspace)
			r.Post("/members", wsHandler.CreateMember)
			r.Delete("/members/{memberId}", wsHandler.DeleteMember)
			r.Put("/members/{memberId}/role", wsHandler.UpdateMemberRole)
		})

		// 读操作：viewer 可查看成员列表（仅限人类用户，Agent 被拒绝）
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, "", reg.agentPerm))
			r.Get("/members", wsHandler.ListMembers)
		})

		// 搜索 — 工作区作用域内只读
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, types.PermTaskExecute, reg.agentPerm))
			r.Mount("/search", handler.NewSearchHandler(svc).Routes())
		})

		// 读写分离：viewer+ 可读，member+ 可写
		readPerm := svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, types.PermTaskExecute, reg.agentPerm)
		writePerm := svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member"}, types.PermTaskExecute, reg.agentPerm)
		permByMethod := methodBasedPerm(readPerm, writePerm)

		// 工作流 — 读写分离
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/workflows", handler.NewWorkflowHandler(svc).Routes())
		})

		// 项目 — 读写分离
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/projects", handler.NewProjectHandler(svc).Routes())
		})

		// 代理 — 读写分离
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/agents", handler.NewAgentHandler(svc).Routes())
		})

		// 技能 — 读写分离
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/skills", handler.NewSkillHandler(svc).Routes())
		})

		// MCP 服务器 — 读写分离
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/mcp-servers", handler.NewMcpHandler(svc).Routes())
		})

		// 运行时 — 读写分离
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/runtimes", handler.NewRuntimeHandler(svc).Routes())
		})

		// 通知（仅 member+ 访问，Agent 被拒绝）
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member"}, "", reg.agentPerm))
			r.Mount("/notifications", handler.NewNotificationHandler(svc).Routes())
		})
	})
}

// methodBasedPerm 返回一个中间件，对安全方法（GET、HEAD、OPTIONS）应用 readPerm，
// 对变更方法（POST、PUT、PATCH、DELETE）应用 writePerm。
func methodBasedPerm(readPerm, writePerm func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		readHandler := readPerm(next)
		writeHandler := writePerm(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				readHandler.ServeHTTP(w, r)
			default:
				writeHandler.ServeHTTP(w, r)
			}
		})
	}
}
