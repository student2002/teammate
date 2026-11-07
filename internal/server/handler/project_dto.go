// project_dto.go defines request/response structs and data conversion functions related to Project.
package handler

import (
	"time"

	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// --- request DTOs ---

// createProjectRequest create project request body.
type createProjectRequest struct {
	Name        string `json:"name"`         // project name
	Description string `json:"description"`  // project description
	Icon        string `json:"icon"`         // project icon
	Status      string `json:"status"`       // project status
	RepoUrl     string `json:"repo_url"`     // Git repository URL
	Context     string `json:"context"`      // project context
}

// updateProjectRequest update project request body. All fields are pointers:
// nil keeps the stored value, a provided value overwrites it (partial-update
// semantics owned by the server, identical for every caller).
type updateProjectRequest struct {
	Name        *string `json:"name"`        // project name
	Description *string `json:"description"` // project description
	Status      *string `json:"status"`      // project status
	RepoUrl     *string `json:"repo_url"`    // Git repository URL
	Context     *string `json:"context"`     // project context
}

// addProjectMemberRequest add project member request body.
type addProjectMemberRequest struct {
	MemberType string     `json:"member_type"` // member type (human/agent)
	AgentID    *uuid.UUID `json:"agent_id"`    // Agent ID (required when type is Agent)
	MemberID   *uuid.UUID `json:"member_id"`   // human user ID (required when type is human)
	Role       string     `json:"role"`        // project role
}

// addProjectReviewerRequest add project reviewer request body.
type addProjectReviewerRequest struct {
	MemberType string     `json:"member_type"` // member type (human/agent)
	AgentID    *uuid.UUID `json:"agent_id"`    // Agent ID
	MemberID   *uuid.UUID `json:"member_id"`   // human user ID
}

// createGitCredentialRequest create Git credential request body.
type createGitCredentialRequest struct {
	AgentID  *uuid.UUID `json:"agent_id"` // bound member agent (immutable after creation)
	Username string     `json:"username"` // username
	PAT      string     `json:"pat"`      // personal access token
	// PATType indicates the token type: "fine_grained" (repository scope) or "classic" (account scope).
	// "fine_grained" is recommended for least-privilege access.
	PATType string `json:"pat_type"`
}

// updateGitCredentialRequest update Git credential request body. The agent
// binding is immutable, so only the username and PAT can be changed.
type updateGitCredentialRequest struct {
	Username string `json:"username"` // username
	PAT      string `json:"pat"`      // new personal access token (empty keeps the stored one)
}

// --- response DTOs ---

// encryptedCredential encrypted credential response body (Agent path).
// repo_url is the owning project's repository URL: the credential itself no
// longer stores one, and the agentd contract expects the field.
type encryptedCredential struct {
	ID           uuid.UUID `json:"id"`
	RepoUrl      string    `json:"repo_url"`
	Username     string    `json:"username"`
	EncryptedPAT string    `json:"encrypted_pat"`
}

// maskedCredential masked credential response body (human user path).
type maskedCredential struct {
	ID        uuid.UUID  `json:"id"`
	ProjectID uuid.UUID  `json:"project_id"`
	AgentID   uuid.UUID  `json:"agent_id"`
	AgentName string     `json:"agent_name"`
	Username  string     `json:"username"`
	MaskedPAT string     `json:"pat_masked"`
	CreatedBy *uuid.UUID `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// credentialResponse Git credential create/update response body.
type credentialResponse struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	AgentName    string     `json:"agent_name"`
	Username     string     `json:"username"`
	MaskedPAT    string     `json:"pat_masked"`
	ScopeWarning string     `json:"scope_warning,omitempty"`
	CreatedBy    *uuid.UUID `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Project alias for project (domain type).
type Project = apitypes.Project

// ProjectStatus alias for project status (domain type string).
type ProjectStatus = string

// project status constants.
const (
	ProjectStatusPlanned   = apitypes.ProjectStatusPlanned
	ProjectStatusActive    = apitypes.ProjectStatusActive
	ProjectStatusPaused    = apitypes.ProjectStatusPaused
	ProjectStatusCompleted = apitypes.ProjectStatusCompleted
	ProjectStatusArchived  = apitypes.ProjectStatusArchived
)

// buildCreateProjectParams builds types.CreateProjectParams from handler-layer input.
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

// buildUpdateProjectParams builds types.UpdateProjectParams from handler-layer input.
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

// buildCreateProjectMemberParams builds types.CreateProjectMemberParams from handler-layer input.
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

// buildCreateProjectReviewerParams builds types.CreateProjectReviewerParams from handler-layer input.
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

// buildCreateGitCredentialParams builds types.CreateGitCredentialParams from handler-layer input.
func buildCreateGitCredentialParams(
	projectID uuid.UUID,
	agentID uuid.UUID,
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
		AgentID:      agentID.String(),
		Username:     username,
		EncryptedPAT: encryptedPat,
		CreatedBy:    cb,
	}
}

// buildUpdateGitCredentialParams builds types.UpdateGitCredentialParams from handler-layer input.
func buildUpdateGitCredentialParams(
	id uuid.UUID,
	username string,
	encryptedPat string,
) apitypes.UpdateGitCredentialParams {
	return apitypes.UpdateGitCredentialParams{
		ID:           id.String(),
		Username:     username,
		EncryptedPAT: encryptedPat,
	}
}
