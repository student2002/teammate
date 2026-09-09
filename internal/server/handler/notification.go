// notification.go provides HTTP API endpoints for querying the notification list.
//
// This file provides the following HTTP API endpoints:
//   - GET /workspaces/{workspaceId}/notifications: list notifications under the workspace, supports filtering by the member_id query parameter
//
// The notification list is queried by NotificationService and returns all notifications in the specified workspace or notifications for a specific member.
// The request path parameter workspaceId must be a valid UUID format; the member_id query parameter is optional, and if provided, only that member's notifications are returned.

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// NotificationHandler handles HTTP requests for notification queries.
type NotificationHandler struct {
	Svc *service.Service
}

// NewNotificationHandler creates a NotificationHandler instance.
//
// Parameters:
//   - svc: business logic service instance, provides notification query capability
//
// Returns:
//   - *NotificationHandler: notification handler instance
func NewNotificationHandler(svc *service.Service) *NotificationHandler {
	return &NotificationHandler{Svc: svc}
}

// Routes returns the route table for notifications.
//
// Returns:
//   - chi.Router: router containing notification-related endpoints
func (h *NotificationHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListNotifications)

	return r
}

// ListNotifications handles the GET /workspaces/{workspaceId}/notifications endpoint, listing notifications under the workspace, supports filtering by member ID.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID, query parameter member_id is optional
//
// Returns:
//   - no return value, writes a JSON response via w containing the notification list or an error message
func (h *NotificationHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	memberIDStr := r.URL.Query().Get("member_id")
	memberID, err := uuid.Parse(memberIDStr)
	if err != nil {
		memberID = uuid.Nil
	}

	notifSvc := service.NewNotificationService(h.Svc)
	notifications, err := notifSvc.ListNotifications(r.Context(), workspaceID, memberID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, notifications)
}
