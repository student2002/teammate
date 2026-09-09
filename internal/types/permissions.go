// permissions.go defines permission constants, role definitions, and role hierarchy helper functions.
//
// This file contains:
//   - Agent permission constants (Perm*) and predefined roles
//   - Workspace member role constants (MemberRole*) and hierarchy functions
//   - Project role constants (ProjectRole*) and hierarchy functions
//
// The permission system is based on fine-grained string identifiers (e.g. "task:claim"),
// supporting authorization by resource type and ID.
// Permissions are divided into two categories:
//   - Default-granted: automatically granted when an Agent is created (task:claim, task:execute, task:comment, memory:read)
//   - Manual-grant: explicitly granted by an admin (task:approve, git:push, etc.)
//
// Predefined Agent roles:
//   - developer: development role, can claim/execute tasks and push code
//   - reviewer: review role, can claim/execute tasks and approve/reject
//   - ops: operations role, has full operational permissions
package types

// Permission constant definitions — used by the RequireAccess middleware and the agent_permissions table.
// Centrally defined to catch typos at compile time.
const (
	PermTaskClaim      = "task:claim"       // Claim a pending node
	PermTaskExecute    = "task:execute"     // Execute a node task
	PermTaskApprove    = "task:approve"     // Approve node completion
	PermTaskReject     = "task:reject"      // Reject a node
	PermTaskComment    = "task:comment"     // Post a task comment
	PermMemoryCreate   = "memory:create"    // Create a knowledge memory
	PermMemoryRead     = "memory:read"      // Read knowledge memories
	PermGitPush        = "git:push"         // Push code to a remote repository
	PermGitForcePush   = "git:force-push"   // Force-push code
	PermResourceDelete = "resource:delete"  // Delete a resource
	PermConfigModify   = "config:modify"    // Modify configuration
)

// DefaultAgentPermissions is the set of permissions automatically granted to a newly created Agent.
//
// It includes basic operation permissions that do not require manual grant by an admin.
var DefaultAgentPermissions = []string{
	PermTaskClaim,
	PermTaskExecute,
	PermTaskComment,
	PermMemoryRead,
}

// DeniedByDefaultAgentPermissions is the set of permissions not granted by default; they require manual authorization.
//
// It includes sensitive operation permissions that require explicit grant by an admin to use.
var DeniedByDefaultAgentPermissions = []string{
	PermTaskApprove,
	PermTaskReject,
	PermMemoryCreate,
	PermGitPush,
	PermGitForcePush,
	PermResourceDelete,
	PermConfigModify,
}

// AllAgentPermissions returns all known Agent permissions (default + manual-grant).
//
// Returns:
//   - []string: list of all permission strings
func AllAgentPermissions() []string {
	all := make([]string, 0, len(DefaultAgentPermissions)+len(DeniedByDefaultAgentPermissions))
	all = append(all, DefaultAgentPermissions...)
	all = append(all, DeniedByDefaultAgentPermissions...)
	return all
}

// IsValidAgentPermission checks whether the permission string is a known Agent permission.
//
// Parameters:
//   - perm: permission string
//
// Returns:
//   - bool: whether it is a valid permission
func IsValidAgentPermission(perm string) bool {
	for _, p := range AllAgentPermissions() {
		if p == perm {
			return true
		}
	}
	return false
}

// AgentRole defines a named set of permissions for an Agent role.
//
// A role bundles multiple permissions into a named set for easy management and assignment.
type AgentRole struct {
	Name        string   `json:"name"`        // Role name (e.g. "developer")
	Description string   `json:"description"` // Role description
	Permissions []string `json:"permissions"` // List of permissions held by this role
}

// Predefined Agent role definitions.
var (
	// AgentRoleDeveloper developer role: can claim/execute tasks, comment, create memories, and push code
	AgentRoleDeveloper = AgentRole{
		Name:        "developer",
		Description: "Can claim, execute, comment on tasks, create memories, and push code",
		Permissions: []string{
			PermTaskClaim,
			PermTaskExecute,
			PermTaskComment,
			PermMemoryRead,
			PermMemoryCreate,
			PermGitPush,
		},
	}

	// AgentRoleReviewer reviewer role: can claim/execute tasks, comment, and approve/reject reviews
	AgentRoleReviewer = AgentRole{
		Name:        "reviewer",
		Description: "Can claim, execute, comment on tasks, and approve/reject reviews",
		Permissions: []string{
			PermTaskClaim,
			PermTaskExecute,
			PermTaskComment,
			PermMemoryRead,
			PermTaskApprove,
			PermTaskReject,
		},
	}

	// AgentRoleOps ops role: has full operational permissions
	AgentRoleOps = AgentRole{
		Name:        "ops",
		Description: "Full operational access including force push, resource deletion, and config modification",
		Permissions: []string{
			PermTaskClaim,
			PermTaskExecute,
			PermTaskComment,
			PermMemoryRead,
			PermMemoryCreate,
			PermGitPush,
			PermGitForcePush,
			PermResourceDelete,
			PermConfigModify,
		},
	}

	// AgentRoles maps role names to their definitions.
	AgentRoles = map[string]AgentRole{
		AgentRoleDeveloper.Name: AgentRoleDeveloper,
		AgentRoleReviewer.Name:  AgentRoleReviewer,
		AgentRoleOps.Name:       AgentRoleOps,
	}
)

// ListAgentRoles returns a list of all predefined Agent roles.
//
// Returns:
//   - []AgentRole: role list
func ListAgentRoles() []AgentRole {
	roles := make([]AgentRole, 0, len(AgentRoles))
	for _, r := range AgentRoles {
		roles = append(roles, r)
	}
	return roles
}

// ---------------------------------------------------------------------------
// Workspace member roles
// ---------------------------------------------------------------------------

// MemberRole defines workspace member roles.
//
// Role hierarchy: owner > admin > member > viewer
const (
	MemberRoleOwner  = "owner"  // Owner (all permissions)
	MemberRoleAdmin  = "admin"  // Admin (member management + all operations)
	MemberRoleMember = "member" // Regular member (create/edit operations)
	MemberRoleViewer = "viewer" // Read-only member
)

// ProjectRole — role hierarchy: lead > developer > reviewer.
const (
	ProjectRoleLead      = "lead"      // Project lead (project management, config modification)
	ProjectRoleDeveloper = "developer" // Developer (task operations)
	ProjectRoleReviewer  = "reviewer"  // Reviewer (review operations)
)

// MemberRoleLevel returns the hierarchy value of a member role; a higher value means higher permissions.
//
// Note: the caller must first check claims.UserType; Agents use an independent permission system (agent_permissions table).
//
// Parameters:
//   - role: role name
//
// Returns:
//   - int: hierarchy value (owner=4, admin=3, member=2, viewer=1)
func MemberRoleLevel(role string) int {
	switch role {
	case "owner":
		return 4
	case "admin":
		return 3
	case "member":
		return 2
	case "agent":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}

// ProjectRoleLevel returns the numeric level of a project role (higher value means higher permissions).
//
// Parameters:
//   - role: role name
//
// Returns:
//   - int: hierarchy value (lead=3, developer=2, reviewer=1)
func ProjectRoleLevel(role string) int {
	switch role {
	case ProjectRoleLead:
		return 3
	case ProjectRoleDeveloper:
		return 2
	case ProjectRoleReviewer:
		return 1
	default:
		return 0
	}
}
