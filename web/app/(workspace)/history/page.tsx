"use client";
// 历史任务页：分页展示已完成/已取消任务。标题列下方显示紧凑协作链（或工作流名 · 节点数）；
// 类型/状态用统一徽章，优先级只用文字色；行点击进入运行视图(/workflows/view?taskId=)。

import { useEffect, useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { useTranslations, useLocale } from "next-intl";
import { useAppStore } from "@/lib/store";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import EmptyState from "@/lib/ui/EmptyState";
import StatusBadge from "@/lib/ui/StatusBadge";
import TaskStateBadge from "@/lib/product/TaskStateBadge";
import CollaborationChain from "@/lib/product/CollaborationChain";
import { priorityTextCls } from "@/lib/product/presentation";
import type { MappedTask, TaskType } from "@/lib/types";
import EmptyStateGuide from "@/lib/EmptyStateGuide";
import {
  Search,
  ChevronLeft,
  ChevronRight,
  FileCheck,
  Loader2,
} from "lucide-react";

const PAGE_SIZE = 20;

const typeTone: Record<TaskType, "primary" | "danger" | "neutral"> = {
  story: "primary",
  bug: "danger",
  task: "neutral",
};

const typeKeys: Record<TaskType, string> = {
  story: "typeStory",
  bug: "typeBug",
  task: "typeTask",
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

  const projectName = projects.find((p) => p.id === selectedProjectId)?.name || "";

  return (
    <div className="flex flex-col h-full page-enter">
      <PageHeader
        title={t("title")}
        meta={
          <>
            {projectName && <span className="text-xs text-fg-subtle">{projectName}</span>}
            {historyTotal > 0 && (
              <>
                <span className="text-fg-faint">·</span>
                <span className="text-xs text-fg-faint">{t("total", { count: historyTotal })}</span>
              </>
            )}
          </>
        }
        actions={
          <>
            {projects.length > 1 && (
              <select
                value={selectedProjectId || ""}
                onChange={(e) => setSelectedProjectId(e.target.value)}
                className="h-[34px] px-2.5 text-sm rounded-[var(--radius-control)] border border-line-strong bg-surface-2 text-fg-muted focus:outline-none focus:border-primary cursor-pointer"
              >
                {projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            )}
            <div className="relative">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-fg-faint" />
              <input
                type="text"
                value={searchInput}
                onChange={(e) => setSearchInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={t("searchPlaceholder")}
                className="h-[34px] w-56 pl-8 pr-3 text-sm rounded-[var(--radius-control)] border border-line-strong bg-surface-2 text-fg-muted placeholder:text-fg-faint focus:outline-none focus:border-primary"
              />
            </div>
          </>
        }
      />

      {/* 任务列表 */}
      <div className="flex-1 overflow-y-auto px-6 pb-4">
        {historyLoading && historyTasks.length === 0 ? (
          <div className="flex items-center justify-center h-64">
            <Loader2 className="w-6 h-6 animate-spin text-fg-faint" />
          </div>
        ) : historyTasks.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full">
            <EmptyState icon={FileCheck} title={t("empty")} />
            <div className="w-full max-w-2xl">
              <EmptyStateGuide page="history" />
            </div>
          </div>
        ) : (
          <div className="rounded-[var(--radius-card)] border border-line overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-surface-2/80">
                <tr className="text-xs text-fg-subtle">
                  <th className="text-left px-4 py-2.5 font-medium w-24">{t("thRef")}</th>
                  <th className="text-left px-4 py-2.5 font-medium">{t("thTitle")}</th>
                  <th className="text-left px-4 py-2.5 font-medium w-20">{t("thType")}</th>
                  <th className="text-left px-4 py-2.5 font-medium w-24">{t("thPriority")}</th>
                  <th className="text-left px-4 py-2.5 font-medium w-24">{t("thStatus")}</th>
                  <th className="text-left px-4 py-2.5 font-medium w-40">{t("thCompletedAt")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {historyTasks.map((task) => (
                  <TaskRow key={task.id} task={task} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* 分页 */}
      {historyTotal > PAGE_SIZE && (
        <div className="flex items-center justify-between px-6 py-3 border-t border-line shrink-0">
          <span className="text-xs text-fg-faint">
            {t("pageRange", {
              from: (historyPage - 1) * PAGE_SIZE + 1,
              to: Math.min(historyPage * PAGE_SIZE, historyTotal),
              count: historyTotal,
            })}
          </span>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="sm" onClick={() => goToPage(historyPage - 1)} disabled={historyPage <= 1} aria-label="previous page">
              <ChevronLeft className="w-4 h-4" />
            </Button>
            {renderPageNumbers(historyPage, totalPages, goToPage)}
            <Button variant="ghost" size="sm" onClick={() => goToPage(historyPage + 1)} disabled={historyPage >= totalPages} aria-label="next page">
              <ChevronRight className="w-4 h-4" />
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

function TaskRow({ task }: { task: MappedTask }) {
  const t = useTranslations("history");
  const locale = useLocale();
  const router = useRouter();

  // 从 _rawNodes 获取完成时间(最后一个成功节点的完成时刻)
  const rawNodes = task._rawNodes as Array<{ completed_at?: string; status?: string }> | undefined;
  const lastCompletedNode = rawNodes
    ?.filter((n) => n.status === "succeeded" && n.completed_at)
    .sort((a, b) => (b.completed_at || "").localeCompare(a.completed_at || ""))[0];
  const completedAt = lastCompletedNode?.completed_at;
  const hasChain = (task.nodeNames?.length || 0) > 0;

  return (
    <tr
      tabIndex={0}
      onClick={() => router.push(`/workflows/view?taskId=${task.id}`)}
      onKeyDown={(e) => {
        if (e.key === "Enter") router.push(`/workflows/view?taskId=${task.id}`);
      }}
      className="cursor-pointer transition-colors-fast hover:bg-surface-2/40 focus-visible:outline-none"
    >
      <td className="px-4 py-3 font-mono text-xs text-fg-subtle align-top">{task.taskRef}</td>
      <td className="px-4 py-3">
        <div className="text-fg-muted">{task.title}</div>
        {hasChain ? (
          <div className="mt-2 max-w-lg opacity-90">
            <CollaborationChain
              density="compact"
              names={task.nodeNames || []}
              statuses={task.nodesStatus || []}
              agents={task.nodeAgents || []}
              currentIndex={task.currentNode}
            />
          </div>
        ) : (
          task.workflowName && (
            <div className="text-[10px] text-fg-faint mt-0.5">
              {task.workflowName}
            </div>
          )
        )}
      </td>
      <td className="px-4 py-3 align-top">
        <StatusBadge tone={typeTone[task.type] || "neutral"}>{t(typeKeys[task.type] || "typeTask")}</StatusBadge>
      </td>
      <td className={`px-4 py-3 align-top text-xs ${priorityTextCls(task.priority)}`}>
        {t(`priority.${task.priority}`)}
      </td>
      <td className="px-4 py-3 align-top">
        <TaskStateBadge state={task.priorityState} />
      </td>
      <td className="px-4 py-3 align-top text-fg-subtle text-xs">
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
        className={`min-w-[28px] h-7 rounded-[var(--radius-control)] text-xs focus-visible:outline-none ${
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
