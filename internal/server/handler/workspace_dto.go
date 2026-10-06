// workspace_dto.go defines Workspace-related request/response structs and data conversion functions.
package handler

import (
	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// --- request DTOs ---

// createWorkspaceRequest is the create workspace request body.
type createWorkspaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IssuePrefix string `json:"issue_prefix"`
}

// updateWorkspaceRequest is the update workspace request body. Fields are
// pointers: a nil field keeps the stored value (partial update).
type updateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// createMemberRequest is the create member request body.
type createMemberRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// updateMemberRoleRequest is the update member role request body.
type updateMemberRoleRequest struct {
	Role string `json:"role"`
}

// buildCreateWorkspaceParams builds types.CreateWorkspaceParams from the handler-layer input.
// An empty issuePrefix falls back to the schema-level default "MUL": the
// params struct always writes the column, so the DB default never applies.
func buildCreateWorkspaceParams(
	name string,
	description string,
	issuePrefix string,
	isDefault bool,
) apitypes.CreateWorkspaceParams {
	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	if issuePrefix == "" {
		issuePrefix = "MUL"
	}
	return apitypes.CreateWorkspaceParams{
		Name:        name,
		Description: descPtr,
		IssuePrefix: issuePrefix,
		IsDefault:   isDefault,
	}
}

// buildUpdateWorkspaceParams builds types.UpdateWorkspaceParams from the
// partial request fields merged over the stored workspace: nil request fields
// keep the current values.
func buildUpdateWorkspaceParams(
	id uuid.UUID,
	current apitypes.Workspace,
	req updateWorkspaceRequest,
) apitypes.UpdateWorkspaceParams {
	name := current.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := current.Description
	if req.Description != nil {
		description = *req.Description
	}
	var descPtr *string
	if description != "" {
		descPtr = &description
	}
	return apitypes.UpdateWorkspaceParams{
		ID:          id.String(),
		Name:        name,
		Description: descPtr,
	}
}

// buildUpdateMemberRoleParams builds types.UpdateMemberRoleParams from the handler-layer input.
func buildUpdateMemberRoleParams(
	workspaceID uuid.UUID,
	role string,
	memberID uuid.UUID,
) apitypes.UpdateMemberRoleParams {
	return apitypes.UpdateMemberRoleParams{
		WorkspaceID: workspaceID.String(),
		MemberID:    memberID.String(),
		Role:        role,
	}
}
