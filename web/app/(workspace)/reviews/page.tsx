"use client";
// 审查队列页：展示待审查任务及审查记录。

import React, { useState, useEffect } from "react";
import {
  Shield,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Filter,
  Clock,
  User,
  Bot,
  ChevronDown,
  Eye,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";
import type { MappedTask } from "@/lib/types";
import EmptyStateGuide from "@/lib/EmptyStateGuide";

// ==========================================
// 退回原因弹窗
// ==========================================
interface RejectReasonDialogProps {
  task: MappedTask;
  onConfirm: (taskId: number, reason: string) => void;
  onClose: () => void;
}

function RejectReasonDialog({
  task,
  onConfirm,
  onClose,
}: RejectReasonDialogProps) {
  const [reason, setReason] = useState("");

  const handleSubmit = () => {
    if (!reason.trim()) return;
    onConfirm(task.id, reason);
    setReason("");
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[480px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center">
          <h3 className="text-lg font-bold text-white flex items-center">
            <XCircle className="w-5 h-5 mr-2 text-orange-400" /> 退回任务
          </h3>
          <button
            onClick={onClose}
            className="p-1 hover:bg-slate-700 rounded text-slate-400 hover:text-white transition-colors-fast"
          >
            <XCircle className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div className="text-sm text-slate-300">
            <span className="text-blue-400 font-mono mr-2">{task.taskRef}</span>
            {task.title}
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              退回原因 <span className="text-red-400">*</span>
            </label>
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              autoFocus
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-3 text-sm text-slate-300 focus:outline-none focus:border-orange-500 resize-none transition-colors-fast"
              placeholder="请详细说明退回原因..."
            />
          </div>
        </div>
        <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
          >
            取消
          </button>
          <button
            onClick={handleSubmit}
            disabled={!reason.trim()}
            className="flex items-center px-5 py-2 bg-orange-600 hover:bg-orange-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press transition-colors-fast"
          >
            <XCircle className="w-4 h-4 mr-2" /> 确认退回
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 自我审查风险检测（本地逻辑）
// ==========================================
function checkSelfReviewRisk(task: MappedTask): string | null {
  const currentNodeIdx = task.currentNode;
  const currentAgent = task.nodeAgents?.[currentNodeIdx];
  if (!currentAgent) return null;

  // 检查当前审查节点的代理是否也出现在之前的"standard"节点
  for (let i = 0; i < currentNodeIdx; i++) {
    const nodeStatus = task.nodesStatus?.[i];
    // 只看已完成或进行中的标准节点（排除 pending）
    if (nodeStatus === "completed" || nodeStatus === "in_progress") {
      if (task.nodeAgents?.[i] === currentAgent) {
        return currentAgent;
      }
    }
  }
  return null;
}

// ==========================================
// 自我审查 API 检测徽章
// ==========================================
interface SelfReviewBadgeProps {
  task: MappedTask;
}

function SelfReviewBadge({ task }: SelfReviewBadgeProps) {
  const [isSelfReview, setIsSelfReview] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    // 从 _rawNodes 中查找当前审查节点 ID
    const rawNodes = task._rawNodes as Array<{
      id?: string;
      status?: string;
    }> | undefined;
    const currentNode = (rawNodes || []).find(
      (n) => n.status === "in_progress" || n.status === "pending"
    );
    if (!currentNode || !currentNode.id) {
      setLoading(false);
      return;
    }
    api
      .checkSelfReview(task.id, currentNode.id)
      .then((data) => {
        setIsSelfReview(
          (data as Record<string, unknown>).is_self_review === true
        );
      })
      .catch(() => {
        setIsSelfReview(false);
      })
      .finally(() => setLoading(false));
  }, [task.id, task._rawNodes]);

  if (loading || !isSelfReview) return null;

  return (
    <span className="flex items-center gap-1 text-xs px-2 py-1 rounded-full border font-medium bg-amber-500/10 text-amber-400 border-amber-500/30">
      <AlertTriangle className="w-3 h-3" /> 自审风险
    </span>
  );
}

// ==========================================
// 审查队列主视图 (Next.js Page)
// ==========================================
export default function ReviewsPage() {
  const tasks = useAppStore((s) => s.tasks);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);

  const [filter, setFilter] = useState<"all" | "rejected" | "pending_review">(
    "all"
  );
  const [showFilterMenu, setShowFilterMenu] = useState(false);
  const [rejectingTask, setRejectingTask] = useState<MappedTask | null>(null);
  const [reviewTaskIds, setReviewTaskIds] = useState<Set<number>>(new Set());

  // 从后端获取审查队列（review 类型节点中 pending/in_progress 的任务）
  useEffect(() => {
    if (!currentProjectId) return;
    api
      .getReviewQueue(currentProjectId)
      .then((items: unknown) => {
        const list = Array.isArray(items)
          ? (items as Array<{ task_id: number }>)
          : [];
        setReviewTaskIds(new Set(list.map((i) => i.task_id)));
      })
      .catch(() => setReviewTaskIds(new Set()));
  }, [currentProjectId]);

  // 审查任务 = 后端审查队列（待审查节点）∪ 本地被退回的任务
  const reviewTasks = tasks
    .filter((t) => {
      const nodeStatus = t.nodesStatus?.[t.currentNode];
      const isRejected =
        t.priorityState === "rejected" || nodeStatus === "rejected";
      return reviewTaskIds.has(t.id) || isRejected;
    })
    .filter((t) => {
      // 进一步筛选：只保留当前节点是审查相关或被退回的
      const nodeName = t.nodeNames?.[t.currentNode] || "";
      const nodeStatus = t.nodesStatus?.[t.currentNode];
      return (
        nodeStatus === "rejected" ||
        nodeName.includes("审查") ||
        nodeName.includes("Review") ||
        t.priorityState === "rejected"
      );
    });

  const filteredTasks = reviewTasks.filter((t) => {
    if (filter === "rejected")
      return (
        t.priorityState === "rejected" ||
        t.nodesStatus?.[t.currentNode] === "rejected"
      );
    if (filter === "pending_review")
      return (
        t.priorityState !== "rejected" &&
        t.nodesStatus?.[t.currentNode] !== "rejected"
      );
    return true;
  });

  const filterLabels: Record<string, string> = {
    all: "全部",
    rejected: "已退回",
    pending_review: "待审查",
  };

  const handleApprove = async (taskId: number) => {
    const task = tasks.find((t) => t.id === taskId);
    if (!task) return;
    const rawNodes = task._rawNodes as Array<{
      id?: string;
      status?: string;
    }> | undefined;
    const currentNode = (rawNodes || []).find(
      (n) =>
        n.status === "manual_intervention" ||
        n.status === "rejected" ||
        n.status === "in_progress"
    );
    const nodeId = currentNode?.id;
    if (!nodeId) return;
    try {
      await api.approveNode(taskId, nodeId);
      if (task._projectId) {
        await loadProjectData(task._projectId);
      }
    } catch (err) {
      console.error("Approve failed:", err);
    }
  };

  const handleRejectConfirm = async (taskId: number, reason: string) => {
    const task = rejectingTask;
    if (!task) return;
    const rawNodes = task._rawNodes as Array<{
      id?: string;
      status?: string;
    }> | undefined;
    const currentNode = (rawNodes || []).find(
      (n) => n.status === "manual_intervention" || n.status === "rejected"
    );
    const nodeId = currentNode?.id || undefined;
    try {
      await api.rejectNode(taskId, nodeId!, { reason });
      if (task._projectId) {
        await loadProjectData(task._projectId);
      }
    } catch (err) {
      console.error("Reject failed:", err);
    }
    setRejectingTask(null);
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      {/* 页头 */}
      <div className="flex justify-between items-center mb-8 shrink-0">
        <div>
          <h1 className="text-2xl font-bold text-white mb-2 flex items-center">
            <Shield className="w-7 h-7 mr-3 text-amber-400" />
            审查队列
          </h1>
          <p className="text-sm text-slate-400">
            {reviewTasks.length} 个任务待审查 · 自动检测自我审查风险
          </p>
        </div>

        {/* 筛选器 */}
        <div className="relative">
          <button
            onClick={() => setShowFilterMenu(!showFilterMenu)}
            className="flex items-center gap-2 px-4 py-2 bg-slate-800/60 border border-slate-700/60 rounded-xl text-sm text-slate-300 hover:bg-slate-700/60 hover:border-slate-600 transition-colors-fast backdrop-blur-sm"
          >
            <Filter className="w-4 h-4 text-slate-400" />
            <span>{filterLabels[filter]}</span>
            <ChevronDown
              className={`w-4 h-4 text-slate-500 transition-transform ${showFilterMenu ? "rotate-180" : ""}`}
            />
          </button>
          {showFilterMenu && (
            <>
              <div
                className="fixed inset-0 z-40"
                onClick={() => setShowFilterMenu(false)}
              />
              <div className="absolute right-0 top-full mt-2 bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-xl shadow-2xl z-50 min-w-[160px] overflow-hidden animate-scale-in">
                {Object.entries(filterLabels).map(([key, label]) => (
                  <button
                    key={key}
                    onClick={() => {
                      setFilter(
                        key as "all" | "rejected" | "pending_review"
                      );
                      setShowFilterMenu(false);
                    }}
                    className={`w-full text-left px-4 py-2.5 text-sm transition-colors-fast ${
                      filter === key
                        ? "bg-blue-500/10 text-blue-400"
                        : "text-slate-300 hover:bg-slate-800"
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </>
          )}
        </div>
      </div>

      {/* 任务列表 */}
      {filteredTasks.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center text-slate-500">
          <Shield className="w-16 h-16 mb-4 text-slate-700" />
          <div className="text-lg font-medium mb-1">暂无待审查任务</div>
          <div className="text-sm">
            所有任务均已通过审查或无审查任务
          </div>
          <div className="mt-8 w-full max-w-2xl text-left">
            <EmptyStateGuide page="reviews" />
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          {filteredTasks.map((task) => {
            const selfReviewAgent = checkSelfReviewRisk(task);
            const isRejected =
              task.priorityState === "rejected" ||
              task.nodesStatus?.[task.currentNode] === "rejected";
            const currentNodeName =
              task.nodeNames?.[task.currentNode] || "未知节点";
            const currentAgent =
              task.nodeAgents?.[task.currentNode] || "未分配";

            return (
              <div
                key={task.id}
                className={`bg-slate-800/40 border rounded-xl backdrop-blur-sm p-5 transition-all hover:shadow-lg card-hover ${
                  isRejected
                    ? "border-orange-500/40 hover:border-orange-500/60 hover:shadow-orange-500/5"
                    : "border-slate-700/60 hover:border-slate-600/60 hover:shadow-blue-500/5"
                }`}
              >
                {/* 顶部：任务ID + 标题 + 状态 */}
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3 min-w-0 flex-1">
                    <span className="text-blue-400 font-mono text-sm bg-blue-400/10 px-2 py-1 rounded shrink-0">
                      {task.taskRef}
                    </span>
                    <h3 className="text-white font-medium truncate">
                      {task.title}
                    </h3>
                  </div>
                  <div className="flex items-center gap-2 shrink-0 ml-4">
                    {isRejected ? (
                      <span className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full border font-medium bg-orange-500/10 text-orange-400 border-orange-500/30">
                        <XCircle className="w-3.5 h-3.5" /> 已退回
                      </span>
                    ) : (
                      <span className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full border font-medium bg-blue-500/10 text-blue-400 border-blue-500/30">
                        <Clock className="w-3.5 h-3.5" /> 待审查
                      </span>
                    )}
                    {task.rejectCount > 0 && (
                      <span className="flex items-center gap-1 text-xs px-2 py-1 rounded-full border font-medium bg-red-500/10 text-red-400 border-red-500/20">
                        <AlertTriangle className="w-3 h-3" /> 退回{" "}
                        {task.rejectCount} 次
                      </span>
                    )}
                    <SelfReviewBadge task={task} />
                  </div>
                </div>

                {/* 中间：节点信息 + 代理 */}
                <div className="flex items-center gap-6 mb-4 text-sm">
                  <div className="flex items-center gap-2 text-slate-300">
                    <Eye className="w-4 h-4 text-slate-500" />
                    <span className="text-slate-500">当前节点:</span>
                    <span className="font-medium">{currentNodeName}</span>
                  </div>
                  <div className="flex items-center gap-2 text-slate-300">
                    {currentAgent.includes("Bot") ||
                    currentAgent.includes("Claude") ||
                    currentAgent.includes("GPT") ? (
                      <Bot className="w-4 h-4 text-blue-400" />
                    ) : (
                      <User className="w-4 h-4 text-emerald-400" />
                    )}
                    <span className="text-slate-500">审查者:</span>
                    <span className="font-medium">{currentAgent}</span>
                  </div>
                  {task.message && (
                    <div
                      className="text-xs text-slate-400 truncate max-w-[300px]"
                      title={task.message}
                    >
                      {task.message}
                    </div>
                  )}
                </div>

                {/* 自我审查风险警告 */}
                {selfReviewAgent && (
                  <div className="flex items-center gap-3 bg-amber-500/10 border border-amber-500/30 rounded-lg px-4 py-3 mb-4">
                    <AlertTriangle className="w-5 h-5 text-amber-400 shrink-0" />
                    <span className="text-sm text-amber-300 font-medium">
                      {"\u26A0"} 自我审查风险: {selfReviewAgent}{" "}
                      同时参与了编码和审查
                    </span>
                    <span className="text-xs text-amber-400/70 ml-auto shrink-0">
                      建议更换审查者
                    </span>
                  </div>
                )}

                {/* 底部：操作按钮 */}
                <div className="flex items-center gap-3 pt-3 border-t border-slate-700/40">
                  <button
                    onClick={() => handleApprove(task.id)}
                    className="flex items-center gap-2 px-4 py-2 bg-emerald-600/20 hover:bg-emerald-600/30 text-emerald-400 rounded-lg text-sm font-medium border border-emerald-500/30 transition-colors-fast btn-press"
                  >
                    <CheckCircle2 className="w-4 h-4" /> 通过
                  </button>
                  <button
                    onClick={() => setRejectingTask(task)}
                    className="flex items-center gap-2 px-4 py-2 bg-orange-600/10 hover:bg-orange-600/20 text-orange-400 rounded-lg text-sm font-medium border border-orange-500/30 transition-colors-fast btn-press"
                  >
                    <XCircle className="w-4 h-4" /> 退回
                  </button>
                  <div className="flex-1" />
                  <div className="flex items-center gap-4 text-xs text-slate-500">
                    <span>
                      进度{" "}
                      {(task.nodesStatus || []).filter(
                        (s) => s === "completed"
                      ).length}
                      /{task.nodeNames?.length || 0}
                    </span>
                    {task.agent && (
                      <span className="flex items-center gap-1">
                        <Bot className="w-3 h-3" /> {task.agent}
                      </span>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* 退回弹窗 */}
      {rejectingTask && (
        <RejectReasonDialog
          task={rejectingTask}
          onConfirm={handleRejectConfirm}
          onClose={() => setRejectingTask(null)}
        />
      )}
    </div>
  );
}
