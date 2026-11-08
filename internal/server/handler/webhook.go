// webhook.go provides the public GitHub webhook entry (plan §10.2): it
// verifies the X-Hub-Signature-256 HMAC, stores the delivery as a durable
// workflow_events row and returns immediately — the event service processes
// pending events asynchronously. The handler never executes a workflow and
// never logs secret material.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/integration/github"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// maxWebhookBodyBytes bounds the delivery body (plan §10.2: 2 MiB).
const maxWebhookBodyBytes = 2 << 20

// WebhookHandler handles the unauthenticated webhook entry.
type WebhookHandler struct {
	Svc *service.Service
}

func NewWebhookHandler(svc *service.Service) *WebhookHandler {
	return &WebhookHandler{Svc: svc}
}

// Routes mounts the webhook route table.
func (h *WebhookHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(svcmw.BodyLimitMiddlewareWithSize(maxWebhookBodyBytes))
	r.Post("/github/{integrationId}", h.HandleGitHub)
	return r
}

// HandleGitHub handles POST /api/webhooks/github/{integrationId}.
//
// Response:
//   - 202: delivery stored (or duplicate delivery — idempotent)
//   - 400: missing headers or non-JSON body
//   - 401: missing/unverified signature or no webhook secret configured
//   - 404: unknown integration
//   - 413: body exceeds the 2 MiB limit
func (h *WebhookHandler) HandleGitHub(w http.ResponseWriter, r *http.Request) {
	integrationID, err := uuid.Parse(chi.URLParam(r, "integrationId"))
	if err != nil {
		response.BadRequest(w, "invalid integration id")
		return
	}

	secret, ok, err := service.NewIntegrationService(h.Svc).GetWebhookSecret(r.Context(), integrationID)
	if err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			response.NotFound(w, "integration not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}
	if !ok {
		// A known integration without a stored secret cannot verify any
		// signature; reject before reading further.
		response.Unauthorized(w, "webhook signature verification failed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		if svcmw.IsMaxBytesError(err) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		response.BadRequest(w, "read body failed")
		return
	}

	if !github.VerifyWebhookSignature(secret, body, r.Header.Get(github.SignatureHeader)) {
		response.Unauthorized(w, "webhook signature verification failed")
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if eventType == "" || deliveryID == "" {
		response.BadRequest(w, "X-GitHub-Event and X-GitHub-Delivery headers are required")
		return
	}

	payload, err := github.NormalizeWebhookPayload(eventType, deliveryID, body)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	stored, err := service.NewWorkflowEventService(h.Svc).RecordDelivery(r.Context(), types.WorkflowEvent{
		Provider:      "github",
		IntegrationID: integrationID.String(),
		ExternalID:    deliveryID,
		EventType:     eventType,
		Action:        payload.Action,
		Payload:       encoded,
	})
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	_ = stored // duplicate deliveries return the same 202 (idempotent)
	w.WriteHeader(http.StatusAccepted)
	response.JSON(w, r, map[string]string{"status": "accepted"})
}
