// skill_dto.go provides parameter builders and response conversion functions for skill.go.
package handler

import (
	"database/sql"

	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// ---- parameter builders ----

// buildCreateSkillParams builds apitypes.CreateSkillParams from request fields.
func buildCreateSkillParams(
	workspaceID uuid.UUID,
	name string,
	description string,
	category string,
	promptTemplate string,
) apitypes.CreateSkillParams {
	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	var catPtr *string
	if category != "" {
		c := category
		catPtr = &c
	}
	var ptPtr *string
	if promptTemplate != "" {
		p := promptTemplate
		ptPtr = &p
	}
	return apitypes.CreateSkillParams{
		WorkspaceID:    workspaceID.String(),
		Name:           name,
		Description:    descPtr,
		Category:       catPtr,
		PromptTemplate: ptPtr,
	}
}

// ---- response conversion functions ----

// skillResponse converts a domain Skill to an API response.
func skillResponse(skill Skill) apitypes.SkillResponse {
	id, _ := uuid.Parse(skill.ID)
	wsID, _ := uuid.Parse(skill.WorkspaceID)
	return apitypes.SkillResponse{
		ID:             id,
		WorkspaceID:    wsID,
		Name:           skill.Name,
		Description:    skill.Description,
		Category:       skill.Category,
		PromptTemplate: skill.PromptTemplate,
		CreatedAt:      skill.CreatedAt,
	}
}

// nullStringValue converts a sql.NullString to a plain string (still depended on by mcp_dto.go).
func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
