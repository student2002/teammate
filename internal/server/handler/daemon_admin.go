// daemon_admin.go — web-facing HTTP endpoints for agentd instance (daemon) management:
// creating a daemon (one-time td_ token), listing, deleting, and rotating the token.
// The machine-side registration/heartbeat endpoints live in the daemon API surface.
package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// DaemonHandler handles web requests for daemon management within a workspace.
type DaemonHandler struct {
	Svc *service.Service
}

func NewDaemonHandler(svc *service.Service) *DaemonHandler {
	return &DaemonHandler{Svc: svc}
}

// Routes returns the daemon management route table.
func (h *DaemonHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateDaemon)
	r.Get("/", h.ListDaemons)
	r.Delete("/{id}", h.DeleteDaemon)
	r.Post("/{id}/rotate-token", h.RotateToken)
	r.Post("/{id}/agents", h.CreateDaemonAgent)

	return r
}

// daemonAgentSummary describes an agent instance reported by the daemon.
// Populated once registration reporting lands; web-created pending agents
// also surface here with status "pending".
type daemonAgentSummary struct {
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Status         string `json:"status"`
	LastReportedAt string `json:"last_reported_at,omitempty"`
}

// daemonResponse is the wire shape consumed by the web agents page.
type daemonResponse struct {
	ID            uuid.UUID            `json:"id"`
	Name          string               `json:"name"`
	DeviceName    string               `json:"device_name"`
	Version       string               `json:"version"`
	Status        string               `json:"status"`
	LastHeartbeat *string              `json:"last_heartbeat,omitempty"`
	Providers     json.RawMessage      `json:"providers"`
	Agents        []daemonAgentSummary `json:"agents"`
	CreatedAt     string               `json:"created_at"`
}

func daemonToResponse(d types.Daemon) daemonResponse {
	resp := daemonResponse{
		ID:         d.ID,
		Name:       d.Name,
		DeviceName: d.DeviceName,
		Version:    d.Version,
		Status:     d.Status,
		Providers:  d.Providers,
		Agents:     []daemonAgentSummary{},
		CreatedAt:  d.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if d.LastHeartbeat != nil {
		ts := d.LastHeartbeat.UTC().Format("2006-01-02T15:04:05Z07:00")
		resp.LastHeartbeat = &ts
	}
	return resp
}

type createDaemonRequest struct {
	Name string `json:"name"`
}

type createDaemonResponse struct {
	daemonResponse
	DaemonToken string `json:"daemon_token"`
}

type rotateTokenResponse struct {
	ID          uuid.UUID `json:"id"`
	DaemonToken string    `json:"daemon_token"`
}

// CreateDaemon handles POST /workspaces/{workspaceId}/daemons.
//
// Response:
//   - 201: daemon created; daemon_token appears exactly once, in this response
//   - 400: missing or empty name
func (h *DaemonHandler) CreateDaemon(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	var req createDaemonRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	daemonSvc := service.NewDaemonService(h.Svc)
	result, err := daemonSvc.Create(r.Context(), workspaceID, req.Name)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, createDaemonResponse{
		daemonResponse: daemonToResponse(result.Daemon),
		DaemonToken:    result.Token,
	})
}

// ListDaemons handles GET /workspaces/{workspaceId}/daemons.
func (h *DaemonHandler) ListDaemons(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	daemonSvc := service.NewDaemonService(h.Svc)
	daemons, err := daemonSvc.List(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	out := make([]daemonResponse, 0, len(daemons))
	for _, d := range daemons {
		resp := daemonToResponse(d)
		agents, err := daemonSvc.ListAgents(r.Context(), d.ID)
		if err == nil {
			resp.Agents = make([]daemonAgentSummary, 0, len(agents))
			for _, a := range agents {
				resp.Agents = append(resp.Agents, daemonAgentSummary{
					Name:           a.Name,
					Provider:       a.Provider,
					Status:         a.Status,
					LastReportedAt: a.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				})
			}
		}
		out = append(out, resp)
	}
	response.JSON(w, r, out)
}

// DeleteDaemon handles DELETE /workspaces/{workspaceId}/daemons/{id}.
// Reported agents go offline but keep their configuration.
func (h *DaemonHandler) DeleteDaemon(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}
	daemonID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid daemon id")
		return
	}

	daemonSvc := service.NewDaemonService(h.Svc)
	if err := daemonSvc.Delete(r.Context(), workspaceID, daemonID); err != nil {
		response.NotFound(w, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RotateToken handles POST /workspaces/{workspaceId}/daemons/{id}/rotate-token.
// The previous token is invalidated immediately; the new plaintext is returned once.
func (h *DaemonHandler) RotateToken(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}
	daemonID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid daemon id")
		return
	}

	daemonSvc := service.NewDaemonService(h.Svc)
	token, err := daemonSvc.RotateToken(r.Context(), workspaceID, daemonID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	response.JSON(w, r, rotateTokenResponse{ID: daemonID, DaemonToken: token})
}

type createDaemonAgentRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// CreateDaemonAgent handles POST /workspaces/{workspaceId}/daemons/{id}/agents:
// creates a web-pushed instance under the daemon's tool node. The provider must
// be present in the daemon's detected-tools snapshot; the instance stays
// "pending" until the daemon adopts it via registration reporting.
//
// Response:
//   - 201: instance created (pending)
//   - 400: invalid body, empty name, or provider not detected on the daemon
//   - 404: daemon does not exist in this workspace
//   - 409: an instance with the same name already exists on the daemon
func (h *DaemonHandler) CreateDaemonAgent(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}
	daemonID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid daemon id")
		return
	}
	var req createDaemonAgentRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	agent, err := service.NewDaemonService(h.Svc).CreateAgent(r.Context(), workspaceID, daemonID, req.Name, req.Provider)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate key") {
			response.Conflict(w, "an instance with this name already exists on the daemon")
			return
		}
		if strings.Contains(err.Error(), "not detected") || strings.Contains(err.Error(), "name is required") ||
			strings.Contains(err.Error(), "belongs") {
			response.BadRequest(w, err.Error())
			return
		}
		response.NotFound(w, err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, agentToResponse(agent))
}
