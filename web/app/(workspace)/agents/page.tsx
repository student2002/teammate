"use client";
// 代理列表页：代理由守护进程上报自动出现，按所属守护进程分组展示，此处管理其能力配置（指令/技能/MCP/Git 身份）
// 与守护进程的接入（创建/token）、删除。

import React, { useState, useEffect, useRef } from "react";
import {
  Bot,
  Terminal,
  Zap,
  Database,
  X,
  Cpu,
  Activity,
  Clock,
  CheckCircle2,
  Plus,
  Save,
  Trash2,
  ChevronDown,
  Check,
  Copy,
  Loader2,
  AlertTriangle,
  PlayCircle,
  PauseCircle,
  Monitor,
  AlertCircle,
  Server,
  KeyRound,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { useTranslations } from "next-intl";
import type {
  MappedAgent,
  MappedDaemon,
  DaemonStatus,
  Skill,
  McpServer,
  AgentStatus,
} from "@/lib/types";
import { mapAgentFromApi, mapDaemonFromApi } from "@/lib/mappers";
import ModalPortal from "@/lib/ModalPortal";


// ==========================================
// AgentStatusBadge 组件
// ==========================================
const STATUS_STYLES: Record<string, string> = {
  online: "bg-success/10 text-success-fg border-success/20",
  busy: "bg-primary/10 text-primary-fg border-primary/20",
  paused: "bg-warning/10 text-warning-fg border-warning/20",
  offline: "bg-fg-faint/10 text-fg-subtle border-line-strong/20",
  pending: "bg-warning/10 text-warning-fg border-warning/20 border-dashed",
};

const STATUS_KEYS: Record<string, string> = {
  online: "online",
  busy: "busy",
  paused: "paused",
  offline: "offline",
  pending: "pending",
};

function AgentStatusBadge({ status }: { status: AgentStatus | string }) {
  const t = useTranslations("agents.status");
  const s = STATUS_STYLES[status] || STATUS_STYLES.offline;
  return (
    <div
      className={`flex items-center px-2.5 py-1 rounded-full text-xs font-medium border ${s}`}
    >
      {status === "online" ? (
        <PlayCircle className="w-3 h-3 mr-1 animate-pulse-soft" />
      ) : status === "busy" ? (
        <Activity className="w-3 h-3 mr-1" />
      ) : status === "paused" ? (
        <PauseCircle className="w-3 h-3 mr-1" />
      ) : status === "pending" ? (
        <Clock className="w-3 h-3 mr-1 animate-pulse-soft" />
      ) : (
        <PauseCircle className="w-3 h-3 mr-1 opacity-50" />
      )}
      {t(STATUS_KEYS[status] || "offline")}
    </div>
  );
}

// ==========================================
// 在工具运行时节点下创建代理实例(Web 端创建,下发到该机器由 agentd 收编)
// 三层模型:agentd(机器)→ 工具运行时(检测到的 provider)→ agent 实例
// ==========================================
const PROVIDER_LABELS: Record<string, string> = {
  claude: "Claude Code",
  openclaw: "OpenClaw",
  opencode: "OpenCode",
  atomcode: "AtomCode",
  mimocode: "MiMoCode",
};

interface AddAgentTarget {
  daemon: MappedDaemon;
  provider: string;
  version: string;
}

function AddAgentModal({
  target,
  onSave,
  onClose,
  saving,
  error,
}: {
  target: AddAgentTarget;
  onSave: (data: { name: string; provider: string }) => void;
  onClose: () => void;
  saving: boolean;
  error: string | null;
}) {
  const t = useTranslations("agents.addDialog");
  const [name, setName] = useState("");
  const providerLabel = PROVIDER_LABELS[target.provider] || target.provider;

  return (
    <div
      className="modal-overlay fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[500px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center">
          <h2 className="text-xl font-bold text-fg flex items-center">
            <Monitor className="w-5 h-5 mr-2 text-cyan-400" />
            {t("title", { daemon: target.daemon.name, provider: providerLabel })}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg text-fg-subtle hover:text-fg"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <p className="text-sm text-fg-subtle leading-relaxed">
            {t("desc", { provider: providerLabel })}
          </p>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("nameLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value.trim())}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
              placeholder={t("namePlaceholder")}
              autoFocus
            />
            <p className="text-[11px] text-fg-faint mt-1.5">{t("nameHint")}</p>
          </div>
          <div className="bg-bg/60 border border-line rounded-lg px-4 py-2.5 flex items-center justify-between">
            <span className="text-sm text-fg-subtle">{t("providerLabel")}</span>
            <span className="text-sm text-fg-muted font-mono">
              {providerLabel}
              {target.version ? ` · v${target.version}` : ""}
            </span>
          </div>
          {error && (
            <p className="text-sm text-danger-fg bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
              {error}
            </p>
          )}
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
          >
            {t("cancel")}
          </button>
          <button
            onClick={() =>
              name &&
              onSave({
                name,
                provider: target.provider,
              })
            }
            disabled={!name || saving}
            className="btn-press flex items-center px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {saving ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Plus className="w-4 h-4 mr-2" />
            )}
            {t("submit")}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 守护进程相关：相对时间 / 状态徽标 / 弹窗
// ==========================================
function formatRelative(t: ReturnType<typeof useTranslations>, iso: string): string {
  if (!iso) return "—";
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000));
  if (seconds < 10) return t("justNow");
  if (seconds < 60) return t("secondsAgo", { n: seconds });
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return t("minutesAgo", { n: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("hoursAgo", { n: hours });
  return t("daysAgo", { n: Math.floor(hours / 24) });
}

function DaemonStatusBadge({ status }: { status: DaemonStatus }) {
  const t = useTranslations("daemons.status");
  const online = status === "online";
  return (
    <div
      className={`flex items-center px-2.5 py-1 rounded-full text-xs font-medium border ${
        online
          ? "bg-success/10 text-success-fg border-success/20"
          : "bg-fg-faint/10 text-fg-subtle border-line-strong/20"
      }`}
    >
      <span
        className={`w-2 h-2 rounded-full mr-1.5 ${
          online ? "bg-success-fg status-glow animate-pulse-soft" : "bg-fg-subtle"
        }`}
      />
      {online ? t("online") : t("offline")}
    </div>
  );
}

function CreateDaemonModal({
  onSave,
  onClose,
  saving,
  error,
}: {
  onSave: (name: string) => void;
  onClose: () => void;
  saving: boolean;
  error: string | null;
}) {
  const t = useTranslations("daemons.create");
  const [name, setName] = useState("");

  return (
    <div
      className="modal-overlay fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[480px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center">
          <h2 className="text-xl font-bold text-fg">{t("title")}</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg text-fg-subtle hover:text-fg"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <p className="text-sm text-fg-subtle leading-relaxed">{t("desc")}</p>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("nameLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
              placeholder={t("namePlaceholder")}
              autoFocus
            />
          </div>
          {error && (
            <p className="text-sm text-danger-fg bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
              {error}
            </p>
          )}
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
          >
            {t("cancel")}
          </button>
          <button
            onClick={() => name.trim() && onSave(name.trim())}
            disabled={!name.trim() || saving}
            className="btn-press flex items-center px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {saving ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Plus className="w-4 h-4 mr-2" />
            )}
            {t("submit")}
          </button>
        </div>
      </div>
    </div>
  );
}

function DaemonTokenRevealModal({
  daemonName,
  daemonToken,
  onClose,
}: {
  daemonName: string;
  daemonToken: string;
  onClose: () => void;
}) {
  const t = useTranslations("daemons.tokenModal");
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(daemonToken);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div
      className="modal-overlay fixed inset-0 z-[60] flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/98 backdrop-blur-xl border border-warning/30 rounded-2xl w-[560px] max-h-[85vh] overflow-y-auto shadow-2xl shadow-warning/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-warning/20 flex justify-between items-center">
          <h2 className="text-xl font-bold text-fg flex items-center gap-2">
            <span className="text-warning-fg">&#9888;</span> {t("title")}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg text-fg-subtle hover:text-fg"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-5">
          <div className="bg-warning/5 border border-warning/20 rounded-lg p-4">
            <p className="text-sm text-warning-fg font-medium mb-1">{t("importantTitle")}</p>
            <p className="text-xs text-warning-fg/70">{t("importantDesc")}</p>
          </div>

          <div>
            <div className="flex items-center justify-between mb-2">
              <label className="block text-xs font-semibold text-fg-subtle uppercase tracking-wider">
                {t("tokenLabel")} · {daemonName}
              </label>
              <button
                onClick={handleCopy}
                className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors-fast ${
                  copied
                    ? "bg-success-strong text-fg"
                    : "bg-primary/10 text-primary-fg border border-primary/20 hover:bg-primary/20"
                }`}
              >
                {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
                {copied ? t("copied") : t("copy")}
              </button>
            </div>
            <div className="bg-bg border border-line-strong rounded-lg p-4 break-all font-mono text-sm text-fg select-all">
              {daemonToken}
            </div>
          </div>

          <div className="bg-surface-2/50 border border-line-strong rounded-lg p-4">
            <p className="text-xs font-semibold text-fg-subtle uppercase tracking-wider mb-3">
              {t("stepsTitle")}
            </p>
            <ol className="space-y-2 text-sm text-fg-muted">
              {["step1", "step2", "step3"].map((key, i) => (
                <li key={key} className="flex items-start gap-2">
                  <span className="text-primary-fg font-bold shrink-0">{i + 1}.</span>
                  <span>
                    {t.rich(key, {
                      code: (chunks) => (
                        <code className="text-primary-fg bg-primary/10 px-1.5 py-0.5 rounded">
                          {chunks}
                        </code>
                      ),
                    })}
                  </span>
                </li>
              ))}
            </ol>
          </div>
        </div>
        <div className="p-5 border-t border-line flex justify-end">
          <button
            onClick={onClose}
            className="btn-press px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium"
          >
            {t("close")}
          </button>
        </div>
      </div>
    </div>
  );
}

function ConfirmDeleteDaemonModal({
  daemon,
  onConfirm,
  onClose,
  deleting,
  error,
}: {
  daemon: MappedDaemon;
  onConfirm: () => void;
  onClose: () => void;
  deleting: boolean;
  error: string | null;
}) {
  const t = useTranslations("daemons.deleteDialog");
  return (
    <div
      className="modal-overlay fixed inset-0 z-[60] flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-danger/30 rounded-2xl w-[440px] shadow-2xl shadow-danger/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 flex items-start gap-4">
          <div className="w-10 h-10 rounded-full bg-danger/10 flex items-center justify-center shrink-0 mt-0.5">
            <AlertTriangle className="w-5 h-5 text-danger-fg" />
          </div>
          <div className="flex-1">
            <h3 className="text-lg font-bold text-fg mb-1">{t("title")}</h3>
            <p className="text-sm text-fg-subtle">
              {t.rich("confirm", {
                name: daemon.name,
                count: daemon.agentCount,
                span: (chunks) => (
                  <span className="text-fg font-medium">{chunks}</span>
                ),
              })}
            </p>
            {error && (
              <p className="text-sm text-danger-fg mt-2 bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
                {error}
              </p>
            )}
          </div>
        </div>
        <div className="p-4 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={deleting}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast disabled:opacity-50"
          >
            {t("cancel")}
          </button>
          <button
            onClick={onConfirm}
            disabled={deleting}
            className="btn-press flex items-center px-4 py-2 bg-danger-strong hover:bg-danger text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {deleting ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Trash2 className="w-4 h-4 mr-2" />
            )}
            {t("confirmDelete")}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 重置 daemon token 确认弹窗(旧 token 立即失效)
// ==========================================
function ConfirmRotateTokenModal({
  daemon,
  onConfirm,
  onClose,
  rotating,
  error,
}: {
  daemon: MappedDaemon;
  onConfirm: () => void;
  onClose: () => void;
  rotating: boolean;
  error: string | null;
}) {
  const t = useTranslations("daemons.rotateDialog");
  return (
    <div
      className="modal-overlay fixed inset-0 z-[60] flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-warning/30 rounded-2xl w-[440px] shadow-2xl shadow-warning/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 flex items-start gap-4">
          <div className="w-10 h-10 rounded-full bg-warning/10 flex items-center justify-center shrink-0 mt-0.5">
            <KeyRound className="w-5 h-5 text-warning-fg" />
          </div>
          <div className="flex-1">
            <h3 className="text-lg font-bold text-fg mb-1">{t("title")}</h3>
            <p className="text-sm text-fg-subtle leading-relaxed">
              {t.rich("confirm", {
                name: daemon.name,
                span: (chunks) => (
                  <span className="text-fg font-medium">{chunks}</span>
                ),
              })}
            </p>
            {error && (
              <p className="text-sm text-danger-fg mt-2 bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
                {error}
              </p>
            )}
          </div>
        </div>
        <div className="p-4 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={rotating}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast disabled:opacity-50"
          >
            {t("cancel")}
          </button>
          <button
            onClick={onConfirm}
            disabled={rotating}
            className="btn-press flex items-center px-4 py-2 bg-warning-strong hover:bg-warning text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {rotating ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <KeyRound className="w-4 h-4 mr-2" />
            )}
            {t("confirmRotate")}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 下拉选择器组件（支持多选）
// ==========================================
interface MultiSelectOption {
  id: string;
  name: string;
  enabled?: boolean;
  category?: string;
  type?: string;
}

function MultiSelect({
  options,
  selected,
  onChange,
  placeholder,
  accentColor = "indigo",
}: {
  options: MultiSelectOption[];
  selected: string[];
  onChange: (val: string[]) => void;
  placeholder: string;
  accentColor?: string;
}) {
  const t = useTranslations("agents.multiSelect");
  const [isOpen, setIsOpen] = useState(false);

  const colorMap: Record<
    string,
    {
      bg: string;
      border: string;
      text: string;
      focus: string;
      hover: string;
    }
  > = {
    indigo: {
      bg: "bg-accent/10",
      border: "border-accent/20",
      text: "text-accent-fg",
      focus: "focus:border-accent",
      hover: "hover:border-accent/40",
    },
    emerald: {
      bg: "bg-success/10",
      border: "border-success/20",
      text: "text-success-fg",
      focus: "focus:border-success",
      hover: "hover:border-success/40",
    },
  };
  const c = colorMap[accentColor] || colorMap.indigo;

  const handleToggle = (id: string) => {
    if (selected.includes(id)) {
      onChange(selected.filter((s) => s !== id));
    } else {
      onChange([...selected, id]);
    }
  };

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setIsOpen(!isOpen)}
        className={`w-full bg-bg border border-line-strong rounded-lg px-3 py-2 text-left text-sm ${c.hover} transition-colors-fast flex items-center justify-between min-h-[38px]`}
      >
        <div className="flex flex-wrap gap-1 flex-1">
          {selected.length === 0 ? (
            <span className="text-fg-faint">{placeholder}</span>
          ) : (
            selected.map((s) => {
              const label = options.find((opt) => opt.id === s)?.name || s;
              return (
                <span
                  key={s}
                  className={`inline-flex items-center px-2 py-0.5 text-[11px] rounded ${c.bg} ${c.text} ${c.border} border`}
                >
                  {label}
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleToggle(s);
                    }}
                    className="ml-1 hover:text-fg"
                  >
                    <X className="w-3 h-3" />
                  </button>
                </span>
              );
            })
          )}
        </div>
        <ChevronDown
          className={`w-4 h-4 text-fg-faint shrink-0 ml-2 transition-transform ${isOpen ? "rotate-180" : ""}`}
        />
      </button>
      {isOpen && (
        <>
          <div
            className="fixed inset-0 z-[60]"
            onClick={() => setIsOpen(false)}
          />
          <div
            className="animate-scale-in absolute top-full left-0 right-0 mt-1 bg-surface border border-line-strong rounded-lg shadow-xl z-[70] max-h-48 overflow-y-auto overscroll-contain"
            onWheel={(e) => e.stopPropagation()}
          >
            {options.length === 0 ? (
              <div className="px-3 py-2 text-sm text-fg-faint">
                {t("noOptions")}
              </div>
            ) : (
              options.map((opt) => {
                const isSelected = selected.includes(opt.id);
                return (
                  <button
                    key={opt.id}
                    type="button"
                    onClick={() => handleToggle(opt.id)}
                    className={`w-full text-left px-3 py-2 text-sm flex items-center justify-between transition-colors-fast ${
                      isSelected
                        ? `${c.bg} ${c.text}`
                        : "text-fg-muted hover:bg-surface-2"
                    } ${!opt.enabled ? "opacity-40" : ""}`}
                  >
                    <span className="flex items-center gap-2">
                      {isSelected && <Check className="w-3.5 h-3.5" />}
                      <span>{opt.name}</span>
                      {!opt.enabled && (
                        <span className="text-[10px] text-fg-faint">
                          ({t("disabled")})
                        </span>
                      )}
                    </span>
                    <span className="text-[10px] text-fg-faint">
                      {opt.category || opt.type}
                    </span>
                  </button>
                );
              })
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ==========================================
// 删除确认弹窗
// ==========================================
function ConfirmDeleteModal({
  agentName,
  onConfirm,
  onClose,
  deleting,
  error,
}: {
  agentName: string;
  onConfirm: () => void;
  onClose: () => void;
  deleting: boolean;
  error: string | null;
}) {
  const t = useTranslations("agents.deleteDialog");
  return (
    <div
      className="modal-overlay fixed inset-0 z-[60] flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-danger/30 rounded-2xl w-[440px] shadow-2xl shadow-danger/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 flex items-start gap-4">
          <div className="w-10 h-10 rounded-full bg-danger/10 flex items-center justify-center shrink-0 mt-0.5">
            <AlertTriangle className="w-5 h-5 text-danger-fg" />
          </div>
          <div className="flex-1">
            <h3 className="text-lg font-bold text-fg mb-1">{t("title")}</h3>
            <p className="text-sm text-fg-subtle">
              {t.rich("confirm", {
                name: agentName,
                span: (chunks) => (
                  <span className="text-fg font-medium">{chunks}</span>
                ),
              })}
            </p>
            {error && (
              <p className="text-sm text-danger-fg mt-2 bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
                {error}
              </p>
            )}
          </div>
        </div>
        <div className="p-4 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={deleting}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast disabled:opacity-50"
          >
            {t("cancel")}
          </button>
          <button
            onClick={onConfirm}
            disabled={deleting}
            className="btn-press flex items-center px-4 py-2 bg-danger-strong hover:bg-danger text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {deleting ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Trash2 className="w-4 h-4 mr-2" />
            )}
            {t("confirmDelete")}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 代理编辑弹窗（技能/MCP 管理）
// ==========================================
// ==========================================
// 代理详情弹窗(查看 + 内置编辑模式,点击「编辑」原地切换)
// ==========================================
interface HistoryItem {
  task: string;
  node: string;
  status: string;
}

function AgentDetailModal({
  agent,
  onClose,
  onSaveEdit,
  saving,
  skills,
  mcpServers,
}: {
  agent: MappedAgent;
  onClose: () => void;
  onSaveEdit: (id: string, fields: Partial<MappedAgent>) => Promise<boolean>;
  saving: boolean;
  skills: Skill[];
  mcpServers: McpServer[];
}) {
  const t = useTranslations("agents.detail");
  const te = useTranslations("agents.edit");
  const ta = useTranslations("agents");
  const [editing, setEditing] = useState(false);
  const [identity, setIdentity] = useState("");
  const [gitName, setGitName] = useState("");
  const [gitEmail, setGitEmail] = useState("");
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([]);

  const enabledSkills = skills.filter(
    (s) => (s as Skill & { enabled?: boolean }).enabled !== false
  );
  const enabledMcps = mcpServers.filter(
    (m) => (m as McpServer & { enabled?: boolean }).enabled !== false
  );

  const enterEdit = () => {
    setIdentity(agent.identityInstruction || "");
    setGitName(agent.gitName || "");
    setGitEmail(agent.gitEmail || "");
    setSelectedSkills((agent.skills || []).map((s) => (typeof s === "string" ? s : s.id)));
    setSelectedMcps((agent.mcpServers || []).map((m) => (typeof m === "string" ? m : m.id)));
    setEditing(true);
  };

  const handleSave = async () => {
    const ok = await onSaveEdit(agent.id, {
      identityInstruction: identity,
      gitName,
      gitEmail,
      skills: selectedSkills as unknown as Skill[],
      mcpServers: selectedMcps as unknown as McpServer[],
    });
    if (ok) setEditing(false);
  };

  return (
    <div
      className="modal-overlay fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[640px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-6 border-b border-line flex justify-between items-center sticky top-0 bg-surface z-10">
          <div className="flex items-center">
            <div className="w-12 h-12 rounded-xl bg-surface-3/80 border border-line-strong flex items-center justify-center mr-4">
              <Bot className="w-7 h-7 text-primary-fg" />
            </div>
            <div>
              <h2 className="text-xl font-bold text-fg">{agent.name}</h2>
              <div className="text-xs text-fg-subtle flex items-center mt-1">
                <Terminal className="w-3 h-3 mr-1" /> {agent.tool}
              </div>
              {agent.daemonName && (
                <div className="text-[11px] text-fg-faint flex items-center mt-1">
                  <Monitor className="w-3 h-3 mr-1" />
                  {ta("fromDaemon")} {agent.daemonName}
                </div>
              )}
            </div>
          </div>
          <div className="flex items-center gap-2">
            {!editing && (
              <button
                onClick={enterEdit}
                className="flex items-center px-3 py-1.5 text-xs font-medium text-primary-fg hover:text-primary-fg bg-primary/10 border border-primary/20 rounded-md transition-colors-fast"
              >
                <Save className="w-3.5 h-3.5 mr-1" /> {t("editButton")}
              </button>
            )}
            <button
              onClick={onClose}
              className="p-2 hover:bg-surface-3 rounded-lg text-fg-subtle hover:text-fg"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {editing ? (
          <>
            <div className="p-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {te("identityLabel")}
                </label>
                <textarea
                  value={identity}
                  onChange={(e) => setIdentity(e.target.value)}
                  rows={3}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-sm text-fg-muted focus:outline-none focus:border-primary resize-none font-mono"
                />
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-fg-muted mb-2">
                    {te("gitNameLabel")}
                  </label>
                  <input
                    value={gitName}
                    onChange={(e) => setGitName(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                    placeholder={te("gitNamePlaceholder")}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-fg-muted mb-2">
                    {te("gitEmailLabel")}
                  </label>
                  <input
                    value={gitEmail}
                    onChange={(e) => setGitEmail(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                    placeholder={te("gitEmailPlaceholder")}
                  />
                </div>
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2 flex items-center">
                  <Zap className="w-4 h-4 mr-1.5 text-accent-fg" /> {te("skillsLabel")}
                </label>
                <MultiSelect
                  options={enabledSkills.map((s) => ({ id: s.id, name: s.name, enabled: true }))}
                  selected={selectedSkills}
                  onChange={setSelectedSkills}
                  placeholder={te("skillsPlaceholder")}
                  accentColor="indigo"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2 flex items-center">
                  <Database className="w-4 h-4 mr-1.5 text-success-fg" /> {te("mcpLabel")}
                </label>
                <MultiSelect
                  options={enabledMcps.map((m) => ({ id: m.id, name: m.name, enabled: true }))}
                  selected={selectedMcps}
                  onChange={setSelectedMcps}
                  placeholder={te("mcpPlaceholder")}
                  accentColor="emerald"
                />
              </div>
            </div>
            <div className="p-5 border-t border-line flex justify-end gap-3">
              <button
                onClick={() => setEditing(false)}
                className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
              >
                {te("cancel")}
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                className="flex items-center px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
              >
                {saving ? (
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                ) : (
                  <Save className="w-4 h-4 mr-2" />
                )}
                {te("save")}
              </button>
            </div>
          </>
        ) : (
          <div className="p-6 space-y-6">
            <div>
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2">
                {t("identityLabel")}
              </div>
              <div className="bg-bg border border-line rounded-lg p-4 text-sm text-fg-muted leading-relaxed font-mono">
                {agent.identityInstruction}
              </div>
            </div>
            <div>
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3 flex items-center">
                <Zap className="w-3.5 h-3.5 mr-1 text-accent-fg" /> {t("skillsLabel")}
              </div>
              <div className="flex flex-wrap gap-2">
                {(agent.skills || []).map((s) => (
                  <span
                    key={typeof s === "string" ? s : s.name}
                    className="px-3 py-1.5 bg-accent/10 border border-accent/20 text-accent-fg text-xs rounded-lg"
                  >
                    {typeof s === "string" ? s : s.name}
                  </span>
                ))}
              </div>
            </div>
            <div>
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3 flex items-center">
                <Database className="w-3.5 h-3.5 mr-1 text-success-fg" />{" "}
                {t("mcpLabel")}
              </div>
              <div className="space-y-2">
                {(agent.mcpServers || []).map((m) => (
                  <div
                    key={typeof m === "string" ? m : m.name}
                    className="flex items-center text-sm text-fg-muted bg-bg border border-line rounded-lg p-3"
                  >
                    <div className="w-2 h-2 rounded-full bg-success mr-3 status-glow animate-pulse-soft" />{" "}
                    {typeof m === "string" ? m : m.name}
                  </div>
                ))}
              </div>
            </div>
            <div>
              <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3 flex items-center">
                <Cpu className="w-3.5 h-3.5 mr-1 text-primary-fg" /> {t("tokenUsageLabel")}
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="bg-bg border border-line rounded-lg p-4">
                  <div className="text-xs text-fg-subtle mb-1">{t("inputToken")}</div>
                  <div className="text-lg font-bold text-fg">
                    {(agent.tokenUsage || { input: 0, output: 0 }).input.toLocaleString()}
                  </div>
                </div>
                <div className="bg-bg border border-line rounded-lg p-4">
                  <div className="text-xs text-fg-subtle mb-1">{t("outputToken")}</div>
                  <div className="text-lg font-bold text-fg">
                    {(agent.tokenUsage || { input: 0, output: 0 }).output.toLocaleString()}
                  </div>
                </div>
              </div>
            </div>
            {((agent.history as HistoryItem[]) || []).length > 0 && (
              <div>
                <div className="text-xs font-semibold text-fg-faint uppercase tracking-wider mb-3 flex items-center">
                  <Clock className="w-3.5 h-3.5 mr-1 text-fg-subtle" /> {t("historyLabel")}
                </div>
                <div className="space-y-2">
                  {(agent.history as HistoryItem[]).map((h, i) => (
                    <div
                      key={i}
                      className="flex items-center justify-between bg-bg border border-line rounded-lg p-3"
                    >
                      <div className="flex items-center text-sm">
                        <span className="text-fg-muted font-mono mr-2">
                          {h.task}
                        </span>
                        <span className="text-fg-faint">—</span>
                        <span className="text-fg-subtle ml-2">{h.node}</span>
                      </div>
                      <span
                        className={`text-xs px-2 py-0.5 rounded-full ${
                          h.status === "completed"
                            ? "bg-success/10 text-success-fg"
                            : h.status === "manual_intervention"
                              ? "bg-danger/10 text-danger-fg"
                              : "bg-primary/10 text-primary-fg"
                        }`}
                      >
                        {h.status === "completed"
                          ? t("historyCompleted")
                          : h.status === "manual_intervention"
                            ? t("historyIntervention")
                            : t("historyInProgress")}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ==========================================
// 主页面组件
// ==========================================

function idsFromItems<T extends { id: string }>(items: Array<T | string> | undefined): string[] {
  return (items || []).map((item) => (typeof item === "string" ? item : item.id));
}

async function syncAgentBindings(
  workspaceId: string,
  agentId: string,
  currentSkillIds: string[],
  nextSkillIds: string[],
  currentMcpIds: string[],
  nextMcpIds: string[]
) {
  const removeSkills = currentSkillIds.filter((id) => !nextSkillIds.includes(id));
  const addSkills = nextSkillIds.filter((id) => !currentSkillIds.includes(id));
  const removeMcps = currentMcpIds.filter((id) => !nextMcpIds.includes(id));
  const addMcps = nextMcpIds.filter((id) => !currentMcpIds.includes(id));

  await Promise.all([
    ...removeSkills.map((id) => api.removeAgentSkill(workspaceId, agentId, id)),
    ...addSkills.map((id) => api.addAgentSkill(workspaceId, agentId, id)),
    ...removeMcps.map((id) => api.removeAgentMcpServer(workspaceId, agentId, id)),
    ...addMcps.map((id) => api.addAgentMcpServer(workspaceId, agentId, id)),
  ]);
}

async function hydrateAgentBindings(workspaceId: string, agentList: MappedAgent[]) {
  return Promise.all(
    agentList.map(async (agent) => {
      const [boundSkills, boundMcps] = await Promise.all([
        api.listAgentSkills(workspaceId, agent.id).catch(() => []),
        api.listAgentMcpServers(workspaceId, agent.id).catch(() => []),
      ]);
      return {
        ...agent,
        skills: boundSkills as Skill[],
        mcpServers: boundMcps as McpServer[],
      };
    })
  );
}

export default function AgentsPage() {
  const t = useTranslations("agents");
  const td = useTranslations("daemons");
  const tEmpty = useTranslations("daemons.empty");

  const agents = useAppStore((s) => s.agents);
  const setAgents = useAppStore((s) => s.setAgents);
  const tasks = useAppStore((s) => s.tasks);
  const skills = useAppStore((s) => s.skills);
  const mcpServers = useAppStore((s) => s.mcpServers);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  const [selectedAgent, setSelectedAgent] = useState<MappedAgent | null>(null);
  const [saving, setSaving] = useState(false);
  const [deletingAgent, setDeletingAgent] = useState<MappedAgent | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [daemons, setDaemons] = useState<MappedDaemon[]>([]);
  const [showCreateDaemon, setShowCreateDaemon] = useState(false);
  const [creatingDaemon, setCreatingDaemon] = useState(false);
  const [createDaemonError, setCreateDaemonError] = useState<string | null>(null);
  const [newDaemonToken, setNewDaemonToken] = useState<{ name: string; token: string } | null>(null);
  const [deletingDaemon, setDeletingDaemon] = useState<MappedDaemon | null>(null);
  const [deletingDaemonBusy, setDeletingDaemonBusy] = useState(false);
  const [deleteDaemonError, setDeleteDaemonError] = useState<string | null>(null);
  const [rotatingDaemon, setRotatingDaemon] = useState<MappedDaemon | null>(null);
  const [rotatingBusy, setRotatingBusy] = useState(false);
  const [rotateError, setRotateError] = useState<string | null>(null);
  const [addAgentTarget, setAddAgentTarget] = useState<AddAgentTarget | null>(null);
  const [addingAgent, setAddingAgent] = useState(false);
  const [addAgentError, setAddAgentError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  // 折叠状态:daemon 分组与工具节点两级,key 形如 "daemon:{id}" / "daemon:{id}:{provider}"
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggleCollapse = (key: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // 每 10 秒轮询守护进程与代理状态;tick 驱动心跳相对时间重新渲染
  useEffect(() => {
    if (!currentWorkspaceId) return;

    const refresh = async () => {
      try {
        const daemonList = await api.listDaemons(currentWorkspaceId);
        if (Array.isArray(daemonList)) {
          setDaemons(daemonList.map(mapDaemonFromApi));
        }
      } catch {
        // 静默忽略轮询错误
      }
      try {
        const agentList = await api.listAgents(currentWorkspaceId);
        if (agentList && Array.isArray(agentList)) {
          const mapped = agentList.map(mapAgentFromApi);
          setAgents(await hydrateAgentBindings(currentWorkspaceId, mapped));
        }
      } catch {
        // 静默忽略轮询错误
      }
      setTick((n) => n + 1);
    };

    pollingRef.current = setInterval(refresh, 10000);
    refresh();
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [currentWorkspaceId, setAgents]);

  // 支持全局搜索跳转 /agents?id=<agentId>:打开对应实例的详情弹窗后清理 URL 参数
  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    const id = params.get("id");
    if (!id) return;
    const target = agents.find((a) => a.id === id);
    if (!target) return;
    setSelectedAgent(target);
    params.delete("id");
    const qs = params.toString();
    window.history.replaceState({}, "", window.location.pathname + (qs ? `?${qs}` : ""));
  }, [agents]);

  const handleCreateDaemon = async (name: string) => {
    if (!currentWorkspaceId) return;
    setCreatingDaemon(true);
    setCreateDaemonError(null);
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const created: any = await api.createDaemon(currentWorkspaceId, { name });
      setShowCreateDaemon(false);
      try {
        const daemonList = await api.listDaemons(currentWorkspaceId);
        if (Array.isArray(daemonList)) {
          setDaemons(daemonList.map(mapDaemonFromApi));
        }
      } catch {
        /* 忽略刷新错误 */
      }
      if (created?.daemon_token) {
        setNewDaemonToken({ name: created.name || name, token: created.daemon_token });
      }
    } catch (err) {
      setCreateDaemonError(err instanceof Error ? err.message : td("createFailed"));
    } finally {
      setCreatingDaemon(false);
    }
  };

  const confirmDeleteDaemon = async () => {
    if (!deletingDaemon || !currentWorkspaceId) return;
    setDeletingDaemonBusy(true);
    setDeleteDaemonError(null);
    try {
      await api.deleteDaemon(currentWorkspaceId, deletingDaemon.id);
      setDaemons((prev) => prev.filter((d) => d.id !== deletingDaemon.id));
      setDeletingDaemon(null);
    } catch (err) {
      setDeleteDaemonError(err instanceof Error ? err.message : td("deleteFailed"));
    } finally {
      setDeletingDaemonBusy(false);
    }
  };

  // Web 端在工具运行时节点下创建实例:server 记录后下发给该机器的 agentd,
  // 列表先显示"待下发",agentd 收编上线由 10s 轮询自动呈现
  const handleCreateAgent = async (data: { name: string; provider: string }) => {
    if (!addAgentTarget || !currentWorkspaceId) return;
    setAddingAgent(true);
    setAddAgentError(null);
    try {
      await api.createDaemonAgent(currentWorkspaceId, addAgentTarget.daemon.id, data);
      setAddAgentTarget(null);
      const agentList = await api.listAgents(currentWorkspaceId);
      if (Array.isArray(agentList)) {
        setAgents(
          await hydrateAgentBindings(currentWorkspaceId, agentList.map(mapAgentFromApi))
        );
      }
    } catch (err) {
      setAddAgentError(err instanceof Error ? err.message : t("addAgentFailed"));
    } finally {
      setAddingAgent(false);
    }
  };

  // 重置 daemon token:旧 token 立即失效,新 token 仅展示一次(复用 Token 弹窗)
  const confirmRotateToken = async () => {
    if (!rotatingDaemon || !currentWorkspaceId) return;
    setRotatingBusy(true);
    setRotateError(null);
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const res: any = await api.rotateDaemonToken(currentWorkspaceId, rotatingDaemon.id);
      setRotatingDaemon(null);
      if (res?.daemon_token) {
        setNewDaemonToken({ name: rotatingDaemon.name, token: res.daemon_token });
      }
    } catch (err) {
      setRotateError(err instanceof Error ? err.message : td("rotateFailed"));
    } finally {
      setRotatingBusy(false);
    }
  };

  // 详情弹窗内的保存:成功返回 true(弹窗退回查看态),失败返回 false(停留编辑态)
  const handleEditSave = async (id: string, fields: Partial<MappedAgent>): Promise<boolean> => {
    if (!currentWorkspaceId) return false;
    setSaving(true);
    try {
      const agent = agents.find((a) => a.id === id);
      await api.updateAgent(currentWorkspaceId, id, {
        instructions: fields.identityInstruction ?? agent?.identityInstruction ?? "",
        model: "",
        status: agent?.status || "offline",
        git_name: fields.gitName ?? agent?.gitName ?? "",
        git_email: fields.gitEmail ?? agent?.gitEmail ?? "",
      });
      const nextSkillIds = idsFromItems(fields.skills as Array<Skill | string> | undefined);
      const nextMcpIds = idsFromItems(fields.mcpServers as Array<McpServer | string> | undefined);
      const currentSkillIds = idsFromItems(agent?.skills as Array<Skill | string> | undefined);
      const currentMcpIds = idsFromItems(agent?.mcpServers as Array<McpServer | string> | undefined);
      await syncAgentBindings(
        currentWorkspaceId,
        id,
        currentSkillIds,
        nextSkillIds,
        currentMcpIds,
        nextMcpIds
      );
      const normalizedFields = {
        ...fields,
        skills: skills.filter((s) => nextSkillIds.includes(s.id)),
        mcpServers: mcpServers.filter((m) => nextMcpIds.includes(m.id)),
      };
      setAgents(agents.map((a) => (a.id === id ? { ...a, ...normalizedFields } : a)));
      setSelectedAgent((prev) =>
        prev?.id === id ? { ...prev, ...normalizedFields } : prev
      );
      return true;
    } catch (err) {
      console.error("Update agent failed:", err);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = (id: string) => {
    setDeleteError(null);
    setDeletingAgent(agents.find((a) => a.id === id) || null);
  };

  const confirmDelete = async () => {
    if (!deletingAgent || !currentWorkspaceId) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await api.deleteAgent(currentWorkspaceId, deletingAgent.id);
      setAgents(agents.filter((a) => a.id !== deletingAgent.id));
      setSelectedAgent(null);
      setDeletingAgent(null);
    } catch (err) {
      setDeleteError(
        err instanceof Error ? err.message : t("deleteFailed")
      );
    } finally {
      setDeleting(false);
    }
  };

  // 按守护进程分组;daemon 未知或已删除的代理归入「未归组」段
  const groups = daemons.map((daemon) => ({
    daemon,
    groupAgents: agents.filter((a) => a.daemonId === daemon.id),
  }));
  const ungrouped = agents.filter(
    (a) => !a.daemonId || !daemons.some((d) => d.id === a.daemonId)
  );

  const renderAgentCard = (agent: MappedAgent) => {
    const currentTask =
      tasks?.find(
        (t) => String(t.id) === agent.task?.split(" ")[0]
      ) || null;
    const completedCount = ((agent.history as HistoryItem[]) || []).filter(
      (h) => h.status === "completed"
    ).length;
    return (
      <div
        key={agent.id}
        onClick={() => setSelectedAgent(agent)}
        className="card-hover bg-surface-2/40 border border-line-strong/50 rounded-xl backdrop-blur-sm p-6 flex flex-col shadow-sm hover:border-line-strong transition-all cursor-pointer hover:shadow-lg hover:shadow-primary/5"
      >
        <div className="flex justify-between items-start mb-6">
          <div className="flex items-center">
            <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-primary/20 to-accent/20 border border-primary/20 flex items-center justify-center mr-4 shadow-inner">
              <Bot className="w-7 h-7 text-primary-fg" />
            </div>
            <div>
              <h3 className="font-bold text-fg text-lg leading-tight">
                {agent.name}
              </h3>
              <div className="text-xs text-fg-subtle flex items-center mt-1 font-mono">
                <Terminal className="w-3 h-3 mr-1.5" /> {agent.tool}
              </div>
              {agent.origin && (
                <div className="mt-1.5">
                  <span
                    className={`inline-block px-1.5 py-0.5 rounded text-[10px] ${
                      agent.origin === "server"
                        ? "bg-cyan-500/10 text-cyan-300"
                        : "bg-surface-3/40 text-fg-subtle"
                    }`}
                  >
                    {agent.origin === "server" ? t("originServer") : t("originDaemon")}
                  </span>
                </div>
              )}
            </div>
          </div>
          <AgentStatusBadge status={agent.status} />
        </div>

        <div className="space-y-5 mb-6">
          <div>
            <div className="text-xs font-semibold text-fg-faint mb-2 flex items-center">
              <Zap className="w-3.5 h-3.5 mr-1 text-accent-fg" /> {t("cardSkills")}
            </div>
            <div className="flex flex-wrap gap-2">
              {(agent.skills || []).map((s) => (
                <span
                  key={typeof s === "string" ? s : s.name}
                  className="px-2 py-1 bg-accent/10 border border-accent/20 text-accent-fg text-[11px] rounded uppercase tracking-wide font-medium"
                >
                  {typeof s === "string" ? s : s.name}
                </span>
              ))}
            </div>
          </div>
          <div>
            <div className="text-xs font-semibold text-fg-faint mb-2 flex items-center">
              <Database className="w-3.5 h-3.5 mr-1 text-success-fg" />{" "}
              {t("cardMcp")}
            </div>
            <div className="flex flex-col gap-2">
              {(agent.mcpServers || []).map((m) => (
                <div
                  key={typeof m === "string" ? m : m.name}
                  className="flex items-center text-xs text-fg-muted bg-surface/50 border border-line-strong/50 rounded-lg p-2"
                >
                  <div className="w-1.5 h-1.5 rounded-full bg-success mr-2.5 status-glow animate-pulse-soft" />{" "}
                  {typeof m === "string" ? m : m.name}
                </div>
              ))}
            </div>
          </div>
          <div>
            <div className="text-xs font-semibold text-fg-faint mb-2 flex items-center">
              <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-success-fg" />
              {t("completedNodes")}
            </div>
            <div className="text-lg font-bold text-success-fg">
              {completedCount}
            </div>
          </div>
          {(!agent.gitName || !(agent.skills || []).length) && (
            <div className="flex flex-wrap gap-2">
              {!agent.gitName && (
                <span className="flex items-center gap-1 px-2 py-1 bg-warning/10 border border-warning/20 text-warning-fg text-[11px] rounded">
                  <AlertCircle className="w-3 h-3" /> {t("missingGit")}
                </span>
              )}
              {!(agent.skills || []).length && (
                <span className="flex items-center gap-1 px-2 py-1 bg-warning/10 border border-warning/20 text-warning-fg text-[11px] rounded">
                  <AlertCircle className="w-3 h-3" /> {t("missingSkills")}
                </span>
              )}
            </div>
          )}
        </div>

        <div className="mt-auto pt-4 border-t border-line-strong/50 flex justify-between items-center bg-surface-2/20 -mx-6 -mb-6 px-6 pb-6 rounded-b-xl">
          <div className="flex items-center gap-4">
            <div className="text-xs text-fg-subtle">
              {t("currentTask")}{" "}
              <span className="text-fg font-medium ml-1">
                {currentTask?.title
                  ? currentTask.title.substring(0, 14) + "..."
                  : agent.task || t("idle")}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={(e) => {
                e.stopPropagation();
                setSelectedAgent(agent);
              }}
              className="text-fg-subtle hover:text-fg text-xs font-medium bg-surface-3/50 px-3 py-1.5 rounded-md border border-line-strong/50 transition-colors-fast"
            >
              {t("detailButton")}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                handleDelete(agent.id);
              }}
              className="text-danger-fg hover:text-danger-fg text-xs font-medium bg-danger/10 px-3 py-1.5 rounded-md border border-danger/20 transition-colors-fast"
            >
              {t("deleteButton")}
            </button>
          </div>
        </div>
      </div>
    );
  };

  return (
    <div className="page-enter h-full flex flex-col p-8 overflow-y-auto" data-tick={tick}>
      <div className="flex justify-between items-center mb-8 shrink-0">
        <div>
          <h1 className="text-2xl font-bold text-fg mb-2">{t("title")}</h1>
          <p className="text-sm text-fg-subtle">
            {t("subtitle", { count: agents.length })}
          </p>
        </div>
        <button
          onClick={() => setShowCreateDaemon(true)}
          className="btn-press flex items-center px-4 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg font-medium transition-colors-fast shadow-lg shadow-primary/20"
        >
          <Monitor className="w-5 h-5 mr-2" /> {td("addButton")}
        </button>
      </div>

      {daemons.length === 0 && agents.length === 0 ? (
        <div className="bg-gradient-to-br from-surface-2/40 to-surface/40 border border-line-strong/60 rounded-xl p-8 max-w-2xl mx-auto mt-8">
          <div className="flex items-start gap-4 mb-5">
            <div className="w-12 h-12 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center shrink-0">
              <Server className="w-6 h-6 text-primary-fg" />
            </div>
            <div>
              <h3 className="text-lg font-bold text-fg mb-1">{td("empty.title")}</h3>
              <p className="text-sm text-fg-subtle leading-relaxed">{td("empty.desc")}</p>
            </div>
          </div>
          <ol className="space-y-3 mb-6">
            {["step1", "step2", "step3"].map((key, i) => (
              <li key={key} className="flex items-start gap-3 text-sm text-fg-muted">
                <span className="w-6 h-6 rounded-lg bg-primary/10 border border-primary/20 text-primary-fg text-xs font-bold flex items-center justify-center shrink-0">
                  {i + 1}
                </span>
                <span className="leading-relaxed pt-0.5">{tEmpty(key)}</span>
              </li>
            ))}
          </ol>
          <button
            onClick={() => setShowCreateDaemon(true)}
            className="btn-press flex items-center px-4 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium transition-colors-fast"
          >
            <Monitor className="w-4 h-4 mr-2" /> {td("addButton")}
          </button>
        </div>
      ) : (
        <div className="space-y-10">
          {groups.map(({ daemon, groupAgents }) => (
            <section key={daemon.id}>
              {(() => {
                const daemonCollapsed = collapsed.has(`daemon:${daemon.id}`);
                return (
                  <>
                    <div className="flex items-center justify-between mb-4 pb-3 border-b border-line-strong/60">
                      <button
                        className="flex items-center text-left"
                        onClick={() => toggleCollapse(`daemon:${daemon.id}`)}
                      >
                        <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-cyan-500/20 to-primary/20 border border-cyan-500/20 flex items-center justify-center mr-3 shadow-inner">
                          <Monitor className="w-5 h-5 text-cyan-400" />
                        </div>
                        <div>
                          <div className="flex items-center gap-2.5">
                            <h2 className="text-lg font-bold text-fg">{daemon.name}</h2>
                            <DaemonStatusBadge status={daemon.status} />
                            <ChevronDown
                              className={`w-4 h-4 text-fg-faint transition-transform ${daemonCollapsed ? "" : "rotate-180"}`}
                            />
                          </div>
                          <div className="text-xs text-fg-faint flex items-center gap-3 mt-0.5">
                            <span>
                              {daemon.deviceName || td("noDevice")}
                              {daemon.version ? ` · v${daemon.version}` : ""}
                            </span>
                            <span className="flex items-center gap-1">
                              <Clock className="w-3 h-3" />
                              {td("heartbeat")} {formatRelative(td, daemon.lastHeartbeat)}
                            </span>
                            <span>{td("agentsCount", { count: groupAgents.length })}</span>
                            <span className="font-mono">{daemon.id}</span>
                          </div>
                        </div>
                      </button>
                      <div className="flex items-center gap-2">
                        <button
                          onClick={() => {
                            setRotateError(null);
                            setRotatingDaemon(daemon);
                          }}
                          className="flex items-center text-warning-fg hover:text-warning-fg text-xs font-medium bg-warning/10 px-3 py-1.5 rounded-md border border-warning/20 transition-colors-fast"
                        >
                          <KeyRound className="w-3.5 h-3.5 mr-1" /> {td("rotateButton")}
                        </button>
                        <button
                          onClick={() => {
                            setDeleteDaemonError(null);
                            setDeletingDaemon(daemon);
                          }}
                          className="flex items-center text-danger-fg hover:text-danger-fg text-xs font-medium bg-danger/10 px-3 py-1.5 rounded-md border border-danger/20 transition-colors-fast"
                        >
                          <Trash2 className="w-3.5 h-3.5 mr-1" /> {td("deleteButton")}
                        </button>
                      </div>
                    </div>

                    {!daemonCollapsed &&
                      (() => {
                        // 二层:工具运行时节点(来自 daemon 检测上报;存在实例但工具未检测到的作为孤儿节点警示)
                        const detected = daemon.providers.filter((p) => p.installed);
                        const orphans = Array.from(
                          new Set(
                            groupAgents
                              .map((a) => a.provider)
                              .filter((p) => !detected.some((d) => d.provider === p))
                          )
                        );
                        const nodes = [
                          ...detected.map((d) => ({
                            provider: d.provider,
                            version: d.version,
                            missing: false,
                          })),
                          ...orphans.map((p) => ({ provider: p, version: "", missing: true })),
                        ];
                        if (nodes.length === 0) {
                          return (
                            <p className="text-xs text-fg-faint py-3">{td("noProviders")}</p>
                          );
                        }
                        return nodes.map((node) => {
                          const instances = groupAgents.filter(
                            (a) => a.provider === node.provider
                          );
                          const nodeCollapsed = collapsed.has(
                            `daemon:${daemon.id}:${node.provider}`
                          );
                          return (
                            <div key={node.provider} className="mb-6 last:mb-0">
                              <div className="flex items-center justify-between mb-3">
                                <button
                                  className="flex items-center gap-2 text-left"
                                  onClick={() =>
                                    toggleCollapse(`daemon:${daemon.id}:${node.provider}`)
                                  }
                                >
                                  <ChevronDown
                                    className={`w-3.5 h-3.5 text-fg-faint transition-transform ${nodeCollapsed ? "" : "rotate-180"}`}
                                  />
                                  <Terminal className="w-4 h-4 text-primary-fg" />
                                  <span className="text-sm font-semibold text-fg-muted">
                                    {PROVIDER_LABELS[node.provider] || node.provider}
                                  </span>
                                  {node.version && (
                                    <span className="text-[10px] font-mono text-fg-faint">
                                      v{node.version}
                                    </span>
                                  )}
                                  {node.missing && (
                                    <span className="px-1.5 py-0.5 rounded text-[10px] bg-warning/10 text-warning-fg border border-warning/20">
                                      {td("providerMissing")}
                                    </span>
                                  )}
                                  <span className="text-[11px] text-fg-faint">
                                    {td("instancesCount", { count: instances.length })}
                                  </span>
                                </button>
                                <button
                                  onClick={() => {
                                    setAddAgentError(null);
                                    setAddAgentTarget({
                                      daemon,
                                      provider: node.provider,
                                      version: node.version,
                                    });
                                  }}
                                  className="flex items-center text-primary-fg hover:text-primary-fg text-xs font-medium bg-primary/10 px-3 py-1.5 rounded-md border border-primary/20 transition-colors-fast"
                                >
                                  <Plus className="w-3.5 h-3.5 mr-1" /> {t("addAgent")}
                                </button>
                              </div>
                              {!nodeCollapsed &&
                                (instances.length === 0 ? (
                                  <p className="text-xs text-fg-faint py-2 pl-6">
                                    {td("noInstances")}
                                  </p>
                                ) : (
                                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                                    {instances.map(renderAgentCard)}
                                  </div>
                                ))}
                            </div>
                          );
                        });
                      })()}
                  </>
                );
              })()}
            </section>
          ))}
          {ungrouped.length > 0 && (
            <section>
              <div className="flex items-center gap-2 mb-4 pb-3 border-b border-line-strong/60">
                <AlertCircle className="w-4 h-4 text-warning-fg" />
                <h2 className="text-base font-bold text-fg-muted">{t("ungrouped")}</h2>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {ungrouped.map(renderAgentCard)}
              </div>
            </section>
          )}
        </div>
      )}

      {selectedAgent && (
        <ModalPortal>
          <AgentDetailModal
            agent={selectedAgent}
            onClose={() => setSelectedAgent(null)}
            onSaveEdit={handleEditSave}
            saving={saving}
            skills={skills}
            mcpServers={mcpServers}
          />
        </ModalPortal>
      )}
      {deletingAgent && (
        <ModalPortal>
          <ConfirmDeleteModal
            agentName={deletingAgent.name}
            onConfirm={confirmDelete}
            onClose={() => {
              setDeletingAgent(null);
              setDeleteError(null);
            }}
            deleting={deleting}
            error={deleteError}
          />
        </ModalPortal>
      )}
      {showCreateDaemon && (
        <ModalPortal>
          <CreateDaemonModal
            onSave={handleCreateDaemon}
            onClose={() => {
              setShowCreateDaemon(false);
              setCreateDaemonError(null);
            }}
            saving={creatingDaemon}
            error={createDaemonError}
          />
        </ModalPortal>
      )}
      {newDaemonToken && (
        <ModalPortal>
          <DaemonTokenRevealModal
            daemonName={newDaemonToken.name}
            daemonToken={newDaemonToken.token}
            onClose={() => setNewDaemonToken(null)}
          />
        </ModalPortal>
      )}
      {deletingDaemon && (
        <ModalPortal>
          <ConfirmDeleteDaemonModal
            daemon={deletingDaemon}
            onConfirm={confirmDeleteDaemon}
            onClose={() => {
              setDeletingDaemon(null);
              setDeleteDaemonError(null);
            }}
            deleting={deletingDaemonBusy}
            error={deleteDaemonError}
          />
        </ModalPortal>
      )}
      {rotatingDaemon && (
        <ModalPortal>
          <ConfirmRotateTokenModal
            daemon={rotatingDaemon}
            onConfirm={confirmRotateToken}
            onClose={() => {
              setRotatingDaemon(null);
              setRotateError(null);
            }}
            rotating={rotatingBusy}
            error={rotateError}
          />
        </ModalPortal>
      )}
      {addAgentTarget && (
        <ModalPortal>
          <AddAgentModal
            target={addAgentTarget}
            onSave={handleCreateAgent}
            onClose={() => {
              setAddAgentTarget(null);
              setAddAgentError(null);
            }}
            saving={addingAgent}
            error={addAgentError}
          />
        </ModalPortal>
      )}
    </div>
  );
}
