// workspace_dto.go 定义 Workspace 相关的请求/响应结构体和数据转换函数。
package handler

import (
	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// --- 请求 DTO ---

// createWorkspaceRequest 创建工作区请求体。
type createWorkspaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IssuePrefix string `json:"issue_prefix"`
}

// updateWorkspaceRequest 更新工作区请求体。
type updateWorkspaceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// createMemberRequest 创建成员请求体。
type createMemberRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// updateMemberRoleRequest 更新成员角色请求体。
type updateMemberRoleRequest struct {
	Role string `json:"role"`
}

// buildCreateWorkspaceParams 根据 handler 层输入构造 types.CreateWorkspaceParams。
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
	return apitypes.CreateWorkspaceParams{
		Name:        name,
		Description: descPtr,
		IssuePrefix: issuePrefix,
		IsDefault:   isDefault,
	}
}

// buildUpdateWorkspaceParams 根据 handler 层输入构造 types.UpdateWorkspaceParams。
func buildUpdateWorkspaceParams(
	id uuid.UUID,
	name string,
	description string,
) apitypes.UpdateWorkspaceParams {
	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	return apitypes.UpdateWorkspaceParams{
		ID:          id.String(),
		Name:        name,
		Description: descPtr,
	}
}

// buildUpdateMemberRoleParams 根据 handler 层输入构造 types.UpdateMemberRoleParams。
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
