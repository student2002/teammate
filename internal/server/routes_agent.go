// routes_agent.go registers Agent-level routes and miscellaneous routes: agent stats, memories, template stats, community workflows, agent roles.
package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/server/handler"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/types"
)

// registerMiscRoutes registers miscellaneous routes: memories, community workflows, agent roles.
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

	// Predefined agent roles
	r.Get("/agent-roles", func(w http.ResponseWriter, req *http.Request) {
		response.JSON(w, req, types.ListAgentRoles())
	})
}
