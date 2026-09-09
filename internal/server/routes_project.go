// routes_project.go registers project-level routes: tasks, board, review, Git credentials, stats.
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

		// Board
		r.Mount("/board", handler.NewBoardHandler(svc).Routes())

		// Review
		r.Mount("/review", handler.NewReviewHandler(svc).Routes())

		// Git credentials (only create, view, modify are supported; deletion is not allowed, to keep task branches traceable)
		r.Get("/git-credentials", handler.GitCredentialsHandler(svc))
		r.Post("/git-credentials", handler.CreateGitCredentialHandler(svc))
		r.Put("/git-credentials/{credentialId}", handler.UpdateGitCredentialHandler(svc))
	})
}
