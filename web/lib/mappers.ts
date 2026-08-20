// mappers.ts 将后端 snake_case API 数据映射为前端 camelCase 类型。
import type {
  MappedTask,
  MappedAgent,
  MappedTemplate,
  NodeType,
  TaskNodeStatus,
  Skill,
  McpServer,
  McpAuthType,
  McpServerType,
  WorkflowTriggerConfig,
  WorkflowTriggerType,
  Workspace,
} from "./types";

export function formatTaskRef(id: number | string): string {
  return `T-${id}`;
}

const providerMap: Record<string, string> = {
  claude: "Claude Code",
  openclaw: "OpenClaw",
  opencode: "OpenCode",
  atomcode: "AtomCode",
  mimocode: "MiMoCode",
  copilot: "Copilot",
  hermes: "Hermes",
  gemini: "Gemini",
  pi: "Pi",
  cursor: "Cursor",
  kimi: "Kimi",
  kiro: "Kiro",
};


function nullableString(value: unknown): string {
  if (typeof value === "string") return value;
  if (value && typeof value === "object" && "String" in value) {
    const maybe = value as { String?: unknown; Valid?: unknown };
    return maybe.Valid === false ? "" : String(maybe.String || "");
  }
  return "";
}

function rawJsonObject(value: unknown): Record<string, unknown> {
  if (!value || value === "null") return {};
  if (typeof value === "object" && !Array.isArray(value)) return value as Record<string, unknown>;
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value);
      return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : {};
    } catch {
      return {};
    }
  }
  return {};
}

function mapTriggerConfig(value: unknown): WorkflowTriggerConfig {
  const raw = rawJsonObject(value);
  return {
    projectId: String(raw.project_id || raw.projectId || ""),
    intervalMinutes: Number(raw.interval_minutes || raw.intervalMinutes || 0),
    title: String(raw.title || ""),
    description: String(raw.description || ""),
    repoOwner: String(raw.repo_owner || raw.repoOwner || ""),
    repoName: String(raw.repo_name || raw.repoName || ""),
    secret: String(raw.secret || ""),
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapSkillFromApi(apiSkill: any): Skill {
  return {
    id: apiSkill.id,
    name: apiSkill.name || "",
    description: nullableString(apiSkill.description),
    category: nullableString(apiSkill.category),
    promptTemplate: nullableString(apiSkill.prompt_template ?? apiSkill.promptTemplate),
    workspaceId: apiSkill.workspace_id || apiSkill.workspaceId || "",
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapMcpServerFromApi(apiMcp: any): McpServer {
  return {
    id: apiMcp.id,
    name: apiMcp.name || "",
    url: apiMcp.url || "",
    type: (nullableString(apiMcp.type) || "sse") as McpServerType,
    authType: (apiMcp.auth_type || apiMcp.authType || "none") as McpAuthType,
    envVars: rawJsonObject(apiMcp.env_vars ?? apiMcp.envVars),
    status: apiMcp.status || "active",
    workspaceId: apiMcp.workspace_id || apiMcp.workspaceId || "",
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapWorkspaceFromApi(apiWs: any): Workspace {
  return {
    id: apiWs.id,
    name: apiWs.name || "",
    slug: nullableString(apiWs.slug),
    issuePrefix: nullableString(apiWs.issue_prefix ?? apiWs.issuePrefix),
    description: nullableString(apiWs.description),
    createdAt: String(apiWs.created_at || apiWs.createdAt || ""),
    stats: apiWs.stats || undefined,
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapTaskFromApi(item: any, agentsList: any[] = []): MappedTask {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const apiTask = item.task;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const taskNodes: any[] = item.nodes || [];

  const currentNodeIdx = taskNodes.findIndex(
    (n) =>
      n.status === "in_progress" ||
      n.status === "manual_intervention" ||
      n.status === "rejected"
  );
  const currentNode =
    currentNodeIdx >= 0
      ? currentNodeIdx
      : taskNodes.findIndex((n) => n.status === "pending");

  return {
    id: apiTask.id,
    taskRef: formatTaskRef(apiTask.id),
    title: apiTask.title || "",
    type: apiTask.type || "task",
    priority: apiTask.priority || "medium",
    priorityState:
      apiTask.status === "active"
        ? taskNodes.some((n) => n.status === "manual_intervention")
          ? "manual_intervention"
          : taskNodes.some((n) => n.status === "rejected")
            ? "rejected"
            : taskNodes.some((n) => n.status === "in_progress")
              ? "in_progress"
              : "pending"
        : apiTask.status || "pending",
    description: apiTask.description || "",
    constraints: apiTask.constraints || "",
    currentNode: currentNode >= 0 ? currentNode : 0,
    currentNodeType: taskNodes.length > 0
      ? (taskNodes[currentNode >= 0 ? currentNode : 0]?.node_type as NodeType || "standard")
      : "standard",
    agent:
      taskNodes.find(
        (n) =>
          n.assignee_id &&
          (n.status === "in_progress" || n.status === "manual_intervention")
      )?.assignee_id || null,
    message: "",
    nodesStatus: taskNodes.map((n) => (n.status || "pending") as TaskNodeStatus),
    nodeAgents: taskNodes.map((n) => {
      const aid = n.assignee_id;
      if (!aid) return "";
      const agent = agentsList.find((a) => a.id === aid);
      return agent ? agent.name : aid;
    }),
    nodeNames: taskNodes.map((n) => n.name || ""),
    nodeTokens: item.node_tokens || {},
    nodeTimeouts: {},
    dueDate: apiTask.due_date || "",
    labels: apiTask.labels || [],
    rejectCount: apiTask.reject_count || 0,
    logs: {},
    comments: [],
    reviewHistory: [],
    subTasks: [],
    gitBranch: item.git_branch || "",
    parentTask: "",
    interrupted: taskNodes.some((n) => n.status === "manual_intervention"),
    reservedForAgent: null,
    reservationExpiresAt: null,
    nodeSummaries: taskNodes.reduce((acc: Record<string, string>, n: { id: string; summary?: string }) => {
      if (n.summary) acc[n.id] = n.summary;
      return acc;
    }, {}),
    workflowName: item.workflow_name || "",
    _rawNodes: taskNodes,
    _projectId: apiTask.project_id,
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapAgentFromApi(apiAgent: any): MappedAgent {
  return {
    id: apiAgent.id,
    name: apiAgent.name || "",
    tool: providerMap[apiAgent.provider] || apiAgent.provider || "Custom CLI",
    status: apiAgent.status || "offline",
    task: null,
    identityInstruction: apiAgent.instructions || "",
    gitName: apiAgent.git_name || "",
    gitEmail: apiAgent.git_email || "",
    skills: [],
    mcpServers: [],
    tokenUsage: {
      input: apiAgent.input_tokens || 0,
      output: apiAgent.output_tokens || 0,
    },
    history: [],
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapTemplateFromApi(apiTemplate: any): MappedTemplate {
  const template = apiTemplate.template || apiTemplate;
  const nodes = (apiTemplate.nodes || template.nodes || []).map(
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (n: any) => ({
      id: n.sort_order || n.id,
      name: n.name || "",
      nodeType: n.node_type || "standard",
      assigneeType: n.assignee_type || "any_agent",
      assigneeId: n.assignee_id || "",
      description: n.description || "",
      timeout: 0,
      readonlyDirs: n.readonly_dirs || "",
      fullControlDirs: n.full_control_dirs || "",
      artifact: n.artifact || "",
      maxRejectCycles: n.max_reject_cycles || 5,
    })
  );
  return {
    id: template.id || "",
    name: template.name || "",
    description: template.description || "",
    isBuiltIn: template.is_builtin || template.isBuiltIn || false,
    triggerType: (template.trigger_type || template.triggerType || "manual") as WorkflowTriggerType,
    triggerConfig: mapTriggerConfig(template.trigger_config ?? template.triggerConfig),
    triggerEnabled: template.trigger_enabled ?? template.triggerEnabled ?? true,
    nextRunAt: template.next_run_at || template.nextRunAt || "",
    lastTriggeredAt: template.last_triggered_at || template.lastTriggeredAt || "",
    nodes,
  };
}
