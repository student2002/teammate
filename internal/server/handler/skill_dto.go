// skill_dto.go 为 skill.go 提供参数构建器和响应转换函数。
package handler

import (
	"database/sql"

	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// ---- 参数构建器 ----

// buildCreateSkillParams 从请求字段构建 apitypes.CreateSkillParams。
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

// ---- 响应转换函数 ----

// skillResponse 将 domain Skill 转换为 API 响应。
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

// nullStringValue 将 sql.NullString 转换为普通字符串（mcp_dto.go 仍依赖）。
func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
