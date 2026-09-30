"use client";
// 历史任务页：分页展示已完成/已取消任务。

import { useEffect, useCallback, useState } from "react";
import { useTranslations, useLocale } from "next-intl";
import { useAppStore } from "@/lib/store";
import EmptyStateGuide from "@/lib/EmptyStateGuide";
import type { MappedTask, TaskType, Priority } from "@/lib/types";
import {
  Search,
  ChevronLeft,
  ChevronRight,
  FileCheck,
  Loader2,
} from "lucide-react";

const PAGE_SIZE = 20;

const typeCls: Record<TaskType, string> = {
  story: "bg-primary/20 text-primary-fg",
  bug: "bg-danger/20 text-danger-fg",
  task: "bg-green-500/20 text-green-400",
};

const typeKeys: Record<TaskType, string> = {
  story: "typeStory",
  bug: "typeBug",
  task: "typeTask",
};

const priorityCls: Record<Priority, string> = {
  urgent: "bg-danger/20 text-danger-fg",
  high: "bg-orange-500/20 text-orange-400",
  medium: "bg-yellow-500/20 text-yellow-400",
  low: "bg-gray-500/20 text-gray-400",
};

export default function HistoryPage() {
  const t = useTranslations("history");
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const projects = useAppStore((s) => s.projects);
  const historyTasks = useAppStore((s) => s.historyTasks);
  const historyTotal = useAppStore((s) => s.historyTotal);
  const historyPage = useAppStore((s) => s.historyPage);
  const historySearchQuery = useAppStore((s) => s.historySearchQuery);
  const historyLoading = useAppStore((s) => s.historyLoading);
  const loadHistoryTasks = useAppStore((s) => s.loadHistoryTasks);
  const setHistoryPage = useAppStore((s) => s.setHistoryPage);
  const setHistorySearchQuery = useAppStore((s) => s.setHistorySearchQuery);

  const [searchInput, setSearchInput] = useState(historySearchQuery);
  const [selectedProjectId, setSelectedProjectId] = useState(currentProjectId);

  const totalPages = Math.max(1, Math.ceil(historyTotal / PAGE_SIZE));

  const doSearch = useCallback(
    (page?: number) => {
      if (!selectedProjectId) return;
      loadHistoryTasks(selectedProjectId, page, historySearchQuery);
    },
    [selectedProjectId, loadHistoryTasks, historySearchQuery]
  );

  // 初始加载 & 项目切换时重新加载
  useEffect(() => {
    if (selectedProjectId) {
      setHistoryPage(1);
      doSearch(1);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedProjectId]);

  // 同步当前项目
  useEffect(() => {
    if (currentProjectId && currentProjectId !== selectedProjectId) {
      setSelectedProjectId(currentProjectId);
    }
  }, [currentProjectId, selectedProjectId]);

  const handleSearch = () => {
    setHistorySearchQuery(searchInput);
    setHistoryPage(1);
    if (selectedProjectId) {
      loadHistoryTasks(selectedProjectId, 1, searchInput);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleSearch();
  };

  const goToPage = (page: number) => {
    if (page < 1 || page > totalPages) return;
    setHistoryPage(page);
    doSearch(page);
  };

  const projectName =
    projects.find((p) => p.id === selectedProjectId)?.name || "";

  return (
    <div className="flex flex-col h-full">
      {/* 顶部标题栏 */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-line/80">
        <div className="flex items-center gap-3">
          <FileCheck className="w-5 h-5 text-fg-subtle" />
          <h1 className="text-lg font-semibold text-fg">{t("title")}</h1>
          {projectName && (
            <span className="text-sm text-fg-subtle">— {projectName}</span>
          )}
          {historyTotal > 0 && (
            <span className="text-xs text-fg-faint">{t("total", { count: historyTotal })}</span>
          )}
        </div>

        <div className="flex items-center gap-3">
          {/* 项目选择器 */}
          {projects.length > 1 && (
            <select
              value={selectedProjectId || ""}
              onChange={(e) => setSelectedProjectId(e.target.value)}
              className="h-8 px-2 text-sm rounded-md border border-line-strong bg-surface-2 text-fg-muted"
            >
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          )}

          {/* 搜索框 */}
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-fg-faint" />
            <input
              type="text"
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t("searchPlaceholder")}
              className="h-8 pl-8 pr-3 text-sm rounded-md border border-line-strong bg-surface-2 text-fg-muted placeholder:text-fg-faint w-56 focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
        </div>
      </div>

      {/* 任务列表 */}
      <div className="flex-1 overflow-y-auto">
        {historyLoading && historyTasks.length === 0 ? (
          <div className="flex items-center justify-center h-64">
            <Loader2 className="w-6 h-6 animate-spin text-fg-faint" />
          </div>
        ) : historyTasks.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-fg-faint">
            <FileCheck className="w-12 h-12 mb-3 opacity-30" />
            <p className="text-sm">{t("empty")}</p>
            <div className="mt-8 w-full max-w-2xl text-left">
              <EmptyStateGuide page="history" />
            </div>
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-line text-fg-subtle">
                <th className="text-left px-6 py-3 font-medium w-20">{t("thRef")}</th>
                <th className="text-left px-4 py-3 font-medium">{t("thTitle")}</th>
                <th className="text-left px-4 py-3 font-medium w-20">{t("thType")}</th>
                <th className="text-left px-4 py-3 font-medium w-20">{t("thPriority")}</th>
                <th className="text-left px-4 py-3 font-medium w-32">{t("thWorkflow")}</th>
                <th className="text-left px-4 py-3 font-medium w-40">{t("thCompletedAt")}</th>
              </tr>
            </thead>
            <tbody>
              {historyTasks.map((task) => (
                <TaskRow key={task.id} task={task} />
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* 分页 */}
      {historyTotal > PAGE_SIZE && (
        <div className="flex items-center justify-between px-6 py-3 border-t border-line/80">
          <span className="text-xs text-fg-faint">
            {t("pageRange", {
              from: (historyPage - 1) * PAGE_SIZE + 1,
              to: Math.min(historyPage * PAGE_SIZE, historyTotal),
              count: historyTotal,
            })}
          </span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => goToPage(historyPage - 1)}
              disabled={historyPage <= 1}
              className="p-1.5 rounded-md hover:bg-surface-2 disabled:opacity-30 disabled:cursor-not-allowed"
            >
              <ChevronLeft className="w-4 h-4" />
            </button>
            {renderPageNumbers(historyPage, totalPages, goToPage)}
            <button
              onClick={() => goToPage(historyPage + 1)}
              disabled={historyPage >= totalPages}
              className="p-1.5 rounded-md hover:bg-surface-2 disabled:opacity-30 disabled:cursor-not-allowed"
            >
              <ChevronRight className="w-4 h-4" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function TaskRow({ task }: { task: MappedTask }) {
  const t = useTranslations("history");
  const locale = useLocale();
  const tbCls = typeCls[task.type] || typeCls.task;
  const pbCls = priorityCls[task.priority] || priorityCls.medium;

  // 从 _rawNodes 获取完成时间
  const rawNodes = task._rawNodes as
    | Array<{ completed_at?: string; status?: string }>
    | undefined;
  const lastCompletedNode = rawNodes
    ?.filter((n) => n.status === "completed" && n.completed_at)
    .sort((a, b) => (b.completed_at || "").localeCompare(a.completed_at || ""))[0];
  const completedAt = lastCompletedNode?.completed_at;

  return (
    <tr className="border-b border-line/60 hover:bg-surface-2/40 transition-colors">
      <td className="px-6 py-3 font-mono text-xs text-fg-subtle">
        {task.taskRef}
      </td>
      <td className="px-4 py-3">
        <span className="text-fg-muted">{task.title}</span>
      </td>
      <td className="px-4 py-3">
        <span className={`inline-block px-1.5 py-0.5 rounded text-xs ${tbCls}`}>
          {t(typeKeys[task.type] || "typeTask")}
        </span>
      </td>
      <td className="px-4 py-3">
        <span className={`inline-block px-1.5 py-0.5 rounded text-xs ${pbCls}`}>
          {t(`priority.${task.priority}`)}
        </span>
      </td>
      <td className="px-4 py-3 text-fg-subtle text-xs">
        {task.workflowName || "-"}
      </td>
      <td className="px-4 py-3 text-fg-subtle text-xs">
        {completedAt ? new Date(completedAt).toLocaleString(locale) : "-"}
      </td>
    </tr>
  );
}

function renderPageNumbers(
  current: number,
  total: number,
  goToPage: (p: number) => void
) {
  const pages: (number | string)[] = [];
  if (total <= 7) {
    for (let i = 1; i <= total; i++) pages.push(i);
  } else {
    pages.push(1);
    if (current > 3) pages.push("...");
    const start = Math.max(2, current - 1);
    const end = Math.min(total - 1, current + 1);
    for (let i = start; i <= end; i++) pages.push(i);
    if (current < total - 2) pages.push("...");
    pages.push(total);
  }

  return pages.map((p, i) =>
    typeof p === "string" ? (
      <span key={`e${i}`} className="px-1 text-xs text-fg-faint">
        ...
      </span>
    ) : (
      <button
        key={p}
        onClick={() => goToPage(p)}
        className={`min-w-[28px] h-7 rounded text-xs ${
          p === current
            ? "bg-primary-strong text-fg"
            : "hover:bg-surface-2 text-fg-subtle"
        }`}
      >
        {p}
      </button>
    )
  );
}
