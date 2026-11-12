"use client";
// 工作台页：无选中态 = 页头 + 运行态条 + 注意队列 + 工具栏 + 协作看板/列表；
// 选中任务后切换为 TaskCockpit 内联聚焦布局（左侧任务轨 + 右侧工作区）。
// 看板列语义与后端一致：pending / in_progress / succeeded / failed / manual_intervention。

import React, { useState, useEffect, useMemo, useRef } from "react";
import {
  Search,
  CheckCircle2,
  AlertTriangle,
  Inbox,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { mapTaskFromApi } from "@/lib/mappers";
import PageHeader from "@/lib/ui/PageHeader";
import SegmentedControl from "@/lib/ui/SegmentedControl";
import EmptyState from "@/lib/ui/EmptyState";
import TaskStateBadge from "@/lib/product/TaskStateBadge";
import type { MappedTask } from "@/lib/types";
import TaskBoard, { type BoardColumn, type BoardColumnType } from "@/lib/dashboard/TaskBoard";
import TaskList from "@/lib/dashboard/TaskList";
import TaskCockpit from "@/lib/dashboard/TaskCockpit";
import AgentNetworkPanel from "@/lib/agent-network/AgentNetworkPanel";
import { useTranslations } from "next-intl";

const boardTypeMap: Record<string, BoardColumnType> = {
  pending: "pending",
  in_progress: "progress",
  succeeded: "succeeded",
  failed: "failed",
  manual_intervention: "manual",
};

type BoardApiTask = {
  id: number;
  title: string;
  priority: string;
  type: string;
  current_node_status: string;
  current_node_name: string;
  current_node_type: string;
  assignee_id?: string | null;
};

/** 看板 API 行在 store 中找不到完整任务时的最小映射（仅够看板卡展示） */
function minimalTask(bt: BoardApiTask, projectId: string | null): MappedTask {
  return {
    id: bt.id,
    taskRef: `T-${bt.id}`,
    title: bt.title,
    priority: bt.priority as "low" | "medium" | "high" | "urgent",
    type: (bt.type || "task") as "story" | "bug" | "task",
    priorityState: (bt.current_node_status || "waiting") as MappedTask["priorityState"],
    currentNodeType: bt.current_node_type || "",
    description: "",
    constraints: "",
    currentNode: 0,
    agent: bt.assignee_id || null,
    message: "",
    nodesStatus: [bt.current_node_status as MappedTask["priorityState"]],
    nodeAgents: bt.assignee_id ? [bt.assignee_id] : [],
    nodeNames: bt.current_node_name ? [bt.current_node_name] : [],
    nodeTokens: {},
    dueDate: "",
    labels: [],
    logs: {},
    comments: [],
    subTasks: [],
    gitBranch: "",
    parentTask: "",
    reservedForAgent: bt.assignee_id || null,
    nodeSummaries: {},
    workflowName: "",
    _rawNodes: [],
    _projectId: projectId || "",
  };
}

export default function DashboardPage() {
  const t = useTranslations("dashboard");
  const tasks = useAppStore((s) => s.tasks);
  const agents = useAppStore((s) => s.agents);
  const projects = useAppStore((s) => s.projects);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const setCurrentProjectId = useAppStore((s) => s.setCurrentProjectId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);
  const loadError = useAppStore((s) => s.loadError);

  const [selectedTask, setSelectedTask] = useState<MappedTask | null>(null);
  // 深链选中的任务 id：该任务进入列表前，同步 effect 不因缺失而关闭面板
  const deepLinkTaskRef = useRef<number | null>(null);

  // Deep link /dashboard?task=N: open the task cockpit even when the task is
  // outside the current project filter — locate it across projects, switch
  // the project context to its owner, then select it. The lookup is not
  // cancelled by effect re-runs: switching the project mid-flight changes
  // the deps and would swallow the setSelectedTask of the in-flight pass;
  // the guard at the top plus the idempotent set make re-entry harmless.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const taskParam = params.get("task");
    if (!taskParam || projects.length === 0) return;
    const id = Number(taskParam);
    if (!Number.isFinite(id) || id <= 0) return;
    if (selectedTask?.id === id || tasks.some((task) => task.id === id)) return;

    (async () => {
      for (const p of projects) {
        try {
          // The single-task endpoint returns the bare task object (no nodes
          // wrapper), which is enough to locate the owning project.
          const raw = (await api.getTask(p.id, id)) as { id?: number } | null;
          if (raw && typeof raw.id === "number") {
            if (p.id !== currentProjectId) {
              setCurrentProjectId(p.id);
              await loadProjectData(p.id);
            }
            deepLinkTaskRef.current = id;
            // Prefer the fully-shaped task (with nodes) from the refreshed
            // store list; fall back to a bare mapping for the odd case where
            // the list filter hides it.
            const listed = useAppStore.getState().tasks.find((task) => task.id === id);
            setSelectedTask(listed ?? mapTaskFromApi({ task: raw, nodes: [] }));
            return;
          }
        } catch {
          // the task does not belong to this project; try the next one
        }
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projects.length, currentProjectId, tasks.length]);

  // 使 selectedTask 与 store 更新保持同步（如取消/编辑之后）
  useEffect(() => {
    if (selectedTask) {
      const updated = tasks.find((task) => task.id === selectedTask.id);
      if (updated) {
        deepLinkTaskRef.current = null;
        setSelectedTask(updated);
      } else if (deepLinkTaskRef.current !== selectedTask.id) {
        // 任务已被移除（如已删除）。深链任务在目标工程列表刷新完成前
        // 可能暂不在当前列表中，此时不关闭面板
        setSelectedTask(null);
      }
    }
  }, [tasks, selectedTask]);

  const [searchQuery, setSearchQuery] = useState("");
  const [filterAgent, setFilterAgent] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<string>("kanban");
  const [activeTab, setActiveTab] = useState<"active" | "archived">("active");
  const [archivedTasks, setArchivedTasks] = useState<MappedTask[]>([]);
  const [boardColumns, setBoardColumns] = useState<{ key: string; label: string; tasks: BoardApiTask[] }[]>([]);

  const searched = searchQuery
    ? tasks.filter(
        (task) =>
          task.title.toLowerCase().includes(searchQuery.toLowerCase()) ||
          String(task.id).includes(searchQuery)
      )
    : tasks;

  const filteredByAgent = filterAgent
    ? searched.filter((task) => (task.nodeAgents?.[task.currentNode] || task.agent)?.includes(filterAgent))
    : searched;

  const refreshBoard = async (projectId: string) => {
    try {
      const data = await api.getBoardData(projectId);
      setBoardColumns(
        (data as { columns?: { key: string; label: string; tasks: BoardApiTask[] }[] }).columns || []
      );
    } catch (e) {
      // 保留 last-good 看板数据:任务数据仍在 store 中,本地分组兜底可用
      console.error("Failed to fetch board data:", e);
    }
  };

  // 看板模式下从 API 获取看板数据（10s 轮询；同时刷新完整任务数据）
  useEffect(() => {
    if (viewMode === "list" || !currentProjectId) return;
    const fetchBoard = async () => {
      await refreshBoard(currentProjectId);
      loadProjectData(currentProjectId);
    };
    fetchBoard();
    const interval = setInterval(fetchBoard, 10000);
    return () => clearInterval(interval);
  }, [viewMode, currentProjectId, loadProjectData]);

  // 列表模式下自动刷新任务列表
  useEffect(() => {
    if (viewMode !== "list" || !currentProjectId) return;
    const interval = setInterval(() => {
      loadProjectData(currentProjectId);
    }, 10000);
    return () => clearInterval(interval);
  }, [viewMode, currentProjectId, loadProjectData]);

  // 激活归档页签时获取已归档（已取消）任务
  useEffect(() => {
    if (activeTab !== "archived" || !currentProjectId) return;
    const fetchArchived = async () => {
      try {
        const data = (await api.listTasks(currentProjectId, "cancelled")) as {
          task?: Record<string, unknown>;
          nodes?: Record<string, unknown>[];
        }[];
        const mapped = (data || [])
          .filter((item) => item.task)
          .map((item) => ({ ...mapTaskFromApi(item, agents), priorityState: "cancelled" as const }));
        setArchivedTasks(mapped);
      } catch (err) {
        console.error("Failed to fetch archived tasks:", err);
      }
    };
    fetchArchived();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab, currentProjectId]);

  const groups = {
    manual_intervention: filteredByAgent.filter((task) => task.priorityState === "manual_intervention"),
    in_progress: filteredByAgent.filter((task) => task.priorityState === "running"),
    pending: filteredByAgent.filter((task) => task.priorityState === "waiting" || task.priorityState === "ready"),
    succeeded: filteredByAgent.filter((task) => task.priorityState === "succeeded"),
    failed: filteredByAgent.filter((task) => task.priorityState === "failed"),
  };

  // 看板 API 列 → 视图列（store 中有完整任务用完整任务，否则最小映射）
  const boardColumnsView: BoardColumn[] = useMemo(
    () =>
      boardColumns.map((col) => ({
        key: col.key,
        label: col.label,
        type: boardTypeMap[col.key] || "pending",
        tasks: col.tasks.map((bt) => tasks.find((task) => task.id === bt.id) || minimalTask(bt, currentProjectId)),
      })),
    [boardColumns, tasks, currentProjectId]
  );

  // 看板 API 不可用时的本地分组兜底（列顺序与后端一致）
  const fallbackColumns: BoardColumn[] = [
    { key: "pending", label: t("kanbanColumn.pending"), type: "pending", tasks: groups.pending },
    { key: "in_progress", label: t("kanbanColumn.running"), type: "progress", tasks: groups.in_progress },
    { key: "succeeded", label: t("kanbanColumn.succeeded"), type: "succeeded", tasks: groups.succeeded },
    { key: "failed", label: t("kanbanColumn.failed"), type: "failed", tasks: groups.failed },
    { key: "manual_intervention", label: t("kanbanColumn.manualIntervention"), type: "manual", tasks: groups.manual_intervention },
  ];

  const activeViewTasks = activeTab === "archived" ? archivedTasks : filteredByAgent;

  // 注意队列：需人工介入 + 失败的任务
  const attentionTasks = useMemo(
    () =>
      [...filteredByAgent]
        .filter((task) => task.priorityState === "manual_intervention" || task.priorityState === "failed")
        .sort((a, b) => (a.priorityState === "manual_intervention" ? -1 : 1) - (b.priorityState === "manual_intervention" ? -1 : 1)),
    [filteredByAgent]
  );

  const inProgressCount = tasks.filter((task) => task.priorityState === "running").length;
  const pendingCount = tasks.filter((task) => task.priorityState === "waiting" || task.priorityState === "ready").length;
  const manualCount = tasks.filter((task) => task.priorityState === "manual_intervention").length;
  const onlineAgents = agents.filter((a) => a.status === "online" || a.status === "busy").length;
  const currentProject = projects.find((p) => p.id === currentProjectId);

  // ---- 业务逻辑处理器 ----

  // human.input 人工提交(submitted/cancelled),成功后刷新任务与看板
  const handleSubmitHuman = async (taskId: number, nodeId: string, action: "submitted" | "cancelled", data: Record<string, unknown>) => {
    await api.submitHumanInput(taskId, nodeId, { action, data });
    if (currentProjectId) {
      await loadProjectData(currentProjectId);
      if (viewMode === "kanban") await refreshBoard(currentProjectId);
    }
  };

  const handleCancelTask = async (taskId: number) => {
    if (!currentProjectId) return;
    try {
      await api.updateTask(currentProjectId, taskId, { status: "cancelled" });
      await loadProjectData(currentProjectId);
      if (viewMode === "kanban") await refreshBoard(currentProjectId);
      setSelectedTask(null);
    } catch (err) {
      console.error("Cancel task failed:", err);
    }
  };

  const handleAddComment = async (taskId: number, text: string, mentions?: string[]) => {
    try {
      await api.createComment(taskId, { content: text, mentions: mentions || [] });
      if (currentProjectId) await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Add comment failed:", err);
    }
  };

  const handleEditTask = async (taskId: number, updates: { title: string; description: string; priority: string }) => {
    if (!currentProjectId) return;
    try {
      await api.updateTask(currentProjectId, taskId, updates);
      await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Edit task failed:", err);
    }
  };


  const handleDeleteTask = async (taskId: number) => {
    if (!currentProjectId) return;
    try {
      await api.deleteTask(currentProjectId, taskId);
      // 乐观地从本地状态移除，而非重新加载所有任务
      const { tasks: current } = useAppStore.getState();
      useAppStore.setState({ tasks: current.filter((task) => task.id !== taskId) });
    } catch (err) {
      console.error("Delete task failed:", err);
    }
  };

  // ============ 任务选中态：Task Cockpit ============
  if (selectedTask) {
    return (
      <div className="h-full page-enter">
        <TaskCockpit
          task={selectedTask}
          trackTasks={activeViewTasks}
          agents={agents}
          onSelectTask={setSelectedTask}
          onClose={() => setSelectedTask(null)}
          onSubmitHuman={handleSubmitHuman}
          onEditTask={handleEditTask}
          onAddComment={handleAddComment}
          onDeleteTask={handleDeleteTask}
          onCancelTask={handleCancelTask}
        />
      </div>
    );
  }

  // ============ 无选中态 ============
  return (
    <div className="flex flex-col h-full page-enter">
      <PageHeader
        title={t("title")}
        meta={
          <>
            <span className="text-xs text-fg-subtle">{currentProject?.name || t("projects.allProjects")}</span>
            <span className="text-fg-faint">·</span>
            <span className="text-xs text-fg-faint">{t("filters.taskCount", { count: activeViewTasks.length })}</span>
          </>
        }
      />

      {/* 运行态条 */}
      <div className="px-6 pb-3 flex items-center gap-2.5 flex-wrap">
        <StatBlock dot="bg-accent-fg" label={t("stats.inProgress")} value={inProgressCount} />
        <StatBlock dot="bg-fg-faint" label={t("stats.pendingClaim")} value={pendingCount} />
        <StatBlock dot="bg-warning-fg" label={t("stats.needManual")} value={manualCount} />
        <StatBlock dot="bg-success-fg" label={t("stats.onlineAgents")} value={onlineAgents} />
      </div>

      {/* 3D Agent 协作网络:真实 Agent/任务数据,点击节点联动上下文,"查看相关任务"设 filterAgent */}
      <AgentNetworkPanel
        agents={agents}
        tasks={tasks}
        projectId={currentProjectId}
        onViewTasks={setFilterAgent}
      />

      {/* 注意队列 */}
      {attentionTasks.length === 0 ? (
        <div className="mx-6 mb-3 h-[34px] px-3 flex items-center gap-2 text-xs text-fg-faint border border-line/60 rounded-[var(--radius-control)] bg-surface-2/20">
          <CheckCircle2 className="w-3.5 h-3.5 text-success-fg shrink-0" />
          {t("attention.empty")}
        </div>
      ) : (
        <div className="mx-6 mb-3 border border-warning/25 rounded-[var(--radius-card)] bg-warning/5 divide-y divide-warning/10 overflow-hidden">
          {attentionTasks.slice(0, 3).map((task) => (
            <button
              key={task.id}
              onClick={() => setSelectedTask(task)}
              className="w-full text-left px-3 py-2 flex items-center gap-2.5 hover:bg-warning/10 transition-colors-fast focus-visible:outline-none"
            >
              <AlertTriangle className="w-3.5 h-3.5 text-warning-fg shrink-0" />
              <span className="text-[10px] font-mono text-fg-faint shrink-0">{task.taskRef}</span>
              <span className="text-xs text-fg-muted truncate flex-1">{task.title}</span>
              {task.message && (
                <span className="text-[10px] text-warning-fg truncate max-w-[240px] hidden md:block">{task.message}</span>
              )}
              <TaskStateBadge state={task.priorityState} className="shrink-0" />
            </button>
          ))}
          {attentionTasks.length > 3 && (
            <div className="px-3 py-1.5 text-[10px] text-fg-faint">
              {t("attention.more", { count: attentionTasks.length - 3 })}
            </div>
          )}
        </div>
      )}

      {/* 工具栏 */}
      <div className="px-6 pb-3 flex items-center gap-3 flex-wrap shrink-0">
        <SegmentedControl
          ariaLabel={t("tabs.active")}
          value={activeTab}
          onChange={(v) => setActiveTab(v)}
          options={[
            { value: "active", label: t("tabs.active") },
            { value: "archived", label: t("tabs.archived") },
          ]}
        />
        <div className="relative w-64">
          <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
          <input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder={t("search.placeholder")}
            className="w-full h-8 bg-surface-2/60 border border-line-strong/50 rounded-[var(--radius-control)] pl-9 pr-3 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary/50"
          />
        </div>
        <select
          value={filterAgent || ""}
          onChange={(e) => setFilterAgent(e.target.value || null)}
          className="h-8 bg-surface-2/60 border border-line-strong/50 rounded-[var(--radius-control)] px-2.5 text-xs text-fg-muted focus:outline-none cursor-pointer"
        >
          <option value="">{t("agents.allAgents")}</option>
          {agents.map((a) => (
            <option key={a.id} value={a.name}>
              {a.name}
            </option>
          ))}
        </select>
        <div className="flex-1" />
        <SegmentedControl
          ariaLabel={t("view.kanban")}
          value={viewMode}
          onChange={(v) => setViewMode(v)}
          options={[
            { value: "kanban", label: t("view.kanban") },
            { value: "list", label: t("view.list") },
          ]}
        />
      </div>

      {/* 任务区 */}
      <div className="flex-1 min-h-0 px-6 pb-6 overflow-x-auto">
        {activeTab === "archived" ? (
          <div className="h-full flex flex-col rounded-[var(--radius-card)] border border-line bg-surface-2/20">
            <TaskList tasks={archivedTasks} onSelect={setSelectedTask} />
          </div>
        ) : tasks.length === 0 && loadError ? (
          <EmptyState
            icon={AlertTriangle}
            title={t("loadError")}
            action={
              currentProjectId && (
                <button
                  onClick={() => loadProjectData(currentProjectId)}
                  className="h-8 px-3 text-xs bg-surface-2 hover:bg-surface-3 border border-line rounded-[var(--radius-control)] text-fg-muted transition-colors-fast focus-visible:outline-none"
                >
                  {t("retryLoad")}
                </button>
              )
            }
          />
        ) : tasks.length === 0 ? (
          <EmptyState icon={Inbox} title={t("empty.title")} description={t("empty.description")} />
        ) : viewMode === "list" ? (
          <div className="h-full flex flex-col rounded-[var(--radius-card)] border border-line bg-surface-2/20">
            <TaskList tasks={filteredByAgent} onSelect={setSelectedTask} />
          </div>
        ) : (
          <div className="h-full">
            {boardColumnsView.length > 0 ? (
              <TaskBoard columns={boardColumnsView} onSelect={setSelectedTask} />
            ) : (
              <TaskBoard columns={fallbackColumns} onSelect={setSelectedTask} />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** 运行态条统计块 */
function StatBlock({ dot, label, value }: { dot: string; label: string; value: number }) {
  return (
    <div className="flex items-center gap-2 h-[34px] px-3 rounded-[var(--radius-control)] border border-line bg-surface-2/40">
      <span className={`w-2 h-2 rounded-full shrink-0 ${dot}`} />
      <span className="text-xs text-fg-subtle whitespace-nowrap">{label}</span>
      <span className="text-sm font-semibold text-fg font-mono">{value}</span>
    </div>
  );
}
