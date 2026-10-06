// stats.go provides HTTP API endpoints for statistics queries.
//
// This file provides the following HTTP API endpoints:
//   - GET /projects/{projectId}/stats: aggregated statistics for a project (task counts, node completion rate, average completion time)
//
// The endpoint is mounted inside the project route group, so project membership
// is enforced by ProjectMemberMiddleware before the handler runs.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// StatsHandler handles HTTP requests for statistics queries.
type StatsHandler struct {
	Svc *service.Service
}

// NewStatsHandler creates a StatsHandler instance.
func NewStatsHandler(svc *service.Service) *StatsHandler {
	return &StatsHandler{Svc: svc}
}

// GetProjectStats handles the GET /projects/{projectId}/stats endpoint, returning
// task counts grouped by status, node completion rate, and average completion time.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter projectId is the project UUID
//
// Response:
//   - 200: successfully returns the project statistics
//   - 400: invalid project id
func (h *StatsHandler) GetProjectStats(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	stats, err := service.NewStatsService(h.Svc).GetProjectStats(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, stats)
}
