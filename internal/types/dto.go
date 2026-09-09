// dto.go provides the API layer data transfer objects (DTOs), ensuring decoupling between DB models and API responses.
//
// This file contains:
//   - Response DTOs (*Response): public API responses, excluding sensitive fields
//   - Request DTOs (*Req): API request body structures
//   - Helper functions: such as masking
//
// Design principles:
//   - public DTOs do not include secret/sensitive fields
//   - execution DTOs include decrypted sensitive fields, only used by the agent's own endpoints
//   - the handler layer explicitly maps DB models to DTOs to avoid field leakage
package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ─── Skill DTO ────────────────────────────────────────────────

// SkillResponse is the public skill response, excluding internal fields.
type SkillResponse struct {
	ID             uuid.UUID `json:"id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Category       string    `json:"category,omitempty"`
	PromptTemplate string    `json:"prompt_template,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ─── MCP Server DTO ───────────────────────────────────────────

// McpServerResponse is the public MCP server response; env_vars are always masked.
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

// McpServerExecutionResponse is the execution-time MCP server response; env_vars are decrypted and contain plaintext values.
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

// ─── Agent Binding DTO ────────────────────────────────────────

// AgentMcpServerBindingResponse is the Agent-MCP binding response (public).
type AgentMcpServerBindingResponse struct {
	AgentID     uuid.UUID `json:"agent_id"`
	McpServerID uuid.UUID `json:"mcp_server_id"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

// AgentSkillBindingResponse is the Agent-Skill binding response (public).
type AgentSkillBindingResponse struct {
	AgentID   uuid.UUID `json:"agent_id"`
	SkillID   uuid.UUID `json:"skill_id"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── Agent DTO ────────────────────────────────────────────────

// AgentResponse is the public agent response, excluding sensitive fields (custom_env).
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

// MaskedEnvVars returns a masked environment variable map (all values replaced with "********").
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
// API request types
// ---------------------------------------------------------------------------

// CreateWorkspaceReq is the request body for creating a workspace.
type CreateWorkspaceReq struct {
	Name        string `json:"name"`         // Workspace name
	Description string `json:"description"`  // Description
	IssuePrefix string `json:"issue_prefix"` // Issue prefix
}

// UpdateWorkspaceReq is the request body for updating a workspace.
type UpdateWorkspaceReq struct {
	Name        string `json:"name"`        // Name
	Description string `json:"description"` // Description
}

// CreateProjectReq is the request body for creating a project.
type CreateProjectReq struct {
	Name              string  `json:"name"`               // Project name
	Description       string  `json:"description"`        // Description
	Visibility        string  `json:"visibility"`         // Visibility
	RepoURL           string  `json:"repo_url"`           // Repository URL
	DefaultWorkflowID *string `json:"default_workflow_id"` // Default workflow template ID
}

// UpdateProjectReq is the request body for updating a project.
type UpdateProjectReq struct {
	Name              string  `json:"name"`               // Name
	Description       string  `json:"description"`        // Description
	Status            string  `json:"status"`             // Status
	DefaultWorkflowID *string `json:"default_workflow_id"` // Default workflow template ID
	Context           string  `json:"context"`            // Project context
}

// CreateAgentReq is the request body for creating an Agent.
type CreateAgentReq struct {
	Name         string `json:"name"`         // Agent name
	Provider     string `json:"provider"`     // AI provider
	Instructions string `json:"instructions"` // System instructions
	Model        string `json:"model"`        // Model to use
}

// UpdateAgentReq is the request body for updating an Agent.
type UpdateAgentReq struct {
	Instructions string          `json:"instructions"` // System instructions
	Model        string          `json:"model"`        // Model
	Status       string          `json:"status"`       // Status
	CustomEnv    json.RawMessage `json:"custom_env"`   // Custom environment variables
	ExtraArgs    []string        `json:"extra_args"`   // Extra arguments
}

// CreateTaskReq is the request body for creating a task.
type CreateTaskReq struct {
	Title              string     `json:"title"`               // Task title
	Description        string     `json:"description"`         // Task description
	Constraints        string     `json:"constraints"`         // Constraints
	Type               string     `json:"type"`                // Task type
	Priority           string     `json:"priority"`            // Priority
	WorkflowTemplateID string     `json:"workflow_template_id"` // Workflow template ID
	DueDate            *time.Time `json:"due_date"`            // Due date
	Labels             []string   `json:"labels"`              // Labels
}

// UpdateTaskReq is the request body for updating a task.
type UpdateTaskReq struct {
	Title       string     `json:"title"`       // Title
	Description string     `json:"description"` // Description
	Priority    string     `json:"priority"`    // Priority
	Labels      []string   `json:"labels"`      // Labels
	DueDate     *time.Time `json:"due_date"`    // Due date
	Constraints string     `json:"constraints"` // Constraints
}

// CreateWorkflowTemplateReq is the request body for creating a workflow template.
type CreateWorkflowTemplateReq struct {
	Name        string           `json:"name"`        // Template name
	Description string           `json:"description"` // Description
	Nodes       []TemplateNodeDef `json:"nodes"`      // Template node list
}

// TemplateNodeDef defines a single node in a workflow template creation request.
type TemplateNodeDef struct {
	Name            string          `json:"name"`             // Node name
	Description     string          `json:"description"`      // Description
	SortOrder       int             `json:"sort_order"`       // Sort order
	NodeType        string          `json:"node_type"`        // Node type
	AssigneeType    string          `json:"assignee_type"`    // Assignee type
	AssigneeID      *string         `json:"assignee_id"`      // Specified assignee ID
	TimeoutMinutes  int             `json:"timeout_minutes"`  // Timeout duration
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`    // Read-only directories
	FullControlDirs json.RawMessage `json:"full_control_dirs"` // Full control directories
	Artifact        json.RawMessage `json:"artifact"`         // Artifact definition
}

// ClaimNodeReq is the request body for claiming a workflow node.
type ClaimNodeReq struct {
	AgentID string `json:"agent_id"` // Agent ID
}

// ApproveNodeReq is the request body for approving a workflow node.
type ApproveNodeReq struct {
	Comment string `json:"comment"` // Approval comment
}

// RejectNodeReq is the request body for rejecting a workflow node.
type RejectNodeReq struct {
	TargetNodeID string `json:"target_node_id"` // Rollback target node ID
	Comment      string `json:"comment"`        // Rejection comment
}

// ManualInterventionReq is the request body for manual intervention on a workflow node.
type ManualInterventionReq struct {
	Comment string `json:"comment"` // Intervention description
}

// CreateCommentReq is the request body for creating a comment.
type CreateCommentReq struct {
	Content  string   `json:"content"`  // Comment content
	Mentions []string `json:"mentions"` // Mention list
}

// RegisterRuntimeReq is the request body for registering a Runtime.
type RegisterRuntimeReq struct {
	DaemonID string `json:"daemon_id"` // Daemon ID
	Provider string `json:"provider"`  // AI provider
	Version  string `json:"version"`   // Version
}

// ReportTokenUsageReq is the request body for reporting token usage.
type ReportTokenUsageReq struct {
	TaskNodeID   uuid.UUID `json:"task_node_id"`   // Node ID
	InputTokens  int32     `json:"input_tokens"`   // Input token count
	OutputTokens int32     `json:"output_tokens"`  // Output token count
	TotalTokens  int32     `json:"total_tokens"`   // Total token count
	CostEstimate string    `json:"cost_estimate"`  // Cost estimate
}

// CreateSkillReq is the request body for creating a skill.
type CreateSkillReq struct {
	Name           string `json:"name"`            // Skill name
	Description    string `json:"description"`     // Description
	Category       string `json:"category"`        // Category
	PromptTemplate string `json:"prompt_template"` // Prompt template
}

// CreateMcpServerReq is the request body for creating an MCP server.
type CreateMcpServerReq struct {
	Name     string          `json:"name"`      // Server name
	URL      string          `json:"url"`       // URL
	Type     string          `json:"type"`      // Type
	AuthType string          `json:"auth_type"` // Authentication type
	EnvVars  json.RawMessage `json:"env_vars"`  // Environment variables
}
