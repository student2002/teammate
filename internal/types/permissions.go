// permissions.go 定义权限常量、角色定义和角色层级辅助函数。
//
// 本文件包含：
//   - Agent 权限常量（Perm*）和预定义角色
//   - 工作区成员角色常量（MemberRole*）及层级函数
//   - 项目角色常量（ProjectRole*）及层级函数
//
// 权限系统基于细粒度的字符串标识（如 "task:claim"），支持按资源类型和 ID 授权。
// 权限分为两类：
//   - 默认授予：创建 Agent 时自动授予（task:claim, task:execute, task:comment, memory:read）
//   - 需手动授权：管理员显式授予（task:approve, git:push 等）
//
// 预定义 Agent 角色：
//   - developer: 开发角色，可认领执行任务、推送代码
//   - reviewer: 审查角色，可认领执行任务、审批驳回
//   - ops: 运维角色，拥有全部操作权限
package types

// 权限常量定义 —— 用于 RequireAccess 中间件和 agent_permissions 表。
// 集中定义以在编译期捕获拼写错误。
const (
	PermTaskClaim      = "task:claim"       // 认领待处理节点
	PermTaskExecute    = "task:execute"     // 执行节点任务
	PermTaskApprove    = "task:approve"     // 审批节点完成
	PermTaskReject     = "task:reject"      // 驳回节点
	PermTaskComment    = "task:comment"     // 发表任务评论
	PermMemoryCreate   = "memory:create"    // 创建知识记忆
	PermMemoryRead     = "memory:read"      // 读取知识记忆
	PermGitPush        = "git:push"         // 推送代码到远程仓库
	PermGitForcePush   = "git:force-push"   // 强制推送代码
	PermResourceDelete = "resource:delete"  // 删除资源
	PermConfigModify   = "config:modify"    // 修改配置
)

// DefaultAgentPermissions 是新创建 Agent 默认授予的权限集合。
//
// 包含基础的操作权限，无需管理员手动授予。
var DefaultAgentPermissions = []string{
	PermTaskClaim,
	PermTaskExecute,
	PermTaskComment,
	PermMemoryRead,
}

// DeniedByDefaultAgentPermissions 是默认不授予的权限集合，需手动授权。
//
// 包含敏感操作权限，需要管理员显式授予才能使用。
var DeniedByDefaultAgentPermissions = []string{
	PermTaskApprove,
	PermTaskReject,
	PermMemoryCreate,
	PermGitPush,
	PermGitForcePush,
	PermResourceDelete,
	PermConfigModify,
}

// AllAgentPermissions 返回所有已知的 Agent 权限（默认 + 需手动授权）。
//
// 返回：
//   - []string: 所有权限字符串列表
func AllAgentPermissions() []string {
	all := make([]string, 0, len(DefaultAgentPermissions)+len(DeniedByDefaultAgentPermissions))
	all = append(all, DefaultAgentPermissions...)
	all = append(all, DeniedByDefaultAgentPermissions...)
	return all
}

// IsValidAgentPermission 检查权限字符串是否是已知的 Agent 权限。
//
// 参数：
//   - perm: 权限字符串
//
// 返回：
//   - bool: 是否是有效权限
func IsValidAgentPermission(perm string) bool {
	for _, p := range AllAgentPermissions() {
		if p == perm {
			return true
		}
	}
	return false
}

// AgentRole 定义 Agent 角色的命名权限集合。
//
// 角色将多个权限打包为一个命名集合，便于管理和分配。
type AgentRole struct {
	Name        string   `json:"name"`        // 角色名称（如 "developer"）
	Description string   `json:"description"` // 角色描述
	Permissions []string `json:"permissions"` // 该角色拥有的权限列表
}

// 预定义的 Agent 角色定义。
var (
	// AgentRoleDeveloper 开发角色：可认领执行任务、评论、创建记忆、推送代码
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

	// AgentRoleReviewer 审查角色：可认领执行任务、评论、审批驳回
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

	// AgentRoleOps 运维角色：拥有全部操作权限
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

	// AgentRoles 将角色名映射到其定义。
	AgentRoles = map[string]AgentRole{
		AgentRoleDeveloper.Name: AgentRoleDeveloper,
		AgentRoleReviewer.Name:  AgentRoleReviewer,
		AgentRoleOps.Name:       AgentRoleOps,
	}
)

// ListAgentRoles 返回所有预定义的 Agent 角色列表。
//
// 返回：
//   - []AgentRole: 角色列表
func ListAgentRoles() []AgentRole {
	roles := make([]AgentRole, 0, len(AgentRoles))
	for _, r := range AgentRoles {
		roles = append(roles, r)
	}
	return roles
}

// ---------------------------------------------------------------------------
// 工作区成员角色
// ---------------------------------------------------------------------------

// MemberRole 定义工作区成员角色。
//
// 角色层级：owner > admin > member > viewer
const (
	MemberRoleOwner  = "owner"  // 所有者（全部权限）
	MemberRoleAdmin  = "admin"  // 管理员（成员管理 + 全部操作）
	MemberRoleMember = "member" // 普通成员（创建/编辑操作）
	MemberRoleViewer = "viewer" // 只读成员
)

// ProjectRole —— 角色层级：lead > developer > reviewer。
const (
	ProjectRoleLead      = "lead"      // 项目负责人（项目管理、配置修改）
	ProjectRoleDeveloper = "developer" // 开发者（任务操作）
	ProjectRoleReviewer  = "reviewer"  // 审查者（审查操作）
)

// MemberRoleLevel 返回成员角色的层级数值，值越大权限越高。
//
// 注意：调用方必须先检查 claims.UserType，Agent 使用独立的权限系统（agent_permissions 表）。
//
// 参数：
//   - role: 角色名称
//
// 返回：
//   - int: 层级数值（owner=4, admin=3, member=2, viewer=1）
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

// ProjectRoleLevel 返回项目角色的数值等级（值越大权限越高）。
//
// 参数：
//   - role: 角色名称
//
// 返回：
//   - int: 层级数值（lead=3, developer=2, reviewer=1）
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
