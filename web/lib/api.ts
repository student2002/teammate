// api.ts 封装前端 HTTP 请求：统一鉴权头与错误处理，并提供全部后端 API 方法。
const BASE_URL = "/api";

function getAuthHeaders(): Record<string, string> {
  if (typeof window === "undefined") return {};
  const token = localStorage.getItem("token");
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function request<T = unknown>(
  method: string,
  path: string,
  body: unknown = null
): Promise<T> {
  const opts: RequestInit = {
    method,
    headers: { "Content-Type": "application/json", ...getAuthHeaders() },
  };
  if (body) opts.body = JSON.stringify(body);
  const res = await fetch(`${BASE_URL}${path}`, opts);
  if (res.status === 401) {
    localStorage.removeItem("token");
    window.dispatchEvent(new Event("auth:unauthorized"));
    throw new Error("Unauthorized");
  }
  if (!res.ok) {
    // 先按文本读取，再尝试解析为 JSON
    const text = await res.text().catch(() => "");
    let errMsg = res.statusText;
    if (text) {
      try {
        const data = JSON.parse(text);
        errMsg = data.message || data.error || errMsg;
      } catch {
        // 不是 JSON，使用纯文本
        errMsg = text;
      }
    }
    throw new Error(errMsg);
  }
  if (res.status === 204) return null as T;
  return res.json() as Promise<T>;
}

const api = {
  // 认证
  login: (email: string, password: string) =>
    request("POST", "/auth/login", { email, password }),
  register: (name: string, email: string, password: string) =>
    request("POST", "/auth/register", { name, email, password }),
  whoami: () => request("GET", "/auth/whoami"),
  switchWorkspace: (workspaceId: string) =>
    request("POST", "/auth/switch-workspace", { workspace_id: workspaceId }),

  // 工作区
  listWorkspaces: () => request("GET", "/workspaces"),
  getWorkspace: (id: string) => request("GET", `/workspaces/${id}`),
  createWorkspace: (data: Record<string, unknown>) =>
    request("POST", "/workspaces", data),
  updateWorkspace: (id: string, data: Record<string, unknown>) =>
    request("PUT", `/workspaces/${id}`, data),
  deleteWorkspace: (id: string) => request("DELETE", `/workspaces/${id}`),
  listMembers: (wsId: string) =>
    request("GET", `/workspaces/${wsId}/members`),
  createMember: (wsId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/members`, data),
  deleteMember: (wsId: string, memberId: string) =>
    request("DELETE", `/workspaces/${wsId}/members/${memberId}`),
  updateMemberRole: (wsId: string, memberId: string, role: string) =>
    request("PUT", `/workspaces/${wsId}/members/${memberId}/role`, { role }),

  // 项目
  listProjects: (wsId: string) =>
    request("GET", `/workspaces/${wsId}/projects`),
  getProject: (wsId: string, id: string) =>
    request("GET", `/workspaces/${wsId}/projects/${id}`),
  createProject: (wsId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/projects`, data),
  updateProject: (wsId: string, id: string, data: Record<string, unknown>) =>
    request("PUT", `/workspaces/${wsId}/projects/${id}`, data),
  deleteProject: (wsId: string, id: string) =>
    request("DELETE", `/workspaces/${wsId}/projects/${id}`),

  // 工作流
  listWorkflows: (wsId: string) =>
    request("GET", `/workspaces/${wsId}/workflows`),
  createWorkflow: (wsId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/workflows`, data),
  updateWorkflow: (wsId: string, id: string, data: Record<string, unknown>) =>
    request("PUT", `/workspaces/${wsId}/workflows/${id}`, data),
  deleteWorkflow: (wsId: string, id: string) =>
    request("DELETE", `/workspaces/${wsId}/workflows/${id}`),

  // 代理
  listAgents: (wsId: string) =>
    request("GET", `/workspaces/${wsId}/agents`),
  getAgent: (wsId: string, id: string) =>
    request("GET", `/workspaces/${wsId}/agents/${id}`),
  createAgent: (wsId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/agents`, data),
  updateAgent: (wsId: string, id: string, data: Record<string, unknown>) =>
    request("PUT", `/workspaces/${wsId}/agents/${id}`, data),
  deleteAgent: (wsId: string, id: string) =>
    request("DELETE", `/workspaces/${wsId}/agents/${id}`),
  listAgentSkills: (wsId: string, agentId: string) =>
    request("GET", `/workspaces/${wsId}/agents/${agentId}/skills`),
  addAgentSkill: (wsId: string, agentId: string, skillId: string) =>
    request("POST", `/workspaces/${wsId}/agents/${agentId}/skills`, {
      skill_id: skillId,
      enabled: true,
    }),
  removeAgentSkill: (wsId: string, agentId: string, skillId: string) =>
    request("DELETE", `/workspaces/${wsId}/agents/${agentId}/skills/${skillId}`),
  listAgentMcpServers: (wsId: string, agentId: string) =>
    request("GET", `/workspaces/${wsId}/agents/${agentId}/mcp-servers`),
  addAgentMcpServer: (wsId: string, agentId: string, serverId: string) =>
    request("POST", `/workspaces/${wsId}/agents/${agentId}/mcp-servers`, {
      mcp_server_id: serverId,
      enabled: true,
    }),
  removeAgentMcpServer: (wsId: string, agentId: string, serverId: string) =>
    request("DELETE", `/workspaces/${wsId}/agents/${agentId}/mcp-servers/${serverId}`),

  // 技能
  listSkills: (wsId: string) =>
    request("GET", `/workspaces/${wsId}/skills`),
  createSkill: (wsId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/skills`, data),
  updateSkill: (wsId: string, id: string, data: Record<string, unknown>) =>
    request("PUT", `/workspaces/${wsId}/skills/${id}`, data),
  deleteSkill: (wsId: string, id: string) =>
    request("DELETE", `/workspaces/${wsId}/skills/${id}`),

  // MCP 服务
  listMcpServers: (wsId: string) =>
    request("GET", `/workspaces/${wsId}/mcp-servers`),
  createMcpServer: (wsId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/mcp-servers`, data),
  updateMcpServer: (wsId: string, id: string, data: Record<string, unknown>) =>
    request("PUT", `/workspaces/${wsId}/mcp-servers/${id}`, data),
  deleteMcpServer: (wsId: string, id: string) =>
    request("DELETE", `/workspaces/${wsId}/mcp-servers/${id}`),

  // 任务
  listTasks: (projectId: string, status?: string) =>
    request(
      "GET",
      `/projects/${projectId}/tasks${status ? `?status=${status}` : ""}`
    ),
  listTasksPaginated: (projectId: string, params: { status?: string; q?: string; limit?: number; offset?: number }) => {
    const sp = new URLSearchParams();
    if (params.status) sp.set("status", params.status);
    if (params.q) sp.set("q", params.q);
    if (params.limit !== undefined) sp.set("limit", String(params.limit));
    if (params.offset !== undefined) sp.set("offset", String(params.offset));
    const qs = sp.toString();
    return request("GET", `/projects/${projectId}/tasks${qs ? `?${qs}` : ""}`);
  },
  getTask: (projectId: string, id: number) =>
    request("GET", `/projects/${projectId}/tasks/${id}`),
  createTask: (projectId: string, data: Record<string, unknown>) =>
    request("POST", `/projects/${projectId}/tasks`, data),
  updateTask: (projectId: string, id: number, data: Record<string, unknown>) =>
    request("PUT", `/projects/${projectId}/tasks/${id}`, data),
  deleteTask: (projectId: string, id: number) =>
    request("DELETE", `/projects/${projectId}/tasks/${id}`),

  // 节点操作
  claimNode: (taskId: number, nodeId: string, data?: Record<string, unknown>) =>
    request("POST", `/tasks/${taskId}/nodes/${nodeId}/claim`, data),
  approveNode: (
    taskId: number,
    nodeId: string,
    data?: Record<string, unknown>
  ) => request("POST", `/tasks/${taskId}/nodes/${nodeId}/approve`, data),
  rejectNode: (
    taskId: number,
    nodeId: string,
    data?: Record<string, unknown>
  ) => request("POST", `/tasks/${taskId}/nodes/${nodeId}/reject`, data),
  manualIntervention: (
    taskId: number,
    nodeId: string,
    data?: Record<string, unknown>
  ) => request("POST", `/tasks/${taskId}/nodes/${nodeId}/manual`, data),
  resolveNode: (
    taskId: number,
    nodeId: string,
    data?: Record<string, unknown>
  ) => request("POST", `/tasks/${taskId}/nodes/${nodeId}/resolve`, data),

  // 节点流转记录
  getNodeTransitions: (taskId: number, nodeId: string) =>
    request("GET", `/tasks/${taskId}/nodes/${nodeId}/transitions`),

  // 评论
  listComments: (taskId: number, params?: { nodeId?: string; scope?: "task" | "execution_context" }) => {
    const query = new URLSearchParams();
    if (params?.nodeId) query.set("node_id", params.nodeId);
    if (params?.scope) query.set("scope", params.scope);
    const suffix = query.toString();
    return request("GET", `/tasks/${taskId}/comments${suffix ? `?${suffix}` : ""}`);
  },
  createComment: (
    taskId: number,
    data: { content: string; node_id?: string; source_node_id?: string; comment_type?: string; mentions?: string[] } & Record<string, unknown>
  ) => request("POST", `/tasks/${taskId}/comments`, data),

  // 运行时
  registerRuntime: (workspaceId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${workspaceId}/runtimes`, data),
  heartbeat: (workspaceId: string, id: string) =>
    request("POST", `/workspaces/${workspaceId}/runtimes/${id}/heartbeat`),

  // Token 用量
  reportTokenUsage: (taskId: number, data: Record<string, unknown>) =>
    request("POST", `/tasks/${taskId}/token-usage`, data),

  // 搜索
  searchTasks: (workspaceId: string, q: string, projectId?: string) =>
    request(
      "GET",
      `/workspaces/${workspaceId}/search/tasks?q=${encodeURIComponent(q)}${projectId ? `&projectId=${projectId}` : ""}`
    ),
  searchAgents: (workspaceId: string, q: string) =>
    request(
      "GET",
      `/workspaces/${workspaceId}/search/agents?q=${encodeURIComponent(q)}`
    ),

  // 通知
  listNotifications: (workspaceId: string, memberId?: string) =>
    request(
      "GET",
      `/workspaces/${workspaceId}/notifications${memberId ? `?member_id=${memberId}` : ""}`
    ),

  // 看板
  getBoardData: (projectId: string) =>
    request("GET", `/projects/${projectId}/board`),

  // 审查
  getReviewQueue: (projectId: string) =>
    request("GET", `/projects/${projectId}/review/review-queue`),
  checkSelfReview: (taskId: number, nodeId: string) =>
    request("GET", `/tasks/${taskId}/review/nodes/${nodeId}/self-review-check`),

  // 项目成员
  listProjectMembers: (wsId: string, projectId: string) =>
    request("GET", `/workspaces/${wsId}/projects/${projectId}/members`),
  addProjectMember: (wsId: string, projectId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/projects/${projectId}/members`, data),
  removeProjectMember: (wsId: string, projectId: string, memberId: string) =>
    request("DELETE", `/workspaces/${wsId}/projects/${projectId}/members/${memberId}`),

  // 项目审查员
  listProjectReviewers: (wsId: string, projectId: string) =>
    request("GET", `/workspaces/${wsId}/projects/${projectId}/reviewers`),
  addProjectReviewer: (wsId: string, projectId: string, data: Record<string, unknown>) =>
    request("POST", `/workspaces/${wsId}/projects/${projectId}/reviewers`, data),
  removeProjectReviewer: (wsId: string, projectId: string, reviewerId: string) =>
    request("DELETE", `/workspaces/${wsId}/projects/${projectId}/reviewers/${reviewerId}`),

  // Git 凭据
  listGitCredentials: (projectId: string) =>
    request("GET", `/projects/${projectId}/git-credentials`),
  createGitCredential: (projectId: string, data: Record<string, unknown>) =>
    request("POST", `/projects/${projectId}/git-credentials`, data),
  updateGitCredential: (
    projectId: string,
    credentialId: string,
    data: Record<string, unknown>
  ) =>
    request(
      "PUT",
      `/projects/${projectId}/git-credentials/${credentialId}`,
      data
    ),

  // 记忆
  listMemories: (params: Record<string, string>) => {
    const query = new URLSearchParams(params).toString();
    return request("GET", `/memories?${query}`);
  },
  createMemory: (data: Record<string, unknown>) =>
    request("POST", "/memories", data),
  deleteMemory: (id: string) => request("DELETE", `/memories/${id}`),
  searchMemories: (params: Record<string, string>) => {
    const query = new URLSearchParams(params).toString();
    return request("GET", `/memories/search?${query}`);
  },

  // 社区工作流
  getCommunityWorkflows: () => request("GET", "/community/workflows"),
  createCommunityWorkflow: (data: Record<string, unknown>) =>
    request("POST", "/community/workflows", data),
  importCommunityWorkflow: (workflowId: string, workspaceId: string) =>
    request("POST", `/community/workflows/${workflowId}/import`, { workspace_id: workspaceId }),
};

export default api;
