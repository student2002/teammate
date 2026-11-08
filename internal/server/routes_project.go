// routes_project.go registers project-level routes: tasks, board, Git credentials, stats.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
)

// registerProjectRoutes registers project-scoped routes.
func (reg *routeRegistrar) registerProjectRoutes(r chi.Router) {
	svc := reg.svc

	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Use(svcmw.ProjectMemberMiddlewareWithChecker(reg.projectChk))

		// Tasks
		r.Mount("/tasks", handler.NewTaskHandler(svc).Routes())

		// Workflow runs (V2 engine): manual run of a template
		r.Post("/workflows/{templateId}/runs", handler.NewWorkflowRunHandler(svc).StartRun)

		// Board
		r.Mount("/board", handler.NewBoardHandler(svc).Routes())

		// Stats
		r.Get("/stats", handler.NewStatsHandler(svc).GetProjectStats)

		// Git credentials (only create, view, modify are supported; deletion is not allowed, to keep task branches traceable)
		r.Get("/git-credentials", handler.GitCredentialsHandler(svc))
		r.Post("/git-credentials", handler.CreateGitCredentialHandler(svc))
		r.Put("/git-credentials/{credentialId}", handler.UpdateGitCredentialHandler(svc))
	})
}
