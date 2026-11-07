"use client";
// 工作台页：任务列表/看板双视图，实时日志终端、评论与节点流转展示。

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
import ReactMarkdown from "react-markdown";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import { mapTaskFromApi } from "@/lib/mappers";
import { useTaskLogs } from "@/lib/use-task-logs";
import EmptyStateGuide from "@/lib/EmptyStateGuide";
import type { MappedTask, MappedAgent, NodeType, NodeTransition } from "@/lib/types";
import {
  Bot,
  CheckCircle2,
  AlertCircle,
  AlertTriangle,
  XCircle,
  Terminal,
  ShieldAlert,
  Activity,
  Pause,
  ArrowRight,
  MessageSquare,
  Send,
  Loader2,
  Search,
  Edit2,
  Save,
  Clock,
  History,
  GitBranch,
  Plus,
  Trash2,
  Cpu,
  Lock,
  Calendar,
  Tag,
  Timer,
  Ban,
  AlertOctagon,
  ListTree,
  AtSign,
  X,
} from "lucide-react";
import ModalPortal from "@/lib/ModalPortal";
import RunFlowCanvas, { type RunFlowNode } from "@/lib/workflow-canvas/RunFlowDemo";
import { useTranslations, useFormatter } from "next-intl";


// ==========================================
// 退回弹窗
// ==========================================
interface RejectDialogProps {
  task: MappedTask;
  onConfirm: (targetNode: number, reason: string) => void;
  onClose: () => void;
}

// ==========================================
// 重新分配弹窗：把人工介入节点重新分配给指定 AI 代理
// ==========================================
interface ReassignDialogProps {
  agents: MappedAgent[];
  onConfirm: (agentId: string) => void;
  onClose: () => void;
}

function ReassignDialog({ agents, onConfirm, onClose }: ReassignDialogProps) {
  const t = useTranslations("dashboard");
  const [selectedAgentId, setSelectedAgentId] = useState("");

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[420px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center">
          <h3 className="text-lg font-bold text-fg flex items-center">
            <ArrowRight className="w-5 h-5 mr-2 text-accent-fg" /> {t('reassignDialog.title')}
          </h3>
          <button onClick={onClose} className="p-1 hover:bg-surface-3 rounded text-fg-subtle hover:text-fg transition-colors-fast">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="p-5">
          <p className="text-sm text-fg-subtle mb-4">
            {t('reassignDialog.description')}
          </p>
          <div className="space-y-1 max-h-56 overflow-y-auto">
            {agents.length === 0 ? (
              <div className="py-8 text-center text-sm text-fg-faint">{t('reassignDialog.noAgents')}</div>
            ) : (
              agents.map((a) => (
                <button
                  key={a.id}
                  onClick={() => setSelectedAgentId(a.id)}
                  className={`w-full text-left px-3 py-2.5 rounded-lg text-sm flex items-center gap-2 transition-colors-fast border ${
                    selectedAgentId === a.id
                      ? "bg-primary/10 border-primary/40 text-primary-fg"
                      : "border-transparent text-fg-muted hover:bg-surface-2"
                  }`}
                >
                  <Cpu className="w-4 h-4 shrink-0" />
                  <span className="truncate flex-1">{a.name}</span>
                  <span className={`text-[10px] px-1.5 py-0.5 rounded-full ${
                    a.status === "online" ? "bg-success/15 text-success-fg" : "bg-fg-faint/15 text-fg-subtle"
                  }`}>
                    {a.status === "online" ? t('agentStatus.online') : a.status === "busy" ? t('agentStatus.busy') : t('agentStatus.offline')}
                  </span>
                </button>
              ))
            )}
          </div>
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
          >
            {t('common.cancel')}
          </button>
          <button
            onClick={() => onConfirm(selectedAgentId)}
            disabled={!selectedAgentId}
            className="px-5 py-2 bg-accent-strong hover:bg-accent text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast btn-press"
          >
            {t('reassignDialog.confirm')}
          </button>
        </div>
      </div>
    </div>
  );
}

function RejectDialog({ task, onConfirm, onClose }: RejectDialogProps) {
  const t = useTranslations("dashboard");
  const [targetNode, setTargetNode] = useState(Math.max(0, task.currentNode - 1));
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const handleSubmit = () => {
    if (!reason.trim()) return;
    setSubmitting(true);
    setTimeout(() => {
      onConfirm(targetNode, reason);
      setSubmitting(false);
    }, 600);
  };

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[520px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center">
          <h3 className="text-lg font-bold text-fg flex items-center">
            <ArrowRight className="w-5 h-5 mr-2 text-orange-400 rotate-180" /> {t('rejectDialog.title')}
          </h3>
          <button onClick={onClose} className="p-1 hover:bg-surface-3 rounded">
            <XCircle className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-5 space-y-5">
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-3">{t('rejectDialog.selectTarget')}</label>
            <div className="space-y-2 max-h-48 overflow-y-auto">
              {(task.nodeNames || [])
                .slice(0, task.currentNode + 1)
                .map((name: string, idx: number) => {
                  const s = (task.nodesStatus || [])[idx];
                  if (s === "pending") return null;
                  const isActive = idx === targetNode;
                  return (
                    <button
                      key={idx}
                      onClick={() => setTargetNode(idx)}
                      className={`w-full flex items-center p-3 rounded-lg border text-left transition-all ${
                        isActive
                          ? "bg-orange-500/10 border-orange-500/50 text-orange-300 ring-1 ring-orange-500/30"
                          : "bg-surface-2/50 border-line-strong text-fg-subtle hover:border-line-strong"
                      }`}
                    >
                      <div
                        className={`w-7 h-7 rounded-full flex items-center justify-center mr-3 text-xs font-bold border ${
                          isActive
                            ? "bg-orange-500 border-orange-500 text-fg"
                            : "bg-surface-3 border-line-strong"
                        }`}
                      >
                        {idx + 1}
                      </div>
                      <div className="flex-1">
                        <div className="text-sm font-medium">{name}</div>
                        <div className="text-xs text-fg-faint mt-0.5">
                          {task.nodeAgents?.[idx] || t('common.unassigned')}
                        </div>
                      </div>
                      {s === "completed" && <CheckCircle2 className="w-4 h-4 text-success" />}
                      {s === "rejected" && <AlertCircle className="w-4 h-4 text-orange-400" />}
                    </button>
                  );
                })}
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t('rejectDialog.reasonLabel')} <span className="text-danger-fg">*</span>
            </label>
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              autoFocus
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-3 text-sm text-fg-muted focus:outline-none focus:border-orange-500 resize-none"
              placeholder={t('rejectDialog.reasonPlaceholder')}
            />
          </div>
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button onClick={onClose} className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg">
            {t('common.cancel')}
          </button>
          <button
            onClick={handleSubmit}
            disabled={!reason.trim() || submitting}
            className="flex items-center px-5 py-2 bg-orange-600 hover:bg-orange-500 text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {submitting ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <ArrowRight className="w-4 h-4 mr-2 rotate-180" />
            )}
            {t('rejectDialog.confirmTo', { targetNode: targetNode + 1 })}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 编辑任务弹窗
// ==========================================
interface EditTaskDialogProps {
  task: MappedTask;
  onSave: (taskId: number, updates: { title: string; description: string; priority: string }) => void;
  onClose: () => void;
}

function EditTaskDialog({ task, onSave, onClose }: EditTaskDialogProps) {
  const t = useTranslations("dashboard");
  const [title, setTitle] = useState(task.title);
  const [desc, setDesc] = useState(task.description || "");
  const [priority, setPriority] = useState<string>(task.priority || "medium");
  const [saving, setSaving] = useState(false);

  const handleSave = () => {
    if (!title.trim()) return;
    setSaving(true);
    setTimeout(() => {
      onSave(task.id, { title: title.trim(), description: desc, priority });
      setSaving(false);
      onClose();
    }, 400);
  };

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[500px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center">
          <h3 className="text-lg font-bold text-fg">{t('editTask.title')}</h3>
          <button onClick={onClose} className="p-1 hover:bg-surface-3 rounded">
            <XCircle className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">{t('editTask.titleLabel')}</label>
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg text-lg focus:outline-none focus:border-primary"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">{t('editTask.priorityLabel')}</label>
            <select
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
            >
              <option value="urgent">{t('priority.urgent')}</option>
              <option value="high">{t('priority.high')}</option>
              <option value="medium">{t('priority.medium')}</option>
              <option value="low">{t('priority.low')}</option>
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">{t('editTask.descriptionLabel')}</label>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              rows={5}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-primary resize-none"
            />
          </div>
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button onClick={onClose} className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg">
            {t('common.cancel')}
          </button>
          <button
            onClick={handleSave}
            disabled={!title.trim() || saving}
            className="flex items-center px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {saving ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : <Save className="w-4 h-4 mr-2" />}
            {t('editTask.save')}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 子任务面板
// ==========================================
interface SubTask {
  id: number;
  title: string;
  status: string;
  gitBranch: string;
  createdAt: string;
}

interface SubTaskPanelProps {
  subTasks?: SubTask[];
  parentId: number;
  onAddSubTask: (parentId: number, subTask: SubTask) => void;
}

function SubTaskPanel({ subTasks = [], parentId, onAddSubTask }: SubTaskPanelProps) {
  const t = useTranslations("dashboard");
  const { dateTime } = useFormatter();
  const { canEdit } = usePermission();
  const [showForm, setShowForm] = useState(false);
  const [title, setTitle] = useState("");

  const handleAdd = () => {
    if (!title.trim()) return;
    const st: SubTask = {
      id: Date.now(),
      title: title.trim(),
      status: "in_progress",
      gitBranch: `teammate/${parentId}/sub/${Date.now().toString(36)}`,
      createdAt: dateTime(new Date(), { hour: "2-digit", minute: "2-digit" }),
    };
    onAddSubTask(parentId, st);
    setTitle("");
    setShowForm(false);
  };

  return (
    <div className="border border-line-strong/60 rounded-xl bg-surface-2/30">
      <div className="px-5 py-3 border-b border-line-strong/60 flex items-center justify-between">
        <div className="flex items-center">
          <GitBranch className="w-4 h-4 mr-2 text-accent-fg" />
          <span className="text-sm font-medium text-fg-muted">{t('subTasks.header', { count: subTasks.length })}</span>
        </div>
        <button
          onClick={() => setShowForm(!showForm)}
          disabled={!canEdit}
          className="flex items-center text-xs text-accent-fg hover:text-accent-fg transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Plus className="w-3.5 h-3.5 mr-1" /> {t('subTasks.new')}
        </button>
      </div>
      {showForm && (
        <div className="px-5 py-3 border-b border-line-strong/30 flex gap-2 animate-scale-in">
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleAdd()}
            className="flex-1 bg-bg border border-line-strong rounded-lg px-3 py-2 text-sm text-fg focus:outline-none focus:border-accent"
            placeholder={t('subTasks.titlePlaceholder')}
          />
          <button
            onClick={handleAdd}
            disabled={!title.trim()}
            className="px-3 py-2 bg-accent-strong hover:bg-accent text-fg rounded-lg text-xs disabled:opacity-50 btn-press"
          >
            {t('subTasks.create')}
          </button>
        </div>
      )}
      {subTasks.length > 0 ? (
        <div className="px-5 py-3 space-y-2">
          {subTasks.map((st) => (
            <div
              key={st.id}
              className="flex items-center justify-between bg-surface/50 border border-line-strong/50 rounded-lg p-3"
            >
              <div className="flex items-center gap-3 min-w-0">
                <Activity className="w-3.5 h-3.5 text-primary-fg shrink-0" />
                <div className="min-w-0">
                  <div className="text-sm text-fg-muted truncate">{st.title}</div>
                  <div className="text-[10px] text-fg-faint font-mono truncate">{st.gitBranch}</div>
                </div>
              </div>
              <div className="text-[10px] text-fg-faint shrink-0 ml-2">{st.createdAt}</div>
            </div>
          ))}
        </div>
      ) : (
        <div className="px-5 py-4 text-center text-xs text-fg-faint">{t('subTasks.empty')}</div>
      )}
      <div className="px-5 py-2 border-t border-line-strong/30 text-[10px] text-fg-faint">
        {t('subTasks.gitIsolationInfo')}
      </div>
    </div>
  );
}

// ==========================================
// 评论面板
// ==========================================
interface RealComment {
  id: string;
  task_id: number;
  node_id?: string | null;
  source_node_id?: string | null;
  content: string;
  comment_type: string;
  author_type: string;
  author_id: string;
  created_at: string;
  parent_id?: string | null;
  mentions?: string[];
}

interface CommentPanelProps {
  comments: RealComment[];
  taskId: number;
  onAddComment: (taskId: number, text: string, mentions?: string[]) => void;
  agents?: MappedAgent[];
  loading?: boolean;
}

function CommentPanel({ comments = [], taskId, onAddComment, agents = [], loading }: CommentPanelProps) {
  const t = useTranslations("dashboard");
  const { dateTime } = useFormatter();
  const { canEdit } = usePermission();
  const [text, setText] = useState("");
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [replyText, setReplyText] = useState("");
  const [mentions, setMentions] = useState<string[]>([]);
  const [showAgentPicker, setShowAgentPicker] = useState(false);

  const handleSend = () => {
    if (!canEdit || !text.trim()) return;
    onAddComment(taskId, text.trim(), mentions);
    setText("");
    setMentions([]);
    setShowAgentPicker(false);
  };

  const toggleMention = (agentId: string) => {
    setMentions((prev) =>
      prev.includes(agentId) ? prev.filter((id) => id !== agentId) : [...prev, agentId]
    );
  };

  const handleChange = (val: string) => {
    setText(val);
  };

  if (loading) {
    return (
      <div className="border border-line-strong/60 rounded-xl bg-surface-2/30 p-5">
        <div className="flex items-center">
          <MessageSquare className="w-4 h-4 mr-2 text-fg-subtle" />
          <span className="text-sm font-medium text-fg-muted">{t('comments.header')}</span>
        </div>
        <div className="mt-3 space-y-2">
          <div className="skeleton h-4 w-3/4 rounded" />
          <div className="skeleton h-4 w-1/2 rounded" />
        </div>
      </div>
    );
  }

  return (
    <div className="border border-line-strong/60 rounded-xl bg-surface-2/30">
      <div className="px-5 py-3 border-b border-line-strong/60 flex items-center">
        <MessageSquare className="w-4 h-4 mr-2 text-fg-subtle" />
        <span className="text-sm font-medium text-fg-muted">{t('comments.headerWithCount', { count: comments.length })}</span>
      </div>

      {comments.length > 0 && (
        <div className="px-5 py-3 space-y-3 max-h-64 overflow-y-auto border-b border-line-strong/30">
          {comments.map((c) => {
            const isAgent = c.author_type === "agent";
            const authorName = isAgent ? `🤖 ${c.author_id?.slice(0, 8) || "Agent"}` : `👤 ${c.author_id?.slice(0, 8) || "User"}`;
            const time = c.created_at ? dateTime(new Date(c.created_at), { hour: "2-digit", minute: "2-digit" }) : "";
            return (
              <div key={c.id} className="bg-bg border border-line rounded-lg p-3">
                <div className="flex items-start gap-2">
                  <div className={`w-6 h-6 rounded-full flex items-center justify-center shrink-0 text-[10px] font-bold ${
                    isAgent ? "bg-primary/20 text-primary-fg" : "bg-surface-3 text-fg-muted"
                  }`}>
                    {isAgent ? "🤖" : "👤"}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`text-xs font-medium ${isAgent ? "text-primary-fg" : "text-fg-muted"}`}>{authorName}</span>
                      <span className="text-[10px] text-fg-faint">{time}</span>
                    </div>
                    <div className="text-sm text-fg-subtle mt-1 whitespace-pre-wrap">
                      <ReactMarkdown
                        components={{
                          p: ({ children }) => <span>{children}</span>,
                          text: ({ children }) => {
                            if (typeof children === "string") {
                              const parts = children.split(/(@[\w\-]+)/g);
                              return parts.map((part, i) =>
                                part.startsWith("@") ? (
                                  <span key={i} className="text-primary-fg font-medium">{part}</span>
                                ) : (
                                  part
                                )
                              );
                            }
                            return children;
                          },
                        }}
                      >
                        {c.content}
                      </ReactMarkdown>
                    </div>
                    <button
                      onClick={() => setReplyTo(c.id)}
                      className="text-[10px] text-primary-fg hover:text-primary-fg mt-1"
                    >
                      {t('comments.reply')}
                    </button>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <div className="px-5 py-3 relative">
        {mentions.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-2">
            {mentions.map((id) => {
              const agent = agents.find((a) => a.id === id);
              return (
                <span
                  key={id}
                  className="inline-flex items-center gap-1 px-2 py-0.5 bg-primary/10 border border-primary/30 rounded-full text-[10px] text-primary-fg"
                >
                  @{agent?.name || id.slice(0, 8)}
                  <button
                    onClick={() => toggleMention(id)}
                    className="hover:text-fg"
                    aria-label={t('comments.removeMention')}
                  >
                    <X className="w-3 h-3" />
                  </button>
                </span>
              );
            })}
          </div>
        )}
        <div className="flex gap-2">
          <input
            value={text}
            onChange={(e) => handleChange(e.target.value)}
            onKeyDown={(e) =>
              e.key === "Enter" && !e.shiftKey && (e.preventDefault(), handleSend())
            }
            disabled={!canEdit}
            placeholder={t('comments.inputPlaceholder')}
            className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2 text-sm text-fg-muted focus:outline-none focus:border-primary placeholder-fg-faint disabled:opacity-50 disabled:cursor-not-allowed"
          />
          <div className="relative">
            <button
              onClick={() => setShowAgentPicker(!showAgentPicker)}
              disabled={!canEdit}
              className={`px-2.5 py-2 rounded-lg border transition-colors-fast btn-press disabled:opacity-50 disabled:cursor-not-allowed ${
                mentions.length > 0
                  ? "bg-primary/15 border-primary/40 text-primary-fg"
                  : "bg-surface-2 border-line-strong text-fg-subtle hover:text-fg"
              }`}
              title={t('comments.mentionAgent')}
            >
              <AtSign className="w-4 h-4" />
            </button>
            {showAgentPicker && (
              <>
                <ModalPortal>
                  <div className="fixed inset-0 z-40" onClick={() => setShowAgentPicker(false)} />
                </ModalPortal>
                <div className="absolute right-0 bottom-full mb-1 w-56 bg-surface-2 border border-line-strong rounded-xl shadow-xl z-50 overflow-hidden animate-scale-in">
                  <div className="px-3 py-2 text-[10px] text-fg-faint border-b border-line-strong/60">
                    {t('comments.mentionAgentTooltip')}
                  </div>
                  <div className="max-h-48 overflow-y-auto">
                    {agents.length === 0 ? (
                      <div className="px-3 py-4 text-center text-xs text-fg-faint">{t('comments.noAgents')}</div>
                    ) : (
                      agents.map((a) => {
                        const selected = mentions.includes(a.id);
                        return (
                          <button
                            key={a.id}
                            onClick={() => toggleMention(a.id)}
                            className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                              selected
                                ? "bg-primary/10 text-primary-fg"
                                : "text-fg-muted hover:bg-surface-3/50"
                            }`}
                          >
                            <Cpu className="w-3.5 h-3.5 shrink-0" />
                            <span className="truncate flex-1">{a.name}</span>
                            {selected && <CheckCircle2 className="w-3.5 h-3.5 text-primary-fg shrink-0" />}
                          </button>
                        );
                      })
                    )}
                  </div>
                </div>
              </>
            )}
          </div>
          <button
            onClick={handleSend}
            disabled={!canEdit || !text.trim()}
            className="px-3 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg disabled:opacity-50 disabled:cursor-not-allowed transition-colors-fast btn-press"
          >
            <Send className="w-4 h-4" />
          </button>
        </div>
        {replyTo && (
          <div className="flex items-center gap-2 mt-2 pl-4 border-l-2 border-primary/30">
            <span className="text-xs text-primary-fg">{t('comments.replying')}</span>
            <input
              value={replyText}
              onChange={(e) => setReplyText(e.target.value)}
              className="flex-1 bg-bg border border-line-strong rounded px-3 py-1.5 text-sm text-fg focus:outline-none focus:border-primary"
              placeholder={t('comments.replyPlaceholder')}
              onKeyDown={(e) => {
                if (e.key === "Enter" && replyText.trim()) {
                  onAddComment(taskId, replyText.trim());
                  setReplyText("");
                  setReplyTo(null);
                }
              }}
            />
            <button onClick={() => setReplyTo(null)} className="text-fg-faint hover:text-fg">
              <X className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

// ==========================================
// 节点总结面板
// ==========================================
interface NodeSummaryPanelProps {
  summary?: string;
  nodeName: string;
}

function NodeSummaryPanel({ summary, nodeName }: NodeSummaryPanelProps) {
  const t = useTranslations("dashboard");
  if (!summary) return null;
  return (
    <div className="border border-line-strong/60 rounded-xl bg-surface-2/30 p-5">
      <div className="flex items-center mb-3">
        <Bot className="w-4 h-4 mr-2 text-primary-fg" />
        <span className="text-sm font-medium text-fg-muted">{t('nodeSummary.header', { nodeName })}</span>
      </div>
      <div className="text-sm text-fg-subtle whitespace-pre-wrap leading-relaxed">
        {summary}
      </div>
    </div>
  );
}

// ==========================================
// 数据传递面板
// ==========================================
interface NodeDataFlowPanelProps {
  task: MappedTask;
  transitions: NodeTransition[];
  loading: boolean;
}

function NodeDataFlowPanel({ task, transitions, loading }: NodeDataFlowPanelProps) {
  const t = useTranslations("dashboard");
  const { dateTime } = useFormatter();
  if (loading) {
    return (
      <div className="border border-line-strong/60 rounded-xl bg-surface-2/30 p-5">
        <div className="flex items-center">
          <History className="w-4 h-4 mr-2 text-fg-subtle" />
          <span className="text-sm font-medium text-fg-muted">{t('dataFlow.header')}</span>
        </div>
        <div className="mt-3 space-y-2">
          <div className="skeleton h-4 w-3/4 rounded" />
          <div className="skeleton h-4 w-1/2 rounded" />
        </div>
      </div>
    );
  }

  if (transitions.length === 0 && !task.gitBranch) return null;

  const actionLabels: Record<string, { label: string; cls: string; icon: string }> = {
    approve: { label: t('dataFlow.action.approve'), cls: "text-success-fg bg-success/10", icon: "✅" },
    reject: { label: t('dataFlow.action.reject'), cls: "text-orange-400 bg-orange-500/10", icon: "❌" },
    manual: { label: t('dataFlow.action.manual'), cls: "text-danger-fg bg-danger/10", icon: "🚩" },
    reclaim: { label: t('dataFlow.action.reclaim'), cls: "text-primary-fg bg-primary/10", icon: "🔄" },
    timeout: { label: t('dataFlow.action.timeout'), cls: "text-warning-fg bg-warning/10", icon: "⏰" },
  };

  // 将节点 ID 映射为名称
  const rawNodes = (task._rawNodes || []) as { id: string; name: string; sort_order: number; status: string }[];
  const nodeMap: Record<string, string> = {};
  rawNodes.forEach((n) => { nodeMap[n.id] = n.name; });

  // 按 ID 查找节点索引
  const nodeIdxMap: Record<string, number> = {};
  rawNodes.forEach((n, i) => { nodeIdxMap[n.id] = i + 1; });

  return (
    <div className="border border-line-strong/60 rounded-xl bg-surface-2/30 p-5">
      <div className="flex items-center mb-4">
        <History className="w-4 h-4 mr-2 text-fg-subtle" />
        <span className="text-sm font-medium text-fg-muted">{t('dataFlow.header')}</span>
      </div>

      {/* 节点流转时间线 */}
      {transitions.length > 0 && (
        <div className="space-y-3 mb-4">
          <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">{t('dataFlow.transitionTitle')}</div>
          {transitions.map((tr, i) => {
            const act = actionLabels[tr.action] || { label: tr.action, cls: "text-fg-subtle bg-fg-faint/10", icon: "•" };
            const nodeName = nodeMap[tr.taskNodeId] || t('dataFlow.nodeFallback', { index: nodeIdxMap[tr.taskNodeId] || "?" });
            const targetName = tr.targetNodeId ? (nodeMap[tr.targetNodeId] || t('dataFlow.nodeFallback', { index: nodeIdxMap[tr.targetNodeId] || "?" })) : null;
            const time = tr.createdAt ? dateTime(new Date(tr.createdAt), { hour: "2-digit", minute: "2-digit" }) : "";
            return (
              <div key={tr.id || i} className="flex gap-3">
                <div className="flex flex-col items-center">
                  <div className={`w-3 h-3 rounded-full border-2 ${
                    tr.action === "approve" ? "border-success bg-success/30"
                    : tr.action === "reject" ? "border-orange-500 bg-orange-500/30"
                    : "border-danger bg-danger/30"
                  }`} />
                  {i < transitions.length - 1 && <div className="w-0.5 flex-1 bg-surface-3 my-1" />}
                </div>
                <div className="flex-1 pb-3">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-fg-muted">{nodeName}</span>
                    <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-medium ${act.cls}`}>
                      {act.icon} {act.label}
                    </span>
                    {targetName && (
                      <>
                        <ArrowRight className="w-3 h-3 text-fg-faint" />
                        <span className="text-sm text-fg-muted">{targetName}</span>
                      </>
                    )}
                    <span className="text-[10px] text-fg-faint">{time}</span>
                  </div>
                  {tr.comment && <div className="text-xs text-fg-subtle mt-1">{tr.comment}</div>}
                  <div className="text-[10px] text-fg-faint mt-0.5">
                    {tr.operatorType === "agent" ? "🤖 Agent" : "👤 Human"}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Git 代码传递 */}
      {task.gitBranch && (
        <div className="mb-4">
          <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">{t('dataFlow.gitTransfer')}</div>
          <div className="flex items-center gap-2 text-sm text-primary-fg font-mono">
            <GitBranch className="w-3.5 h-3.5" />
            <span className="truncate">{task.gitBranch}</span>
          </div>
          <div className="flex flex-wrap gap-2 mt-2">
            {rawNodes.map((n, i) => {
              const isCompleted = n.status === "completed";
              const isCurrent = n.status === "in_progress";
              return (
                <span
                  key={n.id}
                  className={`text-[10px] px-2 py-0.5 rounded-full border ${
                    isCompleted ? "border-success/30 text-success-fg bg-success/5"
                    : isCurrent ? "border-primary/30 text-primary-fg bg-primary/5"
                    : "border-line-strong text-fg-faint"
                  }`}
                >
                  node-{i + 1}-start {isCompleted ? "✓" : isCurrent ? "..." : ""}
                </span>
              );
            })}
          </div>
        </div>
      )}

      {/* 续作权 */}
      {task.reservedForAgent && (
        <div>
          <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">{t('dataFlow.continuationRight')}</div>
          <div className="flex items-center text-xs text-accent-fg bg-accent/10 p-2 rounded border border-accent/30">
            <Lock className="w-3.5 h-3.5 mr-1.5 shrink-0" />
            <span>{t('dataFlow.reservedFor')} <span className="font-semibold text-accent-fg">{task.reservedForAgent}</span></span>
          </div>
        </div>
      )}
    </div>
  );
}

// ==========================================
// 认领按钮 — 用于 pending 节点
// ==========================================
interface ClaimButtonProps {
  task: MappedTask;
  nodeIndex: number;
  onClaim: (taskId: number, nodeIndex: number) => void;
}

function ClaimButton({ task, nodeIndex, onClaim }: ClaimButtonProps) {
  const t = useTranslations("dashboard");
  const { canEdit } = usePermission();
  if (task.nodesStatus[nodeIndex] !== "pending") return null;
  if (
    task.reservedForAgent &&
    task.reservationExpiresAt &&
    new Date(task.reservationExpiresAt).getTime() > Date.now()
  ) {
    const remaining = Math.max(
      0,
      Math.floor((new Date(task.reservationExpiresAt).getTime() - Date.now()) / 1000)
    );
    return (
      <div className="absolute -top-2 right-2 px-2 py-0.5 bg-accent/80 text-fg text-[10px] rounded-full font-bold flex items-center gap-1">
        <Lock className="w-2.5 h-2.5" /> {task.reservedForAgent} ({remaining}s)
      </div>
    );
  }
  return (
    <button
      onClick={() => onClaim(task.id, nodeIndex)}
      disabled={!canEdit}
      className="absolute -top-2 right-2 px-2 py-0.5 bg-accent/80 hover:bg-accent text-fg text-[10px] rounded-full font-bold transition-colors-fast shadow-lg shadow-accent/30 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
    >
      {t('claimButton.claimNode')}
    </button>
  );
}

// ==========================================
// 任务详情底部面板
// ==========================================
// dirsOf 将服务端节点返回的目录权限（JSON 数组或逗号分隔字符串）归一化为字符串数组。
function dirsOf(value: unknown): string[] {
  if (Array.isArray(value)) return value.map(String).filter(Boolean);
  if (typeof value === "string" && value.trim()) {
    return value
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
  }
  return [];
}

interface WorkflowDetailsPanelProps {
  task: MappedTask;
  onClose: () => void;
  onApprove: (taskId: number) => void;
  onReject: (taskId: number, targetNode: number, reason: string) => void;
  onManual: (taskId: number, nodeId: string, action: string, agentId?: string) => void;
  onAddComment: (taskId: number, text: string, mentions?: string[]) => void;
  onClaim: (taskId: number, nodeIndex: number) => void;
  onEditTask: (taskId: number, updates: { title: string; description: string; priority: string }) => void;
  onAddReviewRecord: (taskId: number, record: { action: string; reviewer: string; time: string; comment?: string; targetNode?: number }) => void;
  onAddSubTask: (parentId: number, subTask: SubTask) => void;
  onDeleteTask: (taskId: number) => void;
  agents?: MappedAgent[];
}

function WorkflowDetailsPanel({
  task,
  onClose,
  onApprove,
  onReject,
  onManual,
  onAddComment,
  onClaim,
  onEditTask,
  onAddReviewRecord,
  onAddSubTask,
  onDeleteTask,
  agents,
}: WorkflowDetailsPanelProps) {
  const t = useTranslations("dashboard");
  const { dateTime } = useFormatter();
  const { canEdit } = usePermission();
  const [activeTerminalNode, setActiveTerminalNode] = useState(task.currentNode);
  const [userSelectedNode, setUserSelectedNode] = useState(false);
  const terminalRef = useRef<HTMLDivElement>(null);
  const [showRejectDialog, setShowRejectDialog] = useState(false);
  const [showEditDialog, setShowEditDialog] = useState(false);
  const [showReassignDialog, setShowReassignDialog] = useState(false);
  const [nodeTransitions, setNodeTransitions] = useState<NodeTransition[]>([]);
  const [transitionsLoading, setTransitionsLoading] = useState(false);
  const [realComments, setRealComments] = useState<RealComment[]>([]);
  const [commentsLoading, setCommentsLoading] = useState(false);

  // 自动跟随当前执行节点，除非用户手动选择了其他节点
  useEffect(() => {
    if (!userSelectedNode) {
      setActiveTerminalNode(task.currentNode);
    }
  }, [task.currentNode, userSelectedNode]);

  const handleNodeClick = useCallback((index: number) => {
    setUserSelectedNode(index !== task.currentNode);
    setActiveTerminalNode(index);
  }, [task.currentNode]);

  // 根据当前选中的终端节点推导 activeNodeId
  const activeNodeId = useMemo(() => {
    const rawNodes = (task._rawNodes || []) as { id: string }[];
    return rawNodes[activeTerminalNode]?.id || null;
  }, [task._rawNodes, activeTerminalNode]);

  // 连接 WebSocket 获取实时日志 — 仅针对当前活动节点
  useTaskLogs(task.id, activeNodeId);
  const taskLogs = useAppStore((s) => s.taskLogs[task.id]);

  // 构建 node_id 到节点索引的映射，用于日志查询
  const nodeIdToIndex = useMemo(() => {
    const map: Record<string, number> = {};
    const rawNodes = (task._rawNodes || []) as { id: string }[];
    rawNodes.forEach((n, i) => { map[n.id] = i; });
    return map;
  }, [task._rawNodes]);

  // 将 taskLogs（以 node_id 为键）转换为按节点索引的日志
  const logsByIndex = useMemo(() => {
    const result: Record<number, string[]> = {};
    if (!taskLogs) return result;
    for (const [nodeId, lines] of Object.entries(taskLogs)) {
      const idx = nodeIdToIndex[nodeId];
      if (idx !== undefined) {
        result[idx] = lines;
      } else if (nodeId === "_default") {
        // 兜底：若没有 node_id 则归入当前节点
        result[task.currentNode] = lines;
      }
    }
    return result;
  }, [taskLogs, nodeIdToIndex, task.currentNode]);

  // 有新日志时自动将终端滚动到底部
  const currentLogsLength = (logsByIndex[activeTerminalNode] || []).length;
  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [currentLogsLength]);

  // 获取节点流转记录
  // 使用序列化后的节点 ID 作为依赖，避免因对象引用变化而重复请求
  const rawNodeIds = useMemo(() => {
    if (!task._rawNodes) return "";
    const nodes = task._rawNodes as { id: string }[];
    return nodes.map((n) => n.id).sort().join(",");
  }, [task._rawNodes]);

  useEffect(() => {
    if (!task.id || !rawNodeIds) return;
    const nodeIds = rawNodeIds.split(",");
    if (nodeIds.length === 0 || (nodeIds.length === 1 && nodeIds[0] === "")) return;
    setTransitionsLoading(true);
    Promise.all(
      nodeIds.map((nid) => api.getNodeTransitions(task.id, nid).catch(() => []))
    ).then((results) => {
      const all = results.flat() as NodeTransition[];
      all.sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime());
      setNodeTransitions(all);
      setTransitionsLoading(false);
    }).catch(() => {
      setTransitionsLoading(false);
    });
  }, [task.id, rawNodeIds]);

  // 获取真实评论
  useEffect(() => {
    if (!task.id) return;
    setCommentsLoading(true);
    api.listComments(task.id).then((data) => {
      setRealComments((data as RealComment[]) || []);
      setCommentsLoading(false);
    }).catch(() => {
      setRealComments([]);
      setCommentsLoading(false);
    });
  }, [task.id]);

  const isCompleted = task.priorityState === "completed";
  const isInProgress = task.priorityState === "in_progress";
  const isInterrupted = task.interrupted;
  const isCancelled = task.priorityState === "cancelled";
  // 当前节点的实际状态（比任务级 priorityState 更准确，避免数据滞后时误显操作按钮）
  const currentNodeStatus = (task._rawNodes as Array<{ status?: string }> | undefined)?.[activeTerminalNode]?.status;
  const nodeInProgress = currentNodeStatus === "in_progress";
  const nodeNames = task.nodeNames || [];
  const isOverdue =
    task.dueDate &&
    new Date(task.dueDate) < new Date() &&
    task.priorityState !== "completed" &&
    task.priorityState !== "cancelled";

  const handleRejectConfirm = (targetNode: number, reason: string) => {
    onReject(task.id, targetNode, reason);
    onAddReviewRecord(task.id, {
      action: "reject",
      reviewer: "Admin User",
      time: dateTime(new Date(), { hour: "2-digit", minute: "2-digit" }),
      comment: reason,
      targetNode,
    });
    setShowRejectDialog(false);
  };

  const handleDoApprove = () => {
    onApprove(task.id);
    onAddReviewRecord(task.id, {
      action: "approve",
      reviewer: "Admin User",
      time: dateTime(new Date(), { hour: "2-digit", minute: "2-digit" }),
      comment: t('reviewComment.nodeApproved'),
    });
  };

  const handleDoManual = (action: string, agentId?: string) => {
    if (action === "cancel") {
      onManual(task.id, "__cancel_task__", action);
      return;
    }
    const currentNodeIdx = task.currentNode;
    const rawNode = (task._rawNodes as { id?: string }[])?.[currentNodeIdx];
    const nodeId = rawNode?.id;
    if (!nodeId) {
      return;
    }
    onManual(task.id, nodeId, action, agentId);
    if (action === "interrupt" || action === "manual_intervention") {
      onAddReviewRecord(task.id, {
        action: "manual",
        reviewer: "Admin User",
        time: dateTime(new Date(), { hour: "2-digit", minute: "2-digit" }),
        comment: action === "interrupt" ? t('reviewComment.adminInterrupt') : t('reviewComment.markManual'),
      });
    }
  };

  const badgeMap: Record<string, string> = {
    completed: "bg-success/10 text-success-fg border-success/30",
    cancelled: "bg-fg-faint/10 text-fg-subtle border-line-strong/30",
    manual_intervention: "bg-danger/10 text-danger-fg border-danger/30",
    rejected: "bg-orange-500/10 text-orange-400 border-orange-500/30",
    in_progress: "bg-primary/10 text-primary-fg border-primary/30",
  };
  const badgeLabelMap: Record<string, string> = {
    completed: t('status.completed'),
    cancelled: t('status.cancelled'),
    manual_intervention: t('status.manualIntervention'),
    rejected: t('status.rejected'),
    in_progress: t('status.inProgress'),
  };

  const totalNodes = nodeNames.length;
  const completedCount = task.nodesStatus.filter((s) => s === "completed").length;
  // 画布节点:与 nodeNames/nodesStatus 同序(数组下标对齐)
  const rawFlowNodes = (task._rawNodes || []) as RunFlowNode[];
  const pendingClaimNodes = rawFlowNodes
    .map((node, index) => ({ node, index }))
    .filter(({ index }) => (task.nodesStatus || [])[index] === "pending");

  return (
    <div className="absolute bottom-0 left-0 right-0 max-h-[85vh] flex flex-col bg-surface/95 backdrop-blur-xl border-t border-line-strong panel-shadow z-50 animate-fade-in-up">
      {/* 页头 */}
      <div className="p-4 border-b border-line-strong/60 flex justify-between items-center bg-surface-2/60 backdrop-blur-sm shrink-0">
        <div className="flex items-center gap-3 min-w-0 flex-wrap">
          <span className="text-primary-fg font-mono text-sm bg-primary-fg/10 px-2 py-1 rounded shrink-0">
            {task.taskRef}
          </span>
          <h2 className="text-lg font-semibold text-fg truncate">{task.title}</h2>
          <span
            className={`text-xs px-2 py-0.5 rounded-full border font-medium shrink-0 ${badgeMap[task.priorityState] || "bg-fg-faint/10 text-fg-subtle"}`}
          >
            {badgeLabelMap[task.priorityState]}
          </span>
          <span className="text-xs text-fg-faint bg-surface-2 px-2 py-0.5 rounded font-mono shrink-0">
            {task.type?.toUpperCase() || "TASK"}
          </span>
          <span className="text-xs text-fg-faint">
            {completedCount}/{totalNodes}
          </span>
          {/* 截止日期 */}
          {task.dueDate && (
            <span
              className={`text-xs flex items-center gap-1 ${isOverdue ? "text-danger-fg bg-danger/10 px-2 py-0.5 rounded" : "text-fg-subtle"}`}
            >
              <Calendar className="w-3 h-3" />
              {task.dueDate}
              {isOverdue ? ` ${t('overdue')}` : ""}
            </span>
          )}
          {/* 退回计数 */}
          {task.rejectCount > 0 && (
            <span className="text-xs text-orange-400 bg-orange-500/10 px-2 py-0.5 rounded border border-orange-500/20">
              <AlertOctagon className="w-3 h-3 inline mr-0.5" />
              {t('rejectCount', { count: task.rejectCount })}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {task.priorityState !== "completed" && task.priorityState !== "cancelled" && (
            <>
              <button
                onClick={() => setShowEditDialog(true)}
                disabled={!canEdit}
                className="p-1.5 hover:bg-surface-3 rounded text-fg-subtle hover:text-primary-fg transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <Edit2 className="w-4 h-4" />
              </button>
              {!isInterrupted && (
                <button
                  onClick={() => handleDoManual("cancel")}
                  disabled={!canEdit}
                  className="px-3 py-1.5 text-xs bg-danger/10 hover:bg-danger/20 text-danger-fg border border-danger/30 rounded-lg transition-colors-fast flex items-center disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  <Ban className="w-3.5 h-3.5 mr-1" />
                  {t('actions.cancelTask')}
                </button>
              )}
            </>
          )}
          {(task.priorityState === "completed" || isCancelled) && onDeleteTask && (
            <button
              onClick={() => {
                onDeleteTask(task.id);
                onClose();
              }}
              disabled={!canEdit}
              className="px-3 py-1.5 text-xs bg-danger/10 hover:bg-danger/20 text-danger-fg border border-danger/30 rounded-lg transition-colors-fast flex items-center disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Trash2 className="w-3.5 h-3.5 mr-1" />
              {t('actions.deleteTask')}
            </button>
          )}
          <button onClick={onClose} className="p-1 hover:bg-surface-3 rounded text-fg-subtle hover:text-fg">
            <XCircle className="w-6 h-6" />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="grid grid-cols-3 gap-6 h-full">
          {/* ============ 左列 (col-span-2) ============ */}
          <div className="col-span-2 space-y-6">
            {/* 子任务标记 */}
            {task.parentTask && (
              <div className="bg-accent/5 border border-accent/20 rounded-xl p-3 text-xs text-accent-fg flex items-center">
                <Bot className="w-3.5 h-3.5 mr-2" /> {t('subTasks.parentInfo', { parentTask: task.parentTask })}
              </div>
            )}

            {/* 工作流进度(画布:并发链路/当前节点一目了然,替换原一维进度条) */}
            <div>
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-4 flex items-center justify-between">
                <span>{t('workflow.progress')}</span>
                <div className="w-1/3 h-1.5 bg-surface-2 rounded-full overflow-hidden shrink-0 ml-4">
                  <div
                    className="h-full bg-gradient-to-r from-primary to-success rounded-full transition-all"
                    style={{ width: `${(completedCount / totalNodes) * 100}%` }}
                  />
                </div>
              </div>
              <RunFlowCanvas
                nodes={rawFlowNodes}
                currentIndex={task.currentNode}
                activeIndex={activeTerminalNode}
                onNodeClick={handleNodeClick}
              />
              {/* 待认领节点(保留原进度条的认领入口) */}
              {pendingClaimNodes.length > 0 && (
                <div className="flex items-center gap-2 flex-wrap mt-2">
                  {pendingClaimNodes.map(({ node, index }) => (
                    <div
                      key={node.id}
                      className="flex items-center gap-2 text-xs text-fg-subtle bg-surface-2/40 border border-line-strong/50 rounded-md px-2.5 py-1.5"
                    >
                      <span>{node.name}</span>
                      <ClaimButton task={task} nodeIndex={index} onClaim={onClaim} />
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* 人工介入原因提示 */}
            {task.interrupted && (() => {
              const manualTransitions = nodeTransitions.filter(
                (tr) => tr.toStatus === "manual_intervention" && tr.comment
              );
              if (manualTransitions.length === 0) return null;
              const latest = manualTransitions[manualTransitions.length - 1];
              const rawNodes = (task._rawNodes || []) as { id: string; name: string }[];
              const nodeName = rawNodes.find((n) => n.id === latest.taskNodeId)?.name || "";
              return (
                <div className="bg-danger/10 border border-danger/30 rounded-xl p-4">
                  <div className="flex items-center gap-2 mb-2">
                    <ShieldAlert className="w-4 h-4 text-danger-fg" />
                    <span className="text-sm font-semibold text-danger-fg">{t('manualIntervention.title')}</span>
                    {nodeName && <span className="text-xs text-danger-fg/70">— {nodeName}</span>}
                  </div>
                  <div className="text-sm text-danger-fg whitespace-pre-wrap leading-relaxed">
                    {latest.comment}
                  </div>
                </div>
              );
            })()}

            {/* 完整描述 */}
            {task.description && (
              <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4">
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">
                  {t('taskDescription')}
                </div>
                <div className="text-sm text-fg-muted whitespace-pre-wrap leading-relaxed">
                  {task.description}
                </div>
              </div>
            )}

            {/* 标签 */}
            {task.labels && task.labels.length > 0 && (
              <div className="flex items-center gap-2 flex-wrap">
                <Tag className="w-4 h-4 text-fg-subtle" />
                {task.labels.map((l: string) => (
                  <span
                    key={l}
                    className="text-xs px-2 py-1 bg-surface-3/50 text-fg-muted rounded-full border border-line-strong/50"
                  >
                    {l}
                  </span>
                ))}
              </div>
            )}

            {/* 终端日志 */}
            <div className="border border-line-strong/80 rounded-xl overflow-hidden bg-bg shadow-2xl">
              <div className="bg-surface-2 px-4 py-2.5 border-b border-line/80 flex items-center select-none">
                <div className="flex items-center space-x-2 w-16">
                  <div className="w-3 h-3 rounded-full bg-[#ff5f56] border border-[#e0443e] shadow-sm" />
                  <div className="w-3 h-3 rounded-full bg-[#ffbd2e] border border-[#dea123] shadow-sm" />
                  <div className="w-3 h-3 rounded-full bg-[#27c93f] border border-[#1aab29] shadow-sm" />
                </div>
                <div className="flex items-center flex-1 justify-center">
                  <Terminal className="w-3.5 h-3.5 text-fg-faint mr-2" />
                  <span className="text-xs font-medium text-fg-subtle font-mono">
                    bash — {nodeNames[activeTerminalNode] || "terminal"}
                  </span>
                </div>
                <span className="text-[10px] text-fg-faint font-mono w-16 text-right">
                  {t('terminal.nodeLabel', { index: activeTerminalNode + 1 })}
                </span>
              </div>
              <div ref={terminalRef} className="p-5 h-52 overflow-y-auto font-mono text-[13px] leading-relaxed relative selection:bg-primary/30">
                {(logsByIndex[activeTerminalNode] || [
                  `> ${t('terminal.noLogs')}`,
                ]).map((log: string, i: number) => {
                  let cls = "text-fg-muted";
                  let prompt = false;
                  if (log.startsWith("✖")) {
                    cls = "text-danger-fg font-medium";
                  } else if (log.startsWith("✔")) {
                    cls = "text-success-fg font-medium";
                  } else if (log.startsWith(">")) {
                    cls = "text-primary-fg";
                    prompt = true;
                  } else if (log.includes("added") || log.includes("audited")) {
                    cls = "text-fg-faint";
                  }
                  return (
                    <div key={i} className={`${cls} mb-1.5 flex items-start`}>
                      {prompt ? (
                        <span className="text-fuchsia-500 mr-3 shrink-0 select-none">❯</span>
                      ) : (
                        <span className="w-5 shrink-0" />
                      )}
                      <span className="whitespace-pre-wrap">{log.replace(/^>\s*/, "")}</span>
                    </div>
                  );
                })}
                {task.currentNode === activeTerminalNode && task.priorityState === "in_progress" && (
                  <div className="flex items-center text-success/80 mt-3 font-medium">
                    <span className="w-5 shrink-0 text-fuchsia-500">❯</span>
                    <span className="w-2.5 h-4 bg-success/80 animate-pulse cursor-blink" />
                  </div>
                )}
              </div>
            </div>

            {/* 节点总结 */}
            <NodeSummaryPanel
              summary={activeNodeId ? task.nodeSummaries?.[activeNodeId] : undefined}
              nodeName={nodeNames[activeTerminalNode] || ""}
            />

            {/* 数据传递 */}
            <NodeDataFlowPanel task={task} transitions={nodeTransitions} loading={transitionsLoading} />

            {/* 子任务面板 */}
            <SubTaskPanel subTasks={task.subTasks as SubTask[]} parentId={task.id} onAddSubTask={onAddSubTask} />

            {/* 评论 */}
            <CommentPanel comments={realComments} taskId={task.id} onAddComment={onAddComment} agents={agents} loading={commentsLoading} />
          </div>

          {/* ============ 右列 (col-span-1) ============ */}
          <div className="col-span-1 space-y-4">
            {/* 当前节点操作面板 */}
            <div className="bg-bg/80 p-4 rounded-xl border border-line">
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3">
                {t('nodeActions.title')}
              </div>
              <div className="space-y-2">
                <div className="text-sm text-fg-muted flex items-center">
                  <span className="text-fg-subtle">{t('nodeActions.status')}</span>
                  <span
                    className={`ml-2 text-xs px-2 py-0.5 rounded-full border font-medium ${badgeMap[task.priorityState] || "bg-fg-faint/10 text-fg-subtle"}`}
                  >
                    {badgeLabelMap[task.priorityState]}
                  </span>
                </div>
                <div className="text-sm text-fg-muted">{task.message}</div>
                {(() => {
                  const nodes = (task._rawNodes || []) as Array<{
                    readonly_dirs?: unknown;
                    full_control_dirs?: unknown;
                  }>;
                  const node = nodes[activeTerminalNode];
                  const roDirs = dirsOf(node?.readonly_dirs);
                  const fcDirs = dirsOf(node?.full_control_dirs);
                  if (roDirs.length === 0 && fcDirs.length === 0) return null;
                  return (
                    <div className="mt-2 space-y-1 text-[11px]">
                      {roDirs.length > 0 && (
                        <div className="text-warning-fg/90">
                          <Lock className="w-3 h-3 inline mr-1" />
                          {t('nodeActions.readonlyDirs')} {roDirs.join(", ")}
                        </div>
                      )}
                      {fcDirs.length > 0 && (
                        <div className="text-success-fg/90">
                          <Cpu className="w-3 h-3 inline mr-1" />
                          {t('nodeActions.fullControl')} {fcDirs.join(", ")}
                        </div>
                      )}
                    </div>
                  );
                })()}
                {task.nodeAgents?.[task.currentNode] && (
                  <div className="flex items-center text-xs text-primary-fg mt-1">
                    <Bot className="w-3 h-3 mr-1.5" /> {t('nodeActions.executor')} {task.nodeAgents[task.currentNode]}
                  </div>
                )}
                {!isCompleted && !isCancelled && (
                  <div className="flex flex-col gap-2 pt-3 border-t border-line mt-3">
                    {isInterrupted ? (
                      <>
                        <button
                          onClick={() => handleDoManual("resume")}
                          disabled={!canEdit}
                          className="flex items-center justify-center px-4 py-2 bg-success-strong/20 hover:bg-success-strong/30 text-success-fg rounded-lg text-sm font-medium border border-success/30 transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          <Activity className="w-4 h-4 mr-2" /> {t('actions.resume')}
                        </button>
                        <button
                          onClick={() => setShowReassignDialog(true)}
                          disabled={!canEdit}
                          className="flex items-center justify-center px-4 py-2 bg-accent-strong/20 hover:bg-accent-strong/30 text-accent-fg rounded-lg text-sm font-medium border border-accent/30 transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          <ArrowRight className="w-4 h-4 mr-2" /> {t('actions.reassign')}
                        </button>
                        <button
                          onClick={() => handleDoManual("cancel")}
                          disabled={!canEdit}
                          className="flex items-center justify-center px-4 py-2 bg-danger-strong/20 hover:bg-danger-strong/30 text-danger-fg rounded-lg text-sm font-medium border border-danger/30 transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          <XCircle className="w-4 h-4 mr-2" /> {t('actions.cancelTask')}
                        </button>
                      </>
                    ) : (
                      <>
                        {isInProgress || task.priorityState === "rejected" ? (
                          <>
                            <button
                              onClick={handleDoApprove}
                              disabled={!canEdit}
                              className="flex items-center justify-center px-4 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium shadow-lg shadow-primary/20 transition-colors-fast btn-press disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                              <CheckCircle2 className="w-4 h-4 mr-2" /> {t('actions.approve')}
                            </button>
                            <button
                              onClick={() => setShowRejectDialog(true)}
                              disabled={!canEdit}
                              className="flex items-center justify-center px-4 py-2 bg-surface-2 hover:bg-surface-3 text-orange-400 rounded-lg text-sm font-medium border border-line-strong transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                            >
                              <ArrowRight className="w-4 h-4 mr-2 rotate-180" /> {t('actions.rejectTo')}
                            </button>
                            {nodeInProgress && (
                              <button
                                onClick={() => handleDoManual("manual_intervention")}
                                disabled={!canEdit}
                                className="flex items-center justify-center px-4 py-2 bg-surface-2 hover:bg-warning-strong/50 text-warning-fg rounded-lg text-sm font-medium border border-line-strong hover:border-warning/50 transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                              >
                                <Pause className="w-4 h-4 mr-2" /> {t('actions.manualIntervention')}
                              </button>
                            )}
                            {nodeInProgress && (
                              <button
                                onClick={() => handleDoManual("interrupt")}
                                disabled={!canEdit}
                                className="flex items-center justify-center px-4 py-2 bg-surface-2 hover:bg-danger-strong/50 text-danger-fg rounded-lg text-sm font-medium border border-line-strong hover:border-danger/50 transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                              >
                                <Ban className="w-3.5 h-3.5 mr-2" /> {t('actions.interrupt')}
                              </button>
                            )}
                          </>
                        ) : (
                          <button
                            onClick={() => handleDoManual("manual_intervention")}
                            disabled={!canEdit}
                            className="flex items-center justify-center px-4 py-2 bg-surface-2 hover:bg-warning-strong/50 text-warning-fg rounded-lg text-sm font-medium border border-line-strong hover:border-warning/50 transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed"
                          >
                            <AlertCircle className="w-4 h-4 mr-2" /> {t('actions.markManual')}
                          </button>
                        )}
                      </>
                    )}
                  </div>
                )}
              </div>
            </div>

            {/* 属性面板 */}
            <div className="bg-surface-2/30 border border-line-strong/60 rounded-xl p-4">
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3">{t('attributes.title')}</div>
              <div className="space-y-2.5 text-xs">
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t('attributes.type')}</span>
                  <span className="text-fg font-mono">{task.type || "story"}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t('attributes.priority')}</span>
                  <span
                    className={`font-medium ${
                      (task.priority as string) === "urgent"
                        ? "text-orange-400"
                        : task.priority === "high"
                          ? "text-warning-fg"
                          : task.priority === "medium"
                            ? "text-yellow-400" : "text-fg-subtle"
                    }`}
                  >
                    {task.priority || "medium"}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t('attributes.workflow')}</span>
                  <span className="text-fg-muted text-right max-w-[120px] truncate">
                    {task.workflowName || "-"}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t('attributes.dueDate')}</span>
                  <span className={`${isOverdue ? "text-danger-fg" : "text-fg-muted"}`}>
                    {task.dueDate || "-"}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t('attributes.labels')}</span>
                  <span className="text-fg-muted">{t('attributes.labelCount', { count: task.labels?.length || 0 })}</span>
                </div>
                {task.rejectCount > 0 && (
                  <div className="flex justify-between text-orange-400">
                    <span>{t('attributes.rejectCount')}</span>
                    <span>{t('attributes.rejectCountValue', { count: task.rejectCount })}</span>
                  </div>
                )}
                <div className="flex justify-between">
                  <span className="text-fg-subtle">{t('attributes.progress')}</span>
                  <span className="text-fg-muted">
                    {completedCount}/{totalNodes}
                  </span>
                </div>
              </div>
            </div>

            {/* Git 分支信息 */}
            {task.gitBranch && (
              <div className="bg-surface-2/30 border border-line-strong/50 rounded-xl p-4">
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2 flex items-center">
                  <GitBranch className="w-3.5 h-3.5 mr-1 text-fg-subtle" /> {t('gitBranch.title')}
                </div>
                <div className="text-sm text-primary-fg font-mono truncate">{task.gitBranch}</div>
                <div className="text-[10px] text-fg-faint mt-1">
                  {t('gitBranch.baseline', { ref: task.taskRef, index: task.currentNode })}
                </div>
              </div>
            )}

            {/* 约束与警告 */}
            {task.constraints && (
              <div className="bg-warning/5 border border-warning/20 rounded-xl p-4">
                <div className="text-xs font-semibold text-warning-fg uppercase tracking-wider mb-2 flex items-center">
                  <AlertTriangle className="w-3.5 h-3.5 mr-1" /> {t('constraints.title')}
                </div>
                <div className="text-sm text-warning-fg whitespace-pre-wrap leading-relaxed">
                  {task.constraints}
                </div>
              </div>
            )}

            {/* 节点级 Token 用量 */}
            {task.nodeTokens && Object.keys(task.nodeTokens).length > 0 && (
              <div className="border border-line-strong/60 rounded-xl bg-surface-2/30 p-4">
                <div className="flex items-center mb-3">
                  <Cpu className="w-4 h-4 mr-2 text-primary-fg" />
                  <span className="text-xs font-semibold text-fg-muted">{t('tokenUsage.title')}</span>
                </div>
                <div className="space-y-2">
                  {Object.entries(task.nodeTokens).map(([nodeIdx, tokens]) => (
                    <div
                      key={nodeIdx}
                      className="flex items-center justify-between bg-surface/50 border border-line-strong/50 rounded-lg px-3 py-2"
                    >
                      <span className="text-[10px] text-fg-faint">
                        {nodeNames[parseInt(nodeIdx)]}
                      </span>
                      <span className="text-xs font-bold text-fg">{tokens as string}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {showRejectDialog && (
        <ModalPortal>
        <RejectDialog task={task} onConfirm={handleRejectConfirm} onClose={() => setShowRejectDialog(false)} />
        </ModalPortal>
      )}
      {showEditDialog && (
        <ModalPortal>
        <EditTaskDialog task={task} onSave={onEditTask} onClose={() => setShowEditDialog(false)} />
        </ModalPortal>
      )}
      {showReassignDialog && (
        <ModalPortal>
        <ReassignDialog
          agents={agents || []}
          onConfirm={(agentId) => {
            handleDoManual("reassign", agentId);
            setShowReassignDialog(false);
          }}
          onClose={() => setShowReassignDialog(false)}
        />
        </ModalPortal>
      )}
    </div>
  );
}

// ==========================================
// 优先续约权倒计时组件
// ==========================================
interface ContinuationBadgeProps {
  agentName: string;
  expiresAt: string;
}

function ContinuationBadge({ agentName, expiresAt }: ContinuationBadgeProps) {
  const t = useTranslations("dashboard");
  const [remaining, setRemaining] = useState(
    Math.max(0, Math.floor((new Date(expiresAt).getTime() - Date.now()) / 1000))
  );

  useEffect(() => {
    const timer = setInterval(() => {
      const r = Math.max(0, Math.floor((new Date(expiresAt).getTime() - Date.now()) / 1000));
      setRemaining(r);
      if (r <= 0) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [expiresAt]);

  if (remaining <= 0) return null;

  return (
    <div className="mt-2 flex items-center text-xs text-accent-fg bg-accent/10 p-2 rounded border border-accent/30">
      <Lock className="w-3.5 h-3.5 mr-1.5 shrink-0" />
      <span>
        {t('continuationBadge.waiting', { agentName, seconds: remaining })}
      </span>
    </div>
  );
}

// ==========================================
// 看板列
// ==========================================
interface TaskColumnProps {
  title: string;
  type: string;
  tasks: MappedTask[];
  onSelect: (task: MappedTask) => void;
  selectedId?: number;
}

function TaskColumn({ title, type, tasks, onSelect, selectedId }: TaskColumnProps) {
  const t = useTranslations("dashboard");
  const styles: Record<string, string> = {
    manual: "border-danger/30 bg-danger/5",
    rejected: "border-orange-500/30 bg-orange-500/5",
    progress: "border-primary/30 bg-primary/5",
    pending: "border-line-strong/30 bg-fg-faint/5",
    completed: "border-success/30 bg-success/5",
  };
  const badgeStyles: Record<string, string> = {
    manual: "bg-danger",
    rejected: "bg-orange-500",
    progress: "bg-primary",
    pending: "bg-fg-faint",
    completed: "bg-success",
  };
  const icons: Record<string, React.ReactNode> = {
    manual: <ShieldAlert className="w-3.5 h-3.5" />,
    rejected: <AlertTriangle className="w-3.5 h-3.5" />,
    progress: <Activity className="w-3.5 h-3.5" />,
    pending: <Clock className="w-3.5 h-3.5" />,
    completed: <CheckCircle2 className="w-3.5 h-3.5" />,
  };

  return (
    <div className={`w-80 flex flex-col rounded-xl border ${styles[type]} shrink-0 backdrop-blur-sm`}>
      <div className="p-4 border-b border-line-strong/40 flex items-center justify-between">
        <h3 className="font-semibold text-fg-muted">{title}</h3>
        <span className={`text-xs px-2 py-0.5 rounded-full font-bold text-fg ${badgeStyles[type]}`}>
          {tasks.length}
        </span>
      </div>
      <div className="p-3 flex-1 overflow-y-auto space-y-3 min-h-[200px] stagger-children">
        {tasks.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-24 text-fg-faint text-xs">
            <div className="w-8 h-8 rounded-full bg-surface-2/50 flex items-center justify-center mb-2">
              {icons[type]}
            </div>
            {t('taskColumn.empty')}
          </div>
        ) : (
          tasks.map((task) => {
            const totalNodes = task.nodeNames?.length || 7;
            const completedNodes = task.nodesStatus.filter((s) => s === "completed").length;
            const isOverdue =
              task.dueDate &&
              new Date(task.dueDate) < new Date() &&
              task.priorityState !== "completed" &&
              task.priorityState !== "cancelled";
            const currentTimeout = (task.nodeTimeouts as Record<string, number>)?.[task.currentNode];
            return (
              <div
                key={task.id}
                onClick={() => onSelect(task)}
                className={`p-4 rounded-lg border transition-all cursor-pointer card-hover ${
                  selectedId === task.id
                    ? "border-primary/60 bg-primary/5 shadow-lg shadow-primary/10"
                    : "border-line-strong/50 bg-surface-2/30 hover:bg-surface-2/50 hover:border-line-strong/60"
                }`}
              >
                <div className="flex justify-between items-start mb-2">
                  <div className="flex items-center gap-2 min-w-0">
                    <span className="text-xs font-mono text-fg-subtle">{task.taskRef}</span>
                    {task.type === "bug" && (
                      <span className="text-[10px] text-danger-fg bg-danger-fg/10 px-1.5 rounded">BUG</span>
                    )}
                    {(task.priority as string) === "urgent" && (
                      <span className="text-[10px] text-orange-400 bg-orange-400/10 px-1.5 rounded">{t('badge.urgent')}</span>
                    )}
                    {task.currentNodeType === "review" && (
                      <span className="text-[10px] text-purple-400 bg-purple-400/10 px-1.5 rounded">{t('badge.review')}</span>
                    )}
                  </div>
                  <span className="text-xs text-fg-faint shrink-0">
                    {completedNodes}/{totalNodes}
                  </span>
                </div>
                <h4 className="font-medium text-fg mb-3 text-sm leading-snug line-clamp-2">{task.title}</h4>
                {task.reservedForAgent &&
                  task.reservationExpiresAt &&
                  new Date(task.reservationExpiresAt).getTime() > Date.now() && (
                    <div className="flex items-center gap-1.5 mt-2 px-2 py-1 bg-primary/10 border border-primary/20 rounded-md">
                      <div className="w-1.5 h-1.5 rounded-full bg-primary-fg animate-pulse" />
                      <Clock className="w-3 h-3 text-primary-fg" />
                      <span className="text-[10px] text-primary-fg font-medium">
                        {t('taskCard.priorityRenewal', { agentName: task.reservedForAgent, seconds: Math.ceil((new Date(task.reservationExpiresAt).getTime() - Date.now()) / 1000) })}
                      </span>
                    </div>
                  )}
                <div className="flex items-center text-xs text-fg-subtle mb-2">
                  <Bot className="w-3 h-3 mr-1.5" />{" "}
                  {task.nodeAgents?.[task.currentNode] || task.agent || t('taskCard.awaitingClaim')}
                </div>
                {/* 标签 */}
                {task.labels && task.labels.length > 0 && (
                  <div className="flex flex-wrap gap-1 mb-2">
                    {task.labels.map((l: string) => (
                      <span
                        key={l}
                        className="text-[9px] px-1.5 py-0.5 bg-surface-3/50 text-fg-subtle rounded"
                      >
                        {l}
                      </span>
                    ))}
                  </div>
                )}
                {/* 截止日期 */}
                {task.dueDate && (
                  <div className={`flex items-center text-[10px] mb-2 ${isOverdue ? "text-danger-fg" : "text-fg-faint"}`}>
                    <Calendar className="w-3 h-3 mr-1" />
                    {task.dueDate}
                    {isOverdue && (
                      <span className="ml-1 bg-danger/10 text-danger-fg px-1 rounded">{t('overdue')}</span>
                    )}
                  </div>
                )}
                {/* 节点超时 */}
                {currentTimeout && task.priorityState === "in_progress" && (
                  <div className="flex items-center text-[10px] text-fg-faint mb-2">
                    <Timer className="w-3 h-3 mr-1" /> {t('taskCard.nodeTimeout', { minutes: currentTimeout })}
                  </div>
                )}
                {/* 退回计数 */}
                {task.rejectCount > 0 && (
                  <div className="flex items-center text-[10px] text-orange-400 mb-2">
                    <AlertOctagon className="w-3 h-3 mr-1" /> {t('taskCard.rejectedCount', { count: task.rejectCount })}
                  </div>
                )}
                <div className="w-full h-1 bg-surface-3 rounded-full overflow-hidden">
                  <div
                    className="h-full bg-primary/70 rounded-full transition-all"
                    style={{ width: `${(completedNodes / totalNodes) * 100}%` }}
                  />
                </div>
                {type === "manual" && task.message && (
                  <div className="mt-2 text-xs text-danger-fg bg-danger-fg/10 p-2 rounded flex items-start">
                    <ShieldAlert className="w-3.5 h-3.5 mr-1.5 shrink-0 mt-0.5" />
                    <span className="line-clamp-2">{task.message}</span>
                  </div>
                )}
                {task.currentNodeType === "review" && task.message && (
                  <div className="mt-2 text-xs text-orange-400 bg-orange-400/10 p-2 rounded flex items-start">
                    <AlertTriangle className="w-3.5 h-3.5 mr-1.5 shrink-0 mt-0.5" />
                    <span className="line-clamp-2">{task.message}</span>
                  </div>
                )}
                {task.reservedForAgent && task.reservationExpiresAt && (
                  <ContinuationBadge agentName={task.reservedForAgent} expiresAt={task.reservationExpiresAt} />
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

// ==========================================
// 任务列表视图（表格）
// ==========================================
interface TaskListViewProps {
  tasks: MappedTask[];
  onSelect: (task: MappedTask) => void;
  selectedId?: number;
  onClaim: (taskId: number, nodeIndex: number) => void;
  agents: MappedAgent[];
}

function TaskListView({ tasks, onSelect, selectedId, onClaim }: TaskListViewProps) {
  const t = useTranslations("dashboard");
  const [sortBy, setSortBy] = useState<string>("created");
  const [sortDir, setSortDir] = useState<string>("desc");
  const [filterType, setFilterType] = useState<string>("all");
  const [filterStatus, setFilterStatus] = useState<string>("all");

  const filtered = tasks.filter((t) => {
    if (filterType !== "all" && t.type !== filterType) return false;
    if (filterStatus !== "all" && t.priorityState !== filterStatus) return false;
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

  const statusMap: Record<string, { label: string; cls: string }> = {
    manual_intervention: { label: t('status.manualIntervention'), cls: "bg-danger/10 text-danger-fg border-danger/20" },
    rejected: { label: t('status.rejected'), cls: "bg-orange-500/10 text-orange-400 border-orange-500/20" },
    in_progress: { label: t('status.inProgress'), cls: "bg-primary/10 text-primary-fg border-primary/20" },
    pending: { label: t('status.pending'), cls: "bg-fg-faint/10 text-fg-subtle border-line-strong/20" },
    completed: { label: t('status.completed'), cls: "bg-success/10 text-success-fg border-success/20" },
    cancelled: { label: t('status.cancelled'), cls: "bg-fg-faint/10 text-fg-faint border-line-strong/20" },
  };

  return (
    <div className="flex-1 flex flex-col p-6 pt-3 overflow-hidden">
      {/* 筛选栏 */}
      <div className="flex items-center gap-3 mb-4 shrink-0">
        <select
          value={filterType}
          onChange={(e) => setFilterType(e.target.value)}
          className="bg-surface-2 border border-line-strong rounded-lg px-3 py-1.5 text-xs text-fg-muted focus:outline-none cursor-pointer"
        >
          <option value="all">{t('filters.allTypes')}</option>
          <option value="story">Story</option>
          <option value="bug">Bug</option>
          <option value="task">Task</option>
        </select>
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          className="bg-surface-2 border border-line-strong rounded-lg px-3 py-1.5 text-xs text-fg-muted focus:outline-none cursor-pointer"
        >
          <option value="all">{t('filters.allStatuses')}</option>
          <option value="manual_intervention">{t('filters.manualIntervention')}</option>
          <option value="rejected">{t('filters.rejected')}</option>
          <option value="in_progress">{t('filters.inProgress')}</option>
          <option value="pending">{t('filters.pending')}</option>
          <option value="completed">{t('filters.completed')}</option>
          <option value="cancelled">{t('filters.cancelled')}</option>
        </select>
        <div className="flex-1" />
        <span className="text-xs text-fg-faint">{t('filters.taskCount', { count: sorted.length })}</span>
      </div>

      {/* 表格 */}
      <div className="flex-1 overflow-y-auto rounded-xl border border-line-strong/60">
        <table className="w-full text-sm">
          <thead className="bg-surface-2/80 sticky top-0 z-10">
            <tr className="text-xs text-fg-subtle uppercase tracking-wider">
              <th className="text-left px-4 py-3 font-medium">ID</th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.title')}</th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.type')}</th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.priority')}</th>
              <th className="text-left px-4 py-3 font-medium">
                <button
                  onClick={() => {
                    setSortBy("priority");
                    setSortDir((d) => (d === "asc" ? "desc" : "asc"));
                  }}
                  className="flex items-center gap-1 hover:text-fg"
                >
                  {t('table.header.status')} {sortBy === "priority" ? (sortDir === "asc" ? "↑" : "↓") : ""}
                </button>
              </th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.currentNode')}</th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.executor')}</th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.progress')}</th>
              <th className="text-left px-4 py-3 font-medium">{t('table.header.actions')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {sorted.map((task) => {
              const total = task.nodeNames?.length || 7;
              const done = task.nodesStatus.filter((s) => s === "completed").length;
              const sm = statusMap[task.priorityState] || { label: task.priorityState, cls: "" };
              const isOverdue =
                task.dueDate &&
                new Date(task.dueDate) < new Date() &&
                task.priorityState !== "completed" &&
                task.priorityState !== "cancelled";
              return (
                <tr
                  key={task.id}
                  onClick={() => onSelect(task)}
                  className={`hover:bg-surface-2/50 cursor-pointer transition-colors-normal card-hover ${selectedId === task.id ? "bg-primary/5" : ""} ${isOverdue ? "border-l-2 border-l-danger" : ""}`}
                >
                  <td className="px-4 py-3 font-mono text-primary-fg text-xs">{task.taskRef}</td>
                  <td className="px-4 py-3 text-fg font-medium max-w-[200px]">
                    <div className="truncate">{task.title}</div>
                    {task.labels && task.labels.length > 0 && (
                      <div className="flex gap-1 mt-1">
                        {task.labels.slice(0, 3).map((l: string) => (
                          <span
                            key={l}
                            className="text-[9px] px-1 py-0.5 bg-surface-3/50 text-fg-subtle rounded"
                          >
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
                  <td className="px-4 py-3">
                    <span
                      className={`text-xs ${
                        (task.priority as string) === "urgent"
                          ? "text-orange-400"
                          : task.priority === "high"
                            ? "text-warning-fg"
                            : task.priority === "medium"
                              ? "text-yellow-400"
                              : "text-fg-subtle"
                      }`}
                    >
                      {task.priority || "medium"}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    <span className={`text-[10px] px-2 py-0.5 rounded-full border ${sm.cls}`}>
                      {sm.label}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-fg-muted text-xs">
                    {(task.nodeNames || [])[task.currentNode] || "-"}
                  </td>
                  <td className="px-4 py-3 text-xs text-fg-subtle">
                    {task.nodeAgents?.[task.currentNode] || task.agent || "-"}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <div className="w-16 h-1.5 bg-surface-3 rounded-full overflow-hidden">
                        <div
                          className="h-full bg-primary/70 rounded-full"
                          style={{ width: `${(done / total) * 100}%` }}
                        />
                      </div>
                      <span className="text-xs text-fg-faint">
                        {done}/{total}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      {(task.nodesStatus || [])[task.currentNode] === "pending" ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            onClaim(task.id, task.currentNode);
                          }}
                          className="text-[10px] px-2 py-1 bg-accent/20 hover:bg-accent/30 text-accent-fg rounded border border-accent/30 transition-colors-fast"
                        >
                          {t('table.claim')}
                        </button>
                      ) : task.reservedForAgent ? (
                        <span className="text-[10px] text-accent-fg flex items-center">
                          <Lock className="w-3 h-3 mr-0.5" />
                          {task.reservedForAgent}
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

// ==========================================
// 看板主视图（Next.js 页面组件）
// ==========================================
export default function DashboardPage() {
  const t = useTranslations("dashboard");
  const tasks = useAppStore((s) => s.tasks);
  const agents = useAppStore((s) => s.agents);
  const projects = useAppStore((s) => s.projects);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const setCurrentProjectId = useAppStore((s) => s.setCurrentProjectId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);
  const loadError = useAppStore((s) => s.loadError);

  const [selectedTask, setSelectedTask] = useState<MappedTask | null>(null);
  // 深链选中的任务 id：该任务进入列表前，同步 effect 不因缺失而关闭面板
  const deepLinkTaskRef = useRef<number | null>(null);

  // Deep link /dashboard?task=N: open the task panel even when the task is
  // outside the current project filter — locate it across projects, switch
  // the project context to its owner, then select it. The lookup is not
  // cancelled by effect re-runs: switching the project mid-flight changes
  // the deps and would swallow the setSelectedTask of the in-flight pass;
  // the guard at the top plus the idempotent set make re-entry harmless.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const taskParam = params.get("task");
    if (!taskParam || projects.length === 0) return;
    const id = Number(taskParam);
    if (!Number.isFinite(id) || id <= 0) return;
    if (selectedTask?.id === id || tasks.some((t) => t.id === id)) return;

    (async () => {
      for (const p of projects) {
        try {
          // The single-task endpoint returns the bare task object (no nodes
          // wrapper), which is enough to locate the owning project.
          const raw = (await api.getTask(p.id, id)) as { id?: number } | null;
          if (raw && typeof raw.id === "number") {
            if (p.id !== currentProjectId) {
              setCurrentProjectId(p.id);
              await loadProjectData(p.id);
            }
            deepLinkTaskRef.current = id;
            // Prefer the fully-shaped task (with nodes) from the refreshed
            // store list; fall back to a bare mapping for the odd case where
            // the list filter hides it.
            const listed = useAppStore.getState().tasks.find((t) => t.id === id);
            setSelectedTask(listed ?? mapTaskFromApi({ task: raw, nodes: [] }));
            return;
          }
        } catch {
          // the task does not belong to this project; try the next one
        }
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projects.length, currentProjectId, tasks.length]);

  // 使 selectedTask 与 store 更新保持同步（如取消/编辑之后）
  useEffect(() => {
    if (selectedTask) {
      const updated = tasks.find((t) => t.id === selectedTask.id);
      if (updated) {
        deepLinkTaskRef.current = null;
        setSelectedTask(updated);
      } else if (deepLinkTaskRef.current !== selectedTask.id) {
        // 任务已被移除（如已删除）。深链任务在目标工程列表刷新完成前
        // 可能暂不在当前列表中，此时不关闭面板
        setSelectedTask(null);
      }
    }
  }, [tasks, selectedTask]);

  const [searchQuery, setSearchQuery] = useState("");
  const [filterAgent, setFilterAgent] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<string>("kanban");
  const [activeTab, setActiveTab] = useState<"active" | "archived">("active");
  const [archivedTasks, setArchivedTasks] = useState<MappedTask[]>([]);
  const [boardColumns, setBoardColumns] = useState<
    { key: string; label: string; tasks: { id: number; title: string; priority: string; type: string; current_node_status: string; current_node_name: string; current_node_type: string; assignee_id: string }[] }[]
  >([]);

  const searched = searchQuery
    ? tasks.filter(
        (t) =>
          t.title.toLowerCase().includes(searchQuery.toLowerCase()) ||
          String(t.id).includes(searchQuery)
      )
    : tasks;

  const filteredByAgent = filterAgent
    ? searched.filter((t) =>
        (t.nodeAgents?.[t.currentNode] || t.agent)?.includes(filterAgent)
      )
    : searched;

  // 看板模式下从 API 获取看板数据
  useEffect(() => {
    if (viewMode === "list" || !currentProjectId) return;
    const fetchBoard = async () => {
      try {
        const data = await api.getBoardData(currentProjectId);
        setBoardColumns((data as { columns?: { key: string; label: string; tasks: { id: number; title: string; priority: string; type: string; current_node_status: string; current_node_name: string; current_node_type: string; assignee_id: string }[] }[] }).columns || []);
      } catch (e) {
        // 保留 last-good 看板数据:任务数据仍在 store 中,本地分组兜底可用
        console.error("Failed to fetch board data:", e);
      }
      // 同时刷新完整任务数据，确保 store 拥有完整的节点信息
      loadProjectData(currentProjectId);
    };
    fetchBoard();
    const interval = setInterval(fetchBoard, 10000);
    return () => clearInterval(interval);
  }, [viewMode, currentProjectId, loadProjectData]);

  // 列表模式下自动刷新任务列表
  useEffect(() => {
    if (viewMode !== "list" || !currentProjectId) return;
    const interval = setInterval(() => {
      loadProjectData(currentProjectId);
    }, 10000);
    return () => clearInterval(interval);
  }, [viewMode, currentProjectId, loadProjectData]);

  // 激活归档页签时获取已归档（已取消）任务
  useEffect(() => {
    if (activeTab !== "archived" || !currentProjectId) return;
    const fetchArchived = async () => {
      try {
        const data = await api.listTasks(currentProjectId, "cancelled");
        const raw = (data as { task?: { id: number; title: string; priority: string; type: string; status: string }; nodes?: { status: string; name: string; node_type: string; assignee_id?: string }[] }[]) || [];
        const mapped: MappedTask[] = raw.map((item: { task?: { id: number; title: string; priority: string; type: string; status: string }; nodes?: { status: string; name: string; node_type: string; assignee_id?: string }[] }) => {
          const t = item.task;
          if (!t) return null;
          const nodes = item.nodes || [];
          const currentNodeIdx = nodes.findIndex((n: { status: string }) => n.status !== "completed");
          const currentNode = currentNodeIdx >= 0 ? nodes[currentNodeIdx] : nodes[nodes.length - 1];
          return {
            id: t.id,
            taskRef: `T-${t.id}`,
            title: t.title,
            priority: (t.priority || "medium") as "low" | "medium" | "high" | "urgent",
            type: (t.type || "task") as "story" | "bug" | "task",
            priorityState: "cancelled" as const,
            currentNodeType: (currentNode?.node_type || "standard") as NodeType,
            description: "",
            constraints: "",
            currentNode: currentNodeIdx >= 0 ? currentNodeIdx : nodes.length - 1,
            agent: currentNode?.assignee_id || null,
            message: "",
            nodesStatus: nodes.map((n: { status: string }) => n.status as MappedTask["priorityState"]),
            nodeAgents: [] as string[],
            nodeNames: nodes.map((n: { name: string }) => n.name),
            nodeTokens: {},
            nodeTimeouts: {},
            dueDate: "",
            labels: [] as string[],
            rejectCount: 0,
            logs: {},
            comments: [],
            reviewHistory: [],
            subTasks: [],
            gitBranch: "",
            parentTask: "",
            interrupted: false,
            reservedForAgent: null,
            reservationExpiresAt: null,
            nodeSummaries: {},
            workflowName: "",
            _rawNodes: nodes,
            _projectId: currentProjectId,
          } as MappedTask;
        }).filter(Boolean) as MappedTask[];
        setArchivedTasks(mapped);
      } catch (err) {
        console.error("Failed to fetch archived tasks:", err);
      }
    };
    fetchArchived();
  }, [activeTab, currentProjectId]);

  const groups = {
    manual_intervention: filteredByAgent.filter((t) => t.priorityState === "manual_intervention"),
    in_progress: filteredByAgent.filter((t) => t.priorityState === "in_progress"),
    pending: filteredByAgent.filter((t) => t.priorityState === "pending"),
    completed: filteredByAgent.filter((t) => t.priorityState === "completed"),
  };

  // 四栏看板的列样式映射
  const columnStyleMap: Record<string, { type: string; icon: string }> = {
    pending: { type: "pending", icon: "⏳" },
    in_progress: { type: "progress", icon: "🔧" },
    completed: { type: "completed", icon: "✅" },
    manual_intervention: { type: "manual", icon: "🚩" },
  };

  // ---- 业务逻辑处理器 ----

  const handleApprove = async (taskId: number) => {
    const task = tasks.find((t) => t.id === taskId);
    if (!task) return;
    const currentNodeIdx = task.currentNode;
    const rawNode = (task._rawNodes as { id?: string }[])?.[currentNodeIdx];
    const nodeId = rawNode?.id;
    if (!nodeId) return;
    try {
      await api.approveNode(taskId, nodeId);
      if (currentProjectId) await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Approve failed:", err);
    }
  };

  const handleReject = async (taskId: number, _targetNode: number, _reason: string) => {
    const task = tasks.find((t) => t.id === taskId);
    if (!task) return;
    const currentNodeIdx = task.currentNode;
    const rawNode = (task._rawNodes as { id?: string }[])?.[currentNodeIdx];
    const nodeId = rawNode?.id;
    if (!nodeId) return;
    try {
      await api.rejectNode(taskId, nodeId, { reason: _reason, target_node: _targetNode });
      if (currentProjectId) await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Reject failed:", err);
    }
  };

  const handleManual = async (
    taskId: number,
    nodeId: string,
    action: string,
    agentId?: string
  ) => {
    if (action === "cancel" && currentProjectId) {
      try {
        await api.updateTask(currentProjectId, taskId, { status: "cancelled" });
        await loadProjectData(currentProjectId);
        // 若处于看板模式则刷新看板数据
        if (viewMode === "kanban") {
          try {
            const data = await api.getBoardData(currentProjectId);
            setBoardColumns((data as { columns?: { key: string; label: string; tasks: { id: number; title: string; priority: string; type: string; current_node_status: string; current_node_name: string; current_node_type: string; assignee_id: string }[] }[] }).columns || []);
          } catch { /* 尽力而为 */ }
        }
        setSelectedTask(null);
      } catch (err) {
        console.error("Cancel task failed:", err);
      }
      return;
    }
    try {
      if (action === "resume" || action === "reassign") {
        await api.resolveNode(taskId, nodeId, {
          action,
          ...(agentId ? { agent_id: agentId } : {}),
        });
      } else {
        await api.manualIntervention(taskId, nodeId, { action });
      }
      if (currentProjectId) await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Manual intervention failed:", err);
    }
  };

  const handleAddComment = async (taskId: number, text: string, mentions?: string[]) => {
    try {
      await api.createComment(taskId, { content: text, mentions: mentions || [] });
      if (currentProjectId) await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Add comment failed:", err);
    }
  };

  const handleClaim = async (taskId: number, nodeIndex: number) => {
    const task = tasks.find((t) => t.id === taskId);
    if (!task) return;
    const rawNode = (task._rawNodes as { id?: string }[])?.[nodeIndex];
    const nodeId = rawNode?.id;
    if (!nodeId) return;
    try {
      await api.claimNode(taskId, nodeId);
      if (currentProjectId) await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Claim failed:", err);
    }
  };

  const handleEditTask = async (taskId: number, updates: { title: string; description: string; priority: string }) => {
    if (!currentProjectId) return;
    try {
      await api.updateTask(currentProjectId, taskId, updates);
      await loadProjectData(currentProjectId);
    } catch (err) {
      console.error("Edit task failed:", err);
    }
  };

  const handleAddReviewRecord = (taskId: number, record: { action: string; reviewer: string; time: string; comment?: string; targetNode?: number }) => {
    void taskId;
    void record;
    // 审查记录由服务端处理；此处为本地 UI 更新占位
  };

  const handleAddSubTask = (parentId: number, subTask: SubTask) => {
    void parentId;
    void subTask;
    // 子任务创建占位
  };

  const handleDeleteTask = async (taskId: number) => {
    if (!currentProjectId) return;
    try {
      await api.deleteTask(currentProjectId, taskId);
      // 乐观地从本地状态移除，而非重新加载所有任务
      const { tasks } = useAppStore.getState();
      useAppStore.setState({ tasks: tasks.filter((t) => t.id !== taskId) });
    } catch (err) {
      console.error("Delete task failed:", err);
    }
  };


  if (viewMode === "list") {
    return (
      <div className="flex flex-col h-full relative page-enter">
        <div className="px-6 py-3 border-b border-line/60 bg-surface/30 shrink-0">
          <div className="flex items-center gap-3">
            <div className="flex bg-surface-2 rounded-lg p-0.5 border border-line-strong">
              <button
                onClick={() => setActiveTab("active")}
                className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors-normal ${activeTab === "active" ? "bg-primary/20 text-primary-fg" : "text-fg-subtle hover:text-fg"}`}
              >
                {t('tabs.active')}
              </button>
              <button
                onClick={() => setActiveTab("archived")}
                className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors-normal ${activeTab === "archived" ? "bg-primary/20 text-primary-fg" : "text-fg-subtle hover:text-fg"}`}
              >
                {t('tabs.archived')}
              </button>
            </div>
            <div className="relative flex-1 max-w-md">
              <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
              <input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder={t('search.placeholder')}
                className="w-full bg-surface-2 border border-line-strong rounded-lg pl-9 pr-4 py-2 text-sm text-fg focus:outline-none focus:border-primary"
              />
            </div>
            <select
              value={currentProjectId || ""}
              onChange={(e) => setCurrentProjectId(e.target.value || null)}
              className="bg-surface-2 border border-line-strong rounded-lg px-3 py-2 text-xs text-fg-muted focus:outline-none cursor-pointer"
            >
              <option value="">{t('projects.allProjects')}</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <select
              value={filterAgent || ""}
              onChange={(e) => setFilterAgent(e.target.value || null)}
              className="bg-surface-2 border border-line-strong rounded-lg px-3 py-2 text-xs text-fg-muted focus:outline-none cursor-pointer"
            >
              <option value="">{t('agents.allAgents')}</option>
              {agents.map((a) => (
                <option key={a.id} value={a.name}>
                  {a.name}
                </option>
              ))}
            </select>
            <button
              onClick={() => setViewMode(viewMode === "list" ? "kanban" : "list")}
              className="flex items-center px-3 py-2 text-xs text-fg-subtle hover:text-fg bg-surface-2 hover:bg-surface-3 border border-line-strong rounded-lg transition-colors-fast"
            >
              <ListTree className="w-3.5 h-3.5 mr-1" /> {t('view.kanban')}
            </button>
          </div>
        </div>
        <TaskListView
          tasks={activeTab === "archived" ? archivedTasks : filteredByAgent}
          onSelect={setSelectedTask}
          selectedId={selectedTask?.id}
          onClaim={handleClaim}
          agents={agents}
        />
        {selectedTask && (
          <WorkflowDetailsPanel
            task={selectedTask}
            onClose={() => setSelectedTask(null)}
            onApprove={handleApprove}
            onReject={handleReject}
            onManual={handleManual}
            onAddComment={handleAddComment}
            onClaim={handleClaim}
            onEditTask={handleEditTask}
            onAddReviewRecord={handleAddReviewRecord}
            onAddSubTask={handleAddSubTask}
            onDeleteTask={handleDeleteTask}
            agents={agents}
          />
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full relative page-enter">
      <div className="px-6 py-3 border-b border-line/60 bg-surface/30 shrink-0">
        <div className="flex items-center gap-3">
          <div className="flex bg-surface-2 rounded-lg p-0.5 border border-line-strong">
            <button
              onClick={() => setActiveTab("active")}
              className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors-normal ${activeTab === "active" ? "bg-primary/20 text-primary-fg" : "text-fg-subtle hover:text-fg"}`}
            >
              {t('tabs.active')}
            </button>
            <button
              onClick={() => setActiveTab("archived")}
              className={`px-3 py-1.5 text-xs font-medium rounded-md transition-colors-normal ${activeTab === "archived" ? "bg-primary/20 text-primary-fg" : "text-fg-subtle hover:text-fg"}`}
            >
              {t('tabs.archived')}
            </button>
          </div>
          <div className="relative flex-1 max-w-md">
            <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder={t('search.placeholder')}
              className="w-full bg-surface-2 border border-line-strong rounded-lg pl-9 pr-4 py-2 text-sm text-fg focus:outline-none focus:border-primary"
            />
          </div>
          <select
              value={currentProjectId || ""}
              onChange={(e) => setCurrentProjectId(e.target.value || null)}
              className="bg-surface-2 border border-line-strong rounded-lg px-3 py-2 text-xs text-fg-muted focus:outline-none cursor-pointer"
            >
              <option value="">{t('projects.allProjects')}</option>
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <select
              value={filterAgent || ""}
              onChange={(e) => setFilterAgent(e.target.value || null)}
              className="bg-surface-2 border border-line-strong rounded-lg px-3 py-2 text-xs text-fg-muted focus:outline-none cursor-pointer"
            >
              <option value="">{t('agents.allAgents')}</option>
              {agents.map((a) => (
                <option key={a.id} value={a.name}>
                  {a.name}
                </option>
              ))}
            </select>
            <button
              onClick={() => setViewMode(viewMode === "list" ? "kanban" : "list")}
              className="flex items-center px-3 py-2 text-xs text-fg-subtle hover:text-fg bg-surface-2 hover:bg-surface-3 border border-line-strong rounded-lg transition-colors-fast"
            >
              <ListTree className="w-3.5 h-3.5 mr-1" /> {t('view.kanban')}
            </button>
          </div>
        </div>

      <div className="flex-1 p-6 overflow-x-auto relative">
        {activeTab === "archived" ? (
          <TaskListView
            tasks={archivedTasks}
            onSelect={setSelectedTask}
            selectedId={selectedTask?.id}
            onClaim={handleClaim}
            agents={agents}
          />
        ) : tasks.length === 0 && loadError ? (
          <div className="flex items-center gap-2 text-xs text-fg-muted bg-surface-2 border border-line rounded-lg px-4 py-3">
            <span className="inline-block w-2 h-2 rounded-full bg-amber-500 shrink-0" />
            {t('loadError')}
          </div>
        ) : tasks.length === 0 ? (
          <EmptyStateGuide page="dashboard" />
        ) : (
        <div className="flex gap-4 min-w-max h-full pb-32">
          {loadError && (
            <div className="absolute top-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-2 text-xs text-fg-muted bg-surface-2 border border-line rounded-lg px-4 py-2 shadow-lg">
              <span className="inline-block w-2 h-2 rounded-full bg-amber-500 shrink-0" />
              {t('loadError')}
            </div>
          )}
          {boardColumns.length > 0
            ? boardColumns.map((col) => {
                const style = columnStyleMap[col.key] || { type: "pending", icon: "📋" };
                const columnTasks: MappedTask[] = col.tasks.map((bt) => {
                  // 从 store 查找完整任务（含 _rawNodes），找不到则退回最小对象
                  const storeTask = tasks.find((t) => t.id === bt.id);
                  if (!storeTask && currentProjectId) {
                    // store 中没有该任务 — 在后台触发一次刷新
                    loadProjectData(currentProjectId);
                  }
                  return storeTask || {
                    id: bt.id,
                    taskRef: `T-${bt.id}`,
                    title: bt.title,
                    priority: bt.priority as "low" | "medium" | "high" | "urgent",
                    type: (bt.type || "task") as "story" | "bug" | "task",
                    priorityState: bt.current_node_status,
                    currentNodeType: (bt.current_node_type || "standard") as NodeType,
                    description: "",
                    constraints: "",
                    currentNode: 0,
                    agent: bt.assignee_id || null,
                    message: "",
                    nodesStatus: [bt.current_node_status as "pending" | "in_progress" | "completed" | "rejected" | "manual_intervention"],
                    nodeAgents: bt.current_node_status === "completed" ? [t('status.completed')] : bt.assignee_id ? [bt.assignee_id] : [],
                    nodeNames: bt.current_node_name ? [bt.current_node_name] : [],
                    nodeTokens: {},
                    nodeTimeouts: {},
                    dueDate: "",
                    labels: [],
                    rejectCount: 0,
                    logs: {},
                    comments: [],
                    reviewHistory: [],
                    subTasks: [],
                    gitBranch: "",
                    parentTask: "",
                    interrupted: false,
                    reservedForAgent: null,
                    reservationExpiresAt: null,
                    nodeSummaries: {},
                    workflowName: "",
                    _rawNodes: [],
                    _projectId: currentProjectId || "",
                  };
                });
                return (
                  <TaskColumn
                    key={col.key}
                    title={`${style.icon} ${col.label}`}
                    type={style.type}
                    tasks={columnTasks}
                    onSelect={setSelectedTask}
                    selectedId={selectedTask?.id}
                  />
                );
              })
            : // 兜底：若看板 API 不可用则使用本地分组
              (
                <>
                  <TaskColumn title={t('kanbanColumn.manualIntervention')} type="manual" tasks={groups.manual_intervention} onSelect={setSelectedTask} selectedId={selectedTask?.id} />
                  <TaskColumn title={t('kanbanColumn.inProgress')} type="progress" tasks={groups.in_progress} onSelect={setSelectedTask} selectedId={selectedTask?.id} />
                  <TaskColumn title={t('kanbanColumn.pending')} type="pending" tasks={groups.pending} onSelect={setSelectedTask} selectedId={selectedTask?.id} />
                  <TaskColumn title={t('kanbanColumn.completed')} type="completed" tasks={groups.completed} onSelect={setSelectedTask} selectedId={selectedTask?.id} />
                </>
              )}
        </div>
        )}
      </div>

      {selectedTask && (
        <WorkflowDetailsPanel
          task={selectedTask}
          onClose={() => setSelectedTask(null)}
          onApprove={handleApprove}
          onReject={handleReject}
          onManual={handleManual}
          onAddComment={handleAddComment}
          onClaim={handleClaim}
          onEditTask={handleEditTask}
          onAddReviewRecord={handleAddReviewRecord}
          onAddSubTask={handleAddSubTask}
          onDeleteTask={handleDeleteTask}
          agents={agents}
        />
      )}
    </div>
  );
}
