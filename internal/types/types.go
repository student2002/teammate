// types.go defines all shared business enum constants and core domain structs in the project.
//
// This file contains:
//   - Enum constants (Agent providers, statuses, node types, task types, etc.)
//   - Core business structs (Workspace, Agent, Task, TaskNode, etc.)
//
// Other shared definitions have been split into dedicated files in the same package:
//   - permissions.go: permission constants and role definitions
//   - events.go: SSE event constants and structs
//   - errors.go: API error codes and error responses
//   - dto.go: API request/response structs
//   - comment.go: comment type constants
//
// All type definitions are shared by the handler, service, and CLI layers.
package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Enum constant definitions (string type)
// ---------------------------------------------------------------------------

// AgentProvider defines the AI providers supported by an Agent.
const (
	AgentProviderClaude   = "claude"    // Claude (Anthropic)
	AgentProviderOpenClaw = "openclaw"  // OpenClaw
	AgentProviderOpenCode = "opencode"  // OpenCode
	AgentProviderAtomCode = "atomcode"  // AtomCode
	AgentProviderMiMoCode = "mimocode"  // MiMoCode
	AgentProviderCopilot  = "copilot"   // GitHub Copilot
	AgentProviderHermes   = "hermes"    // Hermes
	AgentProviderGemini   = "gemini"    // Gemini (Google)
	AgentProviderPi       = "pi"        // Pi
	AgentProviderCursor   = "cursor"    // Cursor
	AgentProviderKimi     = "kimi"      // Kimi
	AgentProviderKiro     = "kiro"      // Kiro
)

// AgentStatus defines the running status of an Agent.
const (
	AgentStatusOnline  = "online"  // Online
	AgentStatusOffline = "offline" // Offline
	AgentStatusBusy    = "busy"    // Busy (currently executing a task)
	AgentStatusPaused  = "paused"  // Paused
)

// TaskType defines the type classification of a task.
const (
	TaskTypeStory = "story" // User story
	TaskTypeBug   = "bug"   // Bug fix
	TaskTypeTask  = "task"  // Generic task
)

// TaskPriority defines the priority of a task.
const (
	TaskPriorityUrgent = "urgent" // Urgent
	TaskPriorityHigh   = "high"   // High
	TaskPriorityMedium = "medium" // Medium
	TaskPriorityLow    = "low"    // Low
)

// TaskStatus defines the status of a task. The terminal state is decided by
// the ports of the flow.end inbound edges: completed / failed.
const (
	TaskStatusActive    = "active"    // Active
	TaskStatusCompleted = "completed" // Completed
	TaskStatusFailed    = "failed"    // Failed
	TaskStatusCancelled = "cancelled" // Cancelled
)

// TaskNodeStatus defines the V2 status machine of a workflow node:
//
//	waiting → ready → running → succeeded / failed
//	   └────────────────────────┴── skipped (branch not activated)
//	any state → cancelled; running → manual_intervention
const (
	TaskNodeStatusWaiting            = "waiting"             // Waiting for inbound edges / timer / event
	TaskNodeStatusReady              = "ready"               // Ready to be claimed
	TaskNodeStatusRunning            = "running"             // Executing
	TaskNodeStatusSucceeded          = "succeeded"           // Succeeded
	TaskNodeStatusFailed             = "failed"              // Failed
	TaskNodeStatusSkipped            = "skipped"             // Skipped (branch not activated)
	TaskNodeStatusCancelled          = "cancelled"           // Cancelled
	TaskNodeStatusManualIntervention = "manual_intervention" // Requires manual intervention
)

// ActivationMode defines how a node's inbound edges activate it.
const (
	ActivationModeAll = "all" // All inbound edges must be activated
	ActivationModeAny = "any" // Any inbound edge activates the node
)

// TransitionAction defines the action type of a node state transition,
// matching the V2 status machine one-to-one.
const (
	TransitionActionReady        = "ready"         // Waiting → ready
	TransitionActionStart        = "start"         // Ready → running
	TransitionActionComplete     = "complete"      // Running → succeeded
	TransitionActionFail         = "fail"          // Running → failed
	TransitionActionRetry        = "retry"         // Failed → waiting (scheduled retry)
	TransitionActionSkip         = "skip"          // → skipped
	TransitionActionCancel       = "cancel"        // → cancelled
	TransitionActionManual       = "manual"        // → manual_intervention
	TransitionActionTimeout      = "timeout"       // Timeout-driven transition
	TransitionActionInterruptAck = "interrupt_ack" // Interrupt acknowledgement
)

// ProjectStatus defines the status of a project.
const (
	ProjectStatusPlanned   = "planned"   // Planned
	ProjectStatusActive    = "active"    // Active
	ProjectStatusPaused    = "paused"    // Paused
	ProjectStatusCompleted = "completed" // Completed
	ProjectStatusArchived  = "archived"  // Archived
)

// TokenType defines the type of an auth Token.
const (
	TokenTypeAPI     = "api"     // API Token (long-lived)
	TokenTypeSession = "session" // Session Token (valid for 7 days)
	TokenTypeTask    = "task"    // Task Token
)

// McpAuthType defines the authentication type of an MCP server.
const (
	McpAuthTypeNone   = "none"    // No authentication
	McpAuthTypeAPIKey = "api_key" // API Key authentication
	McpAuthTypeOAuth  = "oauth"   // OAuth authentication
)

// MemoryType defines the type classification of a memory entry.
const (
	MemoryTypeArchitecture = "architecture" // Architecture decision
	MemoryTypeCommand      = "command"      // Command reference
	MemoryTypeConvention   = "convention"   // Coding convention
	MemoryTypeDecision     = "decision"     // Technical decision
	MemoryTypeInsight      = "insight"      // Insight
	MemoryTypeEnvironment  = "environment"  // Environment configuration
)

// ---------------------------------------------------------------------------
// Core business structs
// ---------------------------------------------------------------------------

// Workspace represents a team workspace.
//
// A workspace is the top-level organizational unit, containing projects, agents, members, and workflow templates.
type Workspace struct {
	ID          string    `json:"id" db:"id"`                     // Workspace UUID
	Name        string    `json:"name" db:"name"`                 // Workspace name
	Description string    `json:"description" db:"description"`   // Description
	IssuePrefix string    `json:"issue_prefix" db:"issue_prefix"` // Issue prefix (e.g. "TM")
	IsDefault   bool      `json:"is_default" db:"is_default"`     // Whether this is the default workspace
	CreatedAt   time.Time `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`     // Update time
}

// Member represents a human member in a workspace.
type Member struct {
	ID          string    `json:"id" db:"id"`                    // Member UUID
	WorkspaceID string    `json:"workspace_id" db:"workspace_id"` // Workspace ID
	Name        string    `json:"name" db:"name"`                // Name
	Email       string    `json:"email" db:"email"`              // Email
	Role        string    `json:"role" db:"role"`                // Role
	CreatedAt   time.Time `json:"created_at" db:"created_at"`    // Creation time
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`    // Update time
}

// Agent represents an AI agent in a workspace.
//
// An agent can claim and execute task nodes and supports multiple AI providers.
type Agent struct {
	ID            string          `json:"id" db:"id"`                      // Agent UUID
	WorkspaceID   string          `json:"workspace_id" db:"workspace_id"`   // Workspace ID
	Name          string          `json:"name" db:"name"`                  // Name
	Provider      string          `json:"provider" db:"provider"`          // AI provider
	Instructions  string          `json:"instructions" db:"instructions"`  // System instructions
	Model         string          `json:"model" db:"model"`                // Model in use
	Status        string          `json:"status" db:"status"`              // Running status
	CustomEnv     json.RawMessage `json:"custom_env" db:"custom_env"`      // Custom environment variables (JSON)
	ExtraArgs     []string        `json:"extra_args" db:"extra_args"`      // Extra command-line arguments
	TotalCompleted int            `json:"total_completed" db:"total_completed"` // Total completed tasks
	TotalTokens   int64           `json:"total_tokens" db:"total_tokens"`   // Total token usage
	TotalInputTokens  int64       `json:"total_input_tokens"`              // Aggregated input tokens (list assembly)
	TotalOutputTokens int64       `json:"total_output_tokens"`             // Aggregated output tokens (list assembly)
	CurrentTask   *AgentCurrentTask `json:"current_task,omitempty"`        // Currently active task node (list assembly)
	GitName       string          `json:"git_name" db:"git_name"`          // Git commit author name
	GitEmail      string          `json:"git_email" db:"git_email"`        // Git commit author email
	DaemonID      string          `json:"daemon_id,omitempty" db:"daemon_id"` // Reporting daemon (agentd instance); empty when unmanaged
	DaemonName    string          `json:"daemon_name,omitempty" db:"daemon_name"` // Name of the reporting daemon
	Origin        string          `json:"origin,omitempty" db:"origin"`       // Creation path: "server" (web-created); "daemon" is a legacy machine-report value no longer produced
	PersonaKey    string          `json:"persona_key,omitempty" db:"persona_key"` // Cross-workspace memory identity; instances with the same key share one memory on one agentd machine
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`      // Creation time
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`      // Update time
}

// AgentCurrentTask describes the task node an agent is currently working on.
type AgentCurrentTask struct {
	TaskID   int32  `json:"task_id"`
	Title    string `json:"title"`
	NodeName string `json:"node_name"`
}

// DaemonRegistrationUpdate carries the machine-reported registration fields.
type DaemonRegistrationUpdate struct {
	DeviceName string
	Version    string
	PublicKey  string
	Providers  json.RawMessage
}

// AgentExecution is one row of an agent's recent execution history.
type AgentExecution struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      int32      `json:"task_id"`
	TaskTitle   string     `json:"task_title"`
	NodeName    string     `json:"node_name"`
	Status      string     `json:"status"`
	Branch      string     `json:"branch"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Daemon represents an agentd instance running on a work machine.
// One daemon per machine; it registers once with a daemon token and reports
// its local agent instances and detected tool providers to the server.
type Daemon struct {
	ID            uuid.UUID       `json:"id" db:"id"`                       // Daemon UUID
	WorkspaceID   uuid.UUID       `json:"workspace_id" db:"workspace_id"`   // Owning workspace
	Name          string          `json:"name" db:"name"`                   // Display name
	DeviceName    string          `json:"device_name" db:"device_name"`     // Reported machine description
	Version       string          `json:"version" db:"version"`             // agentd version
	Status        string          `json:"status" db:"status"`               // online/offline derived from heartbeat freshness
	PublicKey     string          `json:"public_key,omitempty" db:"public_key"` // Daemon RSA public key (git credential encryption)
	Providers     json.RawMessage `json:"providers" db:"providers"`         // Latest detected tools snapshot [{provider,version,installed}]
	LastHeartbeat *time.Time      `json:"last_heartbeat,omitempty" db:"last_heartbeat"` // Last heartbeat time
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`       // Creation time
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`       // Update time
}

// WorkflowTemplate represents a reusable workflow definition template.
//
// The template is a graph of typed nodes (type_key) connected by port-based
// edges; triggering is configured on the template's trigger node, and version
// increments on every full-graph save.
type WorkflowTemplate struct {
	ID          string    `json:"id" db:"id"`                     // Template UUID
	WorkspaceID string    `json:"workspace_id" db:"workspace_id"` // Workspace ID
	Name        string    `json:"name" db:"name"`                 // Template name
	Description string    `json:"description" db:"description"`   // Description
	IsBuiltin   bool      `json:"is_builtin" db:"is_builtin"`     // Whether it is a built-in template
	Version     int       `json:"version" db:"version"`           // Graph version (bumped on full-graph save)
	Enabled     bool      `json:"enabled" db:"enabled"`           // Whether the template can be instantiated
	CreatedAt   time.Time `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`     // Update time
}

// WorkflowTemplateNode represents a single typed node in a workflow template.
// type_key resolves against the Server-side node registry; config is the
// node-type-specific payload and ui_config only stores canvas position/folding.
type WorkflowTemplateNode struct {
	ID         string          `json:"id" db:"id"`                    // Node UUID
	TemplateID string          `json:"template_id" db:"template_id"`  // Owning template ID
	Name       string          `json:"name" db:"name"`                // Node name
	SortOrder  int             `json:"sort_order" db:"sort_order"`    // Sort order
	TypeKey    string          `json:"type_key" db:"type_key"`        // Node type key (registry-resolved)
	Config     json.RawMessage `json:"config" db:"config"`            // Node-type-specific configuration (JSON)
	UIConfig   json.RawMessage `json:"ui_config" db:"ui_config"`      // Canvas layout (JSON)
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`    // Creation time
	UpdatedAt  time.Time       `json:"updated_at" db:"updated_at"`    // Update time
}

// WorkflowTemplateEdge represents a directed port-to-port connection between
// two template nodes: when the source port fires, the target node wakes up.
type WorkflowTemplateEdge struct {
	ID           string          `json:"id" db:"id"`                     // Edge UUID
	TemplateID   string          `json:"template_id" db:"template_id"`   // Owning template ID
	SourceNodeID string          `json:"source_node_id" db:"source_node_id"` // Source node UUID
	SourcePort   string          `json:"source_port" db:"source_port"`   // Output port on the source node
	TargetNodeID string          `json:"target_node_id" db:"target_node_id"` // Target node UUID
	TargetPort   string          `json:"target_port" db:"target_port"`   // Input port on the target node
	Label        string          `json:"label" db:"label"`               // Edge label
	SortOrder    int             `json:"sort_order" db:"sort_order"`     // Sort order
	Config       json.RawMessage `json:"config" db:"config"`             // Edge configuration (JSON)
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`     // Creation time
}

// Project represents a software project within a workspace.
type Project struct {
	ID                 string    `json:"id" db:"id"`                               // Project UUID
	WorkspaceID        string    `json:"workspace_id" db:"workspace_id"`            // Workspace ID
	Name               string    `json:"name" db:"name"`                           // Project name
	Description        string    `json:"description" db:"description"`             // Description
	Icon               string    `json:"icon" db:"icon"`                           // Icon
	Status             string    `json:"status" db:"status"`                       // Project status
	RepoURL            string    `json:"repo_url" db:"repo_url"`                   // Repository URL
	Context            string    `json:"context" db:"context"`                     // Project context description
	CreatedAt          time.Time `json:"created_at" db:"created_at"`               // Creation time
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`               // Update time
}

// ProjectMember represents the association between a project and a member (Agent or human).
type ProjectMember struct {
	ID         string    `json:"id" db:"id"`                    // Association record UUID
	ProjectID  string    `json:"project_id" db:"project_id"`    // Project ID
	MemberType string    `json:"member_type" db:"member_type"`  // Member type ("agent" or "member")
	AgentID    *string   `json:"agent_id" db:"agent_id"`        // Agent ID (when member_type=agent)
	MemberID   *string   `json:"member_id" db:"member_id"`      // Human member ID (when member_type=member)
	Role       string    `json:"role" db:"role"`                // Project role
	CreatedAt  time.Time `json:"created_at" db:"created_at"`    // Creation time
}

// Task represents a unit of work in a project.
//
// A task is instantiated from a workflow template as a graph of task nodes;
// workflow_version/source_event_id/trigger_payload snapshot the instantiation
// context and context/failure are maintained by the engine during execution.
type Task struct {
	ID                 int32           `json:"id" db:"id"`                     // Task ID (auto-increment integer)
	ProjectID          string          `json:"project_id" db:"project_id"`     // Owning project ID
	WorkflowName       string          `json:"workflow_name" db:"workflow_name"` // Workflow name
	WorkflowTemplateID *string         `json:"workflow_template_id" db:"workflow_template_id"` // Creating template (nil for ad-hoc tasks)
	WorkflowVersion    int             `json:"workflow_version" db:"workflow_version"` // Template graph version at instantiation
	SourceEventID      *string         `json:"source_event_id" db:"source_event_id"` // Originating workflow_events row (nil for manual creation)
	TriggerPayload     json.RawMessage `json:"trigger_payload" db:"trigger_payload"` // Trigger payload snapshot (JSON)
	Context            json.RawMessage `json:"context" db:"context"`           // Engine run context: trigger/task/vars/nodes roots (JSON)
	Failure            json.RawMessage `json:"failure" db:"failure"`           // Task failure detail (JSON)
	Title              string          `json:"title" db:"title"`               // Task title
	Description        string          `json:"description" db:"description"`   // Task description
	Constraints        string          `json:"constraints" db:"constraints"`   // Constraints
	Type               string          `json:"type" db:"type"`                 // Task type (story/bug/task)
	Priority           string          `json:"priority" db:"priority"`         // Priority
	Status             string          `json:"status" db:"status"`             // Task status
	AuthorType         string          `json:"author_type" db:"author_type"`   // Author type
	AuthorID           string          `json:"author_id" db:"author_id"`       // Author ID
	DueDate            *time.Time      `json:"due_date" db:"due_date"`         // Due date
	Labels             []string        `json:"labels" db:"labels"`             // List of labels
	Sequence           int             `json:"sequence" db:"sequence"`         // Sort sequence number
	ParentTaskID       *int32          `json:"parent_task_id" db:"parent_task_id"` // Parent task ID (for subtasks)
	GitBranch          *string         `json:"git_branch" db:"git_branch"`     // Associated Git branch
	CreatedAt          time.Time       `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt          time.Time       `json:"updated_at" db:"updated_at"`     // Update time
}

// TaskNode represents a single node instance of a task run (a run snapshot of
// a template node, unique per (task_id, template_node_id)).
//
// V2 status machine: waiting → ready → running → succeeded/failed;
// skipped marks unactivated branches; manual_intervention requires a human.
type TaskNode struct {
	ID                 string          `json:"id" db:"id"`                     // Node UUID
	TaskID             int32           `json:"task_id" db:"task_id"`           // Owning task ID
	TemplateNodeID     string          `json:"template_node_id" db:"template_node_id"` // Originating template node UUID
	Name               string          `json:"name" db:"name"`                 // Node name
	SortOrder          int             `json:"sort_order" db:"sort_order"`     // Sort order
	TypeKey            string          `json:"type_key" db:"type_key"`         // Node type key (registry-resolved)
	Config             json.RawMessage `json:"config" db:"config"`             // Node-type-specific configuration snapshot (JSON)
	Status             string          `json:"status" db:"status"`             // Node status (V2 machine)
	ActivationMode     string          `json:"activation_mode" db:"activation_mode"` // Inbound edge activation mode (all/any)
	Input              json.RawMessage `json:"input" db:"input"`               // Resolved input snapshot (JSON)
	Output             json.RawMessage `json:"output" db:"output"`             // Execution output (JSON)
	Error              json.RawMessage `json:"error" db:"error"`               // Failure detail (JSON)
	AttemptCount       int             `json:"attempt_count" db:"attempt_count"` // Executed attempt count
	MaxAttempts        int             `json:"max_attempts" db:"max_attempts"` // Maximum attempts before failure
	NextRetryAt        *time.Time      `json:"next_retry_at" db:"next_retry_at"` // Scheduled retry time
	WaitKey            string          `json:"wait_key" db:"wait_key"`         // Wait registration key (empty = not waiting)
	WaitExpiresAt      *time.Time      `json:"wait_expires_at" db:"wait_expires_at"` // Wait deadline
	Version            int             `json:"version" db:"version"`           // Optimistic lock version
	ReservedForAgentID *string         `json:"reserved_for_agent_id" db:"reserved_for_agent_id"` // Agent currently reserved/assigned
	Summary            string          `json:"summary" db:"summary"`           // Node execution summary
	ReadonlyDirs       json.RawMessage `json:"readonly_dirs" db:"readonly_dirs"` // Read-only directories (JSON array, from template node)
	FullControlDirs    json.RawMessage `json:"full_control_dirs" db:"full_control_dirs"` // Full control directories (JSON array, from template node)
	CompletedAt        *time.Time      `json:"completed_at" db:"completed_at"` // Terminal transition time
	CreatedAt          time.Time       `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt          time.Time       `json:"updated_at" db:"updated_at"`     // Update time
}

// NodeTransition records the state transition history of a workflow node.
//
// A transition record is created on every node state change, used for auditing and debugging.
type NodeTransition struct {
	ID           string    `json:"id" db:"id"`                     // Transition record UUID
	TaskNodeID   string    `json:"task_node_id" db:"task_node_id"` // Node ID
	FromStatus   string    `json:"from_status" db:"from_status"`   // Source status
	ToStatus     string    `json:"to_status" db:"to_status"`       // Target status
	Action       string    `json:"action" db:"action"`             // Action type
	TargetNodeID *string   `json:"target_node_id" db:"target_node_id"` // Target node ID (on reject)
	Comment      string    `json:"comment" db:"comment"`           // Action comment
	OperatorID   *string   `json:"operator_id" db:"operator_id"`   // Operator ID
	OperatorType string    `json:"operator_type" db:"operator_type"` // Operator type
	CreatedAt    time.Time `json:"created_at" db:"created_at"`     // Creation time
}

// Comment represents a comment on a task.
type Comment struct {
	ID           string          `json:"id" db:"id"`                     // Comment UUID
	TaskID       int32           `json:"task_id" db:"task_id"`           // Owning task ID
	NodeID       *string         `json:"node_id" db:"node_id"`           // Associated node ID (for node comments)
	SourceNodeID *string         `json:"source_node_id" db:"source_node_id"` // Source node ID (for handoff comments)
	ParentID     *string         `json:"parent_id" db:"parent_id"`       // Parent comment ID (for replies)
	AuthorType   string          `json:"author_type" db:"author_type"`   // Author type
	AuthorID     string          `json:"author_id" db:"author_id"`       // Author ID
	Content      string          `json:"content" db:"content"`           // Comment content
	CommentType  string          `json:"comment_type" db:"comment_type"` // Comment type
	Metadata     json.RawMessage `json:"metadata" db:"metadata"`         // Extension metadata (JSON)
	Mentions     []string        `json:"mentions" db:"mentions"`         // Mentions list (UUID array)
	EditedAt     *time.Time      `json:"edited_at" db:"edited_at"`       // Edit time
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`     // Update time
}


// Skill represents a reusable skill definition.
//
// Skills are specialized capabilities that an Agent can use, such as code review, test writing, etc.
type Skill struct {
	ID            string    `json:"id" db:"id"`                      // Skill UUID
	WorkspaceID   string    `json:"workspace_id" db:"workspace_id"`   // Workspace ID
	Name          string    `json:"name" db:"name"`                  // Skill name
	Description   string    `json:"description" db:"description"`    // Description
	Category      string    `json:"category" db:"category"`          // Category
	PromptTemplate string   `json:"prompt_template" db:"prompt_template"` // Prompt template
	CreatedAt     time.Time `json:"created_at" db:"created_at"`      // Creation time
}

// McpServer represents an MCP server configuration.
//
// MCP (Model Context Protocol) servers provide external tools and data sources for Agents.
type McpServer struct {
	ID          string          `json:"id" db:"id"`                    // Server UUID
	WorkspaceID string          `json:"workspace_id" db:"workspace_id"` // Workspace ID
	Name        string          `json:"name" db:"name"`                // Server name
	URL         string          `json:"url" db:"url"`                  // Server URL
	Type        string          `json:"type" db:"type"`                // Server type
	AuthType    string          `json:"auth_type" db:"auth_type"`      // Authentication type
	EnvVars     json.RawMessage `json:"env_vars" db:"env_vars"`        // Environment variables (JSON)
	Status      string          `json:"status" db:"status"`            // Server status
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`    // Creation time
}

// AgentSkill represents the association between an Agent and a skill.
type AgentSkill struct {
	AgentID   string    `json:"agent_id" db:"agent_id"`   // Agent ID
	SkillID   string    `json:"skill_id" db:"skill_id"`   // Skill ID
	Enabled   bool      `json:"enabled" db:"enabled"`     // Whether enabled
	CreatedAt time.Time `json:"created_at" db:"created_at"` // Creation time
}

// AgentMcpServer represents the association between an Agent and an MCP server.
type AgentMcpServer struct {
	AgentID     string    `json:"agent_id" db:"agent_id"`         // Agent ID
	McpServerID string    `json:"mcp_server_id" db:"mcp_server_id"` // MCP server ID
	Enabled     bool      `json:"enabled" db:"enabled"`           // Whether enabled
	CreatedAt   time.Time `json:"created_at" db:"created_at"`     // Creation time
}

// TokenUsage records token consumption during workflow node execution.
type TokenUsage struct {
	ID           int64          `json:"id"`             // Record ID
	TaskNodeID   string         `json:"task_node_id"`   // Node ID
	AgentID      string         `json:"agent_id"`       // Agent ID
	InputTokens  int32          `json:"input_tokens"`   // Input token count
	OutputTokens int32          `json:"output_tokens"`  // Output token count
	TotalTokens  int32          `json:"total_tokens"`   // Total token count
	CostEstimate *string        `json:"cost_estimate"`  // Cost estimate (optional)
	CreatedAt    time.Time      `json:"created_at"`     // Creation time
}

// AuthToken represents an authentication token.
//
// Supports three types: api (long-lived API token), session (short-lived session token), task (task token).
type AuthToken struct {
	ID        string     `json:"id" db:"id"`                   // Token record UUID
	TokenHash string     `json:"token_hash" db:"token_hash"`   // Token hash (bcrypt)
	TokenType string     `json:"token_type" db:"token_type"`   // Token type
	OwnerType string     `json:"owner_type" db:"owner_type"`   // Owner type
	OwnerID   string     `json:"owner_id" db:"owner_id"`       // Owner ID
	ExpiresAt time.Time  `json:"expires_at" db:"expires_at"`   // Expiration time
	CreatedAt time.Time  `json:"created_at" db:"created_at"`   // Creation time
}

// GitCredential stores an encrypted Git PAT used for repository access authentication.
// One credential per (project, member agent); the agent binding is immutable after creation.
type GitCredential struct {
	ID           string     `json:"id" db:"id"`                     // Credential UUID
	ProjectID    string     `json:"project_id" db:"project_id"`     // Owning project ID
	AgentID      string     `json:"agent_id" db:"agent_id"`         // Bound agent ID
	Username     string     `json:"username" db:"username"`         // Username
	EncryptedPAT string     `json:"encrypted_pat" db:"encrypted_pat"` // Encrypted PAT
	CreatedBy    *string    `json:"created_by" db:"created_by"`     // Creator ID
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`     // Update time
}

// Memory represents a knowledge memory entry for the workspace.
//
// Memory supports text search and semantic search (pgvector), used for Agent learning and reference.
type Memory struct {
	ID           string          `json:"id" db:"id"`                     // Memory UUID
	WorkspaceID  string          `json:"workspace_id" db:"workspace_id"`  // Workspace ID
	SourceTaskID *int32          `json:"source_task_id" db:"source_task_id"` // Source task ID
	Type         string          `json:"type" db:"type"`                 // Memory type
	Title        string          `json:"title" db:"title"`               // Title
	Content      string          `json:"content" db:"content"`           // Content
	Tags         []string        `json:"tags" db:"tags"`                 // List of tags
	Confidence   float64         `json:"confidence" db:"confidence"`     // Confidence (0-1)
	Verified     bool            `json:"verified" db:"verified"`         // Whether verified
	Stale        bool            `json:"stale" db:"stale"`               // Whether stale
	Metadata     json.RawMessage `json:"metadata" db:"metadata"`         // Extension metadata (JSON)
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`     // Creation time
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`     // Update time
}

// CommunityWorkflow represents a shared workflow from the community.
type CommunityWorkflow struct {
	ID                           string          `json:"id" db:"id"`                               // Workflow UUID
	Name                         string          `json:"name" db:"name"`                           // Name
	Description                  string          `json:"description" db:"description"`             // Description
	Author                       string          `json:"author" db:"author"`                       // Author
	Version                      string          `json:"version" db:"version"`                     // Version
	WorkflowDefinition           json.RawMessage `json:"workflow_definition" db:"workflow_definition"` // Workflow definition (JSON)
	RequiredSkills               json.RawMessage `json:"required_skills" db:"required_skills"`     // Required skills (JSON)
	RequiredMcpServers           json.RawMessage `json:"required_mcp_servers" db:"required_mcp_servers"` // Required MCP servers (JSON)
	RecommendedAgentInstructions json.RawMessage `json:"recommended_agent_instructions" db:"recommended_agent_instructions"` // Recommended Agent instructions (JSON)
	Downloads                    int             `json:"downloads" db:"downloads"`                 // Download count
	IsOfficial                   bool            `json:"is_official" db:"is_official"`             // Whether it is official
	CreatedAt                    time.Time       `json:"created_at" db:"created_at"`               // Creation time
	UpdatedAt                    time.Time       `json:"updated_at" db:"updated_at"`               // Update time
}

// ProjectReviewer represents a reviewer assignment for a project.
type ProjectReviewer struct {
	ID         string    `json:"id" db:"id"`                   // Record UUID
	ProjectID  string    `json:"project_id" db:"project_id"`   // Project ID
	MemberType string    `json:"member_type" db:"member_type"` // Member type
	AgentID    *string   `json:"agent_id" db:"agent_id"`       // Agent ID
	MemberID   *string   `json:"member_id" db:"member_id"`     // Human member ID
	CreatedAt  time.Time `json:"created_at" db:"created_at"`   // Creation time
}

// ---------------------------------------------------------------------------
// Entity structs backfilled to match db/generated/models.go
// ---------------------------------------------------------------------------

// AgentPermission represents a fine-grained permission granted to an Agent.
type AgentPermission struct {
	ID           string  `json:"id" db:"id"`                          // Record UUID
	AgentID      string  `json:"agent_id" db:"agent_id"`              // Agent ID
	Permission   string  `json:"permission" db:"permission"`          // Permission name
	ResourceType string  `json:"resource_type" db:"resource_type"`    // Resource type
	ResourceID   *string `json:"resource_id" db:"resource_id"`        // Resource ID (optional)
	GrantedBy    *string `json:"granted_by" db:"granted_by"`          // Grantor ID (optional)
	CreatedAt    time.Time `json:"created_at" db:"created_at"`        // Creation time
}

// AuditLog represents a system audit log entry, recording key actions by users/Agents.
type AuditLog struct {
	ID           int64           `json:"id" db:"id"`                              // Log ID
	WorkspaceID  string          `json:"workspace_id" db:"workspace_id"`         // Workspace ID
	ActorType    string          `json:"actor_type" db:"actor_type"`             // Actor type
	ActorID      string          `json:"actor_id" db:"actor_id"`                 // Actor ID
	Action       string          `json:"action" db:"action"`                     // Action type
	ResourceType string          `json:"resource_type" db:"resource_type"`       // Resource type
	ResourceID   string          `json:"resource_id" db:"resource_id"`           // Resource ID
	Details      json.RawMessage `json:"details" db:"details"`                   // Action details (JSON)
	IPAddress    string          `json:"ip_address" db:"ip_address"`             // Request source IP
	UserAgent    *string         `json:"user_agent" db:"user_agent"`             // Client User-Agent
	RequestID    *string         `json:"request_id" db:"request_id"`             // Request trace ID
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`             // Creation time
}

// ExecutionSession represents a node execution session, recording runtime information when an Agent executes a node.
type ExecutionSession struct {
	ID              string     `json:"id" db:"id"`                              // Session UUID
	DaemonID        *string    `json:"daemon_id" db:"daemon_id"`                // Reporting daemon ID
	AgentID         *string    `json:"agent_id" db:"agent_id"`                  // Executing Agent ID
	TaskNodeID      string     `json:"task_node_id" db:"task_node_id"`          // Executed node ID
	Attempt         int32      `json:"attempt" db:"attempt"`                    // Attempt count
	Status          string     `json:"status" db:"status"`                      // Session status
	Workdir         *string    `json:"workdir" db:"workdir"`                    // Working directory
	Branch          *string    `json:"branch" db:"branch"`                      // Git branch
	BaseCommit      *string    `json:"base_commit" db:"base_commit"`            // Starting commit
	HeadCommit      *string    `json:"head_commit" db:"head_commit"`            // Latest commit
	ClaudeSessionID *string    `json:"claude_session_id" db:"claude_session_id"` // Claude session ID
	StartedAt       time.Time  `json:"started_at" db:"started_at"`              // Start time
	CompletedAt     *time.Time `json:"completed_at" db:"completed_at"`          // Completion time
	InterruptedAt   *time.Time `json:"interrupted_at" db:"interrupted_at"`      // Interruption time
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`              // Creation time
}

// Invitation represents a workspace invitation, sent by email to a member pending to join.
type Invitation struct {
	ID          string     `json:"id" db:"id"`                       // Invitation UUID
	WorkspaceID string     `json:"workspace_id" db:"workspace_id"`   // Target workspace ID
	Email       string     `json:"email" db:"email"`                 // Invitee email
	Role        string     `json:"role" db:"role"`                   // Invited role
	TokenHash   string     `json:"token_hash" db:"token_hash"`       // Invitation token hash
	InvitedBy   *string    `json:"invited_by" db:"invited_by"`       // Inviter ID
	ExpiresAt   time.Time  `json:"expires_at" db:"expires_at"`       // Invitation expiration time
	AcceptedAt  *time.Time `json:"accepted_at" db:"accepted_at"`     // Acceptance time
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`       // Creation time
}

// SseEventBuffer represents an SSE event buffer entry, used to replay lost events on reconnection.
type SseEventBuffer struct {
	ID         int64           `json:"id" db:"id"`                        // Buffer entry ID
	Subscriber string          `json:"runtime_id" db:"runtime_id"`       // Target subscriber key (daemon stream)
	EventType  string          `json:"event_type" db:"event_type"`       // Event type
	EventData  json.RawMessage `json:"event_data" db:"event_data"`       // Event data (JSON)
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`       // Creation time
}

// TaskLog represents a task execution log entry, recording key events during node execution.
type TaskLog struct {
	ID        string    `json:"id" db:"id"`                  // Log UUID
	TaskID    int32     `json:"task_id" db:"task_id"`        // Owning task ID
	NodeID    string    `json:"node_id" db:"node_id"`        // Associated node ID
	Type      string    `json:"type" db:"type"`              // Log type
	Content   string    `json:"content" db:"content"`        // Log content
	Timestamp time.Time `json:"timestamp" db:"timestamp"`    // Log timestamp
	CreatedAt time.Time `json:"created_at" db:"created_at"`  // Creation time
}

// TaskLogChunk represents chunked data of a task log, supporting chunked upload for large logs.
type TaskLogChunk struct {
	ID         int64     `json:"id" db:"id"`                    // Chunk ID
	TaskNodeID string    `json:"task_node_id" db:"task_node_id"` // Associated node ID
	ChunkIndex int32     `json:"chunk_index" db:"chunk_index"`   // Chunk sequence number
	Data       []byte    `json:"data" db:"data"`                 // Chunk data
	Size       int32     `json:"size" db:"size"`                 // Chunk size (bytes)
	UploadedAt time.Time `json:"uploaded_at" db:"uploaded_at"`   // Upload time
}

// WorkspaceMember represents a workspace member association record.
type WorkspaceMember struct {
	ID          string    `json:"id" db:"id"`                       // Record UUID
	WorkspaceID string    `json:"workspace_id" db:"workspace_id"`   // Workspace ID
	MemberID    string    `json:"member_id" db:"member_id"`         // Member ID
	Role        string    `json:"role" db:"role"`                   // Member's role in the workspace
	CreatedAt   time.Time `json:"created_at" db:"created_at"`       // Creation time
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`       // Update time
}
