"use client";
// 人工待办页：读看板 manual_intervention 列(任务级状态=需人工介入的任务),
// 左侧 360px 待办列表,右侧任务上下文;处理动作在任务详情(工作台 cockpit)完成,
// 本页只提供入口链接。路由名 /reviews 保持不变。

import { useState, useEffect, useMemo } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { ArrowRight, Clock, Bot, ShieldAlert, FileText, Lock, Inbox } from "lucide-react";
import { useAppStore } from "@/lib/store";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import EmptyState from "@/lib/ui/EmptyState";
import TaskStateBadge from "@/lib/product/TaskStateBadge";
import CollaborationChain from "@/lib/product/CollaborationChain";
import type { MappedTask } from "@/lib/types";

export default function ReviewsPage() {
  const t = useTranslations("reviews");
  const router = useRouter();
  const tasks = useAppStore((s) => s.tasks);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);

  const [selectedId, setSelectedId] = useState<number | null>(null);

  // 需人工任务来自任务数据(与看板 manual_intervention 列同源),轮询保持新鲜
  useEffect(() => {
    if (!currentProjectId) return;
    loadProjectData(currentProjectId);
    const interval = setInterval(() => loadProjectData(currentProjectId), 10000);
    return () => clearInterval(interval);
  }, [currentProjectId, loadProjectData]);

  const todoTasks = useMemo(
    () => tasks.filter((task) => task.priorityState === "manual_intervention"),
    [tasks]
  );

  const selectedTask = todoTasks.find((task) => task.id === selectedId) || null;

  // 首次进入/选中项被处理后自动选中第一项
  useEffect(() => {
    if (todoTasks.length === 0) {
      if (selectedId !== null) setSelectedId(null);
      return;
    }
    if (!todoTasks.some((task) => task.id === selectedId)) {
      setSelectedId(todoTasks[0].id);
    }
  }, [todoTasks, selectedId]);

  // 当前人工节点:第一个 manual_intervention 状态的节点
  const humanNode = useMemo(() => {
    if (!selectedTask) return null;
    const rawNodes = (selectedTask._rawNodes || []) as Array<{
      id?: string;
      name?: string;
      type_key?: string;
      status?: string;
      config?: { prompt?: unknown };
    }>;
    return rawNodes.find((n) => n.status === "manual_intervention") ?? null;
  }, [selectedTask]);

  const humanPrompt = typeof humanNode?.config?.prompt === "string" ? humanNode.config.prompt : "";

  const rawTypeKeys = (task: MappedTask) =>
    ((task._rawNodes || []) as Array<{ type_key?: string }>).map((n) => n.type_key || "");

  return (
    <div className="h-full flex flex-col page-enter">
      <PageHeader
        title={t("title")}
        meta={<span className="text-xs text-fg-faint">{t("subtitle", { count: todoTasks.length })}</span>}
      />

      <div className="flex-1 flex min-h-0 px-6 pb-6">
        {/* 左侧待办列表 */}
        <div className="w-[360px] shrink-0 border border-line rounded-[var(--radius-card)] bg-surface-2/20 flex flex-col min-h-0 mr-5">
          <div className="flex-1 overflow-y-auto py-1.5">
            {todoTasks.map((task) => {
              const active = task.id === selectedId;
              return (
                <button
                  key={task.id}
                  onClick={() => setSelectedId(task.id)}
                  className={`relative w-full text-left px-4 py-3 flex items-start gap-2.5 transition-colors-fast focus-visible:outline-none ${
                    active ? "bg-primary/10" : "hover:bg-surface-2/60"
                  }`}
                >
                  {active && (
                    <span className="absolute left-0 top-2 bottom-2 w-0.5 bg-primary-fg rounded-r-full" />
                  )}
                  <span className="w-2 h-2 rounded-full mt-1.5 shrink-0 bg-warning-fg" />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="text-[10px] font-mono text-fg-faint shrink-0">{task.taskRef}</span>
                    </div>
                    <div className={`text-xs truncate mt-0.5 ${active ? "text-primary-fg font-medium" : "text-fg-muted"}`}>
                      {task.title}
                    </div>
                    {task.nodeNames?.[task.currentNode] && (
                      <div className="text-[10px] text-fg-faint truncate mt-0.5">
                        {task.nodeNames[task.currentNode]}
                      </div>
                    )}
                  </div>
                </button>
              );
            })}
          </div>
        </div>

        {/* 右侧上下文 */}
        <div className="flex-1 min-w-0 overflow-y-auto">
          {!selectedTask ? (
            <div className="h-full flex items-center justify-center">
              <EmptyState icon={Inbox} title={t("emptyTitle")} description={t("emptyDesc")} />
            </div>
          ) : (
            <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/20 divide-y divide-line">
              {/* 1. 任务头 */}
              <div className="p-5">
                <div className="flex items-start justify-between gap-3 flex-wrap">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <span className="text-primary-fg font-mono text-sm bg-primary/10 px-2 py-0.5 rounded shrink-0">
                      {selectedTask.taskRef}
                    </span>
                    <h2 className="text-base font-semibold text-fg truncate">{selectedTask.title}</h2>
                  </div>
                  <TaskStateBadge state={selectedTask.priorityState} className="shrink-0" />
                </div>
              </div>

              {/* 2. 完整协作链:明确人工节点位置 */}
              <div className="p-5">
                <CollaborationChain
                  density="full"
                  names={selectedTask.nodeNames || []}
                  statuses={selectedTask.nodesStatus || []}
                  agents={selectedTask.nodeAgents || []}
                  currentIndex={selectedTask.currentNode}
                  typeKeys={rawTypeKeys(selectedTask)}
                />
              </div>

              {/* 3. 上下文 */}
              <div className="p-5 space-y-4">
                <div className="flex items-center gap-5 text-sm flex-wrap">
                  <div className="flex items-center gap-2 text-fg-muted">
                    <Clock className="w-4 h-4 text-fg-faint" />
                    <span className="text-fg-faint text-xs">{t("currentNode")}</span>
                    <span className="font-medium text-xs">
                      {selectedTask.nodeNames?.[selectedTask.currentNode] || t("unknownNode")}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-fg-muted">
                    <Bot className="w-4 h-4 text-primary-fg" />
                    <span className="text-fg-faint text-xs">{t("executor")}</span>
                    <span className="font-medium text-xs">
                      {selectedTask.nodeAgents?.[selectedTask.currentNode] || t("unassigned")}
                    </span>
                  </div>
                  <span className="text-xs text-fg-faint font-mono">
                    {t("progress", {
                      completed: (selectedTask.nodesStatus || []).filter((s) => s === "succeeded").length,
                      total: selectedTask.nodeNames?.length || 0,
                    })}
                  </span>
                </div>

                {humanPrompt && (
                  <div className="bg-warning/5 border border-warning/20 rounded-[var(--radius-control)] px-3.5 py-2.5 text-xs text-warning-fg whitespace-pre-wrap">
                    {humanPrompt}
                  </div>
                )}

                {selectedTask.message && (
                  <div className="bg-warning/5 border border-warning/20 rounded-[var(--radius-control)] px-3.5 py-2.5 text-xs text-warning-fg whitespace-pre-wrap">
                    {selectedTask.message}
                  </div>
                )}

                {selectedTask.description && (
                  <div>
                    <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-1.5 flex items-center gap-1.5">
                      <FileText className="w-3.5 h-3.5" />
                      {t("description")}
                    </div>
                    <div className="text-sm text-fg-muted whitespace-pre-wrap leading-relaxed">
                      {selectedTask.description}
                    </div>
                  </div>
                )}

                {selectedTask.constraints && (
                  <div>
                    <div className="text-xs font-semibold text-warning-fg uppercase tracking-wider mb-1.5 flex items-center gap-1.5">
                      <Lock className="w-3.5 h-3.5" />
                      {t("constraints")}
                    </div>
                    <div className="text-sm text-warning-fg/90 whitespace-pre-wrap leading-relaxed">
                      {selectedTask.constraints}
                    </div>
                  </div>
                )}
              </div>

              {/* 4. 操作:前往任务详情处理(提交/取消在 cockpit 的人工表单完成) */}
              <div className="p-5">
                <div className="flex items-center gap-2.5">
                  <Button variant="primary" onClick={() => router.push(`/dashboard?task=${selectedTask.id}`)}>
                    <ShieldAlert className="w-4 h-4 mr-1.5" />
                    {t("goHandle")}
                    <ArrowRight className="w-4 h-4 ml-1.5" />
                  </Button>
                  <span className="text-xs text-fg-faint">{t("goHandleHint")}</span>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
