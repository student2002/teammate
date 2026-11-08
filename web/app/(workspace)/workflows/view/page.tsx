"use client";
// 工作流查看页(双模式):?taskId= 运行态——GET /tasks/{id}/graph 驱动,节点状态描边、
// 边 resolution 着色、attempt 面板,human.input 需人工节点可提交/取消;
// ?templateId= 模板预览——GET /workspaces/{ws}/workflows/{id} 只读整图画布。

import { Suspense, useCallback, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ReactFlow, ReactFlowProvider } from "@xyflow/react";
import { useTranslations } from "next-intl";
import { ArrowLeft, Loader2 } from "lucide-react";
import "@xyflow/react/dist/style.css";
import api, { ApiError } from "@/lib/api";
import { useAppStore } from "@/lib/store";
import Button from "@/lib/ui/Button";
import EmptyState from "@/lib/ui/EmptyState";
import StatusBadge, { type BadgeTone } from "@/lib/ui/StatusBadge";
import { def, outputPorts, TONE_VAR } from "@/lib/workflow/catalog";
import { layoutNodes, runEdges, runNodeTypes, type RunRFNode } from "@/lib/workflow/RunCanvas";
import { NodeCard, type EditorNodeData, type EditorRFNode } from "@/lib/workflow/NodeCard";
import { NodeErrorBlock, NodeSkippedHint, UnresolvedRefMark } from "@/lib/workflow/NodeStatusHints";
import type { Edge } from "@xyflow/react";
import type {
  RunNode,
  TemplateEdge,
  TemplateNode,
  EdgeResolution,
  WorkflowTypeKey,
} from "@/lib/workflow/types";

const TASK_STATUS_TONE: Record<string, BadgeTone> = {
  active: "accent",
  completed: "success",
  failed: "danger",
  cancelled: "neutral",
};

function fmtTime(iso: string | undefined): string {
  return iso ? iso.slice(0, 19).replace("T", " ") : "—";
}

function JsonBlock({ label, value }: { label: string; value: unknown }) {
  if (value === undefined || value === null) return null;
  return (
    <div>
      <div className="text-[10px] font-semibold text-fg-faint uppercase tracking-wider mb-1">{label}</div>
      <pre className="bg-bg border border-line rounded-[var(--radius-control)] p-2 text-[10px] font-mono text-fg-muted overflow-auto max-h-44 whitespace-pre-wrap break-all">
        {typeof value === "string" ? value : JSON.stringify(value, null, 2)}
      </pre>
    </div>
  );
}

// 运行图接口节点(含 attempts 数组) → 画布 RunNode
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

// 服务端模板节点/边响应 → 前端形状(去掉审计字段;ui_config 缺失时留空,
// 让 layoutNodes 走 dagre 自动布局,兜底 (0,0) 会让全部节点叠在原点)
function normalizeGraph(item: { nodes?: unknown[]; edges?: unknown[] }): { nodes: TemplateNode[]; edges: TemplateEdge[] } {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const nodes = ((item.nodes || []) as any[]).map((n) => ({
    id: String(n.id),
    type_key: n.type_key as WorkflowTypeKey,
    name: n.name || "",
    sort_order: Number(n.sort_order) || 0,
    config: n.config ?? {},
    ui_config:
      typeof n.ui_config?.x === "number" && typeof n.ui_config?.y === "number" ? { x: n.ui_config.x, y: n.ui_config.y } : undefined,
  }));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const edges = ((item.edges || []) as any[]).map((e) => ({
    id: String(e.id),
    source_node_id: e.source_node_id,
    source_port: e.source_port,
    target_node_id: e.target_node_id,
    target_port: e.target_port,
    label: e.label || "",
  }));
  return { nodes, edges };
}

function templateEdgeStyle(nodes: TemplateNode[], sourceId: string, sourcePort: string): React.CSSProperties {
  const source = nodes.find((n) => n.id === sourceId);
  if (!source) return { stroke: "var(--line-strong)", strokeWidth: 2 };
  const port = outputPorts(source.type_key, source.config).find((p) => p.key === sourcePort);
  return { stroke: port ? TONE_VAR[port.tone] : "var(--line-strong)", strokeWidth: 2 };
}

// ==========================================
// 运行态:节点详情面板(input/output/attempt/error + 人工提交)
// ==========================================
function RunNodeDetail({
  node,
  onSubmit,
  submitting,
}: {
  node: RunNode | null;
  onSubmit: (action: "submitted" | "cancelled") => void;
  submitting: boolean;
}) {
  const t = useTranslations("workflows.view.run.detail");
  const tv = useTranslations("workflows.view");
  if (!node) {
    return (
      <div className="w-80 shrink-0 border-l border-line bg-surface flex items-center justify-center p-6">
        <p className="text-xs text-fg-faint text-center">{t("empty")}</p>
      </div>
    );
  }
  const d = def(node.type_key);
  const Icon = d.icon;
  const humanPending = node.status === "manual_intervention" && node.type_key === "human.input";
  return (
    <div className="w-80 shrink-0 border-l border-line bg-surface overflow-y-auto p-4 space-y-3.5">
      <div className="flex items-center gap-2">
        <Icon className="w-4 h-4" style={{ color: "var(--primary)" }} />
        <div className="min-w-0 flex-1">
          <div className="text-xs font-bold text-fg truncate">{node.name}</div>
        </div>
        <StatusBadge
          tone={node.status === "succeeded" ? "success" : node.status === "failed" ? "danger" : node.status === "manual_intervention" ? "warning" : node.status === "waiting" ? "neutral" : "accent"}
          dot
        >
          {tv(`run.status.${node.status}`)}
        </StatusBadge>
      </div>

      {humanPending && (
        <div className="rounded-[var(--radius-control)] border border-warning/30 bg-warning/5 p-3 space-y-2">
          <div className="text-xs font-semibold text-warning-fg">{tv("run.humanPendingTitle")}</div>
          {typeof (node.config as { prompt?: unknown })?.prompt === "string" && (
            <div className="text-xs text-fg-muted whitespace-pre-wrap leading-relaxed">
              {(node.config as { prompt: string }).prompt}
            </div>
          )}
          <div className="flex items-center gap-2">
            <Button variant="primary" size="sm" onClick={() => onSubmit("submitted")} disabled={submitting}>
              {tv("run.submitAction")}
            </Button>
            <Button variant="secondary" size="sm" onClick={() => onSubmit("cancelled")} disabled={submitting}>
              {tv("run.cancelNodeAction")}
            </Button>
          </div>
        </div>
      )}

      {node.wait && (
        <div className="rounded-[var(--radius-control)] border border-warning/25 bg-warning/10 px-2.5 py-2 text-[10px] text-warning-fg space-y-0.5">
          <div>{tv("run.waitingFor", { event: tv(`run.waitKind.${node.wait.kind}`) })}</div>
          {node.wait.expires_at && <div>{t("waitExpires", { time: fmtTime(node.wait.expires_at) })}</div>}
        </div>
      )}

      {node.attempts.length > 0 && (
        <div>
          <div className="text-[10px] font-semibold text-fg-faint uppercase tracking-wider mb-1">{t("attempts")}</div>
          <div className="space-y-1.5">
            {node.attempts.map((a) => (
              <div key={a.attempt_no} className="rounded-[var(--radius-control)] border border-line px-2.5 py-1.5 text-[10px] text-fg-muted space-y-0.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold shrink-0">
                    {t("attemptNo", { no: a.attempt_no })} · {t(`attemptStatus.${a.status}`)}
                  </span>
                  <span className="text-fg-faint truncate">
                    {fmtTime(a.started_at)} → {fmtTime(a.completed_at)}
                  </span>
                </div>
                {a.error && (a.error.code || a.error.message) && (
                  <div className="text-danger-fg">
                    {a.error.code ? `${a.error.code}: ` : ""}
                    {a.error.message}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      <JsonBlock label={t("input")} value={node.input} />
      <JsonBlock label={t("output")} value={node.output} />
      {node.status === "failed" && <NodeErrorBlock error={node.error} />}
      {node.status === "skipped" && <NodeSkippedHint />}
    </div>
  );
}

// ==========================================
// 运行态(taskId)
// ==========================================
interface RunViewData {
  taskId: number;
  taskTitle: string;
  taskStatus: string;
  workflowName: string;
  workflowVersion: number;
  startedAt: string;
  nodes: RunNode[];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  edges: any[];
}

function RunMode({ taskId }: { taskId: number }) {
  const t = useTranslations("workflows.view");
  const [data, setData] = useState<RunViewData | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    api
      .getTaskGraph(taskId)
      .then((raw) => {
        const g = raw as {
          task?: Record<string, unknown>;
          nodes?: Record<string, unknown>[];
          edges?: Record<string, unknown>[];
        };
        setData({
          taskId,
          taskTitle: String(g.task?.title ?? ""),
          taskStatus: String(g.task?.status ?? "active"),
          workflowName: String(g.task?.workflow_name ?? ""),
          workflowVersion: Number(g.task?.workflow_version) || 0,
          startedAt: String(g.task?.created_at ?? ""),
          nodes: (g.nodes || []).map(toRunNode),
          edges: g.edges || [],
        });
        setLoadError(null);
        setLoading(false);
      })
      .catch((err) => {
        setLoadError(err instanceof ApiError ? err.message : String(err));
        setLoading(false);
      });
  }, [taskId]);

  useEffect(() => {
    load();
  }, [load]);

  const rfNodes = useMemo<RunRFNode[]>(() => {
    if (!data) return [];
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const runEdgesRaw = data.edges as Array<{ id: string; source_node_id: string; target_node_id: string }>;
    return layoutNodes(data.nodes, runEdgesRaw).map((n) => ({
      id: n.id,
      type: "v2run" as const,
      position: { x: n.ui_config?.x ?? 0, y: n.ui_config?.y ?? 0 },
      data: { node: n },
      selected: n.id === selectedId,
    }));
  }, [data, selectedId]);

  const rfEdges = useMemo(() => {
    if (!data) return [];
    const resolved = (data.edges as Array<{ id: string; source_node_id: string; source_port: string; target_node_id: string; target_port: string; resolution?: string }>).map((e) => ({
      ...e,
      resolution: (e.resolution || "pending") as EdgeResolution,
    }));
    return runEdges(resolved);
  }, [data]);

  const selectedNode = data?.nodes.find((n) => n.id === selectedId) ?? null;

  const handleSubmit = async (action: "submitted" | "cancelled") => {
    if (!selectedNode) return;
    setSubmitting(true);
    try {
      await api.submitHumanInput(taskId, selectedNode.id, { action, data: {} });
      load();
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setSubmitting(false);
    }
  };

  if (loading && !data) {
    return (
      <div className="h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-fg-faint" />
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-line bg-surface shrink-0">
        <button
          onClick={() => history.back()}
          className="inline-flex items-center gap-1.5 h-[34px] px-2.5 text-xs text-fg-subtle hover:text-fg rounded-[var(--radius-control)] hover:bg-surface-2 transition-colors-fast focus-visible:outline-none"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> {t("back")}
        </button>
        <div className="w-px h-5 bg-line" />
        <span className="text-sm font-semibold text-fg truncate">{data?.taskTitle}</span>
        {data && <UnresolvedRefMark title={data.taskTitle} />}
        {data && (
          <StatusBadge tone={TASK_STATUS_TONE[data.taskStatus] ?? "neutral"} dot>
            {t(`run.taskStatus.${data.taskStatus}`)}
          </StatusBadge>
        )}
        <div className="flex-1" />
        {data && data.workflowName && (
          <span className="text-[11px] text-fg-faint truncate max-w-[280px]">
            {data.workflowName}
            {data.workflowVersion > 0 ? ` · v${data.workflowVersion}` : ""}
          </span>
        )}
        <span className="text-[11px] text-fg-faint">{fmtTime(data?.startedAt)}</span>
      </div>

      {loadError && (
        <div className="flex items-center gap-2 px-4 py-1.5 bg-danger/10 border-b border-danger/25 text-xs text-danger-fg shrink-0">
          <span>{loadError}</span>
          <button onClick={load} className="ml-auto text-fg-subtle hover:text-fg">
            {t("retry")}
          </button>
        </div>
      )}

      <div className="flex-1 flex min-h-0">
        <div className="flex-1 relative cockpit-grid bg-surface">
          <ReactFlow
            nodes={rfNodes}
            edges={rfEdges}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onPaneClick={() => setSelectedId(null)}
            nodeTypes={runNodeTypes}
            nodesDraggable={false}
            nodesConnectable={false}
            fitView
            className="bg-transparent"
            proOptions={{ hideAttribution: true }}
          />
        </div>
        <RunNodeDetail node={selectedNode} onSubmit={handleSubmit} submitting={submitting} />
      </div>
    </div>
  );
}

// ==========================================
// 模板预览(templateId)
// ==========================================
function TemplateMode({ templateId }: { templateId: string }) {
  const t = useTranslations("workflows.view");
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const [graph, setGraph] = useState<{ name: string; description: string; isBuiltin: boolean; version: number; enabled: boolean } | null>(null);
  const [nodes, setNodes] = useState<TemplateNode[]>([]);
  const [edges, setEdges] = useState<TemplateEdge[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    if (!currentWorkspaceId) return;
    let cancelled = false;
    setLoading(true);
    api
      .getWorkflow(currentWorkspaceId, templateId)
      .then((raw) => {
        if (cancelled) return;
        const meta = (raw as { template?: Record<string, unknown> }).template ?? {};
        const normalized = normalizeGraph(raw as { nodes?: unknown[]; edges?: unknown[] });
        setGraph({
          name: String(meta.name ?? ""),
          description: String(meta.description ?? ""),
          isBuiltin: meta.is_builtin === true,
          version: Number(meta.version) || 0,
          enabled: meta.enabled !== false,
        });
        setNodes(normalized.nodes);
        setEdges(normalized.edges);
        setLoading(false);
      })
      .catch((err) => {
        if (cancelled) return;
        setLoadError(err instanceof ApiError ? err.message : String(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [currentWorkspaceId, templateId]);

  const rfNodes = useMemo<EditorRFNode[]>(
    () =>
      // ui_config 缺失的模板(如导入文件)在预览画布上走 dagre 自动布局
      layoutNodes(nodes, edges).map((n) => ({
        id: n.id,
        type: "v2" as const,
        position: { x: n.ui_config?.x ?? 0, y: n.ui_config?.y ?? 0 },
        data: { node: n, invalid: false } satisfies EditorNodeData,
        selected: n.id === selectedId,
      })),
    [nodes, edges, selectedId]
  );
  const rfEdges = useMemo<Edge[]>(
    () =>
      edges.map((e) => ({
        id: e.id,
        source: e.source_node_id,
        sourceHandle: e.source_port,
        target: e.target_node_id,
        targetHandle: e.target_port,
        style: templateEdgeStyle(nodes, e.source_node_id, e.source_port),
      })),
    [nodes, edges]
  );

  const selectedNode = nodes.find((n) => n.id === selectedId) ?? null;

  if (loading && !graph) {
    return (
      <div className="h-full flex items-center justify-center">
        <Loader2 className="w-6 h-6 animate-spin text-fg-faint" />
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-line bg-surface shrink-0">
        <button
          onClick={() => history.back()}
          className="inline-flex items-center gap-1.5 h-[34px] px-2.5 text-xs text-fg-subtle hover:text-fg rounded-[var(--radius-control)] hover:bg-surface-2 transition-colors-fast focus-visible:outline-none"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> {t("back")}
        </button>
        <div className="w-px h-5 bg-line" />
        <span className="text-sm font-semibold text-fg truncate">{graph?.name}</span>
        {graph && (
          <StatusBadge tone={graph.enabled ? "success" : "neutral"} dot>
            {t(graph.enabled ? "enabledOn" : "enabledOff")}
          </StatusBadge>
        )}
        {graph?.isBuiltin && (
          <StatusBadge tone="neutral">{t("builtInTag")}</StatusBadge>
        )}
        <div className="flex-1" />
        {graph && graph.version > 0 && (
          <span className="text-[11px] text-fg-faint">v{graph.version}</span>
        )}
      </div>

      {graph?.description && (
        <div className="px-4 py-1.5 bg-surface-2 border-b border-line text-xs text-fg-subtle shrink-0 truncate">
          {graph.description}
        </div>
      )}

      {loadError && (
        <div className="flex items-center gap-2 px-4 py-1.5 bg-danger/10 border-b border-danger/25 text-xs text-danger-fg shrink-0">
          <span>{loadError}</span>
        </div>
      )}

      <div className="flex-1 flex min-h-0">
        <div className="flex-1 relative cockpit-grid bg-surface">
          <ReactFlow
            nodes={rfNodes}
            edges={rfEdges}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onPaneClick={() => setSelectedId(null)}
            nodeTypes={{ v2: NodeCard }}
            nodesDraggable={false}
            nodesConnectable={false}
            fitView
            className="bg-transparent"
            proOptions={{ hideAttribution: true }}
          />
        </div>
        <TemplateNodeDetail node={selectedNode} />
      </div>
    </div>
  );
}

// 模板预览详情:节点类型与配置快照
function TemplateNodeDetail({ node }: { node: TemplateNode | null }) {
  const t = useTranslations("workflows.view");
  const te = useTranslations("workflows.editor");
  if (!node) {
    return (
      <div className="w-80 shrink-0 border-l border-line bg-surface flex items-center justify-center p-6">
        <p className="text-xs text-fg-faint text-center">{t("preview.detailEmpty")}</p>
      </div>
    );
  }
  const d = def(node.type_key);
  const Icon = d.icon;
  return (
    <div className="w-80 shrink-0 border-l border-line bg-surface overflow-y-auto p-4 space-y-3">
      <div className="flex items-center gap-2">
        <Icon className="w-4 h-4" style={{ color: "var(--primary)" }} />
        <div className="min-w-0 flex-1">
          <div className="text-xs font-bold text-fg truncate">{node.name}</div>
          <div className="text-[10px] text-fg-faint">{te(`types.${node.type_key}.label`)}</div>
        </div>
      </div>
      <JsonBlock label={t("preview.config")} value={node.config} />
    </div>
  );
}

// ==========================================
// 入口:按 URL 参数分派模式
// ==========================================
function ViewInner() {
  const t = useTranslations("workflows.view");
  const searchParams = useSearchParams();
  const router = useRouter();
  const taskId = Number(searchParams.get("taskId"));
  const templateId = searchParams.get("templateId");

  if (Number.isFinite(taskId) && taskId > 0) {
    return <RunMode taskId={taskId} />;
  }
  if (templateId) {
    return <TemplateMode templateId={templateId} />;
  }
  return (
    <div className="h-full flex items-center justify-center">
      <EmptyState
        title={t("noParam")}
        action={
          <Button variant="secondary" onClick={() => router.push("/workflows")}>
            <ArrowLeft className="w-3.5 h-3.5 mr-1" /> {t("backToList")}
          </Button>
        }
      />
    </div>
  );
}

export default function WorkflowViewPage() {
  return (
    <Suspense
      fallback={
        <div className="h-full flex items-center justify-center">
          <Loader2 className="w-6 h-6 animate-spin text-fg-faint" />
        </div>
      }
    >
      <ReactFlowProvider>
        <ViewInner />
      </ReactFlowProvider>
    </Suspense>
  );
}
