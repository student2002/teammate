// workspace_params.go defines the domain parameter structs for Workspace/Member domain operations.
//
// These structs are the domain counterparts of the sqlc-generated db.XxxParams,
// with fields mapped one-to-one and types mapped in the domain style:
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - pqtype.NullRawMessage → json.RawMessage
package types

import "time"

// CreateMemberParams is the domain parameter struct for creating a member.
type CreateMemberParams struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	PasswordHash string `json:"password_hash"`
	Role         string `json:"role"`
}

// CreateWorkspaceParams is the domain parameter struct for creating a workspace.
type CreateWorkspaceParams struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IssuePrefix string  `json:"issue_prefix"`
	IsDefault   bool    `json:"is_default"`
}

// CreateWorkspaceMemberParams is the domain parameter struct for creating a workspace-member association.
type CreateWorkspaceMemberParams struct {
	WorkspaceID string `json:"workspace_id"`
	MemberID    string `json:"member_id"`
	Role        string `json:"role"`
}

// DeleteWorkspaceMemberParams is the domain parameter struct for deleting a workspace-member association.
type DeleteWorkspaceMemberParams struct {
	WorkspaceID string `json:"workspace_id"`
	MemberID    string `json:"member_id"`
}

// GetWorkspaceMemberParams is the domain parameter struct for fetching a workspace member.
type GetWorkspaceMemberParams struct {
	WorkspaceID string `json:"workspace_id"`
	MemberID    string `json:"member_id"`
}

// GetWorkspaceMemberRoleParams is the domain parameter struct for querying a workspace member's role.
type GetWorkspaceMemberRoleParams struct {
	WorkspaceID string `json:"workspace_id"`
	MemberID    string `json:"member_id"`
}

// UpdateMemberPasswordHashParams is the domain parameter struct for updating a member's password hash.
type UpdateMemberPasswordHashParams struct {
	ID           string `json:"id"`
	PasswordHash string `json:"password_hash"`
}

// UpdateMemberRoleParams is the domain parameter struct for updating a member's role.
type UpdateMemberRoleParams struct {
	WorkspaceID string `json:"workspace_id"`
	MemberID    string `json:"member_id"`
	Role        string `json:"role"`
}

// UpdateWorkspaceParams is the domain parameter struct for updating a workspace.
type UpdateWorkspaceParams struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	IssuePrefix string    `json:"issue_prefix"`
	UpdatedAt   time.Time `json:"updated_at"`
}
