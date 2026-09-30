// daemon_api.go — machine-side daemon API: registration (agent report + tool
// detection), heartbeat, and graceful deregistration. Authenticated with the
// daemon's td_ token (daemon principal, no X-Agent-ID).
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// DaemonAPIHandler handles machine-facing daemon endpoints.
type DaemonAPIHandler struct {
	Svc *service.Service
}

func NewDaemonAPIHandler(svc *service.Service) *DaemonAPIHandler {
	return &DaemonAPIHandler{Svc: svc}
}

// Routes returns the machine-side daemon route table (mounted at /api/daemons).
func (h *DaemonAPIHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/register", h.Register)
	r.Post("/heartbeat", h.Heartbeat)
	r.Post("/deregister", h.Deregister)

	return r
}

// daemonClaims extracts the daemon principal from the auth context.
func daemonClaims(r *http.Request) (uuid.UUID, bool) {
	claims, has := svcmw.GetAuthFromContext(r.Context())
	if !has || claims.UserType != "daemon" {
		return uuid.Nil, false
	}
	return claims.UserID, true
}

// Register handles POST /api/daemons/register.
//
// Request body:
//   - device_name: machine description reported by agentd
//   - version: agentd version
//   - public_key: daemon RSA public key (git credential encryption)
//   - providers: detected tool snapshot [{provider,version,installed}]
//   - agents: reported agent instances [{name,provider}]
//
// Response:
//   - 200: {agents:[{name,agent_id,status}], desired_agents:[{name,provider}], heartbeat_interval}
//   - 403: not authenticated as a daemon
func (h *DaemonAPIHandler) Register(w http.ResponseWriter, r *http.Request) {
	daemonID, ok := daemonClaims(r)
	if !ok {
		response.Forbidden(w, "daemon authentication required")
		return
	}
	var req service.RegisterReport
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	result, err := service.NewDaemonService(h.Svc).Register(r.Context(), daemonID, req)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, result)
}

// Heartbeat handles POST /api/daemons/heartbeat.
//
// Request body (agents optional):
//   - agents: [{name, busy}]
//
// Response:
//   - 200: {status:"ok", desired_agents, heartbeat_interval}
//   - 404: daemon row no longer exists (re-provision required)
func (h *DaemonAPIHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	daemonID, ok := daemonClaims(r)
	if !ok {
		response.Forbidden(w, "daemon authentication required")
		return
	}
	var req struct {
		Agents []service.HeartbeatAgentStatus `json:"agents"`
	}
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	result, err := service.NewDaemonService(h.Svc).Heartbeat(r.Context(), daemonID, req.Agents)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	response.JSON(w, r, result)
}

// Deregister handles POST /api/daemons/deregister (graceful shutdown:
// daemon and its reported agents go offline).
func (h *DaemonAPIHandler) Deregister(w http.ResponseWriter, r *http.Request) {
	daemonID, ok := daemonClaims(r)
	if !ok {
		response.Forbidden(w, "daemon authentication required")
		return
	}
	if err := service.NewDaemonService(h.Svc).Deregister(r.Context(), daemonID); err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, map[string]string{"status": "ok"})
}
