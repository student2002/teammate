// routes_task.go 注册任务级路由：节点、评论、Token 用量、审查、子任务、日志、Git 分支。
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	svcmw "github.com/teammate/server/internal/server/middleware"
)

// registerTaskRoutes 注册任务作用域的路由。
func (reg *routeRegistrar) registerTaskRoutes(r chi.Router) {
	svc := reg.svc
	s := reg.server

	r.Route("/tasks/{taskId}", func(r chi.Router) {
		r.Use(svcmw.TaskAccessMiddlewareWithChecker(reg.taskChk, reg.wsChk))

		// 节点（带节点级工作区检查）
		r.Route("/nodes", func(r chi.Router) {
			r.Use(svcmw.NodeAccessMiddlewareWithChecker(reg.nodeChk, reg.wsChk))
			r.Mount("/", handler.NewNodeHandler(svc).Routes())
		})

		// 评论
		r.Mount("/comments", handler.NewCommentHandler(svc).Routes())

		// Token 用量
		r.Mount("/token-usage", handler.NewTokenUsageHandler(svc).Routes())

		// 审查（自我审查检查）
		r.Mount("/review", handler.NewReviewHandler(svc).Routes())

		// 日志消息上传（agentd → API）
		r.Post("/messages", handler.NewPostLogMessageHandler(s.Gateway, svc, reg.wsChk).ServeHTTP)

		// 历史日志查询
		r.Get("/logs", handler.NewGetTaskLogsHandler(s.Gateway, svc).ServeHTTP)

		// Git 分支更新（agentd 在 git init 后上报）
		r.Put("/git-branch", handler.NewUpdateGitBranchHandler(svc))
	})
}
