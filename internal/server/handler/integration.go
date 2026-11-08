// integration.go provides the HTTP endpoints of the project-level GitHub
// integrations (plan §12.4): CRUD with encrypted secrets, connection test and
// the referenced-delete guard. Responses never carry plaintext secrets.
package handler

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// IntegrationHandler handles the HTTP requests of GitHub integrations.
type IntegrationHandler struct {
	Svc *service.Service
}

func NewIntegrationHandler(svc *service.Service) *IntegrationHandler {
	return &IntegrationHandler{Svc: svc}
}

// Routes mounts the /integrations/github route table (relative to
// /workspaces/{workspaceId}/projects/{projectId}/integrations).
func (h *IntegrationHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Route("/github", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Route("/{integrationId}", func(r chi.Router) {
			r.Put("/", h.Update)
			r.Post("/test", h.TestConnection)
			r.Delete("/", h.Delete)
		})
	})
	return r
}

// upsertIntegrationRequest is the create/update body (plan §12.4).
type upsertIntegrationRequest struct {
	Name          string `json:"name"`
	RepoOwner     string `json:"repo_owner"`
	RepoName      string `json:"repo_name"`
	PAT           string `json:"pat"`
	WebhookSecret string `json:"webhook_secret"`
}

// integrationCtx bundles the ownership chain one integration operation needs:
// the URL project (verified inside the caller's workspace) and the
// integration, verified to belong to that project.
type integrationCtx struct {
	projectID    uuid.UUID
	integration  service.IntegrationView
	integrationD uuid.UUID
}

// resolveIntegrationContext parses the projectId/integrationId parameters,
// verifies the project lives in the caller's workspace and the integration
// belongs to that project. On failure it writes the response and returns nil.
func resolveIntegrationContext(svc *service.Service, w http.ResponseWriter, r *http.Request) *integrationCtx {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return nil
	}
	project := checkProjectWorkspace(svc, w, r, projectID)
	if project == nil {
		return nil
	}
	integrationID, err := uuid.Parse(chi.URLParam(r, "integrationId"))
	if err != nil {
		response.BadRequest(w, "invalid integration id")
		return nil
	}
	integration, err := service.NewIntegrationService(svc).Get(r.Context(), integrationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "integration not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}
	if integration.ProjectID != projectID.String() {
		response.NotFound(w, "integration not found")
		return nil
	}
	return &integrationCtx{projectID: projectID, integration: integration, integrationD: integrationID}
}

// requireWriteAuth returns the parsed claims after the member-level write
// check; on failure it writes the response and returns false.
func requireWriteAuth(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return false
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return false
	}
	return true
}

// Create handles POST .../integrations/github: persists the integration with
// both secrets encrypted and returns the masked view.
func (h *IntegrationHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !requireWriteAuth(w, r) {
		return
	}
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}
	project := checkProjectWorkspace(h.Svc, w, r, projectID)
	if project == nil {
		return
	}
	var req upsertIntegrationRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	claims, _ := svcmw.GetAuthFromContext(r.Context())

	view, err := service.NewIntegrationService(h.Svc).Create(r.Context(), uuid.MustParse(project.WorkspaceID), projectID, toUpsertInput(req), claims.UserID)
	if err != nil {
		if errors.Is(err, service.ErrInvalidSecret) {
			response.InternalServerError(w, err)
			return
		}
		response.BadRequest(w, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, view)
}

// List handles GET .../integrations/github: the project's GitHub integrations
// in masked view.
func (h *IntegrationHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}
	views, err := service.NewIntegrationService(h.Svc).List(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, map[string]interface{}{"integrations": views})
}

// Update handles PUT .../integrations/github/{id}: full replacement of the
// mutable fields (both secrets re-encrypted).
func (h *IntegrationHandler) Update(w http.ResponseWriter, r *http.Request) {
	if !requireWriteAuth(w, r) {
		return
	}
	ctx := resolveIntegrationContext(h.Svc, w, r)
	if ctx == nil {
		return
	}
	var req upsertIntegrationRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	view, err := service.NewIntegrationService(h.Svc).Update(r.Context(), ctx.integrationD, toUpsertInput(req))
	if err != nil {
		if errors.Is(err, service.ErrIntegrationInvalid) {
			response.BadRequest(w, err.Error())
			return
		}
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, view)
}

// TestConnection handles POST .../integrations/github/{id}/test: verifies the
// stored PAT against the repository endpoint and updates the status. The
// failure detail is returned verbatim minus any secret material.
func (h *IntegrationHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	if !requireWriteAuth(w, r) {
		return
	}
	ctx := resolveIntegrationContext(h.Svc, w, r)
	if ctx == nil {
		return
	}

	view, detail, err := service.NewIntegrationService(h.Svc).TestConnection(r.Context(), ctx.integrationD)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	if detail != "" {
		response.JSON(w, r, map[string]string{"status": view.Status, "error": detail})
		return
	}
	response.JSON(w, r, view)
}

// Delete handles DELETE .../integrations/github/{id}: refuses when an enabled
// template's trigger.github node still references the integration.
func (h *IntegrationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !requireWriteAuth(w, r) {
		return
	}
	ctx := resolveIntegrationContext(h.Svc, w, r)
	if ctx == nil {
		return
	}

	if err := service.NewIntegrationService(h.Svc).Delete(r.Context(), ctx.integrationD); err != nil {
		switch {
		case errors.Is(err, service.ErrIntegrationInUse):
			response.Conflict(w, err.Error())
		case errors.Is(err, sql.ErrNoRows):
			response.NotFound(w, "integration not found")
		default:
			response.InternalServerError(w, err)
		}
		return
	}
	response.JSON(w, r, map[string]string{"status": "deleted"})
}

// toUpsertInput maps the HTTP body to the service input.
func toUpsertInput(req upsertIntegrationRequest) service.UpsertIntegrationInput {
	return service.UpsertIntegrationInput{
		Name:          req.Name,
		RepoOwner:     req.RepoOwner,
		RepoName:      req.RepoName,
		PAT:           req.PAT,
		WebhookSecret: req.WebhookSecret,
	}
}
