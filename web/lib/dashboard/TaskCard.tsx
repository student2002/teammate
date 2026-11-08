"use client";
// TaskCard：看板任务卡。第一行 ID+标题+状态，主体为紧凑协作链，底部只保留
// 真正存在的元信息。hover 只变化背景/边框；选中态 primary 描边 + 左侧光轨；
// 注意级别用左侧 2px 细线表达（danger=逾期，warning=需人工/失败）。

import { useTranslations } from "next-intl";
import { Bot, Calendar } from "lucide-react";
import type { MappedTask } from "@/lib/types";
import CollaborationChain from "@/lib/product/CollaborationChain";
import TaskStateBadge from "@/lib/product/TaskStateBadge";
import { UnresolvedRefMark } from "@/lib/workflow/NodeStatusHints";
import { attentionLevel, attentionRailCls, priorityTextCls } from "@/lib/product/presentation";

export default function TaskCard({
  task,
  selected,
  onSelect,
}: {
  task: MappedTask;
  selected: boolean;
  onSelect: (task: MappedTask) => void;
}) {
  const t = useTranslations("dashboard");
  const level = attentionLevel(task);
  const rail = level !== "none" ? attentionRailCls(level) : "";
  const totalNodes = task.nodeNames?.length || 0;
  const isOverdue = level === "danger";
  const labels = task.labels || [];

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => onSelect(task)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect(task);
        }
      }}
      className={`relative p-3.5 rounded-[var(--radius-card)] border cursor-pointer card-hover transition-colors-fast focus-visible:outline-none ${
        selected
          ? "border-primary/60 bg-primary/5"
          : "border-line-strong/50 bg-surface-2/30 hover:bg-surface-2/60 hover:border-line-strong"
      }`}
    >
      {(selected || level !== "none") && (
        <span
          className={`absolute left-0 top-3 bottom-3 w-0.5 rounded-r-full ${selected ? "bg-primary-fg" : rail}`}
        />
      )}

      {/* 第一行：ID / 类型标记 / 状态 */}
      <div className="flex items-center justify-between gap-2 mb-2">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="text-[10px] font-mono text-primary-fg shrink-0">{task.taskRef}</span>
          {task.type === "bug" && (
            <span className="text-[9px] text-danger-fg bg-danger-fg/10 px-1 rounded shrink-0">BUG</span>
          )}
          {(task.priority as string) === "urgent" && (
            <span className={`text-[9px] px-1 rounded shrink-0 ${priorityTextCls("urgent").replace("font-semibold", "")} bg-warning/10`}>
              {t("badge.urgent")}
            </span>
          )}
          {task.currentNodeType === "human.input" && (
            <span className="text-[9px] text-warning-fg bg-warning/10 px-1 rounded shrink-0">{t("badge.human")}</span>
          )}
        </div>
        <TaskStateBadge state={task.priorityState} className="scale-90 origin-right shrink-0" />
      </div>

      {/* 标题 */}
      <div className="mb-3 flex items-start gap-1.5">
        <h4 className="text-sm font-medium text-fg leading-snug line-clamp-2 min-w-0">{task.title}</h4>
        <UnresolvedRefMark title={task.title} />
      </div>

      {/* 协作链主体 */}
      <CollaborationChain
        density="compact"
        names={task.nodeNames || []}
        statuses={task.nodesStatus || []}
        agents={task.nodeAgents || []}
        currentIndex={task.currentNode}
        typeKeys={rawTypeKeys(task)}
      />

      {/* 底部元信息：只显示真正存在的项 */}
      <div className="mt-3 pt-2.5 border-t border-line/60 flex items-center gap-x-2.5 gap-y-1 flex-wrap text-[10px] text-fg-faint min-h-[18px]">
        <span className="flex items-center gap-1 text-fg-subtle min-w-0">
          <Bot className="w-3 h-3 shrink-0" />
          <span className="truncate max-w-[110px]">
            {task.nodeAgents?.[task.currentNode] || task.agent || t("taskCard.awaitingAgent")}
          </span>
        </span>
        {task.dueDate && (
          <span className={`flex items-center gap-1 ${isOverdue ? "text-danger-fg" : ""}`}>
            <Calendar className="w-3 h-3 shrink-0" />
            {task.dueDate}
          </span>
        )}
        {labels.slice(0, 2).map((l: string) => (
          <span key={l} className="px-1 bg-surface-3/60 rounded text-fg-subtle">
            {l}
          </span>
        ))}
        {labels.length > 2 && <span>+{labels.length - 2}</span>}
        {totalNodes === 0 && <span className="text-fg-faint">{t("chain.noNodes")}</span>}
      </div>

      {task.priorityState === "manual_intervention" && task.message && (
        <div className="mt-2 text-[11px] text-warning-fg bg-warning/10 p-2 rounded leading-snug line-clamp-2">
          {task.message}
        </div>
      )}
    </div>
  );
}

/** 从原始节点提取 type_key 数组（与 nodeNames 同序；无原始节点信息时不指定） */
function rawTypeKeys(task: MappedTask): string[] | undefined {
  const raw = (task._rawNodes || []) as Array<{ type_key?: string }>;
  if (raw.length === 0) return undefined;
  return raw.map((n) => n.type_key || "");
}
