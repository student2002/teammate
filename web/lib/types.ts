// types.ts 定义前端共享的类型定义。
import type { RunNodeStatus } from "./workflow/types";

// ── Task Types ──────────────────────────────────────────────
export type TaskType = "story" | "bug" | "task";
export type TaskStatus = "active" | "completed" | "failed" | "cancelled";
// 运行八态机（waiting/ready/running/succeeded/failed/skipped/cancelled/manual_intervention）
export type TaskNodeStatus = RunNodeStatus;
export type AgentStatus = "online" | "offline" | "busy" | "paused" | "pending";
export type Priority = "low" | "medium" | "high" | "urgent";

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
  comments: Comment[];
  subTasks: Task[];
  parentTaskId?: number;
  gitBranch: string;
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

// 工程 GitHub 集成（服务端只回传掩码后的 PAT 与 webhook secret 是否已设置）
export interface GitHubIntegration {
  id: string;
  workspace_id: string;
  project_id: string;
  provider: string;
  name: string;
  repo_owner: string;
  repo_name: string;
  status: string;
  pat_masked: string;
  webhook_secret_set: boolean;
  created_at: string;
  updated_at: string;
}

export interface GitCredential {
  id: string;
  project_id: string;
  agent_id: string;
  agent_name?: string;
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
  // 任务级展示状态：任务状态(active/completed/failed/cancelled)与当前节点状态的合成
  priorityState: TaskNodeStatus;
  description: string;
  constraints: string;
  currentNode: number;
  // 当前节点 type_key（如 agent.task / human.input）
  currentNodeType: string;
  agent: string | null;
  message: string;
  nodesStatus: TaskNodeStatus[];
  nodeAgents: string[];
  nodeNames: string[];
  // 节点级 Token 用量:键为节点 UUID,值为该节点的输入/输出 Token 计数
  nodeTokens: Record<string, { input_tokens: number; output_tokens: number }>;
  dueDate: string;
  labels: string[];
  logs: Record<string, string[]>;
  comments: Comment[];
  subTasks: unknown[];
  gitBranch: string;
  parentTask: string;
  reservedForAgent: string | null;
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
  provider: string;
  status: AgentStatus;
  task: string | null;
  identityInstruction: string;
  gitName: string;
  gitEmail: string;
  skills: Skill[];
  mcpServers: McpServer[];
  tokenUsage: { input: number; output: number };
  history: unknown[];
  // 上报来源:该代理由哪个守护进程上报
  daemonId?: string;
  daemonName?: string;
  personaKey?: string;
  // 创建路径:"server" = Web 端在守护进程分组下创建并下发;"daemon" = 机器上 agentd 配置后上报
  origin?: string;
}

// ── 守护进程(daemon)类型 ────────────────────────────────────
export type DaemonStatus = "online" | "offline";

// 守护进程上报的代理条目(名称即本机标识,同一守护进程内唯一)
export interface DaemonReportedAgent {
  name: string;
  provider: string;
  status: AgentStatus;
  lastReportedAt: string;
}

// 守护进程检测到的可用编码工具(agentd 主动探测本机已安装的 CLI 后上报)
export interface DaemonProvider {
  provider: string;
  version: string;
  installed: boolean;
}

export interface MappedDaemon {
  id: string;
  name: string;
  deviceName: string;
  version: string;
  status: DaemonStatus;
  lastHeartbeat: string;
  agentCount: number;
  createdAt: string;
  agents: DaemonReportedAgent[];
  providers: DaemonProvider[];
}
