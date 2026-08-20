// project_dto.go 定义 Project 相关的请求/响应结构体和数据转换函数。
package handler

import (
	"time"

	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// --- 请求 DTO ---

// createProjectRequest 创建项目请求体。
type createProjectRequest struct {
	Name        string `json:"name"`         // 项目名称
	Description string `json:"description"`  // 项目描述
	Icon        string `json:"icon"`         // 项目图标
	Status      string `json:"status"`       // 项目状态
	RepoUrl     string `json:"repo_url"`     // Git 仓库 URL
	Context     string `json:"context"`      // 项目上下文
}

// updateProjectRequest 更新项目请求体。
type updateProjectRequest struct {
	Name        string `json:"name"`        // 项目名称
	Description string `json:"description"` // 项目描述
	Status      string `json:"status"`      // 项目状态
	RepoUrl     string `json:"repo_url"`    // Git 仓库 URL
	Context     string `json:"context"`     // 项目上下文
}

// addProjectMemberRequest 添加项目成员请求体。
type addProjectMemberRequest struct {
	MemberType string     `json:"member_type"` // 成员类型（human/agent）
	AgentID    *uuid.UUID `json:"agent_id"`    // Agent ID（Agent 类型时必填）
	MemberID   *uuid.UUID `json:"member_id"`   // 人类用户 ID（human 类型时必填）
	Role       string     `json:"role"`        // 项目角色
}

// addProjectReviewerRequest 添加项目审查者请求体。
type addProjectReviewerRequest struct {
	MemberType string     `json:"member_type"` // 成员类型（human/agent）
	AgentID    *uuid.UUID `json:"agent_id"`    // Agent ID
	MemberID   *uuid.UUID `json:"member_id"`   // 人类用户 ID
}

// createGitCredentialRequest 创建 Git 凭据请求体。
type createGitCredentialRequest struct {
	RepoUrl  string `json:"repo_url"`  // Git 仓库 URL
	Username string `json:"username"`  // 用户名
	PAT      string `json:"pat"`       // 个人访问 Token
	// PATType 指示 Token 类型："fine_grained"（仓库范围）或 "classic"（账户范围）。
	// 建议使用 "fine_grained" 以获得最小权限访问。
	PATType string `json:"pat_type"`
}

// updateGitCredentialRequest 更新 Git 凭据请求体。
type updateGitCredentialRequest struct {
	RepoUrl  string `json:"repo_url"`  // Git 仓库 URL
	Username string `json:"username"`  // 用户名
	PAT      string `json:"pat"`       // 新的个人访问 Token
}

// --- 响应 DTO ---

// encryptedCredential 加密凭据响应体（Agent 路径）。
type encryptedCredential struct {
	ID           uuid.UUID `json:"id"`
	RepoUrl      string    `json:"repo_url"`
	Username     string    `json:"username"`
	EncryptedPAT string    `json:"encrypted_pat"`
}

// maskedCredential 脱敏凭据响应体（人类用户路径）。
type maskedCredential struct {
	ID        uuid.UUID  `json:"id"`
	RepoUrl   string     `json:"repo_url"`
	Username  string     `json:"username"`
	MaskedPAT string     `json:"masked_pat"`
	CreatedBy *uuid.UUID `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// credentialResponse Git 凭据创建/更新响应体。
type credentialResponse struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	RepoUrl      string     `json:"repo_url"`
	Username     string     `json:"username"`
	MaskedPAT    string     `json:"masked_pat"`
	ScopeWarning string     `json:"scope_warning,omitempty"`
	CreatedBy    *uuid.UUID `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Project 项目的别名（领域类型）。
type Project = apitypes.Project

// ProjectStatus 项目状态的别名（领域类型 string）。
type ProjectStatus = string

// 项目状态常量。
const (
	ProjectStatusPlanned = apitypes.ProjectStatusPlanned
)

// buildCreateProjectParams 根据 handler 层输入构造 types.CreateProjectParams。
func buildCreateProjectParams(
	workspaceID uuid.UUID,
	name string,
	description string,
	icon string,
	status string,
	repoUrl string,
	context string,
) apitypes.CreateProjectParams {
	desc := description
	iconV := icon
	repo := repoUrl
	ctx := context
	return apitypes.CreateProjectParams{
		WorkspaceID: workspaceID.String(),
		Name:        name,
		Description: &desc,
		Icon:        &iconV,
		Status:      status,
		RepoURL:     &repo,
		Context:     &ctx,
	}
}

// buildUpdateProjectParams 根据 handler 层输入构造 types.UpdateProjectParams。
func buildUpdateProjectParams(
	id uuid.UUID,
	name string,
	description string,
	status string,
	repoUrl string,
	context string,
) apitypes.UpdateProjectParams {
	desc := description
	repo := repoUrl
	ctx := context
	return apitypes.UpdateProjectParams{
		ID:          id.String(),
		Name:        name,
		Description: &desc,
		Status:      status,
		RepoURL:     &repo,
		Context:     &ctx,
	}
}

// buildCreateProjectMemberParams 根据 handler 层输入构造 types.CreateProjectMemberParams。
func buildCreateProjectMemberParams(
	projectID uuid.UUID,
	memberType string,
	agentID uuid.NullUUID,
	memberID uuid.NullUUID,
	role string,
) apitypes.CreateProjectMemberParams {
	var agentStr *string
	if agentID.Valid {
		s := agentID.UUID.String()
		agentStr = &s
	}
	var memberStr *string
	if memberID.Valid {
		s := memberID.UUID.String()
		memberStr = &s
	}
	return apitypes.CreateProjectMemberParams{
		ProjectID:  projectID.String(),
		MemberType: memberType,
		AgentID:    agentStr,
		MemberID:   memberStr,
		Role:       role,
	}
}

// buildCreateProjectReviewerParams 根据 handler 层输入构造 types.CreateProjectReviewerParams。
func buildCreateProjectReviewerParams(
	projectID uuid.UUID,
	memberType string,
	agentID uuid.NullUUID,
	memberID uuid.NullUUID,
) apitypes.CreateProjectReviewerParams {
	var agentStr *string
	if agentID.Valid {
		s := agentID.UUID.String()
		agentStr = &s
	}
	var memberStr *string
	if memberID.Valid {
		s := memberID.UUID.String()
		memberStr = &s
	}
	return apitypes.CreateProjectReviewerParams{
		ProjectID:  projectID.String(),
		MemberType: memberType,
		AgentID:    agentStr,
		MemberID:   memberStr,
	}
}

// buildCreateGitCredentialParams 根据 handler 层输入构造 types.CreateGitCredentialParams。
func buildCreateGitCredentialParams(
	projectID uuid.UUID,
	repoUrl string,
	username string,
	encryptedPat string,
	createdBy uuid.NullUUID,
) apitypes.CreateGitCredentialParams {
	var cb *string
	if createdBy.Valid {
		s := createdBy.UUID.String()
		cb = &s
	}
	return apitypes.CreateGitCredentialParams{
		ProjectID:    projectID.String(),
		RepoURL:      repoUrl,
		Username:     username,
		EncryptedPAT: encryptedPat,
		CreatedBy:    cb,
	}
}

// buildUpdateGitCredentialParams 根据 handler 层输入构造 types.UpdateGitCredentialParams。
func buildUpdateGitCredentialParams(
	id uuid.UUID,
	repoUrl string,
	username string,
	encryptedPat string,
) apitypes.UpdateGitCredentialParams {
	return apitypes.UpdateGitCredentialParams{
		ID:           id.String(),
		RepoURL:      repoUrl,
		Username:     username,
		EncryptedPAT: encryptedPat,
	}
}
