"use client";
// TaskList：表格视图。表头固定、行高一致；「当前节点+执行者+进度」合并为
// 一列紧凑协作链；执行权在代理/服务端时显示锁定标识，无人工动作列控件。

import { useState } from "react";
import { useTranslations } from "next-intl";
import { Calendar, Lock } from "lucide-react";
import type { MappedTask } from "@/lib/types";
import CollaborationChain from "@/lib/product/CollaborationChain";
import TaskStateBadge from "@/lib/product/TaskStateBadge";
import { attentionLevel, priorityTextCls } from "@/lib/product/presentation";

export default function TaskList({
  tasks,
  onSelect,
  selectedId,
}: {
  tasks: MappedTask[];
  onSelect: (task: MappedTask) => void;
  selectedId?: number;
}) {
  const t = useTranslations("dashboard");
  const [sortBy, setSortBy] = useState<string>("created");
  const [sortDir, setSortDir] = useState<string>("desc");
  const [filterType, setFilterType] = useState<string>("all");
  const [filterStatus, setFilterStatus] = useState<string>("all");

  const filtered = tasks.filter((task) => {
    if (filterType !== "all" && task.type !== filterType) return false;
    if (filterStatus !== "all" && task.priorityState !== filterStatus) return false;
    return true;
  });

  const sorted = [...filtered].sort((a, b) => {
    if (sortBy === "priority") {
      const p: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3 };
      return sortDir === "asc"
        ? (p[a.priority] ?? 99) - (p[b.priority] ?? 99)
        : (p[b.priority] ?? 99) - (p[a.priority] ?? 99);
    }
    return sortDir === "asc" ? a.id - b.id : b.id - a.id;
  });

  return (
    <div className="flex-1 flex flex-col p-6 pt-4 overflow-hidden">
      {/* 筛选栏 */}
      <div className="flex items-center gap-3 mb-3 shrink-0">
        <select
          value={filterType}
          onChange={(e) => setFilterType(e.target.value)}
          className="h-8 bg-surface-2 border border-line-strong rounded-[var(--radius-control)] px-2.5 text-xs text-fg-muted focus:outline-none cursor-pointer"
        >
          <option value="all">{t("filters.allTypes")}</option>
          <option value="story">Story</option>
          <option value="bug">Bug</option>
          <option value="task">Task</option>
        </select>
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          className="h-8 bg-surface-2 border border-line-strong rounded-[var(--radius-control)] px-2.5 text-xs text-fg-muted focus:outline-none cursor-pointer"
        >
          <option value="all">{t("filters.allStatuses")}</option>
          <option value="manual_intervention">{t("filters.manualIntervention")}</option>
          <option value="failed">{t("filters.failed")}</option>
          <option value="running">{t("filters.running")}</option>
          <option value="ready">{t("filters.ready")}</option>
          <option value="waiting">{t("filters.waiting")}</option>
          <option value="succeeded">{t("filters.succeeded")}</option>
          <option value="skipped">{t("filters.skipped")}</option>
          <option value="cancelled">{t("filters.cancelled")}</option>
        </select>
        <div className="flex-1" />
        <span className="text-xs text-fg-faint">{t("filters.taskCount", { count: sorted.length })}</span>
      </div>

      {/* 表格 */}
      <div className="flex-1 overflow-y-auto rounded-[var(--radius-card)] border border-line">
        <table className="w-full text-sm">
          <thead className="bg-surface-2/80 sticky top-0 z-10">
            <tr className="text-xs text-fg-subtle">
              <th className="text-left px-4 py-2.5 font-medium">ID</th>
              <th className="text-left px-4 py-2.5 font-medium">{t("table.header.title")}</th>
              <th className="text-left px-4 py-2.5 font-medium">{t("table.header.type")}</th>
              <th className="text-left px-4 py-2.5 font-medium">
                <button
                  onClick={() => {
                    setSortBy("priority");
                    setSortDir((d) => (d === "asc" ? "desc" : "asc"));
                  }}
                  className="flex items-center gap-1 hover:text-fg focus-visible:outline-none"
                >
                  {t("table.header.priority")} {sortBy === "priority" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                </button>
              </th>
              <th className="text-left px-4 py-2.5 font-medium">{t("table.header.status")}</th>
              <th className="text-left px-4 py-2.5 font-medium">{t("table.header.chain")}</th>
              <th className="text-left px-4 py-2.5 font-medium">{t("table.header.executor")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {sorted.map((task) => {
              const isOverdue = attentionLevel(task) === "danger";
              const chainNames = task.nodeNames || [];
              const executor = task.nodeAgents?.[task.currentNode] || task.agent;
              return (
                <tr
                  key={task.id}
                  tabIndex={0}
                  onClick={() => onSelect(task)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") onSelect(task);
                  }}
                  className={`cursor-pointer transition-colors-fast focus-visible:outline-none ${
                    selectedId === task.id ? "bg-primary/5" : "hover:bg-surface-2/50"
                  }`}
                >
                  <td className="px-4 py-3 font-mono text-primary-fg text-xs">{task.taskRef}</td>
                  <td className="px-4 py-3 text-fg font-medium max-w-[220px]">
                    <div className="truncate">{task.title}</div>
                    {task.labels && task.labels.length > 0 && (
                      <div className="flex gap-1 mt-1">
                        {task.labels.slice(0, 3).map((l: string) => (
                          <span key={l} className="text-[9px] px-1 py-0.5 bg-surface-3/60 text-fg-subtle rounded">
                            {l}
                          </span>
                        ))}
                        {task.labels.length > 3 && (
                          <span className="text-[9px] text-fg-faint">+{task.labels.length - 3}</span>
                        )}
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <span
                      className={`text-[10px] px-1.5 py-0.5 rounded ${
                        task.type === "bug"
                          ? "bg-danger-fg/10 text-danger-fg"
                          : task.type === "story"
                            ? "bg-primary-fg/10 text-primary-fg"
                            : "bg-fg-subtle/10 text-fg-subtle"
                      }`}
                    >
                      {task.type || "task"}
                    </span>
                  </td>
                  <td className={`px-4 py-3 text-xs ${priorityTextCls(task.priority)}`}>
                    {task.priority || "medium"}
                  </td>
                  <td className="px-4 py-3">
                    <TaskStateBadge state={task.priorityState} />
                  </td>
                  <td className="px-4 py-3 min-w-[280px]">
                    {chainNames.length > 0 ? (
                      <CollaborationChain
                        density="compact"
                        names={chainNames}
                        statuses={task.nodesStatus || []}
                        agents={task.nodeAgents || []}
                        currentIndex={task.currentNode}
                        typeKeys={rawTypeKeys(task)}
                      />
                    ) : (
                      <span className="text-xs text-fg-faint">-</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      {executor ? (
                        <span className="text-[10px] text-accent-fg flex items-center">
                          <Lock className="w-3 h-3 mr-0.5" />
                          {executor}
                        </span>
                      ) : (
                        <span className="text-[10px] text-fg-faint">-</span>
                      )}
                      {task.dueDate && (
                        <span className={`text-[10px] ${isOverdue ? "text-danger-fg" : "text-fg-faint"}`}>
                          <Calendar className="w-3 h-3 inline mr-0.5" />
                          {task.dueDate}
                        </span>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/** 从原始节点提取 type_key 数组（与 nodeNames 同序；无原始节点信息时不指定） */
function rawTypeKeys(task: MappedTask): string[] | undefined {
  const raw = (task._rawNodes || []) as Array<{ type_key?: string }>;
  if (raw.length === 0) return undefined;
  return raw.map((n) => n.type_key || "");
}
