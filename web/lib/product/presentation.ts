// presentation.ts：状态到视觉的纯函数映射。入口数据已在页面边界整理，
// 这里不做数据修复；组件据此渲染，主题切换时颜色全部来自语义 utility。

import type { Priority, TaskNodeStatus } from "@/lib/types";

export type BadgeTone = "neutral" | "primary" | "accent" | "success" | "warning" | "danger";

/** 任务级展示状态 → 徽章语义色。需人工=warning（等待人工判断），失败=danger，执行中 accent */
export function taskStateTone(state: string): BadgeTone {
  switch (state) {
    case "failed":
      return "danger";
    case "manual_intervention":
      return "warning";
    case "running":
    case "ready":
      return "accent";
    case "succeeded":
      return "success";
    default:
      return "neutral";
  }
}

/** 任务级展示状态 → dashboard namespace 下的 status.* 文案 key 后缀 */
export function taskStateLabelKey(state: string): string {
  switch (state) {
    case "manual_intervention":
      return "manualIntervention";
    case "waiting":
      return "waiting";
    case "ready":
      return "ready";
    case "running":
      return "running";
    case "succeeded":
      return "succeeded";
    case "failed":
      return "failed";
    case "skipped":
      return "skipped";
    case "cancelled":
      return "cancelled";
    default:
      return state;
  }
}

export interface NodeVisual {
  /** 圆点=执行/控制节点；菱形=human.input 人工节点 */
  shape: "dot" | "diamond";
  /** 节点图形的背景/边框类（语义 utility） */
  className: string;
}

/** 节点状态 → 协作链节点视觉。当前执行节点带 accent 外环与低幅脉冲 */
export function nodeVisual(status: TaskNodeStatus | undefined, isCurrent: boolean, typeKey: string | undefined): NodeVisual {
  const shape: NodeVisual["shape"] = typeKey === "human.input" ? "diamond" : "dot";
  let className: string;
  if (status === "succeeded") {
    className = "bg-success-fg border-success-fg";
  } else if (status === "running" || (isCurrent && status !== "manual_intervention")) {
    className = "bg-accent-fg border-accent-fg ring-4 ring-accent/20 animate-pulse-soft";
  } else if (status === "failed") {
    className = "bg-danger-fg border-danger-fg";
  } else if (status === "manual_intervention") {
    className = "bg-warning-fg border-warning-fg";
  } else {
    className = "bg-surface border-line-strong";
  }
  return { shape, className };
}

/** 优先级 → 文字色。紧急用 warning 文字（紧急≠系统失败），不占用整块语义色 */
export function priorityTextCls(priority: Priority | string | undefined): string {
  switch (priority) {
    case "urgent":
      return "text-warning-fg font-semibold";
    case "high":
      return "text-warning-fg";
    case "medium":
      return "text-fg-muted";
    default:
      return "text-fg-subtle";
  }
}

export interface CompactChain {
  /** 保留展示的节点下标（升序） */
  indices: number[];
  /** 被省略的节点数量 */
  omitted: number;
}

/**
 * 紧凑协作链节点选择：≤max 全展示；超过时保留首节点、当前节点、末节点，
 * 其余计入 +N。当前节点与首/末重合时自动去重。
 */
export function compactChainIndices(count: number, currentIndex: number, max = 5): CompactChain {
  if (count <= 0) return { indices: [], omitted: 0 };
  if (count <= max) return { indices: Array.from({ length: count }, (_, i) => i), omitted: 0 };
  const cur = Math.min(Math.max(currentIndex, 0), count - 1);
  const indices = [...new Set([0, cur, count - 1])].sort((a, b) => a - b);
  return { indices, omitted: count - indices.length };
}

/** 任务注意级别：逾期 > 需人工/失败 > 无。用于任务卡左轨与注意队列排序 */
export function attentionLevel(
  task: { priorityState: string; dueDate: string }
): "danger" | "warning" | "none" {
  const open = task.priorityState !== "succeeded" && task.priorityState !== "cancelled";
  if (open && task.dueDate && new Date(task.dueDate) < new Date()) return "danger";
  if (task.priorityState === "manual_intervention" || task.priorityState === "failed") return "warning";
  return "none";
}

/** 注意级别 → 左轨细线颜色（2px），无注意时不渲染 */
export function attentionRailCls(level: "danger" | "warning" | "none"): string {
  switch (level) {
    case "danger":
      return "bg-danger-fg";
    case "warning":
      return "bg-warning-fg";
    default:
      return "";
  }
}

/** Agent 状态 → 徽章语义色。online=success，busy=accent，paused/pending=warning，其余离线中性 */
export function agentStatusTone(status: string): BadgeTone {
  switch (status) {
    case "online":
      return "success";
    case "busy":
      return "accent";
    case "paused":
    case "pending":
      return "warning";
    default:
      return "neutral";
  }
}

/** 守护进程检测到的编码工具 → 展示名（未知 provider 原样返回） */
const PROVIDER_LABELS: Record<string, string> = {
  claude: "Claude Code",
  openclaw: "OpenClaw",
  opencode: "OpenCode",
  atomcode: "AtomCode",
  mimocode: "MiMoCode",
};

export function providerLabel(provider: string): string {
  return PROVIDER_LABELS[provider] || provider;
}

/** BadgeTone → 状态点背景色（紧凑任务轨等小空间用点表达状态） */
export function toneDotCls(tone: BadgeTone): string {
  switch (tone) {
    case "accent":
      return "bg-accent-fg";
    case "success":
      return "bg-success-fg";
    case "warning":
      return "bg-warning-fg";
    case "danger":
      return "bg-danger-fg";
    case "primary":
      return "bg-primary-fg";
    default:
      return "bg-fg-faint";
  }
}
