// community.go provides HTTP API endpoints for creating, listing, and importing community workflow templates.
//
// Community workflow templates are globally shared and can be imported into any workspace.

package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// CommunityHandler handles HTTP requests related to community workflow templates, including creating, querying, and importing community workflows.
type CommunityHandler struct {
	Svc *service.Service
}

// NewCommunityHandler creates a CommunityHandler instance.
func NewCommunityHandler(svc *service.Service) *CommunityHandler {
	return &CommunityHandler{Svc: svc}
}

// Routes returns the route table for community workflows.
func (h *CommunityHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateCommunityWorkflow)
	r.Get("/", h.ListCommunityWorkflows)
	r.Post("/{id}/import", h.ImportCommunityWorkflow)

	return r
}

// createCommunityWorkflowRequest create community workflow request body.
type createCommunityWorkflowRequest struct {
	Name                         string          `json:"name"`                          // workflow name
	Description                  string          `json:"description"`                   // workflow description
	Author                       string          `json:"author"`                        // author
	Version                      string          `json:"version"`                       // version number
	WorkflowDefinition           json.RawMessage `json:"workflow_definition"`            // workflow definition (JSON)
	RequiredSkills               json.RawMessage `json:"required_skills"`               // required skills (JSON)
	RequiredMcpServers           json.RawMessage `json:"required_mcp_servers"`           // required MCP servers (JSON)
	RecommendedAgentInstructions json.RawMessage `json:"recommended_agent_instructions"` // recommended agent instructions (JSON)
	IsOfficial                   bool            `json:"is_official"`                   // whether it is an official template
}

// CreateCommunityWorkflow handles the POST /community-workflows endpoint, creating a new community workflow template.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, workflow name (required)
//   - description: string, workflow description
//   - author: string, author
//   - version: string, version number, default "1.0.0"
//   - workflow_definition: object, workflow definition
//   - required_skills: object, required skills
//   - required_mcp_servers: object, required MCP servers
//   - recommended_agent_instructions: object, recommended agent instructions
//   - is_official: bool, whether it is an official template
//
// Response:
//   - 201: community workflow created successfully
//   - 400: parameter error
func (h *CommunityHandler) CreateCommunityWorkflow(w http.ResponseWriter, r *http.Request) {
	// parse request body
	var req createCommunityWorkflowRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// set default version
	version := req.Version
	if version == "" {
		version = "1.0.0"
	}

	// convert optional JSON fields
	var requiredSkills pqtype.NullRawMessage
	if req.RequiredSkills != nil {
		requiredSkills = pqtype.NullRawMessage{RawMessage: req.RequiredSkills, Valid: true}
	}
	var requiredMcpServers pqtype.NullRawMessage
	if req.RequiredMcpServers != nil {
		requiredMcpServers = pqtype.NullRawMessage{RawMessage: req.RequiredMcpServers, Valid: true}
	}
	var recommendedAgentInstructions pqtype.NullRawMessage
	if req.RecommendedAgentInstructions != nil {
		recommendedAgentInstructions = pqtype.NullRawMessage{RawMessage: req.RecommendedAgentInstructions, Valid: true}
	}

	// call service to create the community workflow
	commSvc := service.NewCommunityService(h.Svc)
	workflow, err := commSvc.Create(r.Context(), buildCreateCommunityWorkflowParams(
		req.Name, req.Description, req.Author, version, req.WorkflowDefinition,
		requiredSkills, requiredMcpServers, recommendedAgentInstructions, req.IsOfficial,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, workflow)
}

// ListCommunityWorkflows handles the GET /community-workflows endpoint, listing all community workflow templates.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the community workflow list
func (h *CommunityHandler) ListCommunityWorkflows(w http.ResponseWriter, r *http.Request) {
	// call service to query community workflows
	commSvc := service.NewCommunityService(h.Svc)
	workflows, err := commSvc.List(r.Context())
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, workflows)
}

// importCommunityWorkflowRequest import community workflow request body.
type importCommunityWorkflowRequest struct {
	WorkspaceID uuid.UUID `json:"workspace_id"` // target workspace ID
}

// ImportCommunityWorkflow handles the POST /community-workflows/{id}/import endpoint, importing a community workflow into the specified workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - workspace_id: UUID, target workspace ID (required)
//
// Response:
//   - 201: imported successfully, returns the created template and the source workflow
//   - 400: parameter error
//   - 404: community workflow does not exist
func (h *CommunityHandler) ImportCommunityWorkflow(w http.ResponseWriter, r *http.Request) {
	// parse workflow ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid workflow id")
		return
	}

	// parse request body
	var req importCommunityWorkflowRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// call service to import the workflow
	commSvc := service.NewCommunityService(h.Svc)
	result, err := commSvc.ImportWorkflow(r.Context(), id, req.WorkspaceID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			response.NotFound(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// return import result
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"template":        result.Template,
		"source_workflow": result.SourceWorkflow,
	})
}
