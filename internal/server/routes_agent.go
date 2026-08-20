// routes_agent.go 注册 Agent 级路由及杂项路由：Agent 统计、记忆、模板统计、社区工作流、Agent 角色。
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
)

// registerMiscRoutes 注册杂项路由：记忆、社区工作流。
func (reg *routeRegistrar) registerMiscRoutes(r chi.Router) {
	svc := reg.svc

	// 共享记忆
	memoryHandler := handler.NewMemoryHandler(svc, reg.wsChk)
	r.Route("/memories", func(r chi.Router) {
		r.Get("/", memoryHandler.ListMemories)
		r.Post("/", memoryHandler.CreateMemory)
		r.Get("/search", memoryHandler.SearchMemories)
		r.Delete("/{id}", memoryHandler.DeleteMemory)
	})

	// 社区工作流
	r.Mount("/community/workflows", handler.NewCommunityHandler(svc).Routes())
}
