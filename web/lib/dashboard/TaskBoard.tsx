"use client";
// TaskBoard：协作看板。列头用状态色小点 + 中性标题，不用整块色底；
// 列内为 TaskCard。列为空时显示轻量提示。
// 列语义与后端看板 API 一致:pending / in_progress / succeeded / failed / manual_intervention。

import type { ReactNode } from "react";
import { useTranslations } from "next-intl";
import { Activity, CheckCircle2, Clock, ShieldAlert, XCircle } from "lucide-react";
import type { MappedTask } from "@/lib/types";
import TaskCard from "./TaskCard";

export type BoardColumnType = "pending" | "progress" | "succeeded" | "failed" | "manual";

const columnIcon: Record<BoardColumnType, ReactNode> = {
  manual: <ShieldAlert className="w-3.5 h-3.5" />,
  failed: <XCircle className="w-3.5 h-3.5" />,
  progress: <Activity className="w-3.5 h-3.5" />,
  pending: <Clock className="w-3.5 h-3.5" />,
  succeeded: <CheckCircle2 className="w-3.5 h-3.5" />,
};

const columnDot: Record<BoardColumnType, string> = {
  manual: "bg-warning-fg",
  failed: "bg-danger-fg",
  progress: "bg-accent-fg",
  pending: "bg-fg-faint",
  succeeded: "bg-success-fg",
};

export interface BoardColumn {
  key: string;
  label: string;
  type: BoardColumnType;
  tasks: MappedTask[];
}

export default function TaskBoard({
  columns,
  onSelect,
  selectedId,
}: {
  columns: BoardColumn[];
  onSelect: (task: MappedTask) => void;
  selectedId?: number;
}) {
  const t = useTranslations("dashboard");
  return (
    <div className="flex gap-4 min-w-max h-full pb-4">
      {columns.map((col) => (
        <div
          key={col.key}
          className="w-80 flex flex-col rounded-[var(--radius-card)] border border-line bg-surface-2/30 shrink-0"
        >
          <div className="px-4 py-3 border-b border-line flex items-center justify-between">
            <div className="flex items-center gap-2 min-w-0">
              <span className={`w-2 h-2 rounded-full shrink-0 ${columnDot[col.type]}`} />
              <h3 className="text-sm font-semibold text-fg-muted truncate">{col.label}</h3>
            </div>
            <span className="text-xs px-1.5 py-0.5 rounded bg-surface-3/70 text-fg-subtle font-mono shrink-0">
              {col.tasks.length}
            </span>
          </div>
          <div className="p-3 flex-1 overflow-y-auto space-y-3 min-h-[200px]">
            {col.tasks.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-24 text-fg-faint text-xs gap-2">
                {columnIcon[col.type]}
                {t("taskColumn.empty")}
              </div>
            ) : (
              col.tasks.map((task) => (
                <TaskCard key={task.id} task={task} selected={selectedId === task.id} onSelect={onSelect} />
              ))
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
