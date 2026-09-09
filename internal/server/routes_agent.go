// routes_agent.go registers Agent-level routes and miscellaneous routes: agent stats, memories, template stats, community workflows, agent roles.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
)

// registerMiscRoutes registers miscellaneous routes: memories, community workflows.
func (reg *routeRegistrar) registerMiscRoutes(r chi.Router) {
	svc := reg.svc

	// Shared memories
	memoryHandler := handler.NewMemoryHandler(svc, reg.wsChk)
	r.Route("/memories", func(r chi.Router) {
		r.Get("/", memoryHandler.ListMemories)
		r.Post("/", memoryHandler.CreateMemory)
		r.Get("/search", memoryHandler.SearchMemories)
		r.Delete("/{id}", memoryHandler.DeleteMemory)
	})

	// Community workflows
	r.Mount("/community/workflows", handler.NewCommunityHandler(svc).Routes())
}
