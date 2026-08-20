// types.ts 定义前端共享的类型定义。
// ── Task Types ──────────────────────────────────────────────
export type TaskType = "story" | "bug" | "task";
export type TaskStatus = "active" | "completed" | "cancelled";
export type TaskNodeStatus =
  | "pending"
  | "in_progress"
  | "completed"
  | "rejected"
  | "manual_intervention";
export type NodeType = "standard" | "review" | "manual";
export type WorkflowTriggerType = "manual" | "schedule" | "github_issue";
export type AssigneeType = "any_agent" | "specific_agent" | "human" | "auto";
export type AgentStatus = "online" | "offline" | "busy" | "paused";
export type Priority = "low" | "medium" | "high" | "urgent";

export interface TaskNode {
  id: string;
  task_id: number;
  name: string;
  nodeType: NodeType;
  assigneeType: AssigneeType;
  assigneeId?: string;
  status: TaskNodeStatus;
  description: string;
  timeout: number;
  maxRejectCycles: number;
  timeoutMinutes: number;
  readonlyDirs: string;
  fullControlDirs: string;
  artifact: string;
  rejectCount: number;
  sortOrder: number;
  summary?: string;
}

export interface NodeTransition {
  id: string;
  taskNodeId: string;
  fromStatus: TaskNodeStatus;
  toStatus: TaskNodeStatus;
  action: "approve" | "reject" | "manual" | "reclaim" | "timeout";
  targetNodeId?: string;
  comment?: string;
  operatorId?: string;
  operatorType: string;
  createdAt: string;
}

export interface Task {
  id: number;
  title: string;
  type: TaskType;
  priority: Priority;
  status: TaskStatus;
  description: string;
  constraints: string;
  dueDate: string;
  labels: string[];
  projectId: string;
  workflowName: string;
  nodes: TaskNode[];
  comments: Comment[];
  subTasks: Task[];
  parentTaskId?: number;
  gitBranch: string;
  interrupted: boolean;
}

export interface Agent {
  id: string;
  name: string;
  provider: string;
  status: AgentStatus;
  instructions: string;
  skills: Skill[];
  mcpServers: McpServer[];
  tokenUsage: { input: number; output: number };
  currentTaskId?: number;
}

export interface Skill {
  id: string;
  name: string;
  description: string;
  category: string;
  promptTemplate: string;
  workspaceId: string;
}

export type McpServerType = "sse" | "http" | "streamable_http";
export type McpAuthType = "none" | "api_key" | "oauth";

export interface McpServer {
  id: string;
  name: string;
  url: string;
  type: McpServerType;
  authType: McpAuthType;
  envVars: Record<string, unknown>;
  status: string;
  workspaceId: string;
}

export interface WorkflowTemplate {
  id: string;
  name: string;
  description: string;
  isBuiltIn: boolean;
  triggerType: WorkflowTriggerType;
  triggerConfig: WorkflowTriggerConfig;
  triggerEnabled: boolean;
  nextRunAt?: string;
  lastTriggeredAt?: string;
  nodes: WorkflowTemplateNode[];
  workspaceId: string;
}

export interface WorkflowTriggerConfig {
  projectId?: string;
  intervalMinutes?: number;
  title?: string;
  description?: string;
  repoOwner?: string;
  repoName?: string;
  secret?: string;
}

export interface WorkflowTemplateNode {
  id: number;
  name: string;
  nodeType: NodeType;
  assigneeType: AssigneeType;
  assigneeId?: string;
  description: string;
  timeout: number;
  readonlyDirs: string;
  fullControlDirs: string;
  artifact: string;
  maxRejectCycles: number;
}

export interface Workspace {
  id: string;
  name: string;
  slug?: string;
  issuePrefix?: string;
  description: string;
  createdAt: string;
  stats?: { projects: number; agents: number };
}

export interface Project {
  id: string;
  name: string;
  description: string;
  workspaceId: string;
}

export interface GitCredential {
  id: string;
  project_id: string;
  repo_url: string;
  username: string;
  pat_masked: string;
  created_at: string;
  updated_at: string;
}

export type CommentType = "text" | "code_review" | "suggestion" | "question" | "handoff" | "decision" | "execution_summary";

export interface Comment {
  id: string;
  taskId: number;
  nodeId?: string;
  sourceNodeId?: string;
  content: string;
  commentType: CommentType;
  authorId: string;
  authorName: string;
  createdAt: string;
  parentId?: string;
}

export interface Memory {
  id: string;
  content: string;
  source: string;
  confidence: number;
  verified: boolean;
  createdAt: string;
}

export interface User {
  name: string;
  role: string; // "owner" | "admin" | "member" | "viewer"
  email?: string;
  workspaceId?: string;
}

// ── Mapped frontend types (from API responses) ─────────────
export interface MappedTask {
  id: number;
  taskRef: string;
  title: string;
  type: TaskType;
  priority: Priority;
  priorityState: string;
  description: string;
  constraints: string;
  currentNode: number;
  currentNodeType: NodeType;
  agent: string | null;
  message: string;
  nodesStatus: TaskNodeStatus[];
  nodeAgents: string[];
  nodeNames: string[];
  nodeTokens: Record<string, unknown>;
  nodeTimeouts: Record<string, unknown>;
  dueDate: string;
  labels: string[];
  rejectCount: number;
  logs: Record<string, string[]>;
  comments: Comment[];
  reviewHistory: unknown[];
  subTasks: unknown[];
  gitBranch: string;
  parentTask: string;
  interrupted: boolean;
  reservedForAgent: string | null;
  reservationExpiresAt: string | null;
  nodeSummaries: Record<string, string>;
  workflowName: string;
  _rawNodes: unknown[];
  _projectId: string;
}

// 分页任务查询结果（历史任务页面使用）
export interface PaginatedTaskResult {
  tasks: { task: Record<string, unknown>; nodes: Record<string, unknown>[]; workflow_name: string; git_branch: string; node_tokens: Record<string, unknown> }[];
  total: number;
  limit: number;
  offset: number;
}

export interface MappedAgent {
  id: string;
  name: string;
  tool: string;
  status: AgentStatus;
  task: string | null;
  identityInstruction: string;
  gitName: string;
  gitEmail: string;
  skills: Skill[];
  mcpServers: McpServer[];
  tokenUsage: { input: number; output: number };
  history: unknown[];
}

export interface MappedTemplate {
  id: string;
  name: string;
  description: string;
  isBuiltIn: boolean;
  triggerType: WorkflowTriggerType;
  triggerConfig: WorkflowTriggerConfig;
  triggerEnabled: boolean;
  nextRunAt?: string;
  lastTriggeredAt?: string;
  nodes: WorkflowTemplateNode[];
}
