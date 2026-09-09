// skill.go provides HTTP API endpoints for creating, listing, updating, and deleting skills.
//
// A skill is a specific capability that an AI agent can execute, containing a prompt template and category information.

package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	apitypes "github.com/teammate/server/internal/types"
)

// SkillHandler handles HTTP requests for skill management, including creating, querying, updating, and deleting skills.
type SkillHandler struct {
	Svc *service.Service
}

// NewSkillHandler creates a SkillHandler instance.
func NewSkillHandler(svc *service.Service) *SkillHandler {
	return &SkillHandler{Svc: svc}
}

// Routes returns the complete route table for skills (including read and write operations).
func (h *SkillHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateSkill)
	r.Get("/", h.ListSkills)
	r.Put("/{id}", h.UpdateSkill)
	r.Delete("/{id}", h.DeleteSkill)

	return r
}

// ReadRoutes returns the read-only route table for skills.
func (h *SkillHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListSkills)

	return r
}

// WriteRoutes returns the write route table for skills.
func (h *SkillHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateSkill)
	r.Put("/{id}", h.UpdateSkill)
	r.Delete("/{id}", h.DeleteSkill)

	return r
}

// createSkillRequest create skill request body.
type createSkillRequest struct {
	Name           string `json:"name"`            // skill name
	Description    string `json:"description"`     // skill description
	Category       string `json:"category"`        // skill category
	PromptTemplate string `json:"prompt_template"` // prompt template
}

// CreateSkill handles the POST /workspaces/{workspaceId}/skills endpoint, creating a new skill entry.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, skill name (required)
//   - description: string, skill description
//   - category: string, skill category
//   - prompt_template: string, prompt template
//
// Response:
//   - 201: skill created successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
func (h *SkillHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse workspace ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// parse request body
	var req createSkillRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// input validation
	if err := validateCreateSkill(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to create the skill
	skillSvc := service.NewSkillService(h.Svc)
	skill, err := skillSvc.Create(r.Context(), buildCreateSkillParams(
		workspaceID, req.Name, req.Description, req.Category, req.PromptTemplate,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, skillResponse(skill))
}

// ListSkills handles the GET /workspaces/{workspaceId}/skills endpoint, listing all skills under the workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the skill list
//   - 400: invalid workspace ID
func (h *SkillHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// call service to query skills
	skillSvc := service.NewSkillService(h.Svc)
	skills, err := skillSvc.List(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	items := make([]apitypes.SkillResponse, 0, len(skills))
	for _, skill := range skills {
		items = append(items, skillResponse(skill))
	}
	response.JSON(w, r, items)
}

// DeleteSkill handles the DELETE /workspaces/{workspaceId}/skills/{id} endpoint, deleting the specified skill entry.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: deleted successfully
//   - 400: invalid skill ID
//   - 401: not authenticated
//   - 403: no permission
//   - 404: skill does not exist
func (h *SkillHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse skill ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid skill id")
		return
	}

	// verify the skill belongs to the current workspace
	if checkSkillWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// call service to delete the skill
	skillSvc := service.NewSkillService(h.Svc)
	if err := skillSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// updateSkillRequest update skill request body.
type updateSkillRequest struct {
	Name           *string `json:"name,omitempty"`            // skill name (nil=keep)
	Description    *string `json:"description,omitempty"`     // skill description (nil=keep)
	Category       *string `json:"category,omitempty"`        // skill category (nil=keep)
	PromptTemplate *string `json:"prompt_template,omitempty"` // prompt template (nil=keep)
}

// UpdateSkill handles the PUT /workspaces/{workspaceId}/skills/{id} endpoint, updating the skill's configuration info.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, skill name
//   - description: string, skill description
//   - category: string, skill category
//   - prompt_template: string, prompt template
//
// Response:
//   - 200: successfully returns the updated skill info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
//   - 404: skill does not exist
func (h *SkillHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse skill ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid skill id")
		return
	}

	// verify the skill belongs to the current workspace
	if checkSkillWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// parse request body
	var req updateSkillRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// input validation
	if err := validateUpdateSkill(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to update the skill
	skillSvc := service.NewSkillService(h.Svc)
	skill, err := skillSvc.Update(r.Context(), id, req.Name, req.Description, req.Category, req.PromptTemplate)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, skillResponse(skill))
}

// ---- input validation functions ----

// validateCreateSkill validates the input legality of the create skill request.
func validateCreateSkill(req createSkillRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(req.Name) > 200 {
		return fmt.Errorf("name must be at most 200 characters")
	}
	if len(req.PromptTemplate) > 50000 {
		return fmt.Errorf("prompt_template must be at most 50000 characters")
	}
	return nil
}

// validateUpdateSkill validates the input legality of the update skill request.
func validateUpdateSkill(req updateSkillRequest) error {
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return fmt.Errorf("name must not be empty")
		}
		if len(*req.Name) > 200 {
			return fmt.Errorf("name must be at most 200 characters")
		}
	}
	if req.PromptTemplate != nil && len(*req.PromptTemplate) > 50000 {
		return fmt.Errorf("prompt_template must be at most 50000 characters")
	}
	return nil
}
