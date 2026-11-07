"use client";
// 工作流画布(双形态):任务监控进度条(variant=run)与模板只读浏览(variant=template)共用一套渲染。
// 连线优先取节点 depends_on;线性(无 depends_on)按节点顺序连成链。
// run 形态:五态着色/当前节点高亮/点击联动日志终端;template 形态:无状态色,节点卡显示指派+超时。

import { useMemo } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Handle,
  Position,
  Background,
  BackgroundVariant,
  type Node,
  type Edge,
  type NodeProps,
} from "@xyflow/react";
import { useTranslations } from "next-intl";
import Dagre from "@dagrejs/dagre";
import "@xyflow/react/dist/style.css";

// 任务详情响应里的节点(仅取画布所需字段的窄化类型;数组顺序即进度顺序)
export interface RunFlowNode {
  id: string;
  name: string;
  node_type: string;
  status: string;
  depends_on?: string[];
  /** template 形态展示用:指派规则短文案(如"任意空闲代理") */
  assigneeShort?: string;
  /** template 形态展示用:超时分钟数 */
  timeoutMinutes?: number;
}

type FlowNodeData = {
  name: string;
  nodeType: string;
  status: string;
  statusText: string;
  /** 在节点数组中的下标(与 nodesStatus/currentNode 对齐) */
  index: number;
  isCurrent: boolean;
  isActive: boolean;
  variant: "run" | "template";
  assigneeShort?: string;
  timeoutMinutes?: number;
  onNodeClick?: (index: number) => void;
};
type FlowNode = Node<FlowNodeData>;

const TYPE_TITLE: Record<string, string> = {
  standard: "bg-primary/15 text-primary-fg",
  review: "bg-warning/15 text-warning-fg",
  manual: "bg-accent/15 text-accent-fg",
};
const STATUS_BORDER: Record<string, string> = {
  pending: "border-line-strong",
  in_progress: "border-primary",
  completed: "border-success/50",
  rejected: "border-danger/60",
  manual_intervention: "border-warning/60",
};

function statusTextOf(t: ReturnType<typeof useTranslations>, status: string): string {
  if (status === "completed") return t("done");
  if (status === "in_progress") return t("executing");
  if (status === "rejected") return t("rejected");
  if (status === "manual_intervention") return t("manualIntervention");
  return t("waiting");
}

function RunNodeCard({ data }: NodeProps<FlowNode>) {
  const t = useTranslations("dashboard.runCanvas");
  const isTemplate = data.variant === "template";
  const clickable = !isTemplate && data.status !== "pending";
  const isDone = data.status === "completed";
  if (isTemplate) {
    return (
      <div className="bg-surface-2 border border-line-strong rounded-t-lg rounded-b-sm min-w-[180px] shadow-sm">
        <Handle
          type="target"
          position={Position.Left}
          style={{ background: "var(--primary)", width: 9, height: 9, border: "1.5px solid var(--surface)" }}
        />
        <div
          className={`px-3 h-7 flex items-center rounded-t-lg border-b border-line ${
            TYPE_TITLE[data.nodeType] || TYPE_TITLE.standard
          }`}
        >
          <span className="text-xs font-bold truncate">{data.name}</span>
        </div>
        <div className="px-3 py-2 text-[11px] text-fg-subtle flex items-center gap-2">
          {data.assigneeShort && <span>{data.assigneeShort}</span>}
          {data.timeoutMinutes != null && data.timeoutMinutes > 0 && (
            <span className="text-fg-faint">· {data.timeoutMinutes}min</span>
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
  return (
    <div
      onClick={clickable ? () => data.onNodeClick?.(data.index) : undefined}
      className={[
        "bg-surface-2 border rounded-t-lg rounded-b-sm min-w-[170px] shadow-sm",
        STATUS_BORDER[data.status] || STATUS_BORDER.pending,
        data.isActive ? "ring-1 ring-fg shadow-lg" : "",
        clickable ? "cursor-pointer" : "",
        isDone ? "opacity-80" : "",
      ].join(" ")}
      style={data.isCurrent ? { boxShadow: "0 0 14px var(--primary)" } : undefined}
    >
      <Handle
        type="target"
        position={Position.Left}
        style={{ background: "var(--primary)", width: 9, height: 9, border: "1.5px solid var(--surface)" }}
      />
      <div
        className={`px-3 h-7 flex items-center justify-between rounded-t-lg border-b border-line ${
          TYPE_TITLE[data.nodeType] || TYPE_TITLE.standard
        }`}
      >
        <span className="text-xs font-bold truncate">{data.name}</span>
        <span className="flex items-center gap-1 ml-2 shrink-0">
          {data.isCurrent && !isDone && (
            <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-primary text-fg font-bold">
              {t("current")}
            </span>
          )}
          <span className="text-[9px] opacity-70">{data.statusText}</span>
        </span>
      </div>
      <div className="px-3 py-2 text-[11px] text-fg-subtle flex items-center gap-2">
        {isDone && <span className="text-success-fg">✓</span>}
        {data.status === "rejected" && <span className="text-danger-fg">✕</span>}
        {data.status === "in_progress" && (
          <span className="w-1.5 h-1.5 rounded-full bg-primary animate-pulse" />
        )}
        {data.status === "manual_intervention" && (
          <span className="w-1.5 h-1.5 rounded-full bg-warning-fg animate-pulse" />
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

const nodeTypes = { run: RunNodeCard };

function RunFlowInner({
  nodes: rawNodes,
  currentIndex,
  activeIndex,
  onNodeClick,
  variant = "run",
}: {
  nodes: RunFlowNode[];
  currentIndex?: number;
  activeIndex?: number;
  onNodeClick?: (index: number) => void;
  variant?: "run" | "template";
}) {
  const t = useTranslations("dashboard.runCanvas");

  // 连线:任一节点声明了 depends_on 用依赖图;否则按数组顺序连成线性链
  const edgesRaw = useMemo(() => {
    const hasDeps = rawNodes.some((n) => (n.depends_on || []).length > 0);
    if (hasDeps) {
      return rawNodes.flatMap((n) =>
        (n.depends_on || []).map((dep) => [dep, n.id] as [string, string])
      );
    }
    return rawNodes
      .slice(0, -1)
      .map((n, i) => [n.id, rawNodes[i + 1]!.id] as [string, string]);
  }, [rawNodes]);

  const statusById = useMemo(
    () => Object.fromEntries(rawNodes.map((n) => [n.id, n.status])),
    [rawNodes]
  );

  const nodes = useMemo<FlowNode[]>(() => {
    const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
    g.setGraph({ rankdir: "LR", nodesep: 50, ranksep: 90 });
    rawNodes.forEach((n) => g.setNode(n.id, { width: 170, height: 76 }));
    edgesRaw.forEach(([s, tg]) => g.setEdge(s, tg));
    Dagre.layout(g);
    return rawNodes.map((n, index) => {
      const p = g.node(n.id);
      return {
        id: n.id,
        type: "run" as const,
        position: p ? { x: p.x - 85, y: p.y - 38 } : { x: 0, y: 0 },
        data: {
          name: n.name,
          nodeType: n.node_type,
          status: n.status,
          statusText: statusTextOf(t, n.status),
          index,
          isCurrent: index === currentIndex && n.status !== "completed",
          isActive: index === activeIndex,
          variant,
          assigneeShort: n.assigneeShort,
          timeoutMinutes: n.timeoutMinutes,
          onNodeClick,
        },
      };
    });
  }, [rawNodes, edgesRaw, t, currentIndex, activeIndex, onNodeClick]);

  const edges = useMemo<Edge[]>(
    () =>
      edgesRaw.map(([s, tg], i) => ({
        id: `e${i}`,
        source: s,
        target: tg,
        style: {
          stroke:
            variant === "template"
              ? "var(--line-strong)"
              : statusById[s] === "completed"
                ? "var(--success)"
                : "var(--line-strong)",
          strokeWidth: 2,
        },
      })),
    [edgesRaw, statusById]
  );

  return (
    <div className="h-64 w-full bg-surface border border-line-strong/60 rounded-lg overflow-hidden">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        fitView
        onNodesChange={() => {}}
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="var(--line)" />
      </ReactFlow>
    </div>
  );
}

export default function RunFlowCanvas({
  nodes,
  currentIndex,
  activeIndex,
  onNodeClick,
  variant,
}: {
  nodes: RunFlowNode[];
  currentIndex?: number;
  activeIndex?: number;
  onNodeClick?: (index: number) => void;
  variant?: "run" | "template";
}) {
  if (!nodes || nodes.length === 0) return null;
  return (
    <ReactFlowProvider>
      <RunFlowInner
        nodes={nodes}
        currentIndex={currentIndex}
        activeIndex={activeIndex}
        onNodeClick={onNodeClick}
        variant={variant}
      />
    </ReactFlowProvider>
  );
}
