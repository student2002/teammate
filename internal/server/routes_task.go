// routes_task.go registers task-level routes: comments, token usage, subtasks, logs, Git branches.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/types"
)

// registerTaskRoutes registers task-scoped routes.
func (reg *routeRegistrar) registerTaskRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	r.Route("/tasks/{taskId}", func(r chi.Router) {
		r.Use(svcmw.TaskAccessMiddlewareWithChecker(reg.taskChk, reg.wsChk))

		// Comments
		r.Mount("/comments", handler.NewCommentHandler(svc).Routes())

		// Token usage
		r.Mount("/token-usage", handler.NewTokenUsageHandler(svc).Routes())

		// Subtasks
		r.Mount("/subtasks", handler.NewSubtaskHandler(svc).Routes())

		// Log message upload (agentd → API)
		r.Post("/messages", handler.NewPostLogMessageHandler(s.Gateway, svc, reg.wsChk).ServeHTTP)

		// Historical log query
		r.Get("/logs", handler.NewGetTaskLogsHandler(s.Gateway, svc).ServeHTTP)

		// Git branch update (reported by agentd after git init)
		r.Put("/git-branch", handler.NewUpdateGitBranchHandler(svc))

		// Workflow run view (V2 engine)
		r.Get("/graph", handler.NewWorkflowRunHandler(svc).GetTaskGraph)

		// Node execution protocol (S4 agentd): discovery, claim and receipts.
		// Agents need task:execute (granted by default); human claimants are
		// narrowed to member+ inside the handler.
		r.Group(func(r chi.Router) {
			r.Use(svcmw.RequireAccessWithChecker([]string{"owner", "admin", "member", "viewer"}, types.PermTaskExecute, reg.agentPerm))
			r.Mount("/nodes", handler.NewNodeExecutionHandler(svc).Routes())
		})
	})
}
