// types.go 定义项目中所有共享的业务枚举常量和核心领域结构体。
//
// 本文件包含：
//   - 枚举常量（Agent 提供商、状态、节点类型、任务类型等）
//   - 核心业务结构体（Workspace、Agent、Task、TaskNode 等）
//
// 其他共享定义已拆分至同包的专用文件：
//   - permissions.go：权限常量和角色定义
//   - events.go：SSE 事件常量和结构体
//   - errors.go：API 错误码和错误响应
//   - dto.go：API 请求/响应结构体
//   - comment.go：评论类型常量
//
// 所有类型定义供 handler、service、CLI 共享使用。
package types

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// 枚举常量定义（字符串类型）
// ---------------------------------------------------------------------------

// AgentProvider 定义 Agent 支持的 AI 提供商。
const (
	AgentProviderClaude   = "claude"    // Claude（Anthropic）
	AgentProviderOpenClaw = "openclaw"  // OpenClaw
	AgentProviderOpenCode = "opencode"  // OpenCode
	AgentProviderAtomCode = "atomcode"  // AtomCode
	AgentProviderMiMoCode = "mimocode"  // MiMoCode
	AgentProviderCopilot  = "copilot"   // GitHub Copilot
	AgentProviderHermes   = "hermes"    // Hermes
	AgentProviderGemini   = "gemini"    // Gemini（Google）
	AgentProviderPi       = "pi"        // Pi
	AgentProviderCursor   = "cursor"    // Cursor
	AgentProviderKimi     = "kimi"      // Kimi
	AgentProviderKiro     = "kiro"      // Kiro
)

// AgentStatus 定义 Agent 的运行状态。
const (
	AgentStatusOnline  = "online"  // 在线
	AgentStatusOffline = "offline" // 离线
	AgentStatusBusy    = "busy"    // 忙碌（正在执行任务）
	AgentStatusPaused  = "paused"  // 暂停
)

// NodeType 定义工作流节点的类型。
const (
	NodeTypeStandard = "standard" // 标准节点（AI 代理执行）
	NodeTypeReview   = "review"   // 审查节点（AI 或人类审查）
	NodeTypeManual   = "manual"   // 手动节点（必须由人类执行）
)

// AssigneeType 定义节点的执行者类型。
const (
	AssigneeTypeAnyAgent      = "any_agent"      // 任意 Agent（可认领）
	AssigneeTypeSpecificAgent = "specific_agent"  // 指定 Agent
	AssigneeTypeHuman         = "human"          // 人类用户
	AssigneeTypeAuto          = "auto"           // 自动分配
)

// TaskType 定义任务的类型分类。
const (
	TaskTypeStory = "story" // 用户故事
	TaskTypeBug   = "bug"   // Bug 修复
	TaskTypeTask  = "task"  // 通用任务
)

// TaskPriority 定义任务的优先级。
const (
	TaskPriorityUrgent = "urgent" // 紧急
	TaskPriorityHigh   = "high"   // 高
	TaskPriorityMedium = "medium" // 中
	TaskPriorityLow    = "low"    // 低
)

// TaskStatus 定义任务的状态。
const (
	TaskStatusActive    = "active"    // 活跃
	TaskStatusCompleted = "completed" // 已完成
	TaskStatusCancelled = "cancelled" // 已取消
)

// TaskNodeStatus 定义工作流节点的状态。
//
// 节点真实状态（5 种）：
//   - pending: 待处理
//   - in_progress: 进行中
//   - completed: 已完成
//   - rejected: 已退回
//   - manual_intervention: 需人工介入
const (
	TaskNodeStatusPending            = "pending"             // 待处理
	TaskNodeStatusInProgress         = "in_progress"         // 进行中
	TaskNodeStatusCompleted          = "completed"           // 已完成
	TaskNodeStatusRejected           = "rejected"            // 已退回
	TaskNodeStatusManualIntervention = "manual_intervention" // 需人工介入
)

// TransitionAction 定义节点状态流转的操作类型。
const (
	TransitionActionApprove      = "approve"       // 批准
	TransitionActionReject       = "reject"        // 驳回
	TransitionActionManual       = "manual"        // 手动介入
	TransitionActionReclaim      = "reclaim"       // 重新认领
	TransitionActionTimeout      = "timeout"       // 超时
	TransitionActionInterruptAck = "interrupt_ack" // 中断确认
)

// ProjectStatus 定义项目的状态。
const (
	ProjectStatusPlanned   = "planned"   // 计划中
	ProjectStatusActive    = "active"    // 活跃
	ProjectStatusPaused    = "paused"    // 暂停
	ProjectStatusCompleted = "completed" // 已完成
	ProjectStatusArchived  = "archived"  // 已归档
)

// RuntimeStatus 定义 Runtime 的状态。
const (
	RuntimeStatusOnline  = "online"  // 在线
	RuntimeStatusOffline = "offline" // 离线
	RuntimeStatusError   = "error"   // 错误
)

// TokenType 定义认证 Token 的类型。
const (
	TokenTypeAPI     = "api"     // API Token（长期有效）
	TokenTypeSession = "session" // 会话 Token（7天有效）
	TokenTypeTask    = "task"    // 任务 Token
)

// McpAuthType 定义 MCP 服务器的认证类型。
const (
	McpAuthTypeNone   = "none"    // 无认证
	McpAuthTypeAPIKey = "api_key" // API Key 认证
	McpAuthTypeOAuth  = "oauth"   // OAuth 认证
)

// MemoryType 定义记忆条目的类型分类。
const (
	MemoryTypeArchitecture = "architecture" // 架构决策
	MemoryTypeCommand      = "command"      // 命令参考
	MemoryTypeConvention   = "convention"   // 编码约定
	MemoryTypeDecision     = "decision"     // 技术决策
	MemoryTypeInsight      = "insight"      // 洞察发现
	MemoryTypeEnvironment  = "environment"  // 环境配置
)

// ---------------------------------------------------------------------------
// 核心业务结构体
// ---------------------------------------------------------------------------

// Workspace 表示团队工作区。
//
// 工作区是顶级组织单元，包含项目、Agent、成员、工作流模板。
type Workspace struct {
	ID          string    `json:"id" db:"id"`                     // 工作区 UUID
	Name        string    `json:"name" db:"name"`                 // 工作区名称
	Description string    `json:"description" db:"description"`   // 描述
	IssuePrefix string    `json:"issue_prefix" db:"issue_prefix"` // Issue 前缀（如 "TM"）
	IsDefault   bool      `json:"is_default" db:"is_default"`     // 是否为默认工作区
	CreatedAt   time.Time `json:"created_at" db:"created_at"`     // 创建时间
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`     // 更新时间
}

// Member 表示工作区中的人类成员。
type Member struct {
	ID          string    `json:"id" db:"id"`                    // 成员 UUID
	WorkspaceID string    `json:"workspace_id" db:"workspace_id"` // 工作区 ID
	Name        string    `json:"name" db:"name"`                // 名称
	Email       string    `json:"email" db:"email"`              // 邮箱
	Role        string    `json:"role" db:"role"`                // 角色
	CreatedAt   time.Time `json:"created_at" db:"created_at"`    // 创建时间
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`    // 更新时间
}

// Agent 表示工作区中的 AI 代理。
//
// Agent 可以认领和执行任务节点，支持多种 AI 提供商。
type Agent struct {
	ID            string          `json:"id" db:"id"`                      // Agent UUID
	WorkspaceID   string          `json:"workspace_id" db:"workspace_id"`   // 工作区 ID
	Name          string          `json:"name" db:"name"`                  // 名称
	Provider      string          `json:"provider" db:"provider"`          // AI 提供商
	Instructions  string          `json:"instructions" db:"instructions"`  // 系统指令
	Model         string          `json:"model" db:"model"`                // 使用的模型
	Status        string          `json:"status" db:"status"`              // 运行状态
	CustomEnv     json.RawMessage `json:"custom_env" db:"custom_env"`      // 自定义环境变量（JSON）
	ExtraArgs     []string        `json:"extra_args" db:"extra_args"`      // 额外命令行参数
	TotalCompleted int            `json:"total_completed" db:"total_completed"` // 总完成任务数
	TotalTokens   int64           `json:"total_tokens" db:"total_tokens"`   // 总 Token 用量
	GitName       string          `json:"git_name" db:"git_name"`          // Git 提交用户名
	GitEmail      string          `json:"git_email" db:"git_email"`        // Git 提交邮箱
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`      // 创建时间
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`      // 更新时间
}

// WorkflowTemplate 表示可复用的工作流定义模板。
//
// 模板定义了任务执行的有序步骤，如 [需求分析 → 编码 → 审查 → 部署]。
type WorkflowTemplate struct {
	ID              string          `json:"id" db:"id"`                       // 模板 UUID
	WorkspaceID     string          `json:"workspace_id" db:"workspace_id"`   // 工作区 ID
	Name            string          `json:"name" db:"name"`                   // 模板名称
	Description     string          `json:"description" db:"description"`     // 描述
	IsBuiltin       bool            `json:"is_builtin" db:"is_builtin"`       // 是否为内置模板
	TriggerType     string          `json:"trigger_type" db:"trigger_type"`   // 触发器类型
	TriggerConfig   json.RawMessage `json:"trigger_config" db:"trigger_config"` // 触发器配置（JSON）
	TriggerEnabled  bool            `json:"trigger_enabled" db:"trigger_enabled"` // 触发器是否启用
	NextRunAt       *time.Time      `json:"next_run_at" db:"next_run_at"`     // 下次运行时间
	LastTriggeredAt *time.Time      `json:"last_triggered_at" db:"last_triggered_at"` // 上次触发时间
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`       // 创建时间
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`       // 更新时间
}

// WorkflowTriggerRun 表示一次工作流触发器执行记录。
type WorkflowTriggerRun struct {
	ID                 string          `json:"id" db:"id"`                          // 运行记录 UUID
	WorkspaceID        string          `json:"workspace_id" db:"workspace_id"`      // 工作区 ID
	ProjectID          string          `json:"project_id" db:"project_id"`          // 项目 ID
	WorkflowTemplateID string          `json:"workflow_template_id" db:"workflow_template_id"` // 模板 ID
	TriggerType        string          `json:"trigger_type" db:"trigger_type"`      // 触发器类型
	ExternalKey        string          `json:"external_key" db:"external_key"`      // 外部去重键
	Status             string          `json:"status" db:"status"`                  // 状态
	TaskID             *int32          `json:"task_id" db:"task_id"`                // 关联任务 ID
	Payload            json.RawMessage `json:"payload" db:"payload"`                // 触发负载（JSON）
	Error              string          `json:"error" db:"error"`                    // 错误信息
	CreatedAt          time.Time       `json:"created_at" db:"created_at"`          // 创建时间
}

// WorkflowTemplateNode 表示工作流模板中的单个步骤节点。
type WorkflowTemplateNode struct {
	ID              string          `json:"id" db:"id"`                        // 节点 UUID
	TemplateID      string          `json:"template_id" db:"template_id"`      // 所属模板 ID
	Name            string          `json:"name" db:"name"`                    // 节点名称
	Description     string          `json:"description" db:"description"`      // 描述
	SortOrder       int             `json:"sort_order" db:"sort_order"`        // 排序顺序
	NodeType        string          `json:"node_type" db:"node_type"`          // 节点类型
	AssigneeType    string          `json:"assignee_type" db:"assignee_type"`  // 分配者类型
	AssigneeID      *string         `json:"assignee_id" db:"assignee_id"`      // 指定分配者 ID
	TimeoutMinutes  int             `json:"timeout_minutes" db:"timeout_minutes"` // 超时时间（分钟）
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs" db:"readonly_dirs"`   // 只读目录（JSON）
	FullControlDirs json.RawMessage `json:"full_control_dirs" db:"full_control_dirs"` // 完全控制目录（JSON）
	Artifact        json.RawMessage `json:"artifact" db:"artifact"`            // 产物定义（JSON）
	DependsOn       []string        `json:"depends_on" db:"depends_on"`        // 依赖节点 ID 列表
	MaxRejectCycles int             `json:"max_reject_cycles" db:"max_reject_cycles"` // 最大驳回轮次
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`        // 创建时间
}

// Project 表示工作区内的一个软件项目。
type Project struct {
	ID                 string    `json:"id" db:"id"`                               // 项目 UUID
	WorkspaceID        string    `json:"workspace_id" db:"workspace_id"`            // 工作区 ID
	Name               string    `json:"name" db:"name"`                           // 项目名称
	Description        string    `json:"description" db:"description"`             // 描述
	Icon               string    `json:"icon" db:"icon"`                           // 图标
	Status             string    `json:"status" db:"status"`                       // 项目状态
	RepoURL            string    `json:"repo_url" db:"repo_url"`                   // 代码仓库 URL
	Context            string    `json:"context" db:"context"`                     // 项目上下文描述
	DefaultWorkflowID  *string   `json:"default_workflow_id" db:"default_workflow_id"` // 默认工作流模板 ID
	CreatedAt          time.Time `json:"created_at" db:"created_at"`               // 创建时间
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`               // 更新时间
}

// ProjectMember 表示项目与成员（Agent 或人类）的关联关系。
type ProjectMember struct {
	ID         string    `json:"id" db:"id"`                    // 关联记录 UUID
	ProjectID  string    `json:"project_id" db:"project_id"`    // 项目 ID
	MemberType string    `json:"member_type" db:"member_type"`  // 成员类型（"agent" 或 "member"）
	AgentID    *string   `json:"agent_id" db:"agent_id"`        // Agent ID（member_type=agent 时）
	MemberID   *string   `json:"member_id" db:"member_id"`      // 人类成员 ID（member_type=member 时）
	Role       string    `json:"role" db:"role"`                // 项目角色
	CreatedAt  time.Time `json:"created_at" db:"created_at"`    // 创建时间
}

// Task 表示项目中的一个工作单元。
//
// 任务由工作流模板实例化为有序的工作流节点。
type Task struct {
	ID           int32      `json:"id" db:"id"`                     // 任务 ID（自增整数）
	ProjectID    string     `json:"project_id" db:"project_id"`     // 所属项目 ID
	WorkflowName string     `json:"workflow_name" db:"workflow_name"` // 工作流名称
	Title        string     `json:"title" db:"title"`               // 任务标题
	Description  string     `json:"description" db:"description"`   // 任务描述
	Constraints  string     `json:"constraints" db:"constraints"`   // 约束条件
	Type         string     `json:"type" db:"type"`                 // 任务类型（story/bug/task）
	Priority     string     `json:"priority" db:"priority"`         // 优先级
	Status       string     `json:"status" db:"status"`             // 任务状态
	AuthorType   string     `json:"author_type" db:"author_type"`   // 作者类型
	AuthorID     string     `json:"author_id" db:"author_id"`       // 作者 ID
	DueDate      *time.Time `json:"due_date" db:"due_date"`          // 截止日期
	Labels       []string   `json:"labels" db:"labels"`              // 标签列表
	Sequence     int        `json:"sequence" db:"sequence"`          // 排序序号
	ParentTaskID *int32     `json:"parent_task_id" db:"parent_task_id"` // 父任务 ID（子任务时）
	GitBranch    *string    `json:"git_branch" db:"git_branch"`      // 关联的 Git 分支
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`      // 创建时间
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`      // 更新时间
}

// TaskNode 表示任务工作流执行中的单个步骤。
//
// 节点状态机：pending → in_progress → completed/rejected/manual_intervention
type TaskNode struct {
	ID                   string     `json:"id" db:"id"`                                     // 节点 UUID
	TaskID               int32      `json:"task_id" db:"task_id"`                           // 所属任务 ID
	Name                 string     `json:"name" db:"name"`                                 // 节点名称
	Description          string     `json:"description" db:"description"`                   // 描述
	SortOrder            int        `json:"sort_order" db:"sort_order"`                     // 排序顺序
	NodeType             string     `json:"node_type" db:"node_type"`                       // 节点类型
	Status               string     `json:"status" db:"status"`                             // 节点状态
	AssigneeType         string     `json:"assignee_type" db:"assignee_type"`               // 分配者类型
	AssigneeID           *string    `json:"assignee_id" db:"assignee_id"`                   // 分配者 ID
	ReservedForAgentID   *string    `json:"reserved_for_agent_id" db:"reserved_for_agent_id"` // 保留给特定 Agent（续约权）
	RejectCount          int        `json:"reject_count" db:"reject_count"`                 // 驳回次数
	Version              int        `json:"version" db:"version"`                           // 乐观锁版本号
	MaxRejectCycles      int        `json:"max_reject_cycles" db:"max_reject_cycles"`       // 最大驳回轮次
	TimeoutMinutes       int        `json:"timeout_minutes" db:"timeout_minutes"`           // 超时时间（分钟）
	CompletedAt          *time.Time `json:"completed_at" db:"completed_at"`                 // 完成时间
	CompletedBy          *string    `json:"completed_by" db:"completed_by"`                 // 完成者 ID
	Summary              string     `json:"summary" db:"summary"`                           // 节点执行摘要
	PreviousSummary      string     `json:"previous_summary" db:"previous_summary"`         // 上一次执行摘要
	ReservationExpiresAt *time.Time `json:"reservation_expires_at" db:"reservation_expires_at"` // 预留过期时间
	ReadonlyDirs         json.RawMessage `json:"readonly_dirs" db:"readonly_dirs"`               // 只读目录（JSON 数组，来自模板节点）
	FullControlDirs      json.RawMessage `json:"full_control_dirs" db:"full_control_dirs"`       // 完全控制目录（JSON 数组，来自模板节点）
	DependsOn            []string   `json:"depends_on" db:"depends_on"`                     // 依赖节点 ID 列表
	CreatedAt            time.Time  `json:"created_at" db:"created_at"`                     // 创建时间
	UpdatedAt            time.Time  `json:"updated_at" db:"updated_at"`                     // 更新时间
}

// NodeTransition 记录工作流节点的状态流转历史。
//
// 每次节点状态变更都会创建一条流转记录，用于审计和调试。
type NodeTransition struct {
	ID           string    `json:"id" db:"id"`                     // 流转记录 UUID
	TaskNodeID   string    `json:"task_node_id" db:"task_node_id"` // 节点 ID
	FromStatus   string    `json:"from_status" db:"from_status"`   // 源状态
	ToStatus     string    `json:"to_status" db:"to_status"`       // 目标状态
	Action       string    `json:"action" db:"action"`             // 操作类型
	TargetNodeID *string   `json:"target_node_id" db:"target_node_id"` // 目标节点 ID（驳回时）
	Comment      string    `json:"comment" db:"comment"`           // 操作评论
	OperatorID   *string   `json:"operator_id" db:"operator_id"`   // 操作者 ID
	OperatorType string    `json:"operator_type" db:"operator_type"` // 操作者类型
	CreatedAt    time.Time `json:"created_at" db:"created_at"`     // 创建时间
}

// Comment 表示任务上的一条评论。
type Comment struct {
	ID           string          `json:"id" db:"id"`                     // 评论 UUID
	TaskID       int32           `json:"task_id" db:"task_id"`           // 所属任务 ID
	NodeID       *string         `json:"node_id" db:"node_id"`           // 关联节点 ID（节点评论时）
	SourceNodeID *string         `json:"source_node_id" db:"source_node_id"` // 来源节点 ID（交接评论时）
	ParentID     *string         `json:"parent_id" db:"parent_id"`       // 父评论 ID（回复时）
	AuthorType   string          `json:"author_type" db:"author_type"`   // 作者类型
	AuthorID     string          `json:"author_id" db:"author_id"`       // 作者 ID
	Content      string          `json:"content" db:"content"`           // 评论内容
	CommentType  string          `json:"comment_type" db:"comment_type"` // 评论类型
	Metadata     json.RawMessage `json:"metadata" db:"metadata"`         // 扩展元数据（JSON）
	Mentions     []string        `json:"mentions" db:"mentions"`         // 提及列表（UUID 数组）
	EditedAt     *time.Time      `json:"edited_at" db:"edited_at"`       // 编辑时间
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`     // 创建时间
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`     // 更新时间
}

// Runtime 表示正在运行的 Agent 守护进程实例。
//
// 每个 Agent 可以有多个 Runtime（部署在不同机器上），
// 通过心跳维持在线状态。
type Runtime struct {
	ID                string     `json:"id" db:"id"`                           // Runtime UUID
	AgentID           string     `json:"agent_id" db:"agent_id"`               // 所属 Agent ID
	DaemonID          string     `json:"daemon_id" db:"daemon_id"`             // 守护进程 ID
	Provider          string     `json:"provider" db:"provider"`               // AI 提供商
	Version           string     `json:"version" db:"version"`                 // 守护进程版本
	Status            string     `json:"status" db:"status"`                   // 运行状态
	SessionTokenHash  string     `json:"session_token_hash" db:"session_token_hash"` // 会话 Token 哈希
	SessionExpiresAt  *time.Time `json:"session_expires_at" db:"session_expires_at"` // 会话过期时间
	PublicKey         string     `json:"public_key" db:"public_key"`           // RSA 公钥
	LastHeartbeat     *time.Time `json:"last_heartbeat" db:"last_heartbeat"`   // 最后心跳时间
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`           // 创建时间
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`           // 更新时间
}

// Skill 表示可复用的技能定义。
//
// 技能是 Agent 可以使用的专业能力，如代码审查、测试编写等。
type Skill struct {
	ID            string    `json:"id" db:"id"`                      // 技能 UUID
	WorkspaceID   string    `json:"workspace_id" db:"workspace_id"`   // 工作区 ID
	Name          string    `json:"name" db:"name"`                  // 技能名称
	Description   string    `json:"description" db:"description"`    // 描述
	Category      string    `json:"category" db:"category"`          // 分类
	PromptTemplate string   `json:"prompt_template" db:"prompt_template"` // 提示词模板
	CreatedAt     time.Time `json:"created_at" db:"created_at"`      // 创建时间
}

// McpServer 表示 MCP 服务器配置。
//
// MCP（Model Context Protocol）服务器为 Agent 提供外部工具和数据源。
type McpServer struct {
	ID          string          `json:"id" db:"id"`                    // 服务器 UUID
	WorkspaceID string          `json:"workspace_id" db:"workspace_id"` // 工作区 ID
	Name        string          `json:"name" db:"name"`                // 服务器名称
	URL         string          `json:"url" db:"url"`                  // 服务器 URL
	Type        string          `json:"type" db:"type"`                // 服务器类型
	AuthType    string          `json:"auth_type" db:"auth_type"`      // 认证类型
	EnvVars     json.RawMessage `json:"env_vars" db:"env_vars"`        // 环境变量（JSON）
	Status      string          `json:"status" db:"status"`            // 服务器状态
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`    // 创建时间
}

// AgentSkill 表示 Agent 与技能的关联关系。
type AgentSkill struct {
	AgentID   string    `json:"agent_id" db:"agent_id"`   // Agent ID
	SkillID   string    `json:"skill_id" db:"skill_id"`   // 技能 ID
	Enabled   bool      `json:"enabled" db:"enabled"`     // 是否启用
	CreatedAt time.Time `json:"created_at" db:"created_at"` // 创建时间
}

// AgentMcpServer 表示 Agent 与 MCP 服务器的关联关系。
type AgentMcpServer struct {
	AgentID     string    `json:"agent_id" db:"agent_id"`         // Agent ID
	McpServerID string    `json:"mcp_server_id" db:"mcp_server_id"` // MCP 服务器 ID
	Enabled     bool      `json:"enabled" db:"enabled"`           // 是否启用
	CreatedAt   time.Time `json:"created_at" db:"created_at"`     // 创建时间
}

// TokenUsage 记录工作流节点执行过程中的 Token 消耗量。
type TokenUsage struct {
	ID           int64          `json:"id"`             // 记录 ID
	TaskNodeID   string         `json:"task_node_id"`   // 节点 ID
	AgentID      string         `json:"agent_id"`       // Agent ID
	InputTokens  int32          `json:"input_tokens"`   // 输入 Token 数
	OutputTokens int32          `json:"output_tokens"`  // 输出 Token 数
	TotalTokens  int32          `json:"total_tokens"`   // 总 Token 数
	CostEstimate *string        `json:"cost_estimate"`  // 费用估算（可选）
	CreatedAt    time.Time      `json:"created_at"`     // 创建时间
}

// AuthToken 表示认证令牌。
//
// 支持三种类型：api（长期 API Token）、session（短期会话 Token）、task（任务 Token）。
type AuthToken struct {
	ID        string     `json:"id" db:"id"`                   // Token 记录 UUID
	TokenHash string     `json:"token_hash" db:"token_hash"`   // Token 哈希（bcrypt）
	TokenType string     `json:"token_type" db:"token_type"`   // Token 类型
	OwnerType string     `json:"owner_type" db:"owner_type"`   // 所有者类型
	OwnerID   string     `json:"owner_id" db:"owner_id"`       // 所有者 ID
	RuntimeID *string    `json:"runtime_id" db:"runtime_id"`   // 关联的 Runtime ID
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`   // 过期时间
	CreatedAt time.Time  `json:"created_at" db:"created_at"`   // 创建时间
}

// GitCredential 存储加密的 Git PAT，用于仓库访问认证。
type GitCredential struct {
	ID           string     `json:"id" db:"id"`                     // 凭据 UUID
	ProjectID    string     `json:"project_id" db:"project_id"`     // 所属项目 ID
	RepoURL      string     `json:"repo_url" db:"repo_url"`         // 仓库 URL
	Username     string     `json:"username" db:"username"`         // 用户名
	EncryptedPAT string     `json:"encrypted_pat" db:"encrypted_pat"` // 加密的 PAT
	CreatedBy    *string    `json:"created_by" db:"created_by"`     // 创建者 ID
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`     // 创建时间
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`     // 更新时间
}

// Memory 表示工作区的知识记忆条目。
//
// 记忆支持文本搜索和语义搜索（pgvector），用于 Agent 学习和参考。
type Memory struct {
	ID           string          `json:"id" db:"id"`                     // 记忆 UUID
	WorkspaceID  string          `json:"workspace_id" db:"workspace_id"`  // 工作区 ID
	SourceTaskID *int32          `json:"source_task_id" db:"source_task_id"` // 来源任务 ID
	Type         string          `json:"type" db:"type"`                 // 记忆类型
	Title        string          `json:"title" db:"title"`               // 标题
	Content      string          `json:"content" db:"content"`           // 内容
	Tags         []string        `json:"tags" db:"tags"`                 // 标签列表
	Confidence   float64         `json:"confidence" db:"confidence"`     // 置信度（0-1）
	Verified     bool            `json:"verified" db:"verified"`         // 是否已验证
	Stale        bool            `json:"stale" db:"stale"`               // 是否已过期
	Metadata     json.RawMessage `json:"metadata" db:"metadata"`         // 扩展元数据（JSON）
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`     // 创建时间
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`     // 更新时间
}

// CommunityWorkflow 表示来自社区的共享工作流。
type CommunityWorkflow struct {
	ID                           string          `json:"id" db:"id"`                               // 工作流 UUID
	Name                         string          `json:"name" db:"name"`                           // 名称
	Description                  string          `json:"description" db:"description"`             // 描述
	Author                       string          `json:"author" db:"author"`                       // 作者
	Version                      string          `json:"version" db:"version"`                     // 版本
	WorkflowDefinition           json.RawMessage `json:"workflow_definition" db:"workflow_definition"` // 工作流定义（JSON）
	RequiredSkills               json.RawMessage `json:"required_skills" db:"required_skills"`     // 所需技能（JSON）
	RequiredMcpServers           json.RawMessage `json:"required_mcp_servers" db:"required_mcp_servers"` // 所需 MCP 服务器（JSON）
	RecommendedAgentInstructions json.RawMessage `json:"recommended_agent_instructions" db:"recommended_agent_instructions"` // 推荐 Agent 指令（JSON）
	Downloads                    int             `json:"downloads" db:"downloads"`                 // 下载次数
	IsOfficial                   bool            `json:"is_official" db:"is_official"`             // 是否为官方
	CreatedAt                    time.Time       `json:"created_at" db:"created_at"`               // 创建时间
	UpdatedAt                    time.Time       `json:"updated_at" db:"updated_at"`               // 更新时间
}

// ProjectReviewer 表示项目的审查者指派。
type ProjectReviewer struct {
	ID         string    `json:"id" db:"id"`                   // 记录 UUID
	ProjectID  string    `json:"project_id" db:"project_id"`   // 项目 ID
	MemberType string    `json:"member_type" db:"member_type"` // 成员类型
	AgentID    *string   `json:"agent_id" db:"agent_id"`       // Agent ID
	MemberID   *string   `json:"member_id" db:"member_id"`     // 人类成员 ID
	CreatedAt  time.Time `json:"created_at" db:"created_at"`   // 创建时间
}

// ---------------------------------------------------------------------------
// 补齐缺失的实体结构体（对照 db/generated/models.go）
// ---------------------------------------------------------------------------

// AgentPermission 表示授予 Agent 的细粒度权限。
type AgentPermission struct {
	ID           string  `json:"id" db:"id"`                          // 记录 UUID
	AgentID      string  `json:"agent_id" db:"agent_id"`              // Agent ID
	Permission   string  `json:"permission" db:"permission"`          // 权限名称
	ResourceType string  `json:"resource_type" db:"resource_type"`    // 资源类型
	ResourceID   *string `json:"resource_id" db:"resource_id"`        // 资源 ID（可选）
	GrantedBy    *string `json:"granted_by" db:"granted_by"`          // 授权者 ID（可选）
	CreatedAt    time.Time `json:"created_at" db:"created_at"`        // 创建时间
}

// AuditLog 表示系统审计日志条目，记录用户/Agent 的关键操作。
type AuditLog struct {
	ID           int64           `json:"id" db:"id"`                              // 日志 ID
	WorkspaceID  string          `json:"workspace_id" db:"workspace_id"`         // 工作区 ID
	ActorType    string          `json:"actor_type" db:"actor_type"`             // 操作者类型
	ActorID      string          `json:"actor_id" db:"actor_id"`                 // 操作者 ID
	Action       string          `json:"action" db:"action"`                     // 操作类型
	ResourceType string          `json:"resource_type" db:"resource_type"`       // 资源类型
	ResourceID   string          `json:"resource_id" db:"resource_id"`           // 资源 ID
	Details      json.RawMessage `json:"details" db:"details"`                   // 操作详情（JSON）
	IPAddress    string          `json:"ip_address" db:"ip_address"`             // 请求来源 IP
	UserAgent    *string         `json:"user_agent" db:"user_agent"`             // 客户端 User-Agent
	RequestID    *string         `json:"request_id" db:"request_id"`             // 请求追踪 ID
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`             // 创建时间
}

// ExecutionSession 表示一次节点执行会话，记录 Agent 执行节点时的运行时信息。
type ExecutionSession struct {
	ID              string     `json:"id" db:"id"`                              // 会话 UUID
	RuntimeID       *string    `json:"runtime_id" db:"runtime_id"`              // 关联的 Runtime ID
	AgentID         *string    `json:"agent_id" db:"agent_id"`                  // 执行 Agent ID
	TaskNodeID      string     `json:"task_node_id" db:"task_node_id"`          // 执行的节点 ID
	Attempt         int32      `json:"attempt" db:"attempt"`                    // 尝试次数
	Status          string     `json:"status" db:"status"`                      // 会话状态
	Workdir         *string    `json:"workdir" db:"workdir"`                    // 工作目录
	Branch          *string    `json:"branch" db:"branch"`                      // Git 分支
	BaseCommit      *string    `json:"base_commit" db:"base_commit"`            // 起始 commit
	HeadCommit      *string    `json:"head_commit" db:"head_commit"`            // 最新 commit
	ClaudeSessionID *string    `json:"claude_session_id" db:"claude_session_id"` // Claude session ID
	StartedAt       time.Time  `json:"started_at" db:"started_at"`              // 开始时间
	CompletedAt     *time.Time `json:"completed_at" db:"completed_at"`          // 完成时间
	InterruptedAt   *time.Time `json:"interrupted_at" db:"interrupted_at"`      // 中断时间
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`              // 创建时间
}

// Invitation 表示工作区邀请，通过邮件发送给待加入的成员。
type Invitation struct {
	ID          string     `json:"id" db:"id"`                       // 邀请 UUID
	WorkspaceID string     `json:"workspace_id" db:"workspace_id"`   // 目标工作区 ID
	Email       string     `json:"email" db:"email"`                 // 被邀请者邮箱
	Role        string     `json:"role" db:"role"`                   // 邀请的角色
	TokenHash   string     `json:"token_hash" db:"token_hash"`       // 邀请令牌哈希
	InvitedBy   *string    `json:"invited_by" db:"invited_by"`       // 邀请发起者 ID
	ExpiresAt   time.Time  `json:"expires_at" db:"expires_at"`       // 邀请过期时间
	AcceptedAt  *time.Time `json:"accepted_at" db:"accepted_at"`     // 接受时间
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`       // 创建时间
}

// SseEventBuffer 表示 SSE 事件缓冲区条目，用于断线重连时补发丢失的事件。
type SseEventBuffer struct {
	ID        int64           `json:"id" db:"id"`                       // 缓冲条目 ID
	RuntimeID string          `json:"runtime_id" db:"runtime_id"`      // 目标 Runtime ID
	EventType string          `json:"event_type" db:"event_type"`      // 事件类型
	EventData json.RawMessage `json:"event_data" db:"event_data"`      // 事件数据（JSON）
	CreatedAt time.Time       `json:"created_at" db:"created_at"`      // 创建时间
}

// TaskLog 表示任务执行日志条目，记录节点执行过程中的关键事件。
type TaskLog struct {
	ID        string    `json:"id" db:"id"`                  // 日志 UUID
	TaskID    int32     `json:"task_id" db:"task_id"`        // 所属任务 ID
	NodeID    string    `json:"node_id" db:"node_id"`        // 关联节点 ID
	Type      string    `json:"type" db:"type"`              // 日志类型
	Content   string    `json:"content" db:"content"`        // 日志内容
	Timestamp time.Time `json:"timestamp" db:"timestamp"`    // 日志时间戳
	CreatedAt time.Time `json:"created_at" db:"created_at"`  // 创建时间
}

// TaskLogChunk 表示任务日志的分块数据，支持大日志的分块上传。
type TaskLogChunk struct {
	ID         int64     `json:"id" db:"id"`                    // 分块 ID
	TaskNodeID string    `json:"task_node_id" db:"task_node_id"` // 关联节点 ID
	ChunkIndex int32     `json:"chunk_index" db:"chunk_index"`   // 分块序号
	Data       []byte    `json:"data" db:"data"`                 // 分块数据
	Size       int32     `json:"size" db:"size"`                 // 分块大小（字节）
	UploadedAt time.Time `json:"uploaded_at" db:"uploaded_at"`   // 上传时间
}

// WorkspaceMember 表示工作区成员关联记录。
type WorkspaceMember struct {
	ID          string    `json:"id" db:"id"`                       // 记录 UUID
	WorkspaceID string    `json:"workspace_id" db:"workspace_id"`   // 工作区 ID
	MemberID    string    `json:"member_id" db:"member_id"`         // 成员 ID
	Role        string    `json:"role" db:"role"`                   // 成员在工作区中的角色
	CreatedAt   time.Time `json:"created_at" db:"created_at"`       // 创建时间
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`       // 更新时间
}
