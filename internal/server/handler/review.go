// review.go provides HTTP API endpoints for review queue queries and self-review detection.
//
// This file provides the following HTTP API endpoints:
//   - GET /projects/{projectId}/review-queue: get the project's review queue, returning the list of nodes pending review
//   - GET /projects/{projectId}/review/nodes/{nodeId}/self-review-check: detect whether the review node has a self-review conflict
//
// The review queue is queried by ReviewService and lists all nodes pending review in the specified project.
// Self-review detection is used to determine whether the reviewer is the executing Agent of a preceding node, to avoid violating the self-review avoidance mechanism.

package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// ReviewHandler handles HTTP requests related to reviews, including review queue queries and self-review detection.
type ReviewHandler struct {
	Svc *service.Service
}

// NewReviewHandler creates a ReviewHandler instance.
//
// Parameters:
//   - svc: business logic service instance, provides review queue and self-review detection capability
//
// Returns:
//   - *ReviewHandler: review handler instance
func NewReviewHandler(svc *service.Service) *ReviewHandler {
	return &ReviewHandler{Svc: svc}
}

// Routes returns the route table for review-related operations.
//
// Returns:
//   - chi.Router: router containing the review queue query and self-review detection endpoints
func (h *ReviewHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/review-queue", h.GetReviewQueue)
	r.Get("/nodes/{nodeId}/self-review-check", h.CheckSelfReview)

	return r
}

// GetReviewQueue handles the GET /projects/{projectId}/review-queue endpoint, getting the project's review queue and returning the list of nodes pending review.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter projectId is the project UUID
//
// Returns:
//   - no return value, writes a JSON response via w containing the list of nodes pending review or an error message
func (h *ReviewHandler) GetReviewQueue(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	reviewSvc := service.NewReviewService(h.Svc)
	items, err := reviewSvc.GetReviewQueue(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, items)
}

// CheckSelfReview handles the GET /projects/{projectId}/review/nodes/{nodeId}/self-review-check endpoint, detecting whether the review node has a self-review conflict.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter taskId is the task ID, nodeId is the node UUID
//
// Returns:
//   - no return value, writes a JSON response via w containing the self-review detection result or an error message
func (h *ReviewHandler) CheckSelfReview(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "taskId")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}
	nodeID, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return
	}

	reviewSvc := service.NewReviewService(h.Svc)
	result, err := reviewSvc.CheckSelfReview(r.Context(), taskID, nodeID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			response.NotFound(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, result)
}
