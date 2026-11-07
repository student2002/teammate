"use client";
// 工作流模板画布编辑器:创建与编辑模板的唯一入口(替换旧列表式编辑器)。
// 加载:模板节点(uuid 为节点 id)+ depends_on 建边;编辑:面板/双击搜索加节点、连线建依赖
// (自环·重复·成环拦截)、右侧 Inspector 编辑全部节点字段;保存:Kahn 拓扑序生成 sort_order、
// 边反转 depends_on,POST/PUT 后回模板列表。布局由 dagre 从拓扑确定性派生,不存坐标。

import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  ReactFlow,
  ReactFlowProvider,
  Handle,
  Position,
  Background,
  BackgroundVariant,
  Panel,
  addEdge,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Node,
  type Edge,
  type Connection,
  type NodeProps,
} from "@xyflow/react";
import { useTranslations } from "next-intl";
import { ArrowLeft, Plus, Search, Trash2, X } from "lucide-react";
import Dagre from "@dagrejs/dagre";
import "@xyflow/react/dist/style.css";
import api from "@/lib/api";
import { mapTemplateFromApi } from "@/lib/mappers";
import { useAppStore } from "@/lib/store";

type NodeType = "standard" | "review" | "manual";
type AssigneeType = "any_agent" | "specific_agent" | "human";

type EditNodeData = {
  name: string;
  nodeType: NodeType;
  description: string;
  assigneeType: AssigneeType;
  assigneeId: string;
  timeout: number;
  readonlyDirs: string;
  fullControlDirs: string;
  artifact: string;
  maxRejectCycles: number;
};
type WFNode = Node<EditNodeData>;

const TYPE_TITLE: Record<NodeType, string> = {
  standard: "bg-primary/15 text-primary-fg",
  review: "bg-warning/15 text-warning-fg",
  manual: "bg-accent/15 text-accent-fg",
};

const NODE_DEFAULTS: Omit<EditNodeData, "name" | "nodeType"> = {
  description: "",
  assigneeType: "any_agent",
  assigneeId: "",
  timeout: 60,
  readonlyDirs: "",
  fullControlDirs: "",
  artifact: "",
  maxRejectCycles: 5,
};

// 服务端 jsonb 目录字段兼容:数组→逗号串,字符串原样
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function normalizeDirs(v: any): string {
  if (Array.isArray(v)) return v.join(",");
  if (typeof v === "string") return v;
  return "";
}

function WFNodeCard({ data, selected }: NodeProps<WFNode>) {
  const t = useTranslations("workflows.editor");
  const typeText =
    data.nodeType === "review" ? t("palette.review") : data.nodeType === "manual" ? t("palette.manual") : t("palette.standard");
  return (
    <div
      className={`bg-surface-2 border rounded-t-lg rounded-b-sm min-w-[180px] shadow-sm ${
        selected ? "border-primary" : "border-line-strong"
      }`}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ background: "var(--primary)", width: 9, height: 9, border: "1.5px solid var(--surface)" }}
      />
      <div className={`px-3 h-7 flex items-center rounded-t-lg border-b border-line ${TYPE_TITLE[data.nodeType]}`}>
        <span className="text-xs font-bold truncate">{data.name}</span>
      </div>
      <div className="px-3 py-2 space-y-0.5">
        <div className="text-[10px] text-fg-subtle">{typeText}</div>
        {data.description && (
          <div className="text-[10px] text-fg-faint truncate max-w-[150px]">{data.description}</div>
        )}
      </div>
      <Handle
        type="source"
        position={Position.Right}
        style={{ background: "var(--primary)", width: 9, height: 9, border: "1.5px solid var(--surface)" }}
      />
    </div>
  );
}

const nodeTypes = { wf: WFNodeCard };

function layoutNodes(nodes: WFNode[], edges: Edge[], dir: "LR" | "TB"): WFNode[] {
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: dir, nodesep: 50, ranksep: 90 });
  nodes.forEach((n) => g.setNode(n.id, { width: 180, height: 80 }));
  edges.forEach((e) => g.setEdge(e.source, e.target));
  Dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    return {
      ...n,
      position: p ? { x: p.x - 90, y: p.y - 40 } : n.position,
      targetPosition: dir === "LR" ? Position.Left : Position.Top,
      sourcePosition: dir === "LR" ? Position.Right : Position.Bottom,
    };
  });
}

function NodeSearchPalette({
  onPick,
  onClose,
}: {
  onPick: (t: NodeType) => void;
  onClose: () => void;
}) {
  const t = useTranslations("workflows.editor");
  const [q, setQ] = useState("");
  const presets: Array<{ type: NodeType; label: string }> = [
    { type: "standard", label: t("palette.standard") },
    { type: "review", label: t("palette.review") },
    { type: "manual", label: t("palette.manual") },
  ];
  const hits = presets.filter((p) => p.label.includes(q.trim()) || p.type.includes(q.trim().toLowerCase()));
  return (
    <div className="absolute inset-0 z-20 flex items-start justify-center pt-32" onClick={onClose}>
      <div
        className="w-[340px] bg-surface border border-line-strong rounded-lg shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 px-3 py-2.5 border-b border-line">
          <Search className="w-3.5 h-3.5 text-fg-faint" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") onClose();
              if (e.key === "Enter" && hits[0]) onPick(hits[0].type);
            }}
            placeholder={t("palette.placeholder")}
            autoFocus
            className="flex-1 bg-transparent text-sm text-fg placeholder-fg-faint outline-none"
          />
        </div>
        <div className="max-h-64 overflow-y-auto">
          {hits.map((h) => (
            <button
              key={h.type}
              onClick={() => onPick(h.type)}
              className="w-full flex items-center gap-3 px-3 py-2.5 hover:bg-surface-2 text-left"
            >
              <span className={`w-2.5 h-2.5 rounded-sm ${TYPE_TITLE[h.type]}`} />
              <span className="text-sm text-fg-muted">{h.label}</span>
            </button>
          ))}
          {hits.length === 0 && <div className="px-3 py-3 text-xs text-fg-faint">{t("palette.noHit")}</div>}
        </div>
      </div>
    </div>
  );
}

const inputCls =
  "w-full bg-bg border border-line-strong rounded-md px-2.5 py-1.5 text-xs text-fg focus:outline-none focus:border-primary";
const labelCls = "block text-[10px] font-semibold text-fg-faint uppercase tracking-wider mb-1";

function NodeInspector({
  node,
  onPatch,
  onDelete,
}: {
  node: WFNode | null;
  onPatch: (patch: Partial<EditNodeData>) => void;
  onDelete: () => void;
}) {
  const t = useTranslations("workflows.editor");
  const [confirmDelete, setConfirmDelete] = useState(false);
  useEffect(() => setConfirmDelete(false), [node?.id]);
  if (!node) {
    return (
      <div className="w-80 shrink-0 border-l border-line bg-surface/60 flex items-center justify-center p-6">
        <p className="text-xs text-fg-faint text-center">{t("inspectorEmpty")}</p>
      </div>
    );
  }
  const d = node.data;
  return (
    <div className="w-80 shrink-0 border-l border-line bg-surface/60 overflow-y-auto p-4 space-y-3.5">
      <div className="flex items-center justify-between">
        <span className="text-xs font-bold text-fg">{d.name || t("inspectorUntitled")}</span>
      </div>
      <div>
        <label className={labelCls}>{t("stepName")}</label>
        <input value={d.name} onChange={(e) => onPatch({ name: e.target.value })} className={inputCls} placeholder={t("stepNamePlaceholder")} />
      </div>
      <div>
        <label className={labelCls}>{t("nodeTypeLabel")}</label>
        <select value={d.nodeType} onChange={(e) => onPatch({ nodeType: e.target.value as NodeType })} className={inputCls}>
          <option value="standard">{t("optionStandard")}</option>
          <option value="review">{t("optionReview")}</option>
          <option value="manual">{t("optionManual")}</option>
        </select>
      </div>
      <div>
        <label className={labelCls}>{t("assigneeLabel")}</label>
        <select
          value={d.assigneeType}
          onChange={(e) => onPatch({ assigneeType: e.target.value as AssigneeType })}
          className={inputCls}
        >
          <option value="any_agent">{t("optionAnyAgent")}</option>
          <option value="specific_agent">{t("optionSpecificAgent")}</option>
          <option value="human">{t("optionHuman")}</option>
        </select>
      </div>
      {d.assigneeType !== "any_agent" && (
        <div>
          <label className={labelCls}>{t("assigneeIdLabel")}</label>
          <input
            value={d.assigneeId}
            onChange={(e) => onPatch({ assigneeId: e.target.value })}
            className={inputCls}
            placeholder={t("selectAgentPlaceholder")}
          />
        </div>
      )}
      <div>
        <label className={labelCls}>{t("stepDescLabel")}</label>
        <textarea
          value={d.description}
          onChange={(e) => onPatch({ description: e.target.value })}
          rows={3}
          className={`${inputCls} resize-y`}
          placeholder={t("stepDescPlaceholder")}
        />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className={labelCls}>{t("timeoutMinutesLabel")}</label>
          <input
            type="number"
            min={0}
            value={d.timeout}
            onChange={(e) => onPatch({ timeout: Math.max(0, Number(e.target.value) || 0) })}
            className={inputCls}
          />
        </div>
        <div>
          <label className={labelCls}>{t("maxRejectCyclesLabel")}</label>
          <input
            type="number"
            min={0}
            value={d.maxRejectCycles}
            onChange={(e) => onPatch({ maxRejectCycles: Math.max(0, Number(e.target.value) || 0) })}
            className={inputCls}
          />
        </div>
      </div>
      <div>
        <label className={labelCls}>{t("readonlyDirsLabel")}</label>
        <input
          value={d.readonlyDirs}
          onChange={(e) => onPatch({ readonlyDirs: e.target.value })}
          className={inputCls}
          placeholder={t("readonlyDirsPlaceholder")}
        />
        <p className="text-[9px] text-fg-faint mt-1">{t("readonlyDirsHint")}</p>
      </div>
      <div>
        <label className={labelCls}>{t("fullControlDirsLabel")}</label>
        <input
          value={d.fullControlDirs}
          onChange={(e) => onPatch({ fullControlDirs: e.target.value })}
          className={inputCls}
          placeholder={t("fullControlDirsPlaceholder")}
        />
        <p className="text-[9px] text-fg-faint mt-1">{t("fullControlDirsHint")}</p>
      </div>
      <div>
        <label className={labelCls}>{t("artifactLabel")}</label>
        <input value={d.artifact} onChange={(e) => onPatch({ artifact: e.target.value })} className={inputCls} />
      </div>
      <div className="pt-2 border-t border-line">
        {confirmDelete ? (
          <div className="flex items-center gap-2">
            <button onClick={onDelete} className="flex-1 px-2.5 py-1.5 text-xs bg-danger-strong hover:bg-danger text-fg rounded-md">
              {t("deleteConfirm")}
            </button>
            <button onClick={() => setConfirmDelete(false)} className="p-1.5 rounded-md text-fg-subtle hover:bg-surface-2">
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        ) : (
          <button
            onClick={() => setConfirmDelete(true)}
            className="w-full flex items-center justify-center gap-1.5 px-2.5 py-1.5 text-xs text-danger-fg bg-danger/10 hover:bg-danger/20 border border-danger/20 rounded-md"
          >
            <Trash2 className="w-3.5 h-3.5" /> {t("deleteNode")}
          </button>
        )}
      </div>
    </div>
  );
}

function EditorInner() {
  const t = useTranslations("workflows.editor");
  const router = useRouter();
  const searchParams = useSearchParams();
  const templateId = searchParams.get("templateId");
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const setTemplates = useAppStore((s) => s.setTemplates);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [nodes, setNodes, onNodesChange] = useNodesState<WFNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [rejected, setRejected] = useState<string | null>(null);
  const [showPalette, setShowPalette] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(!templateId);
  const seq = useRef(0);
  const { fitView, screenToFlowPosition } = useReactFlow();

  // 编辑模式加载:列表接口返回模板与原始节点(uuid/depends_on),供画布重建图
  const triggerRef = useRef<{
    trigger_type: string;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    trigger_config: any;
    trigger_enabled: boolean;
    next_run_at: string | null;
  }>({ trigger_type: "manual", trigger_config: {}, trigger_enabled: true, next_run_at: null });

  useEffect(() => {
    if (loaded || !templateId || !currentWorkspaceId) return;
    let cancelled = false;
    (async () => {
      try {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const list = (await api.listWorkflows(currentWorkspaceId)) as any[];
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const raw = list?.find((item: any) => (item.template?.id ?? item.id) === templateId);
        if (cancelled) return;
        if (!raw) throw new Error("template not found");
        const tpl = raw.template ?? raw;
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const rawNodes: any[] = (raw.nodes ?? tpl.nodes ?? []).slice().sort((a: any, b: any) => (a.sort_order ?? 0) - (b.sort_order ?? 0));
        setName(tpl.name || "");
        setDescription(tpl.description || "");
        triggerRef.current = {
          trigger_type: tpl.trigger_type ?? "manual",
          trigger_config: tpl.trigger_config ?? {},
          trigger_enabled: tpl.trigger_enabled ?? true,
          next_run_at: tpl.next_run_at ?? null,
        };
        const wfNodes: WFNode[] = rawNodes.map((n, i) => ({
          id: String(n.id),
          type: "wf" as const,
          position: { x: i * 240, y: 0 },
          data: {
            name: n.name || "",
            nodeType: (n.node_type || "standard") as NodeType,
            description: n.description || "",
            assigneeType: (n.assignee_type || "any_agent") as AssigneeType,
            assigneeId: n.assignee_id || "",
            timeout: n.timeout_minutes ?? 60,
            readonlyDirs: normalizeDirs(n.readonly_dirs),
            fullControlDirs: normalizeDirs(n.full_control_dirs),
            artifact: normalizeDirs(n.artifact),
            maxRejectCycles: n.max_reject_cycles ?? 5,
          },
        }));
        const idSet = new Set(wfNodes.map((n) => n.id));
        const wfEdges: Edge[] = wfNodes.flatMap((n, i) =>
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          ((rawNodes[i]?.depends_on ?? []) as any[])
            .filter((dep) => idSet.has(String(dep)))
            .map((dep, j) => ({ id: `e-${n.id}-${String(dep)}-${j}`, source: String(dep), target: n.id }))
        );
        setNodes(layoutNodes(wfNodes, wfEdges, "LR"));
        setEdges(wfEdges);
        setLoaded(true);
        setTimeout(() => fitView({ duration: 300 }), 80);
      } catch {
        if (!cancelled) {
          setSaveError(t("loadFailed"));
          setLoaded(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templateId, currentWorkspaceId, loaded]);

  const addNode = useCallback(
    (nodeType: NodeType) => {
      seq.current += 1;
      const center = screenToFlowPosition({ x: window.innerWidth / 2, y: window.innerHeight / 2 });
      const id = crypto.randomUUID();
      const label = nodeType === "review" ? t("palette.review") : nodeType === "manual" ? t("palette.manual") : t("palette.standard");
      setNodes((nds) => [
        ...nds,
        {
          id,
          type: "wf" as const,
          position: { x: center.x + ((seq.current % 4) - 1.5) * 60, y: center.y + ((seq.current % 3) - 1) * 70 },
          data: { ...NODE_DEFAULTS, name: `${label} ${seq.current}`, nodeType },
        },
      ]);
      setSelectedId(id);
      setShowPalette(false);
      setRejected(null);
    },
    [screenToFlowPosition, setNodes, t]
  );

  const wouldCreateCycle = useCallback(
    (source: string, target: string) => {
      const adj = new Map<string, string[]>();
      edges.forEach((e) => adj.set(e.source, [...(adj.get(e.source) || []), e.target]));
      const seen = new Set<string>();
      const stack = [target];
      while (stack.length) {
        const cur = stack.pop()!;
        if (cur === source) return true;
        if (seen.has(cur)) continue;
        seen.add(cur);
        stack.push(...(adj.get(cur) || []));
      }
      return false;
    },
    [edges]
  );

  const onConnect = useCallback(
    (c: Connection) => {
      setRejected(null);
      if (c.source === c.target) return setRejected(t("reject.self"));
      if (edges.some((e) => e.source === c.source && e.target === c.target)) return setRejected(t("reject.duplicate"));
      if (wouldCreateCycle(c.source, c.target)) return setRejected(t("reject.cycle"));
      setEdges((eds) => addEdge({ ...c, id: `e-${c.source}-${c.target}` }, eds));
    },
    [edges, setEdges, t, wouldCreateCycle]
  );

  const relayout = (dir: "LR" | "TB") => {
    setNodes((nds) => layoutNodes(nds, edges, dir));
    setTimeout(() => fitView({ duration: 300 }), 50);
  };

  const selectedNode = useMemo(() => nodes.find((n) => n.id === selectedId) ?? null, [nodes, selectedId]);

  const patchSelected = useCallback(
    (patch: Partial<EditNodeData>) => {
      if (!selectedId) return;
      setNodes((nds) => nds.map((n) => (n.id === selectedId ? { ...n, data: { ...n.data, ...patch } } : n)));
    },
    [selectedId, setNodes]
  );

  const deleteSelected = useCallback(() => {
    if (!selectedId) return;
    setNodes((nds) => nds.filter((n) => n.id !== selectedId));
    setEdges((eds) => eds.filter((e) => e.source !== selectedId && e.target !== selectedId));
    setSelectedId(null);
  }, [selectedId, setEdges, setNodes]);

  // 保存:Kahn 拓扑排序生成 sort_order(同层按当前数组序稳定),边反转成 depends_on
  const topoOrder = useCallback((ns: WFNode[], es: Edge[]): string[] | null => {
    const indeg = new Map<string, number>(ns.map((n) => [n.id, 0]));
    const adj = new Map<string, string[]>();
    es.forEach((e) => {
      indeg.set(e.target, (indeg.get(e.target) || 0) + 1);
      adj.set(e.source, [...(adj.get(e.source) || []), e.target]);
    });
    const order: string[] = [];
    // 稳定性:每轮取数组序最靠前的零入度节点
    const queue = ns.filter((n) => (indeg.get(n.id) || 0) === 0).map((n) => n.id);
    while (queue.length) {
      const cur = queue.shift()!;
      order.push(cur);
      (adj.get(cur) || []).forEach((next) => {
        const d = (indeg.get(next) || 0) - 1;
        indeg.set(next, d);
        if (d === 0) {
          const idx = ns.findIndex((n) => n.id === next);
          let inserted = false;
          for (let i = 0; i < queue.length; i++) {
            if (ns.findIndex((n) => n.id === queue[i]) > idx) {
              queue.splice(i, 0, next);
              inserted = true;
              break;
            }
          }
          if (!inserted) queue.push(next);
        }
      });
    }
    return order.length === ns.length ? order : null; // 有环(理论上已被连线拦截)
  }, []);

  const save = async () => {
    if (!currentWorkspaceId) return;
    setSaveError(null);
    if (!name.trim()) return setSaveError(t("needName"));
    if (nodes.length === 0) return setSaveError(t("needNode"));
    const order = topoOrder(nodes, edges);
    if (!order) return setSaveError(t("reject.cycle"));
    const sortOrder = new Map(order.map((id, i) => [id, i]));
    const apiNodes = nodes.map((n) => ({
      id: n.id,
      name: n.data.name,
      node_type: n.data.nodeType,
      assignee_type: n.data.assigneeType,
      assignee_id: n.data.assigneeType === "any_agent" ? null : n.data.assigneeId || null,
      description: n.data.description || "",
      sort_order: sortOrder.get(n.id) ?? 0,
      timeout_minutes: n.data.timeout,
      readonly_dirs: n.data.readonlyDirs || "",
      full_control_dirs: n.data.fullControlDirs || "",
      artifact: n.data.artifact || "",
      max_reject_cycles: n.data.maxRejectCycles,
      depends_on: edges.filter((e) => e.target === n.id).map((e) => e.source),
    }));
    const payload = {
      name: name.trim(),
      description: description.trim(),
      ...triggerRef.current,
      nodes: apiNodes,
    };
    setSaving(true);
    try {
      if (templateId) {
        await api.updateWorkflow(currentWorkspaceId, templateId, payload);
      } else {
        await api.createWorkflow(currentWorkspaceId, payload);
      }
      // 回列表前刷新 store,列表页即刻反映
      try {
        const list = await api.listWorkflows(currentWorkspaceId);
        if (Array.isArray(list)) setTemplates(list.map(mapTemplateFromApi));
      } catch {
        /* 列表刷新失败不阻塞返回 */
      }
      router.push("/workflows");
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="h-full flex flex-col">
      {/* 顶栏 */}
      <div className="flex items-center gap-3 px-4 py-2.5 border-b border-line bg-surface shrink-0">
        <button
          onClick={() => router.push("/workflows")}
          className="flex items-center gap-1.5 text-xs text-fg-subtle hover:text-fg px-2 py-1.5 rounded-md hover:bg-surface-2"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> {t("back")}
        </button>
        <div className="w-px h-5 bg-line" />
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("namePlaceholder")}
          className="w-56 bg-transparent text-sm font-semibold text-fg placeholder-fg-faint outline-none px-2 py-1 border-b border-transparent focus:border-primary"
        />
        <input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder={t("descPlaceholder")}
          className="flex-1 max-w-md bg-transparent text-xs text-fg-muted placeholder-fg-faint outline-none px-2 py-1 border-b border-transparent focus:border-primary"
        />
        <div className="flex-1" />
        {(rejected || saveError) && (
          <span className="text-xs text-danger-fg bg-danger/10 px-2 py-1 rounded-md">{saveError || rejected}</span>
        )}
        <button onClick={() => relayout("LR")} className="px-2.5 py-1.5 text-xs bg-surface-2 hover:bg-surface-3 text-fg-muted rounded-md border border-line-strong">
          {t("layoutLR")}
        </button>
        <button onClick={() => relayout("TB")} className="px-2.5 py-1.5 text-xs bg-surface-2 hover:bg-surface-3 text-fg-muted rounded-md border border-line-strong">
          {t("layoutTB")}
        </button>
        <button
          onClick={() => setShowPalette(true)}
          className="flex items-center px-3 py-1.5 text-xs bg-primary-strong hover:bg-primary text-fg rounded-md font-medium"
        >
          <Plus className="w-3.5 h-3.5 mr-1" /> {t("addNode")}
        </button>
        <button
          onClick={save}
          disabled={saving}
          className="flex items-center px-3 py-1.5 text-xs bg-success-strong hover:bg-success text-fg rounded-md font-medium disabled:opacity-50"
        >
          {saving ? t("saving") : t("save")}
        </button>
      </div>

      <div className="flex-1 flex min-h-0">
        <div
          className="flex-1 relative"
          onDoubleClick={(e) => {
            if ((e.target as HTMLElement).closest(".react-flow__node")) return;
            setShowPalette(true);
          }}
        >
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={(_, n) => setSelectedId(n.id)}
            onPaneClick={() => setSelectedId(null)}
            nodeTypes={nodeTypes}
            fitView
            className="bg-surface"
            proOptions={{ hideAttribution: true }}
          >
            <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="var(--line)" />
            <Panel position="top-left">
              <div className="flex items-center gap-2 bg-surface/90 border border-line-strong rounded-md px-2.5 py-1.5 text-[11px] text-fg-subtle">
                {templateId ? t("editing", { id: templateId.slice(0, 8) }) : t("creating")}
                <span className="text-fg-faint">·</span>
                {t("nodeCount", { count: nodes.length })}
                <span className="text-fg-faint">·</span>
                {t("edgeCount", { count: edges.length })}
              </div>
            </Panel>
          </ReactFlow>
          {showPalette && <NodeSearchPalette onPick={addNode} onClose={() => setShowPalette(false)} />}
        </div>
        <NodeInspector node={selectedNode} onPatch={patchSelected} onDelete={deleteSelected} />
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
