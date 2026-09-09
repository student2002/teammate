// routes_workspace.go registers workspace-level routes: workspace CRUD, member management, workflows, skills, MCP, runtimes, notifications.
//
// The read/write boundary is declared explicitly at the route layer: viewer+ can access read routes, member+ can access write routes.
// The handler layer still performs a secondary check as a fallback.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/types"
)

// registerWorkspaceRoutes registers workspace-scoped routes.
func (reg *routeRegistrar) registerWorkspaceRoutes(r chi.Router) {
	svc := reg.svc

	wsHandler := handler.NewWorkspaceHandler(svc)
	r.Get("/workspaces", wsHandler.ListWorkspaces)
	r.Post("/workspaces", wsHandler.CreateWorkspace)
	r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
		r.Use(svcmw.WorkspaceAuthMiddlewareWithChecker(reg.wsChk))

		r.Get("/", wsHandler.GetWorkspace)

		// Write operations require Owner/Admin (human users only; Agents are rejected)
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin"}, "", reg.agentPerm))
			r.Put("/", wsHandler.UpdateWorkspace)
			r.Delete("/", wsHandler.DeleteWorkspace)
			r.Post("/members", wsHandler.CreateMember)
			r.Delete("/members/{memberId}", wsHandler.DeleteMember)
			r.Put("/members/{memberId}/role", wsHandler.UpdateMemberRole)
		})

		// Read operations: viewer can view the member list (human users only; Agents are rejected)
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, "", reg.agentPerm))
			r.Get("/members", wsHandler.ListMembers)
		})

		// Search — read-only within the workspace scope
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, types.PermTaskExecute, reg.agentPerm))
			r.Mount("/search", handler.NewSearchHandler(svc).Routes())
		})

		// Read/write separation: viewer+ can read, member+ can write
		readPerm := svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, types.PermTaskExecute, reg.agentPerm)
		writePerm := svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member"}, types.PermTaskExecute, reg.agentPerm)
		permByMethod := methodBasedPerm(readPerm, writePerm)

		// Workflows — read/write separation
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/workflows", handler.NewWorkflowHandler(svc).Routes())
		})

		// Projects — read/write separation
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/projects", handler.NewProjectHandler(svc).Routes())
		})

		// Agents — read/write separation
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/agents", handler.NewAgentHandler(svc).Routes())
		})

		// Skills — read/write separation
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/skills", handler.NewSkillHandler(svc).Routes())
		})

		// MCP servers — read/write separation
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/mcp-servers", handler.NewMcpHandler(svc).Routes())
		})

		// Runtimes — read/write separation
		r.Group(func(r chi.Router) {
			r.Use(permByMethod)
			r.Mount("/runtimes", handler.NewRuntimeHandler(svc).Routes())
		})

		// Notifications (member+ access only; Agents are rejected)
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member"}, "", reg.agentPerm))
			r.Mount("/notifications", handler.NewNotificationHandler(svc).Routes())
		})
	})
}

// methodBasedPerm returns a middleware that applies readPerm to safe methods (GET, HEAD, OPTIONS)
// and writePerm to mutating methods (POST, PUT, PATCH, DELETE).
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
