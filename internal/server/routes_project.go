// routes_project.go 注册项目级路由：任务、看板、审查、Git 凭据、统计。
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
)

// registerProjectRoutes 注册项目作用域的路由。
func (reg *routeRegistrar) registerProjectRoutes(r chi.Router) {
	svc := reg.svc

	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Use(svcmw.ProjectMemberMiddlewareWithChecker(reg.projectChk))

		// 任务
		r.Mount("/tasks", handler.NewTaskHandler(svc).Routes())

		// 看板
		r.Mount("/board", handler.NewBoardHandler(svc).Routes())

		// 审查
		r.Mount("/review", handler.NewReviewHandler(svc).Routes())

		// Git 凭据（仅支持创建、查看、修改；不允许删除，以保证任务分支可追溯）
		r.Get("/git-credentials", handler.GitCredentialsHandler(svc))
		r.Post("/git-credentials", handler.CreateGitCredentialHandler(svc))
		r.Put("/git-credentials/{credentialId}", handler.UpdateGitCredentialHandler(svc))
	})
}
