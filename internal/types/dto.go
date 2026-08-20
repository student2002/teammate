// dto.go 提供 API 层的数据传输对象（DTO），确保 DB 模型与 API 响应解耦。
//
// 本文件包含：
//   - 响应 DTO（*Response）：公开 API 响应，不包含敏感字段
//   - 请求 DTO（*Req）：API 请求体结构
//   - 辅助函数：如脱敏处理
//
// 设计原则：
//   - public DTO 不包含 secret/敏感字段
//   - execution DTO 包含解密后的敏感字段，仅限 agent 自身端点使用
//   - handler 层显式将 DB model 映射为 DTO，避免字段泄漏
package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── 技能 DTO ────────────────────────────────────────────────

// SkillResponse 公开技能响应，不包含内部字段。
type SkillResponse struct {
	ID             uuid.UUID `json:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Category       string    `json:"category,omitempty"`
	PromptTemplate string    `json:"prompt_template,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ─── MCP 服务器 DTO ───────────────────────────────────────────

// McpServerResponse 公开 MCP 服务器响应，env_vars 永远脱敏。
type McpServerResponse struct {
	ID          uuid.UUID              `json:"id"`
	WorkspaceID uuid.UUID              `json:"workspace_id"`
	Name        string                 `json:"name"`
	Url         string                 `json:"url"`
	Type        string                 `json:"type,omitempty"`
	AuthType    string                 `json:"auth_type"`
	EnvVars     map[string]interface{} `json:"env_vars,omitempty"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
}

// McpServerExecutionResponse 执行期 MCP 服务器响应，env_vars 已解密包含明文值。
type McpServerExecutionResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Url        string    `json:"url"`
	Type       string    `json:"type,omitempty"`
	AuthType   string    `json:"auth_type"`
	EnvVars    string    `json:"env_vars,omitempty"`
	Status     string    `json:"status"`
	Enabled    bool      `json:"enabled"`
	AssignedAt time.Time `json:"assigned_at"`
}

// ─── Agent 绑定 DTO ────────────────────────────────────────

// AgentMcpServerBindingResponse Agent-MCP 绑定响应（公开）。
type AgentMcpServerBindingResponse struct {
	AgentID     uuid.UUID `json:"agent_id"`
	McpServerID uuid.UUID `json:"mcp_server_id"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// AgentSkillBindingResponse Agent-Skill 绑定响应（公开）。
type AgentSkillBindingResponse struct {
	AgentID   uuid.UUID `json:"agent_id"`
	SkillID   uuid.UUID `json:"skill_id"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── Agent DTO ────────────────────────────────────────────────

// AgentResponse 公开代理响应，不包含敏感字段（custom_env）。
type AgentResponse struct {
	ID             uuid.UUID `json:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Name           string    `json:"name"`
	Provider       string    `json:"provider"`
	Instructions   string    `json:"instructions"`
	Model          string    `json:"model,omitempty"`
	Status         string    `json:"status"`
	ExtraArgs      []string  `json:"extra_args"`
	GitName        string    `json:"git_name,omitempty"`
	GitEmail       string    `json:"git_email,omitempty"`
	TotalCompleted int       `json:"total_completed"`
	TotalTokens    int64     `json:"total_tokens"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// MaskedEnvVars 返回脱敏后的环境变量映射（所有值替换为 "********"）。
func MaskedEnvVars(raw json.RawMessage) map[string]string {
	masked := make(map[string]string)
	if len(raw) == 0 || string(raw) == "null" {
		return masked
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return masked
	}
	for key := range obj {
		masked[key] = "********"
	}
	return masked
}

// ---------------------------------------------------------------------------
// API 请求类型
// ---------------------------------------------------------------------------

// CreateWorkspaceReq 是创建工作区的请求体。
type CreateWorkspaceReq struct {
	Name        string `json:"name"`         // 工作区名称
	Description string `json:"description"`  // 描述
	IssuePrefix string `json:"issue_prefix"` // Issue 前缀
}

// UpdateWorkspaceReq 是更新工作区的请求体。
type UpdateWorkspaceReq struct {
	Name        string `json:"name"`        // 名称
	Description string `json:"description"` // 描述
}

// CreateProjectReq 是创建项目的请求体。
type CreateProjectReq struct {
	Name              string  `json:"name"`               // 项目名称
	Description       string  `json:"description"`        // 描述
	Visibility        string  `json:"visibility"`         // 可见性
	RepoURL           string  `json:"repo_url"`           // 代码仓库 URL
	DefaultWorkflowID *string `json:"default_workflow_id"` // 默认工作流模板 ID
}

// UpdateProjectReq 是更新项目的请求体。
type UpdateProjectReq struct {
	Name              string  `json:"name"`               // 名称
	Description       string  `json:"description"`        // 描述
	Status            string  `json:"status"`             // 状态
	DefaultWorkflowID *string `json:"default_workflow_id"` // 默认工作流模板 ID
	Context           string  `json:"context"`            // 项目上下文
}

// CreateAgentReq 是创建 Agent 的请求体。
type CreateAgentReq struct {
	Name         string `json:"name"`         // Agent 名称
	Provider     string `json:"provider"`     // AI 提供商
	Instructions string `json:"instructions"` // 系统指令
	Model        string `json:"model"`        // 使用的模型
}

// UpdateAgentReq 是更新 Agent 的请求体。
type UpdateAgentReq struct {
	Instructions string          `json:"instructions"` // 系统指令
	Model        string          `json:"model"`        // 模型
	Status       string          `json:"status"`       // 状态
	CustomEnv    json.RawMessage `json:"custom_env"`   // 自定义环境变量
	ExtraArgs    []string        `json:"extra_args"`   // 额外参数
}

// CreateTaskReq 是创建任务的请求体。
type CreateTaskReq struct {
	Title              string     `json:"title"`               // 任务标题
	Description        string     `json:"description"`         // 任务描述
	Constraints        string     `json:"constraints"`         // 约束条件
	Type               string     `json:"type"`                // 任务类型
	Priority           string     `json:"priority"`            // 优先级
	WorkflowTemplateID string     `json:"workflow_template_id"` // 工作流模板 ID
	DueDate            *time.Time `json:"due_date"`            // 截止日期
	Labels             []string   `json:"labels"`              // 标签
}

// UpdateTaskReq 是更新任务的请求体。
type UpdateTaskReq struct {
	Title       string     `json:"title"`       // 标题
	Description string     `json:"description"` // 描述
	Priority    string     `json:"priority"`    // 优先级
	Labels      []string   `json:"labels"`      // 标签
	DueDate     *time.Time `json:"due_date"`    // 截止日期
	Constraints string     `json:"constraints"` // 约束条件
}

// CreateWorkflowTemplateReq 是创建工作流模板的请求体。
type CreateWorkflowTemplateReq struct {
	Name        string           `json:"name"`        // 模板名称
	Description string           `json:"description"` // 描述
	Nodes       []TemplateNodeDef `json:"nodes"`      // 模板节点列表
}

// TemplateNodeDef 定义工作流模板创建请求中的单个节点。
type TemplateNodeDef struct {
	Name            string          `json:"name"`             // 节点名称
	Description     string          `json:"description"`      // 描述
	SortOrder       int             `json:"sort_order"`       // 排序顺序
	NodeType        string          `json:"node_type"`        // 节点类型
	AssigneeType    string          `json:"assignee_type"`    // 分配者类型
	AssigneeID      *string         `json:"assignee_id"`      // 指定分配者 ID
	TimeoutMinutes  int             `json:"timeout_minutes"`  // 超时时间
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`    // 只读目录
	FullControlDirs json.RawMessage `json:"full_control_dirs"` // 完全控制目录
	Artifact        json.RawMessage `json:"artifact"`         // 产物定义
}

// ClaimNodeReq 是认领工作流节点的请求体。
type ClaimNodeReq struct {
	AgentID string `json:"agent_id"` // Agent ID
}

// ApproveNodeReq 是审批工作流节点的请求体。
type ApproveNodeReq struct {
	Comment string `json:"comment"` // 审批评论
}

// RejectNodeReq 是退回工作流节点的请求体。
type RejectNodeReq struct {
	TargetNodeID string `json:"target_node_id"` // 回退目标节点 ID
	Comment      string `json:"comment"`        // 驳回评论
}

// ManualInterventionReq 是手动介入工作流节点的请求体。
type ManualInterventionReq struct {
	Comment string `json:"comment"` // 介入说明
}

// CreateCommentReq 是创建评论的请求体。
type CreateCommentReq struct {
	Content  string   `json:"content"`  // 评论内容
	Mentions []string `json:"mentions"` // 提及列表
}

// RegisterRuntimeReq 是注册 Runtime 的请求体。
type RegisterRuntimeReq struct {
	DaemonID string `json:"daemon_id"` // 守护进程 ID
	Provider string `json:"provider"`  // AI 提供商
	Version  string `json:"version"`   // 版本
}

// ReportTokenUsageReq 是上报 Token 用量的请求体。
type ReportTokenUsageReq struct {
	TaskNodeID   uuid.UUID `json:"task_node_id"`   // 节点 ID
	InputTokens  int32     `json:"input_tokens"`   // 输入 Token 数
	OutputTokens int32     `json:"output_tokens"`  // 输出 Token 数
	TotalTokens  int32     `json:"total_tokens"`   // 总 Token 数
	CostEstimate string    `json:"cost_estimate"`  // 费用估算
}

// CreateSkillReq 是创建技能的请求体。
type CreateSkillReq struct {
	Name           string `json:"name"`            // 技能名称
	Description    string `json:"description"`     // 描述
	Category       string `json:"category"`        // 分类
	PromptTemplate string `json:"prompt_template"` // 提示词模板
}

// CreateMcpServerReq 是创建 MCP 服务器的请求体。
type CreateMcpServerReq struct {
	Name     string          `json:"name"`      // 服务器名称
	URL      string          `json:"url"`       // URL
	Type     string          `json:"type"`      // 类型
	AuthType string          `json:"auth_type"` // 认证类型
	EnvVars  json.RawMessage `json:"env_vars"`  // 环境变量
}
