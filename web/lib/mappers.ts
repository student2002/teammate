// mappers.ts 将后端 snake_case API 数据映射为前端 camelCase 类型。
import type {
  MappedTask,
  MappedAgent,
  MappedDaemon,
  TaskNodeStatus,
  Skill,
  McpServer,
  McpAuthType,
  McpServerType,
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
    envVars: apiMcp.env_vars ?? apiMcp.envVars ?? {},
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

// 节点的终态集合:首个未到终态的节点即当前节点(与看板判定一致)
const TERMINAL_NODE_STATUS = new Set(["succeeded", "skipped", "cancelled"]);

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapTaskFromApi(item: any, agentsList: any[] = []): MappedTask {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const apiTask = item.task;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const taskNodes: any[] = item.nodes || [];

  const currentNodeIdx = taskNodes.findIndex((n) => !TERMINAL_NODE_STATUS.has(n.status));
  const currentNode = currentNodeIdx >= 0 ? currentNodeIdx : Math.max(0, taskNodes.length - 1);
  const currentNodeRaw = taskNodes[currentNode];

  // 任务级展示状态:终态任务直接用任务状态,活跃任务用当前节点状态,
  // 存在需人工节点时优先标注
  const priorityState = resolvePriorityState(apiTask.status, taskNodes, currentNodeRaw);

  // 续作权随节点下发(reserved_for_agent_id),聚合到任务级供徽标消费
  const reservedNode = taskNodes.find((n) => n.reserved_for_agent_id);
  const reservedForAgentId = reservedNode?.reserved_for_agent_id || null;

  const agentName = (id: unknown) => {
    if (!id) return "";
    const agent = agentsList.find((a) => a.id === id);
    return agent ? agent.name : String(id);
  };

  return {
    id: apiTask.id,
    taskRef: formatTaskRef(apiTask.id),
    title: apiTask.title || "",
    type: apiTask.type || "task",
    priority: apiTask.priority || "medium",
    priorityState,
    description: apiTask.description || "",
    constraints: apiTask.constraints || "",
    currentNode,
    currentNodeType: currentNodeRaw?.type_key || "",
    agent: agentName(currentNodeRaw?.reserved_for_agent_id) || null,
    message: "",
    nodesStatus: taskNodes.map((n) => (n.status || "waiting") as TaskNodeStatus),
    nodeAgents: taskNodes.map((n) => agentName(n.reserved_for_agent_id)),
    nodeNames: taskNodes.map((n) => n.name || ""),
    nodeTokens: item.node_tokens || {},
    dueDate: apiTask.due_date || "",
    labels: apiTask.labels || [],
    logs: {},
    comments: [],
    subTasks: [],
    gitBranch: item.git_branch || "",
    parentTask: "",
    reservedForAgent: reservedForAgentId ? agentName(reservedForAgentId) : null,
    nodeSummaries: taskNodes.reduce((acc: Record<string, string>, n: { id: string; summary?: string }) => {
      if (n.summary) acc[n.id] = n.summary;
      return acc;
    }, {}),
    workflowName: item.workflow_name || "",
    _rawNodes: taskNodes,
    _projectId: apiTask.project_id,
  };
}

function resolvePriorityState(
  taskStatus: string,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  nodes: any[],
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  currentNodeRaw: any
): TaskNodeStatus {
  if (taskStatus === "cancelled") return "cancelled";
  if (taskStatus === "completed") return "succeeded";
  if (taskStatus === "failed") return "failed";
  if (nodes.some((n) => n.status === "manual_intervention")) return "manual_intervention";
  return (currentNodeRaw?.status as TaskNodeStatus) || "waiting";
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapAgentFromApi(apiAgent: any): MappedAgent {
  return {
    id: apiAgent.id,
    name: apiAgent.name || "",
    tool: providerMap[apiAgent.provider] || apiAgent.provider || "Custom CLI",
    provider: apiAgent.provider || "",
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
    daemonId: apiAgent.daemon_id || "",
    daemonName: apiAgent.daemon_name || "",
    personaKey: apiAgent.persona_key || "",
    origin: apiAgent.origin || "",
  };
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function mapDaemonFromApi(apiDaemon: any): MappedDaemon {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const agents = (apiDaemon.agents || []).map((a: any) => ({
    name: a.name || "",
    provider: a.provider || "",
    status: a.status || "offline",
    lastReportedAt: a.last_reported_at || "",
  }));
  return {
    id: apiDaemon.id,
    name: apiDaemon.name || "",
    deviceName: apiDaemon.device_name || "",
    version: apiDaemon.version || "",
    status: apiDaemon.status === "online" ? "online" : "offline",
    lastHeartbeat: apiDaemon.last_heartbeat || "",
    agentCount: agents.length,
    createdAt: String(apiDaemon.created_at || ""),
    agents,
    providers: (apiDaemon.providers || []).map(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (p: any) => ({
        provider: p.provider || "",
        version: p.version || "",
        installed: p.installed !== false,
      })
    ),
  };
}
