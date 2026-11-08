"use client";
// 运行视图画布(§13.5):全图展示 trigger→action/control→end;
// control 节点显示 succeeded/skipped,等待节点显示等待事件与超时;
// 边按 activated/skipped/pending 着色;点击节点在详情面板查看 input/output/attempt/error。
import { Handle, Position, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useTranslations } from "next-intl";
import Dagre from "@dagrejs/dagre";
import { CircleCheck, CircleDashed, Clock, TriangleAlert, XCircle } from "lucide-react";
import { CATEGORY_TONE, HANDLE_BASE, KNOWN_PORT_KEYS, TONE_VAR, def, outputPorts, portBandHeight, portCenterY } from "./catalog";
import type { EdgeResolution, RunNodeStatus, RunNode, RunEdge, TemplateNode } from "./types";

const NODE_W = 240;
const NODE_H = 96;

// 运行节点没有 ui_config(模板快照不含画布位置)时用 dagre 自动布局;
// 模板节点(带 ui_config)原样返回位置
export function layoutNodes<T extends TemplateNode>(nodes: T[], edges: Array<{ source_node_id: string; target_node_id: string }>): T[] {
  const missing = nodes.some(
    (n) => !n.ui_config || typeof n.ui_config.x !== "number" || typeof n.ui_config.y !== "number"
  );
  if (!missing) return nodes;
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "LR", nodesep: 50, ranksep: 90 });
  nodes.forEach((n) => g.setNode(n.id, { width: NODE_W, height: NODE_H }));
  edges.forEach((e) => g.setEdge(e.source_node_id, e.target_node_id));
  Dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    return p ? { ...n, ui_config: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 } } : n;
  });
}

export interface RunNodeData extends Record<string, unknown> {
  node: RunNode;
}

export type RunRFNode = Node<RunNodeData, "v2run">;

const STATUS_VISUAL: Record<RunNodeStatus, { ring: string; cls: string }> = {
  waiting: { ring: "var(--warning)", cls: "border-[color:var(--warning)]" },
  ready: { ring: "var(--accent)", cls: "border-[color:var(--accent)]" },
  running: { ring: "var(--accent)", cls: "border-[color:var(--accent)]" },
  succeeded: { ring: "var(--success)", cls: "border-[color:var(--success)]" },
  failed: { ring: "var(--danger)", cls: "border-[color:var(--danger)]" },
  skipped: { ring: "var(--fg-faint)", cls: "border-dashed border-[color:var(--fg-faint)] opacity-60" },
  cancelled: { ring: "var(--fg-faint)", cls: "border-dashed border-[color:var(--fg-faint)] opacity-60" },
  manual_intervention: { ring: "var(--danger)", cls: "border-[color:var(--danger)]" },
};

const STATUS_ICON: Partial<Record<RunNodeStatus, typeof CircleCheck>> = {
  succeeded: CircleCheck,
  failed: XCircle,
  waiting: Clock,
  running: CircleDashed,
  ready: CircleDashed,
  manual_intervention: TriangleAlert,
};

const RESOLUTION_STYLE: Record<EdgeResolution, React.CSSProperties> = {
  activated: { stroke: "var(--success)", strokeWidth: 2 },
  skipped: { stroke: "var(--fg-faint)", strokeWidth: 1.5, opacity: 0.65, strokeDasharray: "5 4" },
  pending: { stroke: "var(--line-strong)", strokeWidth: 1.5, opacity: 0.9 },
};

export function RunNodeCard({ data, selected }: NodeProps<RunRFNode>) {
  const t = useTranslations("workflows.editor");
  const tv = useTranslations("workflows.view");
  const node = data.node;
  const d = def(node.type_key);
  const Icon = d.icon;
  const StatusIcon = STATUS_ICON[node.status];
  const visual = STATUS_VISUAL[node.status];
  const outPorts = outputPorts(node.type_key, node.config);
  const inPorts = d.input_ports;
  const portLabel = (key: string) => (KNOWN_PORT_KEYS.has(key) ? t(`ports.${key}`) : key);
  const fixedTitle = node.type_key.startsWith("trigger.") ? t("startTitle") : node.type_key === "flow.end" ? t("endTitle") : null;
  const bandHeight = portBandHeight(inPorts.length, outPorts.length);

  return (
    <div
      className={`relative bg-surface-2 border rounded-[var(--radius-control)] min-w-[220px] max-w-[260px] shadow-sm ${visual.cls} ${
        selected ? "ring-2 ring-[color:var(--primary)]" : ""
      }`}
    >
      <div
        className="px-3 h-7 flex items-center gap-1.5 rounded-t-[var(--radius-control)] border-b border-line"
        style={{ background: `color-mix(in srgb, ${TONE_VAR[CATEGORY_TONE[d.category]]} 12%, transparent)` }}
      >
        <Icon className="w-3.5 h-3.5 shrink-0" style={{ color: TONE_VAR[CATEGORY_TONE[d.category]] }} />
        <span className="text-xs font-bold truncate">{fixedTitle ?? node.name}</span>
        {StatusIcon && <StatusIcon className="w-3.5 h-3.5 ml-auto shrink-0" style={{ color: visual.ring }} />}
      </div>
      {bandHeight > 0 && (
        <div className="relative" style={{ height: bandHeight }}>
          {inPorts.map((p, i) => (
            <Handle key={p.key} id={p.key} type="target" position={Position.Left} style={{ ...HANDLE_BASE, top: portCenterY(i), background: TONE_VAR[p.tone] }} />
          ))}
          {outPorts.map((p, i) => (
            <Handle key={p.key} id={p.key} type="source" position={Position.Right} style={{ ...HANDLE_BASE, top: portCenterY(i), background: TONE_VAR[p.tone] }} />
          ))}
          {inPorts.map((p, i) => (
            <span
              key={`lb-${p.key}`}
              className="absolute left-2.5 -translate-y-1/2 text-[9px] text-fg-faint pointer-events-none whitespace-nowrap max-w-[120px] truncate"
              style={{ top: portCenterY(i) }}
            >
              {portLabel(p.key)}
            </span>
          ))}
          {outPorts.map((p, i) => (
            <span
              key={`lb-${p.key}`}
              className="absolute right-2.5 -translate-y-1/2 text-[9px] text-fg-faint pointer-events-none whitespace-nowrap max-w-[120px] truncate"
              style={{ top: portCenterY(i) }}
            >
              {portLabel(p.key)}
            </span>
          ))}
        </div>
      )}
      <div className="px-3 py-2 space-y-0.5 min-h-[46px]">
        <div className="text-[10px] text-fg-subtle">{tv(`run.status.${node.status}`)}</div>
        {node.status === "waiting" && node.wait && (
          <div className="text-[10px] text-fg-faint truncate max-w-[170px]">
            {tv("run.waitingFor", { event: tv(`run.waitKind.${node.wait.kind}`) })}
          </div>
        )}
        {node.attempts.length > 1 && (
          <div className="text-[10px] text-fg-faint">{tv("run.attemptCount", { count: node.attempts.length })}</div>
        )}
      </div>
    </div>
  );
}

export const runNodeTypes = { v2run: RunNodeCard };

export function runEdges(edges: RunEdge[]): Edge[] {
  return edges.map((e) => ({
    id: e.id,
    source: e.source_node_id,
    sourceHandle: e.source_port,
    target: e.target_node_id,
    targetHandle: e.target_port,
    style: RESOLUTION_STYLE[e.resolution],
  }));
}
