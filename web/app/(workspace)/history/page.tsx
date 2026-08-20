"use client";
// 历史任务页：分页展示已完成/已取消任务。

import { useEffect, useCallback, useState } from "react";
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

const typeBadge: Record<TaskType, { label: string; cls: string }> = {
  story: { label: "需求", cls: "bg-blue-500/20 text-blue-400" },
  bug: { label: "缺陷", cls: "bg-red-500/20 text-red-400" },
  task: { label: "任务", cls: "bg-green-500/20 text-green-400" },
};

const priorityBadge: Record<Priority, { label: string; cls: string }> = {
  urgent: { label: "紧急", cls: "bg-red-500/20 text-red-400" },
  high: { label: "高", cls: "bg-orange-500/20 text-orange-400" },
  medium: { label: "中", cls: "bg-yellow-500/20 text-yellow-400" },
  low: { label: "低", cls: "bg-gray-500/20 text-gray-400" },
};

export default function HistoryPage() {
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
      <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800/80">
        <div className="flex items-center gap-3">
          <FileCheck className="w-5 h-5 text-slate-400" />
          <h1 className="text-lg font-semibold text-white">历史任务</h1>
          {projectName && (
            <span className="text-sm text-slate-400">— {projectName}</span>
          )}
          {historyTotal > 0 && (
            <span className="text-xs text-slate-500">共 {historyTotal} 条</span>
          )}
        </div>

        <div className="flex items-center gap-3">
          {/* 项目选择器 */}
          {projects.length > 1 && (
            <select
              value={selectedProjectId || ""}
              onChange={(e) => setSelectedProjectId(e.target.value)}
              className="h-8 px-2 text-sm rounded-md border border-slate-700 bg-slate-800 text-slate-200"
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
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500" />
            <input
              type="text"
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="搜索历史任务..."
              className="h-8 pl-8 pr-3 text-sm rounded-md border border-slate-700 bg-slate-800 text-slate-200 placeholder:text-slate-500 w-56 focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
          </div>
        </div>
      </div>

      {/* 任务列表 */}
      <div className="flex-1 overflow-y-auto">
        {historyLoading && historyTasks.length === 0 ? (
          <div className="flex items-center justify-center h-64">
            <Loader2 className="w-6 h-6 animate-spin text-slate-500" />
          </div>
        ) : historyTasks.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-64 text-slate-500">
            <FileCheck className="w-12 h-12 mb-3 opacity-30" />
            <p className="text-sm">暂无已完成的历史任务</p>
            <div className="mt-8 w-full max-w-2xl text-left">
              <EmptyStateGuide page="history" />
            </div>
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-800 text-slate-400">
                <th className="text-left px-6 py-3 font-medium w-20">编号</th>
                <th className="text-left px-4 py-3 font-medium">标题</th>
                <th className="text-left px-4 py-3 font-medium w-20">类型</th>
                <th className="text-left px-4 py-3 font-medium w-20">优先级</th>
                <th className="text-left px-4 py-3 font-medium w-32">工作流</th>
                <th className="text-left px-4 py-3 font-medium w-40">完成时间</th>
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
        <div className="flex items-center justify-between px-6 py-3 border-t border-slate-800/80">
          <span className="text-xs text-slate-500">
            第 {(historyPage - 1) * PAGE_SIZE + 1}-
            {Math.min(historyPage * PAGE_SIZE, historyTotal)} 条，共{" "}
            {historyTotal} 条
          </span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => goToPage(historyPage - 1)}
              disabled={historyPage <= 1}
              className="p-1.5 rounded-md hover:bg-slate-800 disabled:opacity-30 disabled:cursor-not-allowed"
            >
              <ChevronLeft className="w-4 h-4" />
            </button>
            {renderPageNumbers(historyPage, totalPages, goToPage)}
            <button
              onClick={() => goToPage(historyPage + 1)}
              disabled={historyPage >= totalPages}
              className="p-1.5 rounded-md hover:bg-slate-800 disabled:opacity-30 disabled:cursor-not-allowed"
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
  const tb = typeBadge[task.type] || typeBadge.task;
  const pb = priorityBadge[task.priority] || priorityBadge.medium;

  // 从 _rawNodes 获取完成时间
  const rawNodes = task._rawNodes as
    | Array<{ completed_at?: string; status?: string }>
    | undefined;
  const lastCompletedNode = rawNodes
    ?.filter((n) => n.status === "completed" && n.completed_at)
    .sort((a, b) => (b.completed_at || "").localeCompare(a.completed_at || ""))[0];
  const completedAt = lastCompletedNode?.completed_at;

  return (
    <tr className="border-b border-slate-800/60 hover:bg-slate-800/40 transition-colors">
      <td className="px-6 py-3 font-mono text-xs text-slate-400">
        {task.taskRef}
      </td>
      <td className="px-4 py-3">
        <span className="text-slate-200">{task.title}</span>
      </td>
      <td className="px-4 py-3">
        <span className={`inline-block px-1.5 py-0.5 rounded text-xs ${tb.cls}`}>
          {tb.label}
        </span>
      </td>
      <td className="px-4 py-3">
        <span className={`inline-block px-1.5 py-0.5 rounded text-xs ${pb.cls}`}>
          {pb.label}
        </span>
      </td>
      <td className="px-4 py-3 text-slate-400 text-xs">
        {task.workflowName || "-"}
      </td>
      <td className="px-4 py-3 text-slate-400 text-xs">
        {completedAt ? new Date(completedAt).toLocaleString("zh-CN") : "-"}
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
      <span key={`e${i}`} className="px-1 text-xs text-slate-500">
        ...
      </span>
    ) : (
      <button
        key={p}
        onClick={() => goToPage(p)}
        className={`min-w-[28px] h-7 rounded text-xs ${
          p === current
            ? "bg-blue-600 text-white"
            : "hover:bg-slate-800 text-slate-400"
        }`}
      >
        {p}
      </button>
    )
  );
}
