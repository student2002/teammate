// sse.go provides Server-Sent Events (SSE) real-time event push endpoints, used for Agent daemon event subscription and reconnection compensation.
//
// SSE event types: node:pending, node:continuation_invite, task:interrupt, node:timeout, etc.
// Reconnection: when an Agent reconnects it sends Last-Event-ID, and the server replays lost events from the Redis buffer.

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
)

// SSEHandler handles HTTP requests for Server-Sent Events real-time event push, supporting reconnection compensation.
type SSEHandler struct {
	Svc *service.Service
	Hub *ws.Hub // SSE event Hub
}

// NewSSEHandler creates an SSEHandler instance.
func NewSSEHandler(svc *service.Service, hub *ws.Hub) *SSEHandler {
	return &SSEHandler{Svc: svc, Hub: hub}
}

// Routes returns the route table for SSE.
func (h *SSEHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.Stream)

	return r
}

// Stream handles the GET /workspaces/{workspaceId}/daemons/{id}/events endpoint,
// establishing a long-lived SSE connection on the daemon's single event stream.
// Events carry an agent_id envelope so the daemon routes them to the local runtime.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request headers:
//   - Last-Event-ID: string, the ID of the last received event (used for reconnection)
//
// Response:
//   - 200: SSE event stream (text/event-stream)
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: daemons can only subscribe to their own stream
//   - 404: daemon does not exist
//
// Processing flow:
//  1. verify authentication and daemon ownership
//  2. check the Last-Event-ID header for reconnection
//  3. replay lost events from the Redis buffer
//  4. subscribe to the daemon event channel
//  5. keep the long connection alive and push events
func (h *SSEHandler) Stream(w http.ResponseWriter, r *http.Request) {
	// parse daemon ID
	daemonID := chi.URLParam(r, "id")
	if daemonID == "" {
		response.BadRequest(w, "missing daemon id")
		return
	}

	// parse workspace ID
	workspaceIDStr := chi.URLParam(r, "workspaceId")
	if workspaceIDStr == "" {
		response.BadRequest(w, "missing workspaceId")
		return
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if err != nil {
		response.BadRequest(w, "invalid workspaceId")
		return
	}

	// verify authentication
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// verify daemon exists and belongs to the URL workspace
	daemonUUID, err := uuid.Parse(daemonID)
	if err != nil {
		response.BadRequest(w, "invalid daemon id")
		return
	}
	daemon, err := service.NewDaemonService(h.Svc).Get(r.Context(), daemonUUID)
	if err != nil || daemon.WorkspaceID != workspaceID {
		response.NotFound(w, "daemon not found")
		return
	}

	// A daemon may only subscribe to its own stream; humans access for debugging
	if claims.UserType == "daemon" && claims.UserID != daemonUUID {
		response.Forbidden(w, "daemons can only subscribe to their own stream")
		return
	}

	subscriberKey := "daemon:" + daemonUUID.String()

	// chi's Timeout middleware wraps the ResponseWriter and hides http.Flusher.
	// We need to unwrap it to get the underlying Flusher.
	var flusher http.Flusher
	var flushOK bool
	flusher, flushOK = w.(http.Flusher)
	if !flushOK {
		// try chi's wrapResponseWriter interface
		type wrapResponseWriter interface {
			http.ResponseWriter
			Unwrap() http.ResponseWriter
		}
		if wrapped, wrapOK := w.(wrapResponseWriter); wrapOK {
			flusher, flushOK = wrapped.Unwrap().(http.Flusher)
		}
	}
	if !flushOK {
		response.InternalServerError(w, fmt.Errorf("streaming not supported"))
		return
	}

	// set SSE response headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// disable the write timeout to prevent the server's WriteTimeout from closing the long connection
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		slog.Warn("failed to disable write deadline for SSE connection", "err", err)
	}

	// check the Last-Event-ID header to support reconnection
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID != "" {
		h.replayBufferedEvents(r.Context(), w, flusher, subscriberKey, lastEventID)
	}

	// subscribe to the daemon event channel
	ch, unsub := h.Hub.Subscribe(subscriberKey)
	defer unsub()

	// send an initial comment to establish the connection
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// start a keepalive timer to prevent the idle connection from being closed
	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	// main loop: push events
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			// send an SSE comment as keepalive - clients ignore comments,
			// but proxies/load balancers see activity and do not close the connection
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, flusher, event)
		}
	}
}

// replayBufferedEvents fetches lost events from the Redis buffer and sends them to the client.
// If the buffer is empty or expired, it sends a sync:required event.
func (h *SSEHandler) replayBufferedEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, subscriberKey string, lastEventID string) {
	events, err := h.Hub.GetBufferedEvents(ctx, subscriberKey, lastEventID)
	if err != nil {
		slog.Error("get buffered events for replay", "subscriber", subscriberKey, "last_event_id", lastEventID, "err", err)
	}

	// buffer is empty or expired - send sync:required
	if len(events) == 0 {
		syncData, _ := json.Marshal(map[string]string{
			"reason": "buffer_expired",
			"action": "full_sync",
		})
		writeSSEEvent(w, flusher, ws.SSEEvent{
			ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
			Event: ws.EventSyncRequired,
			Data:  syncData,
		})
		return
	}

	// replay the buffered events
	for _, event := range events {
		writeSSEEvent(w, flusher, event)
	}
}

// writeSSEEvent writes a single SSE event to the response writer and flushes.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event ws.SSEEvent) {
	if event.ID != "" {
		fmt.Fprintf(w, "id: %s\n", event.ID)
	}
	if event.Event != "" {
		fmt.Fprintf(w, "event: %s\n", event.Event)
	}
	fmt.Fprintf(w, "data: %s\n\n", event.Data)
	flusher.Flush()
}
