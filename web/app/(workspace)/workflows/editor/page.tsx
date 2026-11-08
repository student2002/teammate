"use client";
// 工作流模板编辑器:新建(POST)与编辑(GET 加载 + PUT 整图保存)走真实后端,
// 校验失败展示服务端 error;画布位置写入 ui_config,自动排列是显式操作。
// 起始模板选择器提供空白与预制模板(预制项带"预制"标识)。

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Connection,
  type Edge,
  type NodeChange,
  type OnConnectEnd,
  type OnConnectStart,
  type OnEdgesChange,
  type OnNodesChange,
} from "@xyflow/react";
import { useTranslations } from "next-intl";
import { ArrowLeft, Download, FilePlus2, LayoutGrid, Loader2, RotateCw, Save, TriangleAlert, X } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import Dagre from "@dagrejs/dagre";
import "@xyflow/react/dist/style.css";
import api, { ApiError } from "@/lib/api";
import { useAppStore } from "@/lib/store";
import Button from "@/lib/ui/Button";
import { CATEGORY_TONE, LIBRARY_GROUPS, NODE_CATALOG, defaultConfig, newId, outputPorts, TONE_VAR } from "@/lib/workflow/catalog";
import {
  ISSUE_FIX_TEMPLATE,
  instantiateTemplate,
  buildBlankTemplate,
  buildManualTemplate,
  buildScheduleTemplate,
  buildWebhookTemplate,
  contextPaths,
} from "@/lib/workflow/presets";
import { Inspector } from "@/lib/workflow/inspectors";
import { NodeLibrary } from "@/lib/workflow/NodeLibrary";
import { NodeCard, type EditorNodeData, type EditorRFNode } from "@/lib/workflow/NodeCard";
import { layoutNodes } from "@/lib/workflow/RunCanvas";
import { validateTemplate, type ValidationIssue } from "@/lib/workflow/validation";
import { downloadTemplateFile } from "@/lib/workflow/transfer";
import type { TemplateEdge, TemplateGraph, TemplateNode, WorkflowTypeKey } from "@/lib/workflow/types";

const NODE_W = 240;
const NODE_H = 96;
// 拖线落空浮层的尺寸(与 w-52/max-h-[320px] 对应),用于把浮层夹在画布容器内
const CONNECT_MENU_W = 208;
const CONNECT_MENU_H = 320;

// 服务端节点/边响应 → 前端形状(去掉审计字段;ui_config 缺失时留空,
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

function edgeStyleFor(nodes: TemplateNode[], sourceId: string, sourcePort: string): React.CSSProperties {
  const source = nodes.find((n) => n.id === sourceId);
  if (!source) return { stroke: "var(--line-strong)", strokeWidth: 2 };
  const port = outputPorts(source.type_key, source.config).find((p) => p.key === sourcePort);
  return { stroke: port ? TONE_VAR[port.tone] : "var(--line-strong)", strokeWidth: 2 };
}

function wouldCreateCycle(edges: TemplateEdge[], source: string, target: string): boolean {
  const adj = new Map<string, string[]>();
  for (const e of edges) adj.set(e.source_node_id, [...(adj.get(e.source_node_id) ?? []), e.target_node_id]);
  const seen = new Set<string>();
  const stack = [target];
  while (stack.length) {
    const cur = stack.pop()!;
    if (cur === source) return true;
    if (seen.has(cur)) continue;
    seen.add(cur);
    stack.push(...(adj.get(cur) ?? []));
  }
  return false;
}

function autoLayout(nodes: TemplateNode[], edges: TemplateEdge[], dir: "LR" | "TB"): TemplateNode[] {
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: dir, nodesep: 50, ranksep: 90 });
  nodes.forEach((n) => g.setNode(n.id, { width: NODE_W, height: NODE_H }));
  edges.forEach((e) => g.setEdge(e.source_node_id, e.target_node_id));
  Dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    return p ? { ...n, ui_config: { ...n.ui_config, x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 } } : n;
  });
}

// 新建/重选起始模板:空白骨架或预制模板(每个触发类型一个),选择后整图替换画布
// 每次选用预制都实例化为全新 UUID 的整图:同一预制可在多个工作区/多次新建中
// 各自保存而不撞节点主键(服务端节点 id 是全局 UUID)。
const START_OPTIONS: Array<{ key: string; build: () => TemplateGraph; icon: LucideIcon }> = [
  { key: "blank", build: () => instantiateTemplate(buildBlankTemplate()), icon: FilePlus2 },
  { key: "manual", build: () => instantiateTemplate(buildManualTemplate()), icon: NODE_CATALOG["trigger.manual"].icon },
  { key: "schedule", build: () => instantiateTemplate(buildScheduleTemplate()), icon: NODE_CATALOG["trigger.schedule"].icon },
  { key: "issue-fix", build: () => instantiateTemplate(ISSUE_FIX_TEMPLATE), icon: NODE_CATALOG["trigger.github"].icon },
  { key: "webhook", build: () => instantiateTemplate(buildWebhookTemplate()), icon: NODE_CATALOG["trigger.webhook"].icon },
];

// 起始模板选择:画布左上角的内联固定面板(定位风格同 ConnectNodeMenu,不用遮罩层,
// 画布保持可交互);新建入口默认打开,选择后整图替换画布并关闭,"空白"即不用起始模板
function StartPicker({ onPick, onClose }: { onPick: (build: () => TemplateGraph) => void; onClose: () => void }) {
  const t = useTranslations("workflows.editor");
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="absolute left-3 top-3 z-30 w-72 bg-surface border border-line rounded-[var(--radius-panel)] shadow-2xl py-1.5 max-h-[320px] overflow-y-auto">
      <div className="flex items-center justify-between px-2.5 py-1.5">
        <span className="text-xs font-semibold text-fg">{t("startPicker.title")}</span>
        <button
          onClick={onClose}
          className="p-1 text-fg-faint hover:text-fg rounded-[var(--radius-control)] hover:bg-surface-2"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
      {START_OPTIONS.map((opt) => {
        const Icon = opt.icon;
        return (
          <button
            key={opt.key}
            onClick={() => onPick(opt.build)}
            className="w-full flex items-center gap-2.5 px-2.5 py-2 text-left hover:bg-surface-2 transition-colors-fast"
          >
            <Icon className="w-4 h-4 shrink-0" style={{ color: "var(--primary)" }} />
            <div className="min-w-0">
              <div className="text-xs font-semibold text-fg flex items-center gap-1.5">
                {opt.key !== "blank" && (
                  <span className="text-[9px] px-1.5 py-0.5 rounded bg-primary/15 text-primary-fg">{t("startPicker.presetTag")}</span>
                )}
                {t(`startPicker.opt.${opt.key}.name`)}
              </div>
              <div className="text-[10px] text-fg-faint mt-0.5">{t(`startPicker.opt.${opt.key}.desc`)}</div>
            </div>
          </button>
        );
      })}
      <p className="text-[9px] text-fg-faint px-2.5 pt-1.5">{t("startPicker.hint")}</p>
    </div>
  );
}

// 拖线落空后的节点类型选择浮层:列出节点库全部可加类型,选中后在落点建节点并自动连线
function ConnectNodeMenu({
  position,
  onPick,
  onClose,
}: {
  position: { left: number; top: number };
  onPick: (typeKey: WorkflowTypeKey) => void;
  onClose: () => void;
}) {
  const t = useTranslations("workflows.editor");
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div className="absolute inset-0 z-20" onClick={onClose}>
      <div
        className="absolute w-52 bg-surface border border-line rounded-[var(--radius-panel)] shadow-2xl py-1.5 max-h-[320px] overflow-y-auto"
        style={{ left: position.left, top: position.top }}
        onClick={(e) => e.stopPropagation()}
      >
        {LIBRARY_GROUPS.map((group) => (
          <div key={group.category}>
            <div className="px-2.5 pt-1.5 pb-0.5 text-[9px] text-fg-faint">{t(`library.groups.${group.category}`)}</div>
            {group.typeKeys.map((typeKey) => {
              const d = NODE_CATALOG[typeKey];
              const Icon = d.icon;
              return (
                <button
                  key={typeKey}
                  onClick={() => onPick(typeKey)}
                  className="w-full flex items-center gap-2 px-2.5 py-1.5 text-left text-xs text-fg-muted hover:bg-surface-2 hover:text-fg transition-colors-fast"
                >
                  <Icon className="w-3.5 h-3.5 shrink-0" style={{ color: TONE_VAR[CATEGORY_TONE[d.category]] }} />
                  <span className="truncate">{t(`types.${typeKey}.label`)}</span>
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function EditorInner() {
  const t = useTranslations("workflows.editor");
  const tWorkflows = useTranslations("workflows");
  const router = useRouter();
  const searchParams = useSearchParams();
  const templateId = searchParams.get("templateId");
  const { fitView, screenToFlowPosition } = useReactFlow();
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const refreshTemplates = useAppStore((s) => s.refreshTemplates);
  const refreshIntegrations = useAppStore((s) => s.refreshIntegrations);

  const [template, setTemplate] = useState<TemplateGraph>(() => buildBlankTemplate());
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // 配置面板只在双击节点时打开;单击仅选中,点击画布空白处关闭
  const [panelOpen, setPanelOpen] = useState(false);
  const [issuesOpen, setIssuesOpen] = useState(false);
  const [connectError, setConnectError] = useState<string | null>(null);
  // 拖线落空时的建节点浮层:left/top 为画布容器内坐标,clientX/Y 供换算流坐标
  const [connectMenu, setConnectMenu] = useState<{
    left: number;
    top: number;
    clientX: number;
    clientY: number;
    fromNodeId: string;
    fromHandleId: string;
    fromHandleType: "source" | "target";
  } | null>(null);
  const canvasRef = useRef<HTMLDivElement>(null);
  // 拖线起点(clientX/Y):onConnectEnd 据此区分单击端口与真实拖拽
  const connectStartRef = useRef<{ x: number; y: number } | null>(null);
  // 新建入口先选起始模板;编辑入口(带 templateId)加载服务端整图
  const [pickerOpen, setPickerOpen] = useState(!templateId);
  const [loading, setLoading] = useState(!!templateId);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // 编辑模式:加载服务端整图(ui_config 驱动布局)
  useEffect(() => {
    if (!templateId) return;
    if (!currentWorkspaceId) return;
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    api
      .getWorkflow(currentWorkspaceId, templateId)
      .then((data) => {
        if (cancelled) return;
        const raw = data as { template?: { name?: string; description?: string; enabled?: boolean }; nodes?: unknown[]; edges?: unknown[] };
        const { nodes, edges } = normalizeGraph(raw);
        setTemplate({
          name: raw.template?.name || "",
          description: raw.template?.description || "",
          enabled: raw.template?.enabled ?? true,
          nodes,
          edges,
        });
        setLoading(false);
        setTimeout(() => fitView({ duration: 300 }), 60);
      })
      .catch((err) => {
        if (cancelled) return;
        setLoadError(err instanceof ApiError ? err.message : String(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [templateId, currentWorkspaceId, fitView]);

  const issues = useMemo(() => validateTemplate(template.nodes, template.edges), [template]);
  const issuesByNode = useMemo(() => {
    const map = new Map<string, ValidationIssue[]>();
    for (const issue of issues) if (issue.node_id) map.set(issue.node_id, [...(map.get(issue.node_id) ?? []), issue]);
    return map;
  }, [issues]);

  // 集成下拉数据源:工作区就绪后刷新(与工程/代理同一数据加载时机)
  useEffect(() => {
    if (!currentWorkspaceId) return;
    refreshIntegrations();
  }, [currentWorkspaceId, refreshIntegrations]);

  // 卡上内联编辑:按节点 id 写 config(不依赖当前选中节点)
  const patchConfigById = useCallback((nodeId: string, config: Record<string, unknown>) => {
    setTemplate((prev) => ({ ...prev, nodes: prev.nodes.map((n) => (n.id === nodeId ? { ...n, config } : n)) }));
  }, []);

  const rfNodes = useMemo<EditorRFNode[]>(
    () =>
      // ui_config 缺失的模板(如导入文件)在画布上走 dagre 自动布局;
      // 全部带位置时 layoutNodes 原样返回,行为不变
      layoutNodes(template.nodes, template.edges).map((n) => ({
        id: n.id,
        type: "v2" as const,
        position: { x: n.ui_config?.x ?? 0, y: n.ui_config?.y ?? 0 },
        data: {
          node: n,
          invalid: issuesByNode.has(n.id),
          // 每节点各自的上游路径候选(trigger/上游输出/vars),供卡上字段选择器
          paths: contextPaths(template.nodes, template.edges, n.id),
          onConfig: (config) => patchConfigById(n.id, config),
          // Inspector 打开期间隐藏选中节点的卡片控件,避免双实例草稿互踩
          widgetsHidden: panelOpen && n.id === selectedId,
        } satisfies EditorNodeData,
        selected: n.id === selectedId,
        // 节点只能经 Inspector 删除;键盘删除仅作用于连线
        deletable: false,
      })),
    [template.nodes, template.edges, issuesByNode, selectedId, panelOpen, patchConfigById]
  );

  const rfEdges = useMemo<Edge[]>(
    () =>
      template.edges.map((e) => ({
        id: e.id,
        source: e.source_node_id,
        sourceHandle: e.source_port,
        target: e.target_node_id,
        targetHandle: e.target_port,
        style: edgeStyleFor(template.nodes, e.source_node_id, e.source_port),
      })),
    [template.nodes, template.edges]
  );

  // 拖动位置即时写回 ui_config;其余 change(尺寸/选择)忽略,选择由点击事件管理
  const onNodesChange: OnNodesChange<EditorRFNode> = useCallback(
    (changes: NodeChange<EditorRFNode>[]) => {
      const moves = changes.filter((c) => c.type === "position" && c.position);
      if (moves.length === 0) return;
      setTemplate((prev) => ({
        ...prev,
        nodes: prev.nodes.map((n) => {
          const move = moves.find((c) => c.type === "position" && c.id === n.id);
          return move && move.type === "position" && move.position
            ? { ...n, ui_config: { ...n.ui_config, ...move.position } }
            : n;
        }),
      }));
    },
    []
  );

  // 新建连线与拖线建节点共用的合法性检查:自环/重复/端口多线/成环;非法返回提示文案
  const edgeIssue = useCallback(
    (edges: TemplateEdge[], source: string, sourcePort: string, target: string, targetPort: string): string | null => {
      if (source === target) return t("issues.self");
      const dup = edges.some(
        (e) => e.source_node_id === source && e.source_port === sourcePort && e.target_node_id === target && e.target_port === targetPort
      );
      if (dup) return t("issues.edgeDuplicate");
      const portUsed = edges.some((e) => e.source_node_id === source && e.source_port === sourcePort);
      if (portUsed) return t("issues.portMultiEdge");
      if (wouldCreateCycle(edges, source, target)) return t("issues.cycle");
      return null;
    },
    [t]
  );

  const onConnect = useCallback(
    (c: Connection) => {
      setConnectError(null);
      if (!c.source || !c.target || !c.sourceHandle || !c.targetHandle) return;
      const err = edgeIssue(template.edges, c.source, c.sourceHandle, c.target, c.targetHandle);
      if (err) return setConnectError(err);
      const edge: TemplateEdge = {
        id: newId(),
        source_node_id: c.source,
        source_port: c.sourceHandle,
        target_node_id: c.target,
        target_port: c.targetHandle,
      };
      setTemplate((prev) => ({ ...prev, edges: [...prev.edges, edge] }));
    },
    [template.edges, edgeIssue]
  );

  // 重连:拖动已连边的端点换节点/端口;校验与新建连线一致,但排除被替换的旧边自身
  const onReconnect = useCallback(
    (oldEdge: Edge, c: Connection) => {
      setConnectError(null);
      if (!c.source || !c.target || !c.sourceHandle || !c.targetHandle) return;
      const others = template.edges.filter((e) => e.id !== oldEdge.id);
      const err = edgeIssue(others, c.source, c.sourceHandle, c.target, c.targetHandle);
      if (err) return setConnectError(err);
      setTemplate((prev) => ({
        ...prev,
        edges: prev.edges.map((e) =>
          e.id === oldEdge.id
            ? { ...e, source_node_id: c.source!, source_port: c.sourceHandle!, target_node_id: c.target!, target_port: c.targetHandle! }
            : e
        ),
      }));
    },
    [template.edges, edgeIssue]
  );

  // 拖线开始:记录起点坐标,供 onConnectEnd 区分单击端口与真实拖拽
  const onConnectStart: OnConnectStart = useCallback((event) => {
    const pt = "changedTouches" in event ? event.changedTouches[0] : event;
    connectStartRef.current = pt ? { x: pt.clientX, y: pt.clientY } : null;
  }, []);

  // 拖线未落到有效目标端口(落空或目标不合法)时,在落点打开"新建节点并连线"浮层;
  // 与起点距离过近视为单击端口而非拖拽,不弹浮层
  const onConnectEnd: OnConnectEnd = useCallback((event, state) => {
    if (state.toNode && state.isValid !== false) return;
    const fromNode = state.fromNode;
    const fromHandle = state.fromHandle;
    if (!fromNode || !fromHandle?.id) return;
    const rect = canvasRef.current?.getBoundingClientRect();
    if (!rect) return;
    const pt = "changedTouches" in event ? event.changedTouches[0] : event;
    if (!pt) return;
    const start = connectStartRef.current;
    if (start && Math.hypot(pt.clientX - start.x, pt.clientY - start.y) < 8) return;
    setConnectMenu({
      left: Math.max(8, Math.min(pt.clientX - rect.left, rect.width - CONNECT_MENU_W - 8)),
      top: Math.max(8, Math.min(pt.clientY - rect.top, rect.height - CONNECT_MENU_H - 8)),
      clientX: pt.clientX,
      clientY: pt.clientY,
      fromNodeId: fromNode.id,
      fromHandleId: fromHandle.id,
      fromHandleType: fromHandle.type === "target" ? "target" : "source",
    });
  }, []);

  // 浮层选型:在落点创建节点;拖出端口是输出则接新节点 input,是输入则接新节点 success。
  // 建边走与 onConnect 相同的合法性检查,非法时仍建节点但跳过连线并提示
  const createConnectedNode = useCallback(
    (typeKey: WorkflowTypeKey) => {
      const menu = connectMenu;
      if (!menu) return;
      setConnectMenu(null);
      const flowPos = screenToFlowPosition({ x: menu.clientX, y: menu.clientY });
      const node: TemplateNode = {
        id: newId(),
        type_key: typeKey,
        name: t(`types.${typeKey}.label`),
        sort_order: Math.max(-1, ...template.nodes.map((n) => n.sort_order)) + 1,
        config: defaultConfig(typeKey),
        ui_config: { x: flowPos.x - NODE_W / 2, y: flowPos.y - NODE_H / 2 },
      };
      const fromSource = menu.fromHandleType === "source";
      const edge: TemplateEdge = fromSource
        ? { id: newId(), source_node_id: menu.fromNodeId, source_port: menu.fromHandleId, target_node_id: node.id, target_port: "input" }
        : { id: newId(), source_node_id: node.id, source_port: "success", target_node_id: menu.fromNodeId, target_port: menu.fromHandleId };
      const err = edgeIssue(template.edges, edge.source_node_id, edge.source_port, edge.target_node_id, edge.target_port);
      if (err) setConnectError(err);
      setTemplate((prev) => ({
        ...prev,
        nodes: [...prev.nodes, node],
        edges: err ? prev.edges : [...prev.edges, edge],
      }));
      setSelectedId(node.id);
    },
    [connectMenu, screenToFlowPosition, template.nodes, template.edges, t, edgeIssue]
  );

  // 连线删除(键盘 Delete/Backspace;节点不可经键盘删除)
  const onEdgesChange: OnEdgesChange<Edge> = useCallback((changes) => {
    const removed = changes.filter((c) => c.type === "remove").map((c) => c.id);
    if (removed.length === 0) return;
    setTemplate((prev) => ({ ...prev, edges: prev.edges.filter((e) => !removed.includes(e.id)) }));
  }, []);

  const addNode = useCallback(
    (typeKey: WorkflowTypeKey) => {
      const center = screenToFlowPosition({ x: window.innerWidth / 2, y: window.innerHeight / 2 });
      const node: TemplateNode = {
        id: newId(),
        type_key: typeKey,
        name: t(`types.${typeKey}.label`),
        sort_order: Math.max(-1, ...template.nodes.map((n) => n.sort_order)) + 1,
        config: defaultConfig(typeKey),
        ui_config: { x: center.x - NODE_W / 2, y: center.y - NODE_H / 2 },
      };
      setTemplate((prev) => ({ ...prev, nodes: [...prev.nodes, node] }));
      setSelectedId(node.id);
    },
    [screenToFlowPosition, template.nodes, t]
  );

  const patchName = useCallback(
    (name: string) => {
      if (!selectedId) return;
      setTemplate((prev) => ({ ...prev, nodes: prev.nodes.map((n) => (n.id === selectedId ? { ...n, name } : n)) }));
    },
    [selectedId]
  );

  const patchConfig = useCallback(
    (config: Record<string, unknown>) => {
      if (!selectedId) return;
      setTemplate((prev) => ({ ...prev, nodes: prev.nodes.map((n) => (n.id === selectedId ? { ...n, config } : n)) }));
    },
    [selectedId]
  );

  // 开始节点的触发方式切换:换 type_key 并重置为该方式的默认配置
  const switchTypeKey = useCallback(
    (typeKey: WorkflowTypeKey) => {
      if (!selectedId) return;
      setTemplate((prev) => ({
        ...prev,
        nodes: prev.nodes.map((n) =>
          n.id === selectedId && n.type_key.startsWith("trigger.") ? { ...n, type_key: typeKey, config: defaultConfig(typeKey) } : n
        ),
      }));
    },
    [selectedId]
  );

  const deleteSelected = useCallback(() => {
    if (!selectedId) return;
    const node = template.nodes.find((n) => n.id === selectedId);
    if (!node) return;
    const deletable = node.type_key !== "flow.end" && !node.type_key.startsWith("trigger.");
    if (!deletable) return;
    setTemplate((prev) => ({
      ...prev,
      nodes: prev.nodes.filter((n) => n.id !== selectedId),
      edges: prev.edges.filter((e) => e.source_node_id !== selectedId && e.target_node_id !== selectedId),
    }));
    setSelectedId(null);
    setPanelOpen(false);
  }, [selectedId, template.nodes]);

  const relayout = (dir: "LR" | "TB") => {
    setTemplate((prev) => ({ ...prev, nodes: autoLayout(prev.nodes, prev.edges, dir) }));
    setTimeout(() => fitView({ duration: 300 }), 60);
  };

  const applyStart = useCallback(
    (build: () => TemplateGraph) => {
      setTemplate(structuredClone(build()));
      setSelectedId(null);
      setPanelOpen(false);
      setConnectError(null);
      setIssuesOpen(false);
      setPickerOpen(false);
      setTimeout(() => fitView({ duration: 300 }), 60);
    },
    [fitView]
  );

  // 整图保存:新建 POST,编辑 PUT;服务端校验失败把 error 原文展示在顶栏
  const handleSave = useCallback(async () => {
    if (!currentWorkspaceId || saving) return;
    setSaving(true);
    setSaveError(null);
    try {
      const payload = {
        name: template.name.trim(),
        description: template.description,
        enabled: template.enabled,
        nodes: template.nodes.map((n) => ({
          id: n.id,
          type_key: n.type_key,
          name: n.name,
          sort_order: n.sort_order,
          config: n.config,
          ui_config: n.ui_config,
        })),
        edges: template.edges.map((e) => ({
          id: e.id,
          source_node_id: e.source_node_id,
          source_port: e.source_port,
          target_node_id: e.target_node_id,
          target_port: e.target_port,
          label: e.label || "",
          sort_order: 0,
          config: {},
        })),
      };
      if (templateId) {
        await api.updateWorkflow(currentWorkspaceId, templateId, payload);
      } else {
        await api.createWorkflow(currentWorkspaceId, payload);
      }
      await refreshTemplates();
      router.push("/workflows");
    } catch (err) {
      setSaveError(err instanceof ApiError ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }, [currentWorkspaceId, saving, template, templateId, refreshTemplates, router]);

  const selectedNode = template.nodes.find((n) => n.id === selectedId) ?? null;
  const nodeName = (id: string | undefined) => template.nodes.find((n) => n.id === id)?.name ?? "";
  const selectedHasFailureEdge =
    selectedNode !== null && template.edges.some((e) => e.source_node_id === selectedNode.id && e.source_port === "failed");

  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-line bg-surface shrink-0">
        <button
          onClick={() => router.push("/workflows")}
          className="inline-flex items-center gap-1.5 h-[34px] px-2.5 text-xs text-fg-subtle hover:text-fg rounded-[var(--radius-control)] hover:bg-surface-2 transition-colors-fast focus-visible:outline-none"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> {t("topbar.back")}
        </button>
        <div className="w-px h-5 bg-line" />
        <input
          value={template.name}
          onChange={(e) => setTemplate((prev) => ({ ...prev, name: e.target.value }))}
          placeholder={t("topbar.namePlaceholder")}
          className="w-56 h-[34px] bg-transparent text-sm font-semibold text-fg placeholder-fg-faint outline-none px-2 rounded-[var(--radius-control)] border border-transparent focus:border-primary"
        />
        <input
          value={template.description}
          onChange={(e) => setTemplate((prev) => ({ ...prev, description: e.target.value }))}
          placeholder={t("topbar.descPlaceholder")}
          className="flex-1 max-w-md h-[34px] bg-transparent text-xs text-fg-muted placeholder-fg-faint outline-none px-2 rounded-[var(--radius-control)] border border-transparent focus:border-primary"
        />
        <div className="flex-1" />
        <button
          onClick={() => setIssuesOpen((v) => !v)}
          className={`inline-flex items-center gap-1.5 h-[34px] px-2.5 text-xs shrink-0 whitespace-nowrap rounded-[var(--radius-control)] border transition-colors-fast ${
            issues.length > 0
              ? "text-danger-fg bg-danger/10 border-danger/25"
              : "text-success-fg bg-success/10 border-success/25"
          }`}
        >
          <TriangleAlert className="w-3.5 h-3.5" />
          {issues.length > 0 ? t("topbar.issues", { count: issues.length }) : t("topbar.issuesOk")}
        </button>
        <Button variant="secondary" size="md" onClick={() => setPickerOpen(true)}>
          {t("topbar.startTemplate")}
        </Button>
        <Button variant="secondary" size="md" onClick={() => relayout("LR")}>
          <LayoutGrid className="w-3.5 h-3.5 mr-1" /> {t("topbar.layoutLR")}
        </Button>
        <Button variant="secondary" size="md" onClick={() => relayout("TB")}>
          <RotateCw className="w-3.5 h-3.5 mr-1" /> {t("topbar.layoutTB")}
        </Button>
        <Button variant="secondary" size="md" onClick={() => downloadTemplateFile(template)}>
          <Download className="w-3.5 h-3.5 mr-1" /> {tWorkflows("export")}
        </Button>
        <Button
          variant="primary"
          size="md"
          onClick={handleSave}
          disabled={loading || saving || issues.length > 0 || !template.name.trim() || !currentWorkspaceId}
        >
          {saving ? <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" /> : <Save className="w-3.5 h-3.5 mr-1" />}
          {t("topbar.save")}
        </Button>
      </div>

      {loadError && (
        <div className="flex items-center gap-2 px-4 py-1.5 bg-danger/10 border-b border-danger/25 text-xs text-danger-fg shrink-0">
          <span>{loadError}</span>
          <button onClick={() => router.push("/workflows")} className="ml-auto text-fg-subtle hover:text-fg">
            {t("topbar.back")}
          </button>
        </div>
      )}

      {saveError && (
        <div className="flex items-center gap-2 px-4 py-1.5 bg-danger/10 border-b border-danger/25 text-xs text-danger-fg shrink-0">
          <span className="min-w-0 truncate" title={saveError}>{saveError}</span>
          <button onClick={() => setSaveError(null)} className="ml-auto text-fg-subtle hover:text-fg">
            {t("dismiss")}
          </button>
        </div>
      )}

      {loading && (
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="w-6 h-6 animate-spin text-fg-faint" />
        </div>
      )}

      {issuesOpen && !loading && (
        <div className="px-4 py-2 border-b border-line bg-surface-2 max-h-40 overflow-y-auto shrink-0 space-y-1">
          {issues.length === 0 && <p className="text-xs text-fg-subtle">{t("topbar.issuesOk")}</p>}
          {issues.map((issue, i) => (
            <div key={i} className="text-xs text-danger-fg">
              {issue.node_id ? `${nodeName(issue.node_id)}: ` : ""}
              {t(`issues.${issue.key}`, issue.params)}
            </div>
          ))}
        </div>
      )}

      <div className={`flex-1 flex min-h-0 ${loading ? "hidden" : ""}`}>
        <NodeLibrary onAdd={addNode} />
        <div ref={canvasRef} className="flex-1 relative cockpit-grid bg-surface">
          <ReactFlow
            nodes={rfNodes}
            edges={rfEdges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onReconnect={onReconnect}
            onConnectStart={onConnectStart}
            onConnectEnd={onConnectEnd}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onNodeDoubleClick={(_, n) => {
              setSelectedId(n.id);
              setPanelOpen(true);
            }}
            onPaneClick={() => {
              setSelectedId(null);
              setPanelOpen(false);
            }}
            nodeTypes={{ v2: NodeCard }}
            fitView
            className="bg-transparent"
            proOptions={{ hideAttribution: true }}
            deleteKeyCode={["Backspace", "Delete"]}
          >
            <div className="absolute bottom-3 left-3 z-10 flex items-center gap-2 bg-surface border border-line rounded-[var(--radius-control)] px-2.5 py-1.5 text-[11px] text-fg-subtle">
              {t("topbar.nodeCount", { count: template.nodes.length })}
              <span className="text-fg-faint">·</span>
              {t("topbar.edgeCount", { count: template.edges.length })}
            </div>
          </ReactFlow>
          {connectError && (
            <div className="absolute top-3 left-1/2 -translate-x-1/2 z-20 text-xs text-danger-fg bg-danger/10 border border-danger/25 px-2.5 py-1 rounded-[var(--radius-control)]">
              {connectError}
            </div>
          )}
          {connectMenu && (
            <ConnectNodeMenu
              position={{ left: connectMenu.left, top: connectMenu.top }}
              onPick={createConnectedNode}
              onClose={() => setConnectMenu(null)}
            />
          )}
          {pickerOpen && <StartPicker onPick={applyStart} onClose={() => setPickerOpen(false)} />}
        </div>
        {panelOpen && selectedNode && (
          <Inspector
            key={selectedNode.id}
            node={selectedNode}
            issues={issuesByNode.get(selectedNode.id) ?? []}
            paths={contextPaths(template.nodes, template.edges, selectedId)}
            hasFailureEdge={selectedHasFailureEdge}
            onName={patchName}
            onConfig={patchConfig}
            onTypeKey={switchTypeKey}
            onDelete={deleteSelected}
            onClose={() => setPanelOpen(false)}
            deletable={!selectedNode.type_key.startsWith("trigger.") && selectedNode.type_key !== "flow.end"}
          />
        )}
      </div>
    </div>
  );
}

export default function WorkflowEditorPage() {
  return (
    <Suspense fallback={null}>
      <ReactFlowProvider>
        <EditorInner />
      </ReactFlowProvider>
    </Suspense>
  );
}
