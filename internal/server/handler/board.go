// board.go provides HTTP API endpoints for querying project board data, returning task data organized by column.
//
// This file provides the following HTTP API endpoint:
//   - GET /projects/{projectId}/board: get project board data, returns a task list organized by column (pending/in_progress/completed/rejected/manual_intervention)
//
// The board data is queried by BoardService and arranged in a predefined column order; each column contains a column key, label, and the corresponding task list.
// The request path parameter projectId must be a valid UUID format, otherwise a 400 Bad Request is returned.

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// BoardHandler handles HTTP requests for project board data queries.
type BoardHandler struct {
	Svc *service.Service
}

// NewBoardHandler creates a BoardHandler instance.
//
// Parameters:
//   - svc: business logic service instance, provides data query capability
//
// Returns:
//   - *BoardHandler: board handler instance
func NewBoardHandler(svc *service.Service) *BoardHandler {
	return &BoardHandler{Svc: svc}
}

// Routes returns the route table for the board.
//
// Returns:
//   - chi.Router: routes containing board-related endpoints
func (h *BoardHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.GetBoardData)

	return r
}

// GetBoardData handles the GET /projects/{projectId}/board endpoint, returning board task data organized by column.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter projectId is the project UUID
//
// Returns:
//   - no return value; writes a JSON response via w, containing task data organized by column or an error message
func (h *BoardHandler) GetBoardData(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	boardSvc := service.NewBoardService(h.Svc)
	columns, err := boardSvc.GetBoardData(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// build the result in the defined column order
	type boardColumn struct {
		Key   string                   `json:"key"`
		Label string                   `json:"label"`
		Tasks []service.BoardColumnTask `json:"tasks"`
	}

	result := struct {
		Columns []boardColumn `json:"columns"`
	}{
		Columns: make([]boardColumn, 0, len(columns)),
	}
	for _, col := range columns {
		result.Columns = append(result.Columns, boardColumn{
			Key:   col.Key,
			Label: col.Label,
			Tasks: col.Tasks,
		})
	}

	response.JSON(w, r, result)
}
