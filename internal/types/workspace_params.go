// workspace_params.go 定义 Workspace/Member 领域操作的领域参数结构体。
//
// 这些结构体是 sqlc 生成的 db.XxxParams 的 domain 对应物，
// 字段一一对应，类型按 domain 风格映射：
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - pqtype.NullRawMessage → json.RawMessage
package types

import "time"

// CreateMemberParams 是创建成员的领域参数结构体。
type CreateMemberParams struct {
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	PasswordHash string   `json:"password_hash"`
	Role        string    `json:"role"`
}

// CreateWorkspaceParams 是创建工作区的领域参数结构体。
type CreateWorkspaceParams struct {
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	IssuePrefix string    `json:"issue_prefix"`
	IsDefault   bool      `json:"is_default"`
}

// CreateWorkspaceMemberParams 是创建工作区成员关联的领域参数结构体。
type CreateWorkspaceMemberParams struct {
	WorkspaceID string    `json:"workspace_id"`
	MemberID    string    `json:"member_id"`
	Role        string    `json:"role"`
}

// DeleteWorkspaceMemberParams 是删除工作区成员关联的领域参数结构体。
type DeleteWorkspaceMemberParams struct {
	WorkspaceID string    `json:"workspace_id"`
	MemberID    string    `json:"member_id"`
}

// GetWorkspaceMemberParams 是获取工作区成员的领域参数结构体。
type GetWorkspaceMemberParams struct {
	WorkspaceID string    `json:"workspace_id"`
	MemberID    string    `json:"member_id"`
}

// GetWorkspaceMemberRoleParams 是查询工作区成员角色的领域参数结构体。
type GetWorkspaceMemberRoleParams struct {
	WorkspaceID string    `json:"workspace_id"`
	MemberID    string    `json:"member_id"`
}

// UpdateMemberPasswordHashParams 是更新成员密码哈希的领域参数结构体。
type UpdateMemberPasswordHashParams struct {
	ID           string    `json:"id"`
	PasswordHash string    `json:"password_hash"`
}

// UpdateMemberRoleParams 是更新成员角色的领域参数结构体。
type UpdateMemberRoleParams struct {
	WorkspaceID string    `json:"workspace_id"`
	MemberID    string    `json:"member_id"`
	Role        string    `json:"role"`
}

// UpdateWorkspaceParams 是更新工作区的领域参数结构体。
type UpdateWorkspaceParams struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	IssuePrefix string    `json:"issue_prefix"`
	UpdatedAt   time.Time `json:"updated_at"`
}
