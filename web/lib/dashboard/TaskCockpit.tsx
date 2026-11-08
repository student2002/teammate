"use client";
// TaskCockpit：任务选中态的内联聚焦工作区。左侧 320px 任务轨保留当前筛选结果，
// 可连续切换任务；顶部先展示完整协作链再展示细节；主列承载运行画布、当前节点操作
// （human.input 人工提交）、实时日志、执行尝试与流转；辅助列承载属性/Git/Token/约束。
// 画布与执行历史来自 GET /tasks/{id}/graph；编辑/取消任务在原位内联展开，不用遮罩弹窗。

import { useState, useEffect, useMemo, useRef, useCallback } from "react";
import { useTranslations } from "next-intl";
import { ReactFlow, ReactFlowProvider } from "@xyflow/react";
import {
  Bot,
  AlertTriangle,
  Terminal,
  Edit2,
  Save,
  GitBranch,
  Trash2,
  Cpu,
  Calendar,
  Tag,
  Ban,
  X,
  Play,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import { useTaskLogs } from "@/lib/use-task-logs";
import type { MappedTask, MappedAgent } from "@/lib/types";
import { layoutNodes, runEdges, runNodeTypes, type RunRFNode } from "@/lib/workflow/RunCanvas";
import { def } from "@/lib/workflow/catalog";
import type { WorkflowTypeKey } from "@/lib/workflow/types";
import { NodeErrorBlock, NodeSkippedHint } from "@/lib/workflow/NodeStatusHints";
import type { RunEdge, RunNode } from "@/lib/workflow/types";
import CollaborationChain from "@/lib/product/CollaborationChain";
import TaskStateBadge from "@/lib/product/TaskStateBadge";
import { taskStateTone, toneDotCls } from "@/lib/product/presentation";
import Button from "@/lib/ui/Button";
import InlineConfirm from "@/lib/ui/InlineConfirm";
import { NodeSummaryPanel, TransitionPanel, SubTaskPanel, CommentPanel, type SubTask, type RealComment, type GraphAttempt } from "./TaskActivity";

// dirsOf 将服务端节点返回的目录权限（JSON 数组或逗号分隔字符串）归一化为字符串数组。
function dirsOf(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean);
  if (typeof value === "string" && value.trim()) {
    return value
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
  }
  return [];
}

type InlineForm = "edit" | null;

// 图接口节点(运行态) → 画布 RunNode:补齐画布卡必需的 attempts/ui_config 字段
function toRunNode(raw: Record<string, unknown>): RunNode {
  const waitKey = typeof raw.wait_key === "string" ? raw.wait_key : "";
  const kind = waitKey.startsWith("human") ? "human" : waitKey.startsWith("event") ? "event" : waitKey ? "duration" : null;
  return {
    id: String(raw.id),
    type_key: (raw.type_key as RunNode["type_key"]) || "agent.task",
    name: (raw.name as string) || "",
    sort_order: Number(raw.sort_order) || 0,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    config: (raw.config as any) ?? {},
    // 运行节点不带画布位置;留空让 layoutNodes 走 dagre 自动布局,兜底 (0,0) 会让全部节点叠在原点
    ui_config: undefined,
    status: (raw.status as RunNode["status"]) || "waiting",
    input: raw.input,
    output: raw.output,
    error: (raw.error as RunNode["error"]) ?? null,
    attempts: Array.isArray(raw.attempts) ? (raw.attempts as RunNode["attempts"]) : [],
    wait: kind ? { kind, expires_at: (raw.wait_expires_at as string) || undefined } : null,
  };
}

export interface TaskCockpitProps {
  task: MappedTask;
  trackTasks: MappedTask[];
  agents: MappedAgent[];
  onSelectTask: (task: MappedTask) => void;
  onClose: () => void;
  onSubmitHuman: (taskId: number, nodeId: string, action: "submitted" | "cancelled", data: Record<string, unknown>) => void | Promise<void>;
  onEditTask: (taskId: number, updates: { title: string; description: string; priority: string }) => void;
  onAddComment: (taskId: number, text: string, mentions?: string[]) => void;
  onDeleteTask: (taskId: number) => void;
  onCancelTask: (taskId: number) => void;
}

function CockpitInner({
  task,
  trackTasks,
  agents,
  onSelectTask,
  onClose,
  onSubmitHuman,
  onEditTask,
  onAddComment,
  onDeleteTask,
  onCancelTask,
}: TaskCockpitProps) {
  const t = useTranslations("dashboard");
  const { canEdit } = usePermission();

  // 默认选中最后一个实质执行过的节点(完成节点无执行日志),打开页面即见执行过程
  const [activeTerminalNode, setActiveTerminalNode] = useState<number>(() => {
    const raw = (task._rawNodes || []) as Array<{ type_key?: string; attempts?: unknown[] }>;
    for (let i = raw.length - 1; i >= 0; i--) {
      if (raw[i].type_key === "flow.end") continue;
      if ((raw[i].attempts?.length || 0) > 0) return i;
    }
    return task.currentNode;
  });
  const [userSelectedNode, setUserSelectedNode] = useState(false);
  const terminalRef = useRef<HTMLDivElement>(null);
  const [inlineForm, setInlineForm] = useState<InlineForm>(null);
  const [realComments, setRealComments] = useState<RealComment[]>([]);
  const [commentsLoading, setCommentsLoading] = useState(false);

  // 运行图(GET /tasks/{id}/graph):节点状态/边 resolution/执行尝试的数据源
  const [graphNodes, setGraphNodes] = useState<RunNode[]>([]);
  const [graphEdges, setGraphEdges] = useState<RunEdge[]>([]);
  const [graphAttempts, setGraphAttempts] = useState<GraphAttempt[]>([]);
  const [graphLoading, setGraphLoading] = useState(false);

  const loadGraph = useCallback(() => {
    if (!task.id) return;
    setGraphLoading(true);
    api
      .getTaskGraph(task.id)
      .then((data) => {
        const raw = data as { nodes?: Record<string, unknown>[]; edges?: Record<string, unknown>[]; attempts?: GraphAttempt[] };
        setGraphNodes((raw.nodes || []).map(toRunNode));
        setGraphEdges(
          (raw.edges || []).map((e) => ({
            id: String(e.id),
            source_node_id: String(e.source_node_id),
            source_port: String(e.source_port),
            target_node_id: String(e.target_node_id),
            target_port: String(e.target_port),
            label: (e.label as string) || "",
            resolution: (e.resolution as RunEdge["resolution"]) || "pending",
          }))
        );
        setGraphAttempts(raw.attempts || []);
        setGraphLoading(false);
      })
      .catch(() => setGraphLoading(false));
  }, [task.id]);

  // 首次进入与轮询刷新(与看板轮询同节奏);人工提交后由 onSubmitHuman 的刷新链路重取
  useEffect(() => {
    loadGraph();
    const interval = setInterval(loadGraph, 10000);
    return () => clearInterval(interval);
  }, [loadGraph]);

  // 切换任务时重置聚焦状态；节点推进（轮询刷新）不在此处重置，
  // 当前节点跟随由下方 auto-follow effect 负责（不打断用户手动选择的节点）
  useEffect(() => {
    setActiveTerminalNode(task.currentNode);
    setUserSelectedNode(false);
    setInlineForm(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.id]);

  // 自动跟随：优先聚焦最后一个 agentd 执行的节点（执行日志只产生在 agentd
  // 类节点上）；没有 agentd 节点时回退最后一个实质执行的节点，再回退当前节点。
  // 用户手动选择后不跟随
  useEffect(() => {
    if (userSelectedNode) return;
    const raw = (task._rawNodes || []) as Array<{ id?: string; type_key?: string }>;
    const idxById = new Map(raw.map((n, i) => [n.id as string, i]));
    const isAgentd = (idx: number) => {
      const tk = raw[idx]?.type_key;
      return !!tk && def(tk as WorkflowTypeKey).executor === "agentd";
    };
    let lastExecuted: number | null = null;
    let lastAgentd: number | null = null;
    for (const a of graphAttempts) {
      if (!a.task_node_id) continue;
      const idx = idxById.get(a.task_node_id);
      if (idx === undefined || raw[idx]?.type_key === "flow.end") continue;
      lastExecuted = idx;
      if (isAgentd(idx)) lastAgentd = idx;
    }
    setActiveTerminalNode(lastAgentd ?? lastExecuted ?? task.currentNode);
  }, [task.currentNode, task._rawNodes, graphAttempts, userSelectedNode]);

  const rawNodes = useMemo(
    () =>
      (task._rawNodes || []) as Array<{
        id?: string;
        name?: string;
        type_key?: string;
        status?: string;
        attempts?: unknown[];
        readonly_dirs?: unknown;
        full_control_dirs?: unknown;
      }>,
    [task._rawNodes]
  );

  const handleNodeClick = useCallback(
    (nodeId: string) => {
      const idx = rawNodes.findIndex((n) => n.id === nodeId);
      if (idx < 0) return;
      setUserSelectedNode(idx !== task.currentNode);
      setActiveTerminalNode(idx);
    },
    [rawNodes, task.currentNode]
  );

  // 根据当前选中的终端节点推导 activeNodeId
  const activeNodeId = useMemo(() => rawNodes[activeTerminalNode]?.id || null, [rawNodes, activeTerminalNode]);

  // 连接 WebSocket 获取实时日志 — 仅针对当前活动节点
  useTaskLogs(task.id, activeNodeId);
  const taskLogs = useAppStore((s) => s.taskLogs[task.id]);

  // node_id → 节点索引
  const nodeIdToIndex = useMemo(() => {
    const map: Record<string, number> = {};
    rawNodes.forEach((n, i) => {
      if (n.id) map[n.id] = i;
    });
    return map;
  }, [rawNodes]);

  const logsByIndex = useMemo(() => {
    const result: Record<number, string[]> = {};
    if (!taskLogs) return result;
    for (const [nodeId, lines] of Object.entries(taskLogs)) {
      const idx = nodeIdToIndex[nodeId];
      if (idx !== undefined) {
        result[idx] = lines;
      } else if (nodeId === "_default") {
        result[task.currentNode] = lines;
      }
    }
    return result;
  }, [taskLogs, nodeIdToIndex, task.currentNode]);

  // 有新日志时自动滚动到底部
  const currentLogsLength = (logsByIndex[activeTerminalNode] || []).length;
  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [currentLogsLength]);

  // 真实评论
  useEffect(() => {
    if (!task.id) return;
    setCommentsLoading(true);
    api
      .listComments(task.id)
      .then((data) => {
        setRealComments((data as RealComment[]) || []);
        setCommentsLoading(false);
      })
      .catch(() => {
        setRealComments([]);
        setCommentsLoading(false);
      });
  }, [task.id]);

  const isSucceeded = task.priorityState === "succeeded";
  const isCancelled = task.priorityState === "cancelled";
  const activeGraphNode = graphNodes.find((n) => n.id === activeNodeId) || null;
  const activeNodeStatus = activeGraphNode?.status || rawNodes[activeTerminalNode]?.status;
  const nodeRunning = activeNodeStatus === "running";
  const nodeNames = task.nodeNames || [];
  const nodeAgents = task.nodeAgents || [];
  const nodesStatus = task.nodesStatus || [];
  const isOverdue =
    task.dueDate &&
    new Date(task.dueDate) < new Date() &&
    !isSucceeded &&
    !isCancelled;
  const succeededCount = nodesStatus.filter((s) => s === "succeeded").length;
  const totalNodes = nodeNames.length;

  // 待人工节点(human.input 等待人工处理):主列操作区提交/取消
  const humanNode =
    activeGraphNode?.status === "manual_intervention" && activeGraphNode.type_key === "human.input"
      ? activeGraphNode
      : null;

  // 画布节点:图接口的运行节点;ui_config 缺失时 dagre 自动布局
  const canvasNodes = useMemo<RunRFNode[]>(
    () =>
      layoutNodes(graphNodes, graphEdges).map((n) => ({
        id: n.id,
        type: "v2run" as const,
        position: { x: n.ui_config?.x ?? 0, y: n.ui_config?.y ?? 0 },
        data: { node: n },
        selected: n.id === activeNodeId,
      })),
    [graphNodes, graphEdges, activeNodeId]
  );
  const canvasEdges = useMemo(() => runEdges(graphEdges), [graphEdges]);

  const nodeTypeKeys = useMemo(() => rawNodes.map((n) => n.type_key || ""), [rawNodes]);

  return (
    <div className="flex h-full min-h-0">
      {/* 任务列表轨 */}
      <div className="hidden lg:flex w-80 shrink-0 border-r border-line flex-col min-h-0">
        <div className="px-4 py-2.5 border-b border-line text-xs font-semibold text-fg-faint uppercase tracking-wider shrink-0">
          {t("cockpit.trackTitle", { count: trackTasks.length })}
        </div>
        <div className="flex-1 overflow-y-auto py-1.5">
          {trackTasks.map((track) => {
            const active = track.id === task.id;
            return (
              <button
                key={track.id}
                onClick={() => onSelectTask(track)}
                className={`w-full text-left px-4 py-2.5 flex items-center gap-2 transition-colors-fast focus-visible:outline-none ${
                  active ? "bg-primary/10" : "hover:bg-surface-2/60"
                }`}
              >
                <span
                  className={`w-1.5 h-1.5 rounded-full shrink-0 ${toneDotCls(taskStateTone(track.priorityState))}`}
                />
                <span className="text-[10px] font-mono text-fg-faint shrink-0">{track.taskRef}</span>
                <span className={`text-xs truncate flex-1 ${active ? "text-primary-fg font-medium" : "text-fg-muted"}`}>
                  {track.title}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* 主工作区 */}
      <div className="flex-1 overflow-y-auto min-w-0">
        {/* 头部：ID + 标题 + 状态 + 任务级操作 */}
        <div className="sticky top-0 z-10 bg-surface/95 backdrop-blur-sm border-b border-line px-5 py-3">
          <div className="flex items-center gap-2.5 flex-wrap">
            <span className="text-primary-fg font-mono text-sm bg-primary/10 px-2 py-0.5 rounded shrink-0">
              {task.taskRef}
            </span>
            <h2 className="text-base font-semibold text-fg truncate min-w-0 flex-1">{task.title}</h2>
            <TaskStateBadge state={task.priorityState} className="shrink-0" />
            <span className="text-xs text-fg-faint font-mono shrink-0">{task.type?.toUpperCase() || "TASK"}</span>
            <span className="text-xs text-fg-faint font-mono shrink-0">
              {succeededCount}/{totalNodes}
            </span>
            <div className="flex items-center gap-1.5 shrink-0 ml-auto">
              <button
                onClick={() => setInlineForm(inlineForm === "edit" ? null : "edit")}
                disabled={!canEdit}
                title={t("editTask.title")}
                aria-label={t("editTask.title")}
                className="p-1.5 hover:bg-surface-3 rounded-[var(--radius-control)] text-fg-subtle hover:text-primary-fg transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed focus-visible:outline-none"
              >
                <Edit2 className="w-4 h-4" />
              </button>
              {!isSucceeded && !isCancelled && (
                <InlineConfirm
                  label={
                    <span className="flex items-center gap-1">
                      <Ban className="w-3 h-3" />
                      {t("actions.cancelTask")}
                    </span>
                  }
                  confirmLabel={t("actions.cancelTask")}
                  cancelLabel={t("common.cancel")}
                  onConfirm={() => onCancelTask(task.id)}
                  disabled={!canEdit}
                />
              )}
              {(isSucceeded || isCancelled) && (
                <InlineConfirm
                  label={
                    <span className="flex items-center gap-1">
                      <Trash2 className="w-3 h-3" />
                      {t("actions.deleteTask")}
                    </span>
                  }
                  confirmLabel={t("actions.deleteTask")}
                  cancelLabel={t("common.cancel")}
                  onConfirm={() => {
                    onDeleteTask(task.id);
                    onClose();
                  }}
                  disabled={!canEdit}
                />
              )}
              <button
                onClick={onClose}
                title={t("cockpit.close")}
                aria-label={t("cockpit.close")}
                className="p-1.5 hover:bg-surface-3 rounded-[var(--radius-control)] text-fg-subtle hover:text-fg transition-colors-fast focus-visible:outline-none"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>
          {task.dueDate && (
            <div className={`mt-1.5 text-xs flex items-center gap-1 ${isOverdue ? "text-danger-fg" : "text-fg-faint"}`}>
              <Calendar className="w-3 h-3" />
              {task.dueDate}
              {isOverdue ? ` ${t("overdue")}` : ""}
            </div>
          )}
        </div>

        {/* 完整协作链：强制视觉顺序第一位 */}
        <div className="px-5 py-4 border-b border-line bg-surface-2/20">
          <CollaborationChain
            density="full"
            names={nodeNames}
            statuses={nodesStatus}
            agents={nodeAgents}
            currentIndex={task.currentNode}
            typeKeys={nodeTypeKeys}
          />
        </div>

        {/* 内联表单区 */}
        {inlineForm === "edit" && (
          <EditInlineForm
            task={task}
            onCancel={() => setInlineForm(null)}
            onSave={(updates) => {
              onEditTask(task.id, updates);
              setInlineForm(null);
            }}
          />
        )}

        {/* 双列内容：<1280px 辅助列落到主列下方 */}
        <div className="grid grid-cols-1 lg:grid-cols-[1fr_300px] gap-5 p-5">
          {/* 主列 */}
          <div className="space-y-5 min-w-0">
            {/* 子任务标记 */}
            {task.parentTask && (
              <div className="bg-accent/5 border border-accent/20 rounded-[var(--radius-card)] p-3 text-xs text-accent-fg flex items-center">
                <Bot className="w-3.5 h-3.5 mr-2" /> {t("subTasks.parentInfo", { parentTask: task.parentTask })}
              </div>
            )}

            {/* 运行画布：节点状态描边 + 边 resolution 着色;点击节点切换日志终端 */}
            {graphNodes.length > 0 && (
              <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/20 p-4">
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3 flex items-center gap-2">
                  {t("workflow.progress")}
                  {graphLoading && <span className="text-[9px] text-fg-faint normal-case font-normal">{t("runCanvas.refreshing")}</span>}
                </div>
                <div className="h-[260px] rounded-[var(--radius-control)] border border-line bg-surface overflow-hidden">
                  <ReactFlow
                    nodes={canvasNodes}
                    edges={canvasEdges}
                    onNodeClick={(_, n) => handleNodeClick(n.id)}
                    onPaneClick={() => undefined}
                    nodeTypes={runNodeTypes}
                    nodesDraggable={false}
                    nodesConnectable={false}
                    fitView
                    className="bg-transparent"
                    proOptions={{ hideAttribution: true }}
                  />
                </div>
              </div>
            )}

            {/* 当前节点操作区 */}
            <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30 p-4">
              <div className="flex items-center justify-between mb-3">
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider">
                  {t("nodeActions.title")}
                </div>
                <span className="text-xs text-fg-subtle truncate ml-3">
                  {nodeNames[activeTerminalNode] || "-"}
                </span>
              </div>
              <div className="space-y-2.5">
                <div className="text-sm text-fg-muted">{task.message}</div>
                {(() => {
                  const roDirs = dirsOf(rawNodes[activeTerminalNode]?.readonly_dirs);
                  const fcDirs = dirsOf(rawNodes[activeTerminalNode]?.full_control_dirs);
                  if (roDirs.length === 0 && fcDirs.length === 0) return null;
                  return (
                    <div className="space-y-1 text-[11px]">
                      {roDirs.length > 0 && (
                        <div className="text-warning-fg/90">
                          {t("nodeActions.readonlyDirs")} {roDirs.join(", ")}
                        </div>
                      )}
                      {fcDirs.length > 0 && (
                        <div className="text-success-fg/90">
                          {t("nodeActions.fullControl")} {fcDirs.join(", ")}
                        </div>
                      )}
                    </div>
                  );
                })()}
                {nodeAgents[task.currentNode] && (
                  <div className="flex items-center text-xs text-primary-fg">
                    <Bot className="w-3 h-3 mr-1.5" /> {t("nodeActions.executor")} {nodeAgents[task.currentNode]}
                  </div>
                )}

                {/* 人工待办:human.input 节点等待人工处理 → 提交/取消 */}
                {humanNode ? (
                  <HumanSubmitForm
                    node={humanNode}
                    onSubmit={(data) => onSubmitHuman(task.id, humanNode.id, "submitted", data)}
                    onCancel={() => onSubmitHuman(task.id, humanNode.id, "cancelled", {})}
                    disabled={!canEdit}
                  />
                ) : activeGraphNode?.status === "failed" ? (
                  <div className="space-y-1.5">
                    <div className="flex items-center gap-1.5 text-xs text-danger-fg">
                      <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                      {t("status.failed")}
                    </div>
                    <NodeErrorBlock error={activeGraphNode.error} className="text-xs" />
                  </div>
                ) : activeGraphNode?.status === "skipped" ? (
                  <NodeSkippedHint className="text-xs" />
                ) : activeGraphNode?.status === "manual_intervention" ? (
                  <div className="rounded-[var(--radius-control)] border border-danger/25 bg-danger/10 px-2.5 py-2 text-xs text-danger-fg space-y-1">
                    <div className="flex items-center gap-1.5">
                      <AlertTriangle className="w-3.5 h-3.5" />
                      {t("status.manualIntervention")}
                    </div>
                    {activeGraphNode.error?.message && (
                      <div className="font-mono text-[10px] break-all">
                        {activeGraphNode.error.code ? `${activeGraphNode.error.code}: ` : ""}
                        {activeGraphNode.error.message}
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="flex items-center gap-2 text-xs text-fg-subtle pt-1">
                    <Play className="w-3 h-3" />
                    {activeNodeStatus ? t(`status.${activeNodeStatus === "manual_intervention" ? "manualIntervention" : activeNodeStatus}`) : "-"}
                  </div>
                )}
              </div>
            </div>

            {/* 描述 */}
            {task.description && (
              <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-4">
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">
                  {t("taskDescription")}
                </div>
                <div className="text-sm text-fg-muted whitespace-pre-wrap leading-relaxed">{task.description}</div>
              </div>
            )}

            {/* 标签 */}
            {task.labels && task.labels.length > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <Tag className="w-4 h-4 text-fg-subtle" />
                {task.labels.map((l: string) => (
                  <span key={l} className="text-xs px-2 py-0.5 bg-surface-3/50 text-fg-muted rounded border border-line-strong/50">
                    {l}
                  </span>
                ))}
              </div>
            )}

            {/* 实时日志终端：状态灯 + 节点名 + 连接状态 */}
            <div className="border border-line rounded-[var(--radius-card)] overflow-hidden bg-bg">
              <div className="bg-surface-2 px-4 py-2 border-b border-line flex items-center gap-2.5 select-none">
                <span
                  className={`w-2 h-2 rounded-full shrink-0 ${
                    nodeRunning ? "bg-success-fg status-glow" : "bg-fg-faint"
                  }`}
                />
                <Terminal className="w-3.5 h-3.5 text-fg-faint shrink-0" />
                <span className="text-xs font-medium text-fg-subtle font-mono truncate flex-1">
                  {nodeNames[activeTerminalNode] || "terminal"}
                </span>
                <span className="text-[10px] text-fg-faint font-mono shrink-0">
                  {nodeRunning
                    ? t("terminal.live")
                    : t("terminal.nodeLabel", { index: activeTerminalNode + 1 })}
                </span>
              </div>
              <div
                ref={terminalRef}
                className="p-4 h-52 overflow-y-auto font-mono text-[13px] leading-relaxed relative selection:bg-primary/30"
              >
                {(logsByIndex[activeTerminalNode] || [`> ${t("terminal.noLogs")}`]).map((log: string, i: number) => {
                  let cls = "text-fg-muted";
                  let prompt = false;
                  if (log.startsWith("✖")) {
                    cls = "text-danger-fg font-medium";
                  } else if (log.startsWith("✔")) {
                    cls = "text-success-fg font-medium";
                  } else if (log.startsWith(">")) {
                    cls = "text-primary-fg";
                    prompt = true;
                  } else if (log.includes("added") || log.includes("audited")) {
                    cls = "text-fg-faint";
                  }
                  return (
                    <div key={i} className={`${cls} mb-1.5 flex items-start`}>
                      {prompt ? (
                        <span className="text-accent-fg mr-3 shrink-0 select-none">❯</span>
                      ) : (
                        <span className="w-5 shrink-0" />
                      )}
                      <span className="whitespace-pre-wrap">{log.replace(/^>\s*/, "")}</span>
                    </div>
                  );
                })}
                {nodeRunning && (
                  <div className="flex items-center text-success/80 mt-3 font-medium">
                    <span className="w-5 shrink-0 text-accent-fg">❯</span>
                    <span className="w-2.5 h-4 bg-success/80 animate-pulse cursor-blink" />
                  </div>
                )}
              </div>
            </div>

            {/* 节点总结 / 执行尝试 / 子任务 / 评论 */}
            <NodeSummaryPanel
              summary={activeNodeId ? task.nodeSummaries?.[activeNodeId] : undefined}
              nodeName={nodeNames[activeTerminalNode] || ""}
            />
            <TransitionPanel task={task} attempts={graphAttempts} agents={agents} loading={graphLoading && graphAttempts.length === 0} onPickNode={handleNodeClick} />
            <SubTaskPanel subTasks={task.subTasks as SubTask[]} />
            <CommentPanel comments={realComments} taskId={task.id} onAddComment={onAddComment} agents={agents} loading={commentsLoading} />
          </div>

          {/* 辅助列 */}
          <div className="space-y-4 min-w-0">
            {/* 属性 */}
            <div className="bg-surface-2/30 border border-line rounded-[var(--radius-card)] p-4">
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3">{t("attributes.title")}</div>
              <div className="space-y-2.5 text-xs">
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t("attributes.type")}</span>
                  <span className="text-fg font-mono">{task.type}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t("attributes.priority")}</span>
                  <span className="font-medium">{task.priority || "medium"}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t("attributes.workflow")}</span>
                  <span className="text-fg-muted text-right max-w-[140px] truncate">{task.workflowName || "-"}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t("attributes.dueDate")}</span>
                  <span className={isOverdue ? "text-danger-fg" : "text-fg-muted"}>{task.dueDate || "-"}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t("attributes.labels")}</span>
                  <span className="text-fg-muted">{t("attributes.labelCount", { count: task.labels?.length || 0 })}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t("attributes.progress")}</span>
                  <span className="text-fg-muted">
                    {succeededCount}/{totalNodes}
                  </span>
                </div>
              </div>
            </div>

            {/* Git 分支 */}
            {task.gitBranch && (
              <div className="bg-surface-2/30 border border-line rounded-[var(--radius-card)] p-4">
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2 flex items-center">
                  <GitBranch className="w-3.5 h-3.5 mr-1 text-fg-subtle" /> {t("gitBranch.title")}
                </div>
                <div className="text-sm text-primary-fg font-mono truncate">{task.gitBranch}</div>
                <div className="text-[10px] text-fg-faint mt-1">
                  {t("gitBranch.baseline", { ref: task.taskRef, index: task.currentNode })}
                </div>
              </div>
            )}

            {/* 约束 */}
            {task.constraints && (
              <div className="bg-warning/5 border border-warning/20 rounded-[var(--radius-card)] p-4">
                <div className="text-xs font-semibold text-warning-fg uppercase tracking-wider mb-2 flex items-center">
                  <AlertTriangle className="w-3.5 h-3.5 mr-1" /> {t("constraints.title")}
                </div>
                <div className="text-sm text-warning-fg whitespace-pre-wrap leading-relaxed">{task.constraints}</div>
              </div>
            )}

            {/* 节点级 Token */}
            {task.nodeTokens && Object.keys(task.nodeTokens).length > 0 && (
              <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30 p-4">
                <div className="flex items-center mb-3">
                  <Cpu className="w-4 h-4 mr-2 text-primary-fg" />
                  <span className="text-xs font-semibold text-fg-muted">{t("tokenUsage.title")}</span>
                </div>
                <div className="space-y-2">
                  {Object.entries(task.nodeTokens).map(([nodeID, usage]) => {
                    // 键是节点 UUID,名字从运行节点快照按 id 查;值是 {input_tokens, output_tokens} 对象
                    const name = rawNodes.find((n) => n.id === nodeID)?.name ?? nodeID;
                    const total = usage.input_tokens + usage.output_tokens;
                    return (
                      <div
                        key={nodeID}
                        className="flex items-center justify-between bg-surface/50 border border-line/60 rounded-lg px-3 py-2"
                      >
                        <span className="text-[10px] text-fg-faint truncate mr-2">{name}</span>
                        <span className="text-xs font-bold text-fg shrink-0">{total.toLocaleString()}</span>
                      </div>
                    );
                  })}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// human.input 人工提交表单
// ==========================================
function HumanSubmitForm({
  node,
  onSubmit,
  onCancel,
  disabled,
}: {
  node: RunNode;
  onSubmit: (data: Record<string, unknown>) => void | Promise<void>;
  onCancel: () => void | Promise<void>;
  disabled: boolean;
}) {
  const t = useTranslations("dashboard");
  const cfg = (node.config ?? {}) as { prompt?: unknown; fields?: unknown };
  const prompt = typeof cfg.prompt === "string" ? cfg.prompt : "";
  const fields = Array.isArray(cfg.fields) ? cfg.fields.filter((f): f is string => typeof f === "string") : [];
  const [values, setValues] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // 提交/取消失败(409 节点已处理、网络错误)必须复位按钮并把错误呈现给用户
  const runAction = async (action: () => void | Promise<void>) => {
    setSubmitting(true);
    setSubmitError(null);
    try {
      await action();
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  };

  const collectData = () =>
    fields.reduce<Record<string, unknown>>((acc, f) => {
      if (values[f] !== undefined && values[f] !== "") acc[f] = values[f];
      return acc;
    }, {});

  return (
    <div className="rounded-[var(--radius-control)] border border-warning/30 bg-warning/5 p-3 space-y-2.5">
      <div className="text-xs font-semibold text-warning-fg">{t("humanSubmit.title")}</div>
      {prompt && <div className="text-sm text-fg-muted whitespace-pre-wrap leading-relaxed">{prompt}</div>}
      {fields.map((f) => (
        <div key={f}>
          <label className="block text-[10px] font-semibold text-fg-faint uppercase tracking-wider mb-1">{f}</label>
          <textarea
            value={values[f] ?? ""}
            onChange={(e) => setValues((prev) => ({ ...prev, [f]: e.target.value }))}
            rows={2}
            disabled={disabled || submitting}
            className="w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-2.5 py-1.5 text-xs text-fg focus:outline-none focus:border-primary resize-none disabled:opacity-50"
          />
        </div>
      ))}
      <div className="flex items-center gap-2">
        <Button
          variant="primary"
          size="sm"
          onClick={() => runAction(() => onSubmit(collectData()))}
          disabled={disabled || submitting}
        >
          {t("humanSubmit.submit")}
        </Button>
        <Button
          variant="secondary"
          size="sm"
          onClick={() => runAction(onCancel)}
          disabled={disabled || submitting}
        >
          {t("humanSubmit.cancelNode")}
        </Button>
      </div>
      {submitError && (
        <p className="text-[10px] text-danger-fg bg-danger/10 border border-danger/25 rounded-[var(--radius-control)] px-2 py-1">{submitError}</p>
      )}
    </div>
  );
}

export default function TaskCockpit(props: TaskCockpitProps) {
  return (
    <ReactFlowProvider>
      <CockpitInner {...props} />
    </ReactFlowProvider>
  );
}

// ==========================================
// 内联表单：编辑任务
// ==========================================
function EditInlineForm({
  task,
  onSave,
  onCancel,
}: {
  task: MappedTask;
  onSave: (updates: { title: string; description: string; priority: string }) => void;
  onCancel: () => void;
}) {
  const t = useTranslations("dashboard");
  const [title, setTitle] = useState(task.title);
  const [desc, setDesc] = useState(task.description || "");
  const [priority, setPriority] = useState<string>(task.priority || "medium");
  const [saving, setSaving] = useState(false);

  const handleSave = () => {
    if (!title.trim()) return;
    setSaving(true);
    setTimeout(() => {
      onSave({ title: title.trim(), description: desc, priority });
      setSaving(false);
    }, 400);
  };

  return (
    <div className="mx-5 mt-4 border border-primary/30 rounded-[var(--radius-card)] bg-surface-2/40 p-4 space-y-3 animate-fade-in">
      <div className="text-sm font-semibold text-fg">{t("editTask.title")}</div>
      <div>
        <label className="block text-xs font-medium text-fg-muted mb-1">{t("editTask.titleLabel")}</label>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-primary"
        />
      </div>
      <div>
        <label className="block text-xs font-medium text-fg-muted mb-1">{t("editTask.priorityLabel")}</label>
        <select
          value={priority}
          onChange={(e) => setPriority(e.target.value)}
          className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-primary"
        >
          <option value="urgent">{t("priority.urgent")}</option>
          <option value="high">{t("priority.high")}</option>
          <option value="medium">{t("priority.medium")}</option>
          <option value="low">{t("priority.low")}</option>
        </select>
      </div>
      <div>
        <label className="block text-xs font-medium text-fg-muted mb-1">{t("editTask.descriptionLabel")}</label>
        <textarea
          value={desc}
          onChange={(e) => setDesc(e.target.value)}
          rows={4}
          className="w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 py-2 text-sm text-fg-muted focus:outline-none focus:border-primary resize-none"
        />
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          {t("common.cancel")}
        </Button>
        <Button variant="primary" size="sm" loading={saving} onClick={handleSave} disabled={!title.trim()}>
          <Save className="w-3.5 h-3.5 mr-1" />
          {t("editTask.save")}
        </Button>
      </div>
    </div>
  );
}
