// store.ts 定义全局 Zustand 状态：认证、工作区、项目、任务、技能、MCP 与日志等。
import { create } from "zustand";
import type {
  Workspace,
  Project,
  MappedAgent,
  MappedTask,
  Skill,
  McpServer,
  User,
} from "@/lib/types";
import type { TemplateListItem } from "@/lib/workflow/types";
import type { LogMessage } from "@/lib/use-task-logs";
import api, { type WorkspaceIntegration } from "@/lib/api";
import {
  mapTaskFromApi,
  mapAgentFromApi,
  mapSkillFromApi,
  mapMcpServerFromApi,
  mapWorkspaceFromApi,
} from "@/lib/mappers";

// API 响应的类型辅助
type ProjectList = Project[];
type AgentList = { id: string; name: string; provider: string; status: string; instructions: string }[];
type IntegrationList = WorkspaceIntegration[];
type WorkflowList = TemplateListItem[];
type SkillList = unknown[];
type McpList = unknown[];
type TaskWithNodesList = { task: Record<string, unknown>; nodes: Record<string, unknown>[] }[];

interface AppState {
  // 认证
  user: User | null;
  setUser: (user: User | null) => void;

  // 工作区
  workspaces: Workspace[];
  currentWorkspaceId: string | null;
  currentWs: Workspace | null;
  setWorkspaces: (ws: Workspace[]) => void;
  setCurrentWorkspaceId: (id: string | null) => void;
  createWorkspace: (name: string, issuePrefix: string, description?: string) => Promise<Workspace>;
  switchWorkspace: (id: string) => Promise<void>;

  // 项目
  projects: Project[];
  currentProjectId: string | null;
  setProjects: (projects: Project[] | ((prev: Project[]) => Project[])) => void;
  setCurrentProjectId: (id: string | null) => void;

  // 代理
  agents: MappedAgent[];
  setAgents: (agents: MappedAgent[]) => void;

  // 工作区集成(工作流节点的集成下拉数据源)
  integrations: WorkspaceIntegration[];
  setIntegrations: (integrations: WorkspaceIntegration[]) => void;
  refreshIntegrations: () => Promise<void>;

  // 任务
  tasks: MappedTask[];
  setTasks: (tasks: MappedTask[]) => void;
  // 最近一次任务列表加载是否失败(失败时保留 last-good 数据,视图据此显示降级提示)
  loadError: boolean;

  // 模板(整图列表,snake_case 原样保存)
  templates: TemplateListItem[];
  setTemplates: (templates: TemplateListItem[]) => void;
  refreshTemplates: () => Promise<void>;

  // 技能与 MCP
  skills: Skill[];
  mcpServers: McpServer[];
  setSkills: (skills: Skill[]) => void;
  setMcpServers: (servers: McpServer[]) => void;

  // 自定义主题(运行时上传,部署级共享)
  customThemes: import("./themes/dynamic").CustomThemeInfo[];
  setCustomThemes: (themes: import("./themes/dynamic").CustomThemeInfo[]) => void;

  // 加载
  loading: boolean;
  setLoading: (loading: boolean) => void;

  // 数据加载
  loadInitialData: () => Promise<void>;
  loadProjectData: (projectId: string) => Promise<void>;

  // 历史任务 (completed/cancelled tasks with pagination)
  historyTasks: MappedTask[];
  historyTotal: number;
  historyPage: number;
  historySearchQuery: string;
  historyLoading: boolean;
  setHistoryTasks: (tasks: MappedTask[]) => void;
  setHistoryTotal: (total: number) => void;
  setHistoryPage: (page: number) => void;
  setHistorySearchQuery: (q: string) => void;
  setHistoryLoading: (loading: boolean) => void;
  loadHistoryTasks: (projectId: string, page?: number, q?: string) => Promise<void>;

  // 任务日志（通过 WebSocket 实时获取）
  taskLogs: Record<number, Record<string, string[]>>; // taskId -> nodeId -> log lines
  addLog: (taskId: number, msg: LogMessage) => void;
  setLogs: (taskId: number, logs: LogMessage[]) => void;
  clearLogs: (taskId: number) => void;
  clearNodeLogs: (taskId: number, nodeId: string) => void;
}

export const useAppStore = create<AppState>((set, get) => ({
  // 认证
  user: null,
  setUser: (user) => {
    set({ user });
    if (typeof window !== "undefined") {
      if (user) {
        localStorage.setItem("user", JSON.stringify(user));
      } else {
        localStorage.removeItem("user");
      }
    }
  },

  // 工作区
  workspaces: [],
  currentWorkspaceId: null,
  currentWs: null,
  setWorkspaces: (workspaces) => set({ workspaces }),
  setCurrentWorkspaceId: (id) => {
    const ws = get().workspaces.find((w) => w.id === id) || null;
    if (typeof window !== "undefined" && id) {
      localStorage.setItem("currentWorkspaceId", id);
    }
    set({ currentWorkspaceId: id, currentWs: ws });
  },

  createWorkspace: async (name, issuePrefix, description) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const rawCreated = (await api.createWorkspace({
      name,
      issue_prefix: issuePrefix,
      description: description || "",
    })) as Record<string, unknown>;
    const created = mapWorkspaceFromApi(rawCreated);
    const wsList = [...get().workspaces, created];
    set({ workspaces: wsList });
    await get().switchWorkspace(created.id);
    return created;
  },

  switchWorkspace: async (id) => {
    const result = (await api.switchWorkspace(id)) as {
      workspace_id: string;
      role: string;
    };
    if (typeof window !== "undefined") {
      localStorage.setItem("currentWorkspaceId", id);
    }
    const ws = get().workspaces.find((w) => w.id === id) || null;
    // 更新用户的 workspaceId 和角色
    const currentUser = get().user;
    if (currentUser) {
      const updatedUser = { ...currentUser, workspaceId: id, role: result.role };
      set({ user: updatedUser });
      if (typeof window !== "undefined") {
        localStorage.setItem("user", JSON.stringify(updatedUser));
      }
    }
    set({
      currentWorkspaceId: id,
      currentWs: ws,
      projects: [],
      agents: [],
      integrations: [],
      tasks: [],
      templates: [],
      skills: [],
      mcpServers: [],
      currentProjectId: null,
    });
    if (ws) {
      const [projList, agentList, integrationList, wfList, skillList, mcpList] =
        await Promise.all([
          api.listProjects(id).catch(() => []) as Promise<ProjectList>,
          api.listAgents(id).catch(() => []) as Promise<AgentList>,
          api.listWorkspaceIntegrations(id).catch(() => ({ integrations: [] })) as Promise<{ integrations: IntegrationList }>,
          api.listWorkflows(id).catch(() => []) as Promise<WorkflowList>,
          api.listSkills(id).catch(() => []) as Promise<SkillList>,
          api.listMcpServers(id).catch(() => []) as Promise<McpList>,
        ]);
      set({ projects: projList || [] });
      const mappedAgents = (agentList || []).map(mapAgentFromApi);
      set({ agents: mappedAgents });
      set({ integrations: integrationList.integrations || [] });
      set({ templates: wfList || [] });
      set({ skills: (skillList || []).map(mapSkillFromApi) });
      set({ mcpServers: (mcpList || []).map(mapMcpServerFromApi) });
      if (projList && projList.length > 0) {
        const pId = projList[0].id;
        set({ currentProjectId: pId });
        await get().loadProjectData(pId);
      }
    }
  },

  // 项目
  projects: [],
  currentProjectId: null,
  setProjects: (projects) => {
    const resolved = typeof projects === "function" ? projects(get().projects) : projects;
    set({ projects: resolved });
  },
  setCurrentProjectId: (id) => set({ currentProjectId: id }),

  // 代理
  agents: [],
  setAgents: (agents) => set({ agents }),

  // 工作区集成
  integrations: [],
  setIntegrations: (integrations) => set({ integrations }),
  refreshIntegrations: async () => {
    const wsId = get().currentWorkspaceId;
    if (!wsId) return;
    try {
      const result = (await api.listWorkspaceIntegrations(wsId)) as { integrations: IntegrationList };
      set({ integrations: result.integrations || [] });
    } catch {
      // 失败时保留 last-good 集成数据
    }
  },

  // 任务
  tasks: [],
  setTasks: (tasks) => set({ tasks }),
  loadError: false,

  // 模板(整图列表)
  templates: [],
  setTemplates: (templates) => set({ templates }),
  refreshTemplates: async () => {
    const wsId = get().currentWorkspaceId;
    if (!wsId) return;
    try {
      const wfList = (await api.listWorkflows(wsId).catch(() => [])) as WorkflowList;
      set({ templates: wfList || [] });
    } catch {
      // 保留 last-good 模板数据
    }
  },

  // 技能与 MCP
  skills: [],
  mcpServers: [],
  setSkills: (skills) => set({ skills }),
  setMcpServers: (mcpServers) => set({ mcpServers }),

  // 自定义主题(运行时上传,部署级共享)
  customThemes: [],
  setCustomThemes: (customThemes) => set({ customThemes }),

  // 加载
  loading: true,
  setLoading: (loading) => set({ loading }),

  // 数据加载
  loadInitialData: async () => {
    const token =
      typeof window !== "undefined" ? localStorage.getItem("token") : null;
    if (!token) {
      set({ loading: false });
      return;
    }
    try {
      set({ loading: true });
      // listWorkspaces 是主要的认证检查 — 不要对其 .catch()。
      // 若 Token 无效/已被吊销，会抛出 "Unauthorized"，由下方统一处理。
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const rawWsList = (await api.listWorkspaces()) as Record<string, unknown>[];
      const wsList = (rawWsList || []).map(mapWorkspaceFromApi);
      // Token 有效 — 从 whoami 获取最新用户信息。切换工作区时刷新角色。
      try {
        const whoami = (await api.whoami()) as {
          id: string; name: string; email: string; role: string; workspace_id: string;
        };
        const freshUser = {
          id: whoami.id,
          name: whoami.name,
          email: whoami.email,
          role: whoami.role,
          workspaceId: whoami.workspace_id,
        };
        set({ user: freshUser });
        if (typeof window !== "undefined") {
          localStorage.setItem("user", JSON.stringify(freshUser));
        }
      } catch {
        // whoami 失败时回退到 localStorage
        const savedUser = typeof window !== "undefined" ? localStorage.getItem("user") : null;
        if (savedUser) {
          try { set({ user: JSON.parse(savedUser) }); } catch { /* 忽略 */ }
        }
      }
      set({ workspaces: wsList || [] });
      if (wsList && wsList.length > 0) {
        // 从 localStorage 恢复上次选中的工作区
        const savedWsId = typeof window !== "undefined" ? localStorage.getItem("currentWorkspaceId") : null;
        const wsId = (savedWsId && wsList.find((w) => w.id === savedWsId)) ? savedWsId : wsList[0].id;
        const ws = wsList.find((w) => w.id === wsId) || wsList[0];
        set({
          currentWorkspaceId: wsId,
          currentWs: ws,
        });
        try {
          const switched = (await api.switchWorkspace(wsId)) as { role: string };
          const currentUser = get().user;
          if (currentUser) {
            const updatedUser = { ...currentUser, workspaceId: wsId, role: switched.role };
            set({ user: updatedUser });
            localStorage.setItem("user", JSON.stringify(updatedUser));
          }
        } catch {
          // 工作区列表刚由服务端加载；若此处失败，后续加载会暴露问题。
        }
        const [projList, agentList, integrationList, wfList, skillList, mcpList] =
          await Promise.all([
            api.listProjects(wsId).catch(() => []) as Promise<ProjectList>,
            api.listAgents(wsId).catch(() => []) as Promise<AgentList>,
            api.listWorkspaceIntegrations(wsId).catch(() => ({ integrations: [] })) as Promise<{ integrations: IntegrationList }>,
            api.listWorkflows(wsId).catch(() => []) as Promise<WorkflowList>,
            api.listSkills(wsId).catch(() => []) as Promise<SkillList>,
            api.listMcpServers(wsId).catch(() => []) as Promise<McpList>,
          ]);
        set({ projects: projList || [] });
        const mappedAgents = (agentList || []).map(mapAgentFromApi);
        set({ agents: mappedAgents });
        set({ integrations: integrationList.integrations || [] });
        set({ templates: wfList || [] });
        set({ skills: (skillList || []).map(mapSkillFromApi) });
        set({ mcpServers: (mcpList || []).map(mapMcpServerFromApi) });
        if (projList && projList.length > 0) {
          const pId = projList[0].id;
          set({ currentProjectId: pId });
          await get().loadProjectData(pId);
        } else {
          set({ tasks: [] });
        }
      }
    } catch (err) {
      console.error("Failed to load data from API:", err);
      // 若 Token 无效/已被吊销，清除认证状态并跳转登录页
      if (err instanceof Error && err.message === "Unauthorized") {
        localStorage.removeItem("token");
        localStorage.removeItem("user");
        get().setUser(null);
      }
      // 其他错误（网络、服务端）保持用户登录状态
    } finally {
      set({ loading: false });
    }
  },

  loadProjectData: async (projectId: string) => {
    // 请求期间可能切走工作区:响应返回时若工作区已变,丢弃结果,
    // 避免旧工作区的任务列表(或失败清空)写入新工作区
    const wsAtStart = get().currentWorkspaceId;
    try {
      const result = (await api.listTasks(projectId, "all")) as TaskWithNodesList;
      if (get().currentWorkspaceId !== wsAtStart) return;
      const agents = get().agents;
      const mappedTasks = (result || []).map((item) => mapTaskFromApi(item, agents));
      set({ tasks: mappedTasks, loadError: false });
    } catch {
      if (get().currentWorkspaceId !== wsAtStart) return;
      // 保留 last-good 数据供看板/面板继续使用;仅当现有数据属于其它项目
      // (切换项目失败)时才清空,避免跨项目脏数据
      const current = get().tasks;
      const stale = current.length > 0 && current[0]._projectId !== projectId;
      set(stale ? { tasks: [], loadError: true } : { loadError: true });
    }
  },

  // 历史任务
  historyTasks: [],
  historyTotal: 0,
  historyPage: 1,
  historySearchQuery: "",
  historyLoading: false,
  setHistoryTasks: (tasks) => set({ historyTasks: tasks }),
  setHistoryTotal: (total) => set({ historyTotal: total }),
  setHistoryPage: (page) => set({ historyPage: page }),
  setHistorySearchQuery: (q) => set({ historySearchQuery: q }),
  setHistoryLoading: (loading) => set({ historyLoading: loading }),
  loadHistoryTasks: async (projectId, page, q) => {
    const state = get();
    const currentPage = page ?? state.historyPage;
    const searchQuery = q ?? state.historySearchQuery;
    const limit = 20;
    const offset = (currentPage - 1) * limit;

    set({ historyLoading: true });
    try {
      const result = (await api.listTasksPaginated(projectId, {
        status: "completed",
        q: searchQuery || undefined,
        limit,
        offset,
      })) as { tasks: { task: Record<string, unknown>; nodes: Record<string, unknown>[] }[]; total: number; limit: number; offset: number };

      const agents = state.agents;
      const mapped = (result.tasks || []).map((item) => mapTaskFromApi(item, agents));
      set({
        historyTasks: mapped,
        historyTotal: result.total,
        historyPage: currentPage,
        historySearchQuery: searchQuery,
      });
    } catch (e) {
      console.error("Failed to load history tasks:", e);
    } finally {
      set({ historyLoading: false });
    }
  },

  // 任务日志（通过 WebSocket 实时获取）
  taskLogs: {},
  addLog: (taskId, msg) => {
    const logs = { ...get().taskLogs };
    const taskLogs = { ...(logs[taskId] || {}) };
    const nodeId = msg.node_id || "_default";
    const lines = [...(taskLogs[nodeId] || []), msg.content];
    taskLogs[nodeId] = lines;
    logs[taskId] = taskLogs;
    set({ taskLogs: logs });
  },
  setLogs: (taskId, messages) => {
    const logs = { ...get().taskLogs };
    const taskLogs: Record<string, string[]> = {};
    for (const msg of messages) {
      const nodeId = msg.node_id || "_default";
      if (!taskLogs[nodeId]) taskLogs[nodeId] = [];
      taskLogs[nodeId].push(msg.content);
    }
    logs[taskId] = taskLogs;
    set({ taskLogs: logs });
  },
  clearLogs: (taskId) => {
    const logs = { ...get().taskLogs };
    delete logs[taskId];
    set({ taskLogs: logs });
  },
  clearNodeLogs: (taskId, nodeId) => {
    const logs = { ...get().taskLogs };
    const taskLogs = { ...(logs[taskId] || {}) };
    delete taskLogs[nodeId];
    logs[taskId] = taskLogs;
    set({ taskLogs: logs });
  },
}));

// 监听 api.ts 在收到 401 响应时派发的 auth:unauthorized 事件
// 用于处理 loadInitialData 之外发生 401 的情况（例如正常使用应用期间）
if (typeof window !== "undefined") {
  window.addEventListener("auth:unauthorized", () => {
    localStorage.removeItem("token");
    localStorage.removeItem("user");
    useAppStore.getState().setUser(null);
  });
}
