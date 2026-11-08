// workflow_dto.go defines Workflow-related request/response structs and data conversion functions.
package handler

import (
	"time"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// templateResponse is the workflow template response DTO.
type templateResponse struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsBuiltin   bool      `json:"is_builtin"`
	Version     int       `json:"version"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// templateGraphResponse is the response struct for a workflow template with
// its nodes and edges ({template, nodes, edges} per the V2 API contract).
type templateGraphResponse struct {
	Template templateResponse             `json:"template"`
	Nodes    []types.WorkflowTemplateNode `json:"nodes"`
	Edges    []types.WorkflowTemplateEdge `json:"edges"`
}

// templateGraphRequest is the full-graph write payload shared by create and
// update; node/edge IDs are supplied by the client and stay stable across saves.
type templateGraphRequest struct {
	Name        string                           `json:"name"`
	Description *string                          `json:"description"`
	Enabled     bool                             `json:"enabled"`
	Nodes       []types.CreateTemplateNodeParams `json:"nodes"`
	Edges       []types.CreateTemplateEdgeParams `json:"edges"`
}

// toTemplateResponse converts a domain workflow template record to an API response.
func toTemplateResponse(t types.WorkflowTemplate) templateResponse {
	return templateResponse{
		ID:          t.ID,
		WorkspaceID: t.WorkspaceID,
		Name:        t.Name,
		Description: t.Description,
		IsBuiltin:   t.IsBuiltin,
		Version:     t.Version,
		Enabled:     t.Enabled,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// toTemplateGraphResponse converts a service detail to the API response.
func toTemplateGraphResponse(d service.WorkflowTemplateDetail) templateGraphResponse {
	if d.Nodes == nil {
		d.Nodes = []types.WorkflowTemplateNode{}
	}
	if d.Edges == nil {
		d.Edges = []types.WorkflowTemplateEdge{}
	}
	return templateGraphResponse{
		Template: toTemplateResponse(d.Template),
		Nodes:    d.Nodes,
		Edges:    d.Edges,
	}
}
