// routes_task.go registers task-level routes: nodes, comments, token usage, review, subtasks, logs, Git branches.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
)

// registerTaskRoutes registers task-scoped routes.
func (reg *routeRegistrar) registerTaskRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	r.Route("/tasks/{taskId}", func(r chi.Router) {
		r.Use(svcmw.TaskAccessMiddlewareWithChecker(reg.taskChk, reg.wsChk))

		// Nodes (with node-level workspace check)
		r.Route("/nodes", func(r chi.Router) {
			r.Use(svcmw.NodeAccessMiddlewareWithChecker(reg.nodeChk, reg.wsChk))
			r.Mount("/", handler.NewNodeHandler(svc).Routes())
		})

		// Comments
		r.Mount("/comments", handler.NewCommentHandler(svc).Routes())

		// Token usage
		r.Mount("/token-usage", handler.NewTokenUsageHandler(svc).Routes())

		// Review (self-review check)
		r.Mount("/review", handler.NewReviewHandler(svc).Routes())

		// Log message upload (agentd → API)
		r.Post("/messages", handler.NewPostLogMessageHandler(s.Gateway, svc, reg.wsChk).ServeHTTP)

		// Historical log query
		r.Get("/logs", handler.NewGetTaskLogsHandler(s.Gateway, svc).ServeHTTP)

		// Git branch update (reported by agentd after git init)
		r.Put("/git-branch", handler.NewUpdateGitBranchHandler(svc))
	})
}
