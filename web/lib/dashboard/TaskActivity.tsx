"use client";
// TaskActivity：任务工作区主列的动态区块集合——执行尝试时间线、评论、子任务、节点总结。
// 状态与操作语义沿用原实现；正式状态一律用 lucide 图标，不用 emoji。

import { useState } from "react";
import ReactMarkdown from "react-markdown";
import { useTranslations, useFormatter } from "next-intl";
import {
  MessageSquare,
  Send,
  AtSign,
  X,
  Cpu,
  CheckCircle2,
  RefreshCw,
  Clock,
  GitBranch,
  Lock,
  Activity,
  Bot,
  User,
  History,
  XCircle,
} from "lucide-react";
import type { MappedAgent, MappedTask } from "@/lib/types";
import { usePermission } from "@/lib/use-permission";

// 执行尝试(来自 GET /tasks/{id}/graph 的 attempts 条目)
export interface GraphAttempt {
  id: string;
  task_node_id: string;
  attempt_no: number;
  executor_kind: string;
  executor_id: string;
  status: string;
  error?: { code?: string; message?: string } | null;
  started_at: string;
  completed_at?: string | null;
}

// ==========================================
// 节点总结
// ==========================================
export function NodeSummaryPanel({ summary, nodeName }: { summary?: string; nodeName: string }) {
  const t = useTranslations("dashboard");
  if (!summary) return null;
  return (
    <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30 p-4">
      <div className="flex items-center mb-2.5">
        <Bot className="w-4 h-4 mr-2 text-primary-fg" />
        <span className="text-sm font-medium text-fg-muted">{t("nodeSummary.header", { nodeName })}</span>
      </div>
      <div className="text-sm text-fg-subtle whitespace-pre-wrap leading-relaxed">{summary}</div>
    </div>
  );
}

// ==========================================
// 执行尝试时间线 + Git 传递 + 续作权
// ==========================================
export function TransitionPanel({
  task,
  attempts,
  agents,
  loading,
  onPickNode,
}: {
  task: MappedTask;
  attempts: GraphAttempt[];
  agents: MappedAgent[];
  loading: boolean;
  // 点击某条执行尝试时回调对应节点 id,用于切换日志/操作面板的聚焦节点
  onPickNode?: (nodeId: string) => void;
}) {
  const t = useTranslations("dashboard");
  const { dateTime } = useFormatter();
  if (loading) {
    return (
      <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30 p-4">
        <div className="flex items-center">
          <History className="w-4 h-4 mr-2 text-fg-subtle" />
          <span className="text-sm font-medium text-fg-muted">{t("dataFlow.header")}</span>
        </div>
        <div className="mt-3 space-y-2">
          <div className="skeleton h-4 w-3/4 rounded" />
          <div className="skeleton h-4 w-1/2 rounded" />
        </div>
      </div>
    );
  }

  if (attempts.length === 0 && !task.gitBranch) return null;

  const statusMeta: Record<string, { labelKey: string; badgeCls: string; dotCls: string; icon: React.ReactNode }> = {
    running: {
      labelKey: "statusRunning",
      badgeCls: "text-accent-fg bg-accent/10",
      dotCls: "border-accent bg-accent/30",
      icon: <RefreshCw className="w-3 h-3" />,
    },
    succeeded: {
      labelKey: "statusSucceeded",
      badgeCls: "text-success-fg bg-success/10",
      dotCls: "border-success bg-success/30",
      icon: <CheckCircle2 className="w-3 h-3" />,
    },
    failed: {
      labelKey: "statusFailed",
      badgeCls: "text-danger-fg bg-danger/10",
      dotCls: "border-danger bg-danger/30",
      icon: <XCircle className="w-3 h-3" />,
    },
    cancelled: {
      labelKey: "statusCancelled",
      badgeCls: "text-fg-subtle bg-fg-faint/10",
      dotCls: "border-line-strong bg-surface-3",
      icon: <Clock className="w-3 h-3" />,
    },
  };

  // 将节点 ID 映射为名称 / 序号
  const rawNodes = (task._rawNodes || []) as { id: string; name: string }[];
  const nodeMap: Record<string, string> = {};
  const nodeIdxMap: Record<string, number> = {};
  rawNodes.forEach((n, i) => {
    nodeMap[n.id] = n.name;
    nodeIdxMap[n.id] = i + 1;
  });

  const agentName = (id: string) => agents.find((a) => a.id === id)?.name || id;

  // 时间线按开始时间正序
  const ordered = [...attempts].sort((a, b) => (a.started_at || "").localeCompare(b.started_at || ""));

  return (
    <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30 p-4">
      <div className="flex items-center mb-3.5">
        <History className="w-4 h-4 mr-2 text-fg-subtle" />
        <span className="text-sm font-medium text-fg-muted">{t("dataFlow.header")}</span>
      </div>

      {ordered.length > 0 && (
        <div className="space-y-3 mb-4">
          <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">
            {t("dataFlow.transitionTitle")}
          </div>
          {ordered.map((tr, i) => {
            const meta = statusMeta[tr.status] || {
              labelKey: "statusCancelled",
              badgeCls: "text-fg-subtle bg-fg-faint/10",
              dotCls: "border-line-strong bg-surface-3",
              icon: <span className="w-1.5 h-1.5 rounded-full bg-current" />,
            };
            const nodeName = nodeMap[tr.task_node_id] || t("dataFlow.nodeFallback", { index: nodeIdxMap[tr.task_node_id] || "?" });
            const time = tr.started_at ? dateTime(new Date(tr.started_at), { hour: "2-digit", minute: "2-digit" }) : "";
            return (
              <div
                key={tr.id || i}
                className={`flex gap-3 ${onPickNode ? "cursor-pointer hover:bg-surface-2/60 rounded-[var(--radius-control)] -mx-2 px-2 transition-colors-fast" : ""}`}
                onClick={onPickNode ? () => onPickNode(tr.task_node_id) : undefined}
              >
                <div className="flex flex-col items-center">
                  <div className={`w-3 h-3 rounded-full border-2 flex items-center justify-center ${meta.dotCls}`} />
                  {i < ordered.length - 1 && <div className="w-0.5 flex-1 bg-surface-3 my-1" />}
                </div>
                <div className="flex-1 pb-3">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-sm font-medium text-fg-muted">{nodeName}</span>
                    <span className="text-[10px] text-fg-faint font-mono">
                      {t("dataFlow.attemptNo", { no: tr.attempt_no })}
                    </span>
                    <span className={`inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded font-medium ${meta.badgeCls}`}>
                      {meta.icon}
                      {t(`dataFlow.${meta.labelKey}`)}
                    </span>
                    <span className="text-[10px] text-fg-faint">{time}</span>
                  </div>
                  {tr.error?.message && (
                    <div className="text-xs text-danger-fg mt-1">
                      {tr.error.code ? `${tr.error.code}: ` : ""}
                      {tr.error.message}
                    </div>
                  )}
                  <div className="text-[10px] text-fg-faint mt-0.5 flex items-center gap-1">
                    {tr.executor_kind === "agentd" ? <Bot className="w-3 h-3" /> : <User className="w-3 h-3" />}
                    {tr.executor_kind === "agentd"
                      ? t("dataFlow.executorAgent", { name: agentName(tr.executor_id) })
                      : t("dataFlow.executorKind", { kind: tr.executor_kind })}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {task.gitBranch && (
        <div className="mb-4">
          <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">
            {t("dataFlow.gitTransfer")}
          </div>
          <div className="flex items-center gap-2 text-sm text-primary-fg font-mono">
            <GitBranch className="w-3.5 h-3.5" />
            <span className="truncate">{task.gitBranch}</span>
          </div>
          <div className="flex flex-wrap gap-2 mt-2">
            {((task._rawNodes || []) as { id: string; status: string }[]).map((n, i) => {
              const isDone = n.status === "succeeded";
              const isCurrent = n.status === "running";
              return (
                <span
                  key={n.id}
                  className={`text-[10px] px-2 py-0.5 rounded-full border ${
                    isDone
                      ? "border-success/30 text-success-fg bg-success/5"
                      : isCurrent
                        ? "border-accent/30 text-accent-fg bg-accent/5"
                        : "border-line-strong text-fg-faint"
                  }`}
                >
                  node-{i + 1}-start {isDone ? "✓" : isCurrent ? "..." : ""}
                </span>
              );
            })}
          </div>
        </div>
      )}

      {task.reservedForAgent && (
        <div>
          <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">
            {t("dataFlow.continuationRight")}
          </div>
          <div className="flex items-center text-xs text-accent-fg bg-accent/10 p-2 rounded border border-accent/30">
            <Lock className="w-3.5 h-3.5 mr-1.5 shrink-0" />
            <span>
              {t("dataFlow.reservedFor")} <span className="font-semibold text-accent-fg">{task.reservedForAgent}</span>
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

// ==========================================
// 子任务
// ==========================================
export interface SubTask {
  id: number;
  title: string;
  status: string;
  gitBranch: string;
  createdAt: string;
}

export function SubTaskPanel({
  subTasks = [],
}: {
  subTasks?: SubTask[];
}) {
  const t = useTranslations("dashboard");

  return (
    <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30">
      <div className="px-4 py-2.5 border-b border-line flex items-center justify-between">
        <div className="flex items-center">
          <GitBranch className="w-4 h-4 mr-2 text-accent-fg" />
          <span className="text-sm font-medium text-fg-muted">{t("subTasks.header", { count: subTasks.length })}</span>
        </div>
      </div>
      {subTasks.length > 0 ? (
        <div className="px-4 py-2.5 space-y-2">
          {subTasks.map((st) => (
            <div key={st.id} className="flex items-center justify-between bg-surface/50 border border-line/60 rounded-lg p-2.5">
              <div className="flex items-center gap-2.5 min-w-0">
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
        <div className="px-4 py-3.5 text-center text-xs text-fg-faint">{t("subTasks.empty")}</div>
      )}
      <div className="px-4 py-2 border-t border-line/60 text-[10px] text-fg-faint">{t("subTasks.gitIsolationInfo")}</div>
    </div>
  );
}

// ==========================================
// 评论
// ==========================================
export interface RealComment {
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

export function CommentPanel({
  comments = [],
  taskId,
  onAddComment,
  agents = [],
  loading,
}: {
  comments?: RealComment[];
  taskId: number;
  onAddComment: (taskId: number, text: string, mentions?: string[]) => void;
  agents?: MappedAgent[];
  loading?: boolean;
}) {
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

  if (loading) {
    return (
      <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30 p-4">
        <div className="flex items-center">
          <MessageSquare className="w-4 h-4 mr-2 text-fg-subtle" />
          <span className="text-sm font-medium text-fg-muted">{t("comments.header")}</span>
        </div>
        <div className="mt-3 space-y-2">
          <div className="skeleton h-4 w-3/4 rounded" />
          <div className="skeleton h-4 w-1/2 rounded" />
        </div>
      </div>
    );
  }

  return (
    <div className="border border-line rounded-[var(--radius-card)] bg-surface-2/30">
      <div className="px-4 py-2.5 border-b border-line flex items-center">
        <MessageSquare className="w-4 h-4 mr-2 text-fg-subtle" />
        <span className="text-sm font-medium text-fg-muted">
          {t("comments.headerWithCount", { count: comments.length })}
        </span>
      </div>

      {comments.length > 0 && (
        <div className="px-4 py-3 space-y-3 max-h-64 overflow-y-auto border-b border-line/60">
          {comments.map((c) => {
            const isAgent = c.author_type === "agent";
            const time = c.created_at ? dateTime(new Date(c.created_at), { hour: "2-digit", minute: "2-digit" }) : "";
            return (
              <div key={c.id} className="bg-bg border border-line rounded-lg p-3">
                <div className="flex items-start gap-2">
                  <div
                    className={`w-6 h-6 rounded-full flex items-center justify-center shrink-0 ${
                      isAgent ? "bg-primary/20 text-primary-fg" : "bg-surface-3 text-fg-muted"
                    }`}
                  >
                    {isAgent ? <Bot className="w-3.5 h-3.5" /> : <User className="w-3.5 h-3.5" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className={`text-xs font-medium ${isAgent ? "text-primary-fg" : "text-fg-muted"}`}>
                        {c.author_id?.slice(0, 8) || (isAgent ? "Agent" : "User")}
                      </span>
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
                                  <span key={i} className="text-primary-fg font-medium">
                                    {part}
                                  </span>
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
                      className="text-[10px] text-primary-fg mt-1 focus-visible:outline-none"
                    >
                      {t("comments.reply")}
                    </button>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <div className="px-4 py-3 relative">
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
                    className="hover:text-fg focus-visible:outline-none"
                    aria-label={t("comments.removeMention")}
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
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && !e.shiftKey && (e.preventDefault(), handleSend())}
            disabled={!canEdit}
            placeholder={t("comments.inputPlaceholder")}
            className="flex-1 h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3.5 text-sm text-fg-muted focus:outline-none focus:border-primary placeholder-fg-faint disabled:opacity-50 disabled:cursor-not-allowed"
          />
          <div className="relative">
            <button
              onClick={() => setShowAgentPicker(!showAgentPicker)}
              disabled={!canEdit}
              className={`h-[34px] px-2.5 rounded-[var(--radius-control)] border transition-colors-fast btn-press disabled:opacity-50 disabled:cursor-not-allowed focus-visible:outline-none ${
                mentions.length > 0
                  ? "bg-primary/15 border-primary/40 text-primary-fg"
                  : "bg-surface-2 border-line-strong text-fg-subtle hover:text-fg"
              }`}
              title={t("comments.mentionAgent")}
            >
              <AtSign className="w-4 h-4" />
            </button>
            {showAgentPicker && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setShowAgentPicker(false)} />
                <div className="absolute right-0 bottom-full mb-1 w-56 bg-surface-2 border border-line-strong rounded-[var(--radius-panel)] shadow-xl z-50 overflow-hidden animate-scale-in">
                  <div className="px-3 py-2 text-[10px] text-fg-faint border-b border-line/60">
                    {t("comments.mentionAgentTooltip")}
                  </div>
                  <div className="max-h-48 overflow-y-auto">
                    {agents.length === 0 ? (
                      <div className="px-3 py-4 text-center text-xs text-fg-faint">{t("comments.noAgents")}</div>
                    ) : (
                      agents.map((a) => {
                        const selected = mentions.includes(a.id);
                        return (
                          <button
                            key={a.id}
                            onClick={() => toggleMention(a.id)}
                            className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                              selected ? "bg-primary/10 text-primary-fg" : "text-fg-muted hover:bg-surface-3/50"
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
            className="h-[34px] px-3 bg-primary-strong hover:bg-primary text-fg rounded-[var(--radius-control)] disabled:opacity-50 disabled:cursor-not-allowed transition-colors-fast btn-press focus-visible:outline-none"
            title={t("comments.header")}
          >
            <Send className="w-4 h-4" />
          </button>
        </div>
        {replyTo && (
          <div className="flex items-center gap-2 mt-2 pl-3.5 border-l-2 border-primary/30">
            <span className="text-xs text-primary-fg">{t("comments.replying")}</span>
            <input
              value={replyText}
              onChange={(e) => setReplyText(e.target.value)}
              className="flex-1 h-8 bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg focus:outline-none focus:border-primary"
              placeholder={t("comments.replyPlaceholder")}
              onKeyDown={(e) => {
                if (e.key === "Enter" && replyText.trim()) {
                  onAddComment(taskId, replyText.trim());
                  setReplyText("");
                  setReplyTo(null);
                }
              }}
            />
            <button onClick={() => setReplyTo(null)} className="text-fg-faint hover:text-fg focus-visible:outline-none">
              <X className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
