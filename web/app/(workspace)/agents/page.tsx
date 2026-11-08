"use client";
// 代理列表页：数字队友名册。守护进程（daemon）是一级分组，分组头展示设备、版本、
// 心跳、provider 能力与连接状态；代理卡展示 AgentGlyph、状态、当前任务、能力数量与
// Token 用量，点击在分组内展开全宽详情（原位展开，不用覆盖层）。
// 新建/编辑/删除/令牌展示均为内联表单或原位确认，同一时刻只展开一个表单。

import { useState, useEffect, useRef } from "react";
import {
  Monitor,
  Terminal,
  Zap,
  Database,
  X,
  Clock,
  Plus,
  Trash2,
  ChevronDown,
  Check,
  Copy,
  AlertTriangle,
  MonitorPlay,
  Pencil,
  AlertCircle,
  Server,
  KeyRound,
  Cpu,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import { useTranslations } from "next-intl";
import type {
  MappedAgent,
  MappedDaemon,
  Skill,
  McpServer,
} from "@/lib/types";
import { mapAgentFromApi, mapDaemonFromApi } from "@/lib/mappers";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import StatusBadge from "@/lib/ui/StatusBadge";
import type { BadgeTone } from "@/lib/ui/StatusBadge";
import EmptyState from "@/lib/ui/EmptyState";
import InlineConfirm from "@/lib/ui/InlineConfirm";
import AgentGlyph from "@/lib/product/AgentGlyph";
import { agentStatusTone, taskStateTone, providerLabel } from "@/lib/product/presentation";

// 表单控件统一样式：Label 在输入框上方，控件高 34px
const inputCls =
  "w-full h-[34px] bg-surface-2/60 border border-line-strong/50 rounded-[var(--radius-control)] px-3 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary/60";
const textareaCls =
  "w-full bg-surface-2/60 border border-line-strong/50 rounded-[var(--radius-control)] px-3 py-2 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary/60 resize-y";
const labelCls = "block text-xs font-medium text-fg-muted mb-1.5";

interface HistoryItem {
  task: string;
  node: string;
  status: string;
}

// 历史条目状态 → 徽章语义色（失败用 danger；其余复用任务状态映射）
function historyTone(status: string): BadgeTone {
  return status === "failed" ? "danger" : taskStateTone(status);
}

// 历史条目状态 → 文案（未知状态原样展示，不伪造语义）
function historyStatusText(
  status: string,
  tCompleted: string,
  tIntervention: string,
  tInProgress: string
): string {
  if (status === "completed") return tCompleted;
  if (status === "manual_intervention") return tIntervention;
  if (status === "in_progress") return tInProgress;
  return status;
}

// ==========================================
// 守护进程相关：相对时间
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

// ==========================================
// 下拉多选（技能/MCP 绑定编辑用）：浮动菜单走统一样式
// ==========================================
interface MultiSelectOption {
  id: string;
  name: string;
}

function MultiSelect({
  options,
  selected,
  onChange,
  placeholder,
  tone = "accent",
}: {
  options: MultiSelectOption[];
  selected: string[];
  onChange: (val: string[]) => void;
  placeholder: string;
  tone?: "accent" | "success";
}) {
  const t = useTranslations("agents.multiSelect");
  const [isOpen, setIsOpen] = useState(false);

  const chipCls =
    tone === "success"
      ? "bg-success/10 text-success-fg border-success/20"
      : "bg-accent/10 text-accent-fg border-accent/20";
  const hoverBorderCls = tone === "success" ? "hover:border-success/40" : "hover:border-accent/40";

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
        aria-expanded={isOpen}
        className={`w-full min-h-[34px] bg-surface-2/60 border border-line-strong/50 rounded-[var(--radius-control)] px-2.5 py-1 text-left text-sm transition-colors-fast focus-visible:outline-none ${hoverBorderCls} flex items-center justify-between gap-2`}
      >
        <span className="flex flex-wrap gap-1 flex-1 min-w-0">
          {selected.length === 0 ? (
            <span className="text-fg-faint py-0.5">{placeholder}</span>
          ) : (
            selected.map((s) => {
              const label = options.find((opt) => opt.id === s)?.name || s;
              return (
                <span
                  key={s}
                  className={`inline-flex items-center px-2 py-0.5 text-[11px] rounded border ${chipCls}`}
                >
                  {label}
                  <span
                    role="button"
                    tabIndex={0}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleToggle(s);
                    }}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        e.stopPropagation();
                        handleToggle(s);
                      }
                    }}
                    aria-label={label}
                    className="ml-1 hover:text-fg cursor-pointer"
                  >
                    <X className="w-3 h-3" />
                  </span>
                </span>
              );
            })
          )}
        </span>
        <ChevronDown
          className={`w-4 h-4 text-fg-faint shrink-0 transition-transform-fast ${isOpen ? "rotate-180" : ""}`}
        />
      </button>
      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="animate-scale-in absolute top-full left-0 right-0 mt-1 bg-surface-2 border border-line-strong rounded-[var(--radius-panel)] shadow-xl z-50 max-h-48 overflow-y-auto overscroll-contain">
            {options.length === 0 ? (
              <div className="px-3 py-2 text-sm text-fg-faint">{t("noOptions")}</div>
            ) : (
              options.map((opt) => {
                const isSelected = selected.includes(opt.id);
                return (
                  <button
                    key={opt.id}
                    type="button"
                    onClick={() => handleToggle(opt.id)}
                    className={`w-full text-left px-3 py-2 text-sm flex items-center justify-between transition-colors-fast ${
                      isSelected ? chipCls : "text-fg-muted hover:bg-surface-3"
                    }`}
                  >
                    <span className="flex items-center gap-2">
                      {isSelected && <Check className="w-3.5 h-3.5" />}
                      <span>{opt.name}</span>
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
// 在工具运行时下创建代理实例（Web 端创建，下发到该机器由 agentd 物化）
// 内联表单卡：展开在对应守护进程分组的代理区顶部
// ==========================================
interface AddAgentTarget {
  daemon: MappedDaemon;
  provider: string;
  version: string;
}

function AddAgentCard({
  target,
  providers,
  onSave,
  onClose,
  saving,
  error,
}: {
  target: AddAgentTarget;
  providers: { provider: string; version: string }[];
  onSave: (data: { name: string; provider: string; personaKey: string; instructions: string; gitName: string; gitEmail: string }) => void;
  onClose: () => void;
  saving: boolean;
  error: string | null;
}) {
  const t = useTranslations("agents.addDialog");
  const [name, setName] = useState("");
  const [personaKey, setPersonaKey] = useState("");
  const [instructions, setInstructions] = useState("");
  const [gitName, setGitName] = useState("");
  const [gitEmail, setGitEmail] = useState("");
  const [provider, setProvider] = useState(target.provider);
  const formValid = name.trim() !== "" && gitName.trim() !== "" && gitEmail.trim() !== "";

  return (
    <div className="rounded-[var(--radius-card)] border border-primary/30 bg-primary/5 p-4">
      <div className="flex justify-between items-center pb-3 border-b border-line">
        <h3 className="text-sm font-semibold text-fg">{t("inlineTitle")}</h3>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-surface-3 rounded-[var(--radius-control)] text-fg-subtle hover:text-fg transition-colors-fast focus-visible:outline-none"
          title={t("cancel")}
          aria-label={t("cancel")}
        >
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="space-y-3.5 mt-4">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="min-w-0">
            <span className={labelCls}>{t("targetLabel")}</span>
            <span className="text-sm text-fg-muted font-mono">{target.daemon.name}</span>
          </div>
          <div className="min-w-[180px] flex-1 max-w-xs">
            <label className={labelCls}>{t("providerLabel")}</label>
            <select
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
              className={`${inputCls} cursor-pointer`}
            >
              {providers.map((p) => (
                <option key={p.provider} value={p.provider}>
                  {providerLabel(p.provider)}
                  {p.version ? ` · v${p.version}` : ""}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div>
          <label className={labelCls}>
            {t("nameLabel")} <span className="text-danger-fg">*</span>
          </label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value.trim())}
            className={inputCls}
            placeholder={t("namePlaceholder")}
            autoFocus
          />
          <p className="text-[11px] text-fg-faint mt-1.5">{t("nameHint")}</p>
        </div>
        <div>
          <label className={labelCls}>{t("personaKeyLabel")}</label>
          <input
            value={personaKey}
            onChange={(e) => setPersonaKey(e.target.value.trim().toLowerCase())}
            className={`${inputCls} font-mono`}
            placeholder={t("personaKeyPlaceholder")}
          />
          <p className="text-[11px] text-fg-faint mt-1.5">{t("personaKeyHint")}</p>
        </div>
        <div>
          <label className={labelCls}>{t("instructionsLabel")}</label>
          <textarea
            value={instructions}
            onChange={(e) => setInstructions(e.target.value)}
            rows={3}
            className={textareaCls}
            placeholder={t("instructionsPlaceholder")}
          />
          <p className="text-[11px] text-fg-faint mt-1.5">{t("instructionsHint")}</p>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className={labelCls}>
              {t("gitNameLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              value={gitName}
              onChange={(e) => setGitName(e.target.value)}
              className={inputCls}
              placeholder={t("gitNamePlaceholder")}
            />
          </div>
          <div>
            <label className={labelCls}>
              {t("gitEmailLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              value={gitEmail}
              onChange={(e) => setGitEmail(e.target.value.trim())}
              className={inputCls}
              placeholder={t("gitEmailPlaceholder")}
            />
          </div>
        </div>
        <p className="text-[11px] text-fg-faint">{t("gitHint")}</p>
        {error && (
          <p className="text-sm text-danger-fg bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] px-3 py-2">
            {error}
          </p>
        )}
      </div>
      <div className="mt-4 pt-3 border-t border-line flex justify-end gap-2">
        <Button variant="secondary" size="md" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button
          variant="primary"
          size="md"
          loading={saving}
          disabled={!formValid}
          onClick={() =>
            onSave({
              name,
              provider,
              personaKey,
              instructions: instructions.trim(),
              gitName: gitName.trim(),
              gitEmail: gitEmail.trim(),
            })
          }
        >
          {t("submit")}
        </Button>
      </div>
    </div>
  );
}

// ==========================================
// 接入守护进程的内联表单卡：页面顶部原位展开
// ==========================================
function CreateDaemonCard({
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
    <div className="rounded-[var(--radius-card)] border border-primary/30 bg-primary/5 p-4 mb-6">
      <div className="flex justify-between items-center pb-3 border-b border-line">
        <h3 className="text-sm font-semibold text-fg flex items-center">
          <Server className="w-4 h-4 mr-2 text-primary-fg" />
          {t("title")}
        </h3>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-surface-3 rounded-[var(--radius-control)] text-fg-subtle hover:text-fg transition-colors-fast focus-visible:outline-none"
          title={t("cancel")}
          aria-label={t("cancel")}
        >
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="space-y-3.5 mt-4">
        <p className="text-sm text-fg-subtle leading-relaxed">{t("desc")}</p>
        <div>
          <label className={labelCls}>
            {t("nameLabel")} <span className="text-danger-fg">*</span>
          </label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={inputCls}
            placeholder={t("namePlaceholder")}
            autoFocus
          />
        </div>
        {error && (
          <p className="text-sm text-danger-fg bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] px-3 py-2">
            {error}
          </p>
        )}
      </div>
      <div className="mt-4 pt-3 border-t border-line flex justify-end gap-2">
        <Button variant="secondary" size="md" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button variant="primary" size="md" loading={saving} disabled={!name.trim()} onClick={() => onSave(name.trim())}>
          {t("submit")}
        </Button>
      </div>
    </div>
  );
}

// ==========================================
// daemon token 一次性展示的内联提示条：挂在对应分组头部下方，关闭后不再重现
// ==========================================
function DaemonTokenBanner({
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
    <div className="mx-4 mt-3 rounded-[var(--radius-card)] border border-warning/30 bg-warning/5 p-4">
      <div className="flex justify-between items-center pb-3 border-b border-warning/20">
        <h3 className="text-sm font-semibold text-fg flex items-center">
          <AlertTriangle className="w-4 h-4 text-warning-fg shrink-0 mr-2" />
          {t("title")}
        </h3>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-surface-3 rounded-[var(--radius-control)] text-fg-subtle hover:text-fg transition-colors-fast focus-visible:outline-none"
          title={t("close")}
          aria-label={t("close")}
        >
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="space-y-3.5 mt-3.5">
        <div className="rounded-[var(--radius-control)] border border-warning/20 px-3 py-2.5">
          <p className="text-sm text-warning-fg font-medium mb-1">{t("importantTitle")}</p>
          <p className="text-xs text-warning-fg/70">{t("importantDesc")}</p>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1.5">
            <span className="block text-xs font-medium text-fg-muted">
              {t("tokenLabel")} · {daemonName}
            </span>
            <Button variant="secondary" size="sm" onClick={handleCopy}>
              {copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              {copied ? t("copied") : t("copy")}
            </Button>
          </div>
          <div className="bg-bg border border-line rounded-[var(--radius-control)] p-3 break-all font-mono text-sm text-fg select-all">
            {daemonToken}
          </div>
        </div>

        <div className="rounded-[var(--radius-control)] border border-line bg-surface-2/50 p-3.5">
          <p className="text-xs font-medium text-fg-subtle mb-2.5">{t("stepsTitle")}</p>
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

        <div className="flex justify-end">
          <Button variant="primary" size="md" onClick={onClose}>
            {t("close")}
          </Button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 删除机器的原位二次确认簇：替换分组头部按钮组（携带实例数量警示文案）
// ==========================================
function DaemonDeleteConfirm({
  daemon,
  onConfirm,
  onCancel,
  deleting,
  error,
}: {
  daemon: MappedDaemon;
  onConfirm: () => void;
  onCancel: () => void;
  deleting: boolean;
  error: string | null;
}) {
  const t = useTranslations("daemons.deleteDialog");
  return (
    <div className="flex items-center gap-2 flex-wrap justify-end">
      <span className="flex items-center gap-1.5 text-xs text-danger-fg leading-relaxed">
        <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
        {t.rich("confirm", {
          name: daemon.name,
          count: daemon.agentCount,
          span: (chunks) => <span className="text-fg font-medium">{chunks}</span>,
        })}
      </span>
      {error && (
        <span className="text-xs text-danger-fg bg-danger/10 border border-danger/20 rounded px-2 py-1">
          {error}
        </span>
      )}
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={deleting}>
          {t("cancel")}
        </Button>
        <Button variant="danger" size="sm" loading={deleting} onClick={onConfirm}>
          <Trash2 className="w-3.5 h-3.5" />
          {t("confirmDelete")}
        </Button>
      </div>
    </div>
  );
}

// ==========================================
// 重置 token 的原位二次确认簇：warning 确认态（旧 token 立即失效）
// ==========================================
function DaemonRotateConfirm({
  daemon,
  onConfirm,
  onCancel,
  rotating,
  error,
}: {
  daemon: MappedDaemon;
  onConfirm: () => void;
  onCancel: () => void;
  rotating: boolean;
  error: string | null;
}) {
  const t = useTranslations("daemons.rotateDialog");
  return (
    <div className="flex items-center gap-2 flex-wrap justify-end">
      <span className="flex items-center gap-1.5 text-xs text-warning-fg leading-relaxed">
        <KeyRound className="w-3.5 h-3.5 shrink-0" />
        {t.rich("confirm", {
          name: daemon.name,
          span: (chunks) => <span className="text-fg font-medium">{chunks}</span>,
        })}
      </span>
      {error && (
        <span className="text-xs text-danger-fg bg-danger/10 border border-danger/20 rounded px-2 py-1">
          {error}
        </span>
      )}
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel} disabled={rotating}>
          {t("cancel")}
        </Button>
        <button
          type="button"
          onClick={onConfirm}
          disabled={rotating}
          className="inline-flex items-center justify-center whitespace-nowrap h-7 px-2.5 text-xs rounded-[var(--radius-control)] gap-1 font-medium bg-warning-strong hover:bg-warning text-fg btn-press disabled:opacity-50 disabled:cursor-not-allowed disabled:pointer-events-none focus-visible:outline-none transition-colors-fast"
        >
          <KeyRound className="w-3.5 h-3.5" />
          {t("confirmRotate")}
        </button>
      </div>
    </div>
  );
}

// ==========================================
// 代理详情区（全宽，查看 + 内置编辑模式；挂载在展开的代理卡内部）
// ==========================================
function AgentDetailSection({
  agent,
  onSaveEdit,
  saving,
  skills,
  mcpServers,
  onEditStart,
  canEdit,
  onDelete,
  deleting,
  deleteError,
}: {
  agent: MappedAgent;
  onSaveEdit: (id: string, fields: Partial<MappedAgent>) => Promise<boolean>;
  saving: boolean;
  skills: Skill[];
  mcpServers: McpServer[];
  /** 进入编辑表单时收起页面其他内联表单（同页同时只展开一个表单） */
  onEditStart: () => void;
  canEdit: boolean;
  onDelete: (id: string) => void;
  deleting: boolean;
  deleteError: string | null;
}) {
  const t = useTranslations("agents.detail");
  const tc = useTranslations("agents");
  const te = useTranslations("agents.edit");
  const td = useTranslations("agents.deleteDialog");
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
    onEditStart();
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

  const sectionLabelCls =
    "text-xs font-semibold text-fg-faint uppercase tracking-wider mb-2 flex items-center";

  if (editing) {
    return (
      <div className="border-t border-line px-4 pb-4 pt-3 space-y-4">
        <div>
          <label className={labelCls}>{te("identityLabel")}</label>
          <textarea
            value={identity}
            onChange={(e) => setIdentity(e.target.value)}
            rows={3}
            className={`${textareaCls} font-mono text-fg-muted resize-none`}
          />
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className={labelCls}>{te("gitNameLabel")}</label>
            <input
              value={gitName}
              onChange={(e) => setGitName(e.target.value)}
              className={inputCls}
              placeholder={te("gitNamePlaceholder")}
            />
          </div>
          <div>
            <label className={labelCls}>{te("gitEmailLabel")}</label>
            <input
              value={gitEmail}
              onChange={(e) => setGitEmail(e.target.value)}
              className={inputCls}
              placeholder={te("gitEmailPlaceholder")}
            />
          </div>
        </div>
        <div>
          <label className={`${labelCls} flex items-center`}>
            <Zap className="w-3.5 h-3.5 mr-1.5 text-accent-fg" /> {te("skillsLabel")}
          </label>
          <MultiSelect
            options={enabledSkills.map((s) => ({ id: s.id, name: s.name }))}
            selected={selectedSkills}
            onChange={setSelectedSkills}
            placeholder={te("skillsPlaceholder")}
            tone="accent"
          />
        </div>
        <div>
          <label className={`${labelCls} flex items-center`}>
            <Database className="w-3.5 h-3.5 mr-1.5 text-success-fg" /> {te("mcpLabel")}
          </label>
          <MultiSelect
            options={enabledMcps.map((m) => ({ id: m.id, name: m.name }))}
            selected={selectedMcps}
            onChange={setSelectedMcps}
            placeholder={te("mcpPlaceholder")}
            tone="success"
          />
        </div>
        <div className="pt-3 border-t border-line flex justify-end gap-2">
          <Button variant="secondary" size="md" onClick={() => setEditing(false)}>
            {te("cancel")}
          </Button>
          <Button variant="primary" size="md" loading={saving} onClick={handleSave}>
            {te("save")}
          </Button>
        </div>
      </div>
    );
  }

  const historyItems = (agent.history as HistoryItem[]) || [];
  const tokenUsage = agent.tokenUsage || { input: 0, output: 0 };

  return (
    <div className="border-t border-line px-4 pb-4 pt-3">
      <div className="flex items-center justify-end gap-2">
        {deleteError && (
          <span className="text-xs text-danger-fg bg-danger/10 border border-danger/20 rounded px-2 py-1 mr-auto">
            {deleteError}
          </span>
        )}
        <Button variant="secondary" size="sm" onClick={enterEdit}>
          <Pencil className="w-3.5 h-3.5" /> {t("editButton")}
        </Button>
        <InlineConfirm
          label={tc("deleteButton")}
          onConfirm={() => onDelete(agent.id)}
          confirmLabel={td("confirmDelete")}
          cancelLabel={td("cancel")}
          busy={deleting}
          disabled={!canEdit}
        />
      </div>

      <div className="mt-3 grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-5">
        {agent.personaKey && (
          <div>
            <div className={sectionLabelCls}>
              <span className="flex-1">{t("personaLabel")}</span>
              <button
                onClick={() => navigator.clipboard?.writeText(agent.personaKey || "")}
                className="text-[11px] px-2 py-0.5 bg-surface-2 hover:bg-surface-3 rounded text-fg-subtle hover:text-fg transition-colors-fast focus-visible:outline-none"
                title={t("personaCopy")}
              >
                {t("personaCopy")}
              </button>
            </div>
            <div
              className="bg-bg border border-line rounded-[var(--radius-control)] px-3 py-1.5 text-sm text-fg-muted font-mono break-all select-all"
              title={t("personaBadge", { key: agent.personaKey })}
            >
              {agent.personaKey}
            </div>
          </div>
        )}
        <div>
          <div className={sectionLabelCls}>{t("gitLabel")}</div>
          <div className="bg-bg border border-line rounded-[var(--radius-control)] px-3 py-1.5 text-sm text-fg-muted font-mono break-all">
            {agent.gitName || agent.gitEmail ? `${agent.gitName} <${agent.gitEmail}>` : "—"}
          </div>
        </div>
        <div className="lg:col-span-2">
          <div className={sectionLabelCls}>{t("identityLabel")}</div>
          <div className="bg-bg border border-line rounded-[var(--radius-control)] p-3 text-sm text-fg-muted leading-relaxed font-mono whitespace-pre-wrap break-words">
            {agent.identityInstruction}
          </div>
        </div>
        <div>
          <div className={sectionLabelCls}>{tc("cardSkills")}</div>
          <div className="flex flex-wrap gap-1.5">
            {(agent.skills || []).length === 0 ? (
              <span className="text-sm text-fg-faint">—</span>
            ) : (
              (agent.skills || []).map((s) => (
                <span
                  key={typeof s === "string" ? s : s.id}
                  className="px-2 py-0.5 bg-accent/10 border border-accent/20 text-accent-fg text-xs rounded"
                >
                  {typeof s === "string" ? s : s.name}
                </span>
              ))
            )}
          </div>
        </div>
        <div>
          <div className={sectionLabelCls}>{tc("cardMcp")}</div>
          <div className="flex flex-wrap gap-1.5">
            {(agent.mcpServers || []).length === 0 ? (
              <span className="text-sm text-fg-faint">—</span>
            ) : (
              (agent.mcpServers || []).map((m) => (
                <span
                  key={typeof m === "string" ? m : m.id}
                  className="px-2 py-0.5 bg-success/10 border border-success/20 text-success-fg text-xs rounded"
                >
                  {typeof m === "string" ? m : m.name}
                </span>
              ))
            )}
          </div>
        </div>
        <div>
          <div className={sectionLabelCls}>{t("tokenUsageLabel")}</div>
          <div className="bg-bg border border-line rounded-[var(--radius-control)] px-3 py-1.5 text-sm text-fg-muted">
            {t("inputToken")}{" "}
            <span className="font-mono text-fg">{tokenUsage.input.toLocaleString()}</span>
            <span className="mx-2 text-fg-faint">·</span>
            {t("outputToken")}{" "}
            <span className="font-mono text-fg">{tokenUsage.output.toLocaleString()}</span>
          </div>
        </div>
        <div>
          <div className={sectionLabelCls}>{t("historyLabel")}</div>
          {historyItems.length === 0 ? (
            <div className="bg-bg border border-line rounded-[var(--radius-control)] px-3 py-1.5 text-sm text-fg-faint">
              —
            </div>
          ) : (
            <div className="space-y-1.5">
              {historyItems.map((h, i) => (
                <div
                  key={i}
                  className="flex items-center justify-between gap-2 bg-bg border border-line rounded-[var(--radius-control)] px-3 py-1.5"
                >
                  <div className="flex items-center text-sm min-w-0">
                    <span className="text-fg-muted font-mono truncate">{h.task}</span>
                    <span className="text-fg-faint mx-2 shrink-0">—</span>
                    <span className="text-fg-subtle truncate">{h.node}</span>
                  </div>
                  <StatusBadge tone={historyTone(h.status)} className="shrink-0">
                    {historyStatusText(h.status, t("historyCompleted"), t("historyIntervention"), t("historyInProgress"))}
                  </StatusBadge>
                </div>
              ))}
            </div>
          )}
        </div>
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

/** PageHeader meta 里的状态计数块 */
function MetaStat({ dot, label, value }: { dot: string; label: string; value: number }) {
  return (
    <span className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-[var(--radius-control)] border border-line bg-surface-2/40">
      <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${dot}`} />
      <span className="text-xs text-fg-subtle whitespace-nowrap">{label}</span>
      <span className="text-xs font-semibold text-fg font-mono">{value}</span>
    </span>
  );
}

export default function AgentsPage() {
  const t = useTranslations("agents");
  const td = useTranslations("daemons");
  const { canEdit } = usePermission();

  const agents = useAppStore((s) => s.agents);
  const setAgents = useAppStore((s) => s.setAgents);
  const tasks = useAppStore((s) => s.tasks);
  const skills = useAppStore((s) => s.skills);
  const mcpServers = useAppStore((s) => s.mcpServers);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  // 展开的代理详情：全局同时只展开一个；null = 全部收起
  const [expandedAgentId, setExpandedAgentId] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [daemons, setDaemons] = useState<MappedDaemon[]>([]);
  const [showCreateDaemon, setShowCreateDaemon] = useState(false);
  const [creatingDaemon, setCreatingDaemon] = useState(false);
  const [createDaemonError, setCreateDaemonError] = useState<string | null>(null);
  const [newDaemonToken, setNewDaemonToken] = useState<{ daemonId: string; name: string; token: string } | null>(null);
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
  // 折叠状态：daemon 分组一级，key 形如 "daemon:{id}"
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

  // 每 10 秒轮询守护进程与代理状态；tick 驱动心跳相对时间重新渲染
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

  // 支持全局搜索跳转 /agents?id=<agentId>:展开并滚动定位对应实例卡片,然后清理 URL 参数
  useEffect(() => {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    const id = params.get("id");
    if (!id) return;
    const target = agents.find((a) => a.id === id);
    // 列表已加载却未命中说明 id 无效,清掉参数停止每次轮询重试;列表未加载(空)则留待下次
    if (!target && agents.length > 0) {
      params.delete("id");
      const qsInvalid = params.toString();
      window.history.replaceState(
        {},
        "",
        window.location.pathname + (qsInvalid ? `?${qsInvalid}` : "")
      );
      return;
    }
    if (!target) return;
    setExpandedAgentId(id);
    // 目标实例所在机器分组可能处于折叠态,展开后再滚动定位
    setCollapsed((prev) => {
      const next = new Set(prev);
      next.delete(`daemon:${target.daemonId}`);
      return next;
    });
    params.delete("id");
    const qs = params.toString();
    window.history.replaceState({}, "", window.location.pathname + (qs ? `?${qs}` : ""));
    // 双帧 rAF:等展开+解除折叠的重渲染落 DOM 后再定位
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        document
          .getElementById(`agent-card-${id}`)
          ?.scrollIntoView({ behavior: "smooth", block: "center" });
      })
    );
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
      if (created?.daemon_token && created?.id) {
        setNewDaemonToken({ daemonId: created.id, name: created.name || name, token: created.daemon_token });
      }
    } catch (err) {
      setCreateDaemonError(err instanceof Error ? err.message : td("createFailed"));
    } finally {
      setCreatingDaemon(false);
    }
  };

  // 打开接入守护进程的内联表单;与「添加代理」表单互斥,同页同时只展开一处表单
  const openCreateDaemonForm = () => {
    setAddAgentError(null);
    setAddAgentTarget(null);
    setCreateDaemonError(null);
    setExpandedAgentId(null);
    setShowCreateDaemon(true);
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

  // 在指定守护进程下打开「添加代理」表单（默认选中第一个检测到的工具）；
  // 同一分组重复点击为收起。与接入表单、详情编辑表单互斥。
  const openAddAgentForDaemon = (daemon: MappedDaemon) => {
    const provider = daemon.providers.find((p) => p.installed);
    if (!provider) return;
    setAddAgentError(null);
    setCreateDaemonError(null);
    setShowCreateDaemon(false);
    setExpandedAgentId(null);
    if (addAgentTarget?.daemon.id === daemon.id) {
      setAddAgentTarget(null);
      return;
    }
    setAddAgentTarget({ daemon, provider: provider.provider, version: provider.version });
    setCollapsed((prev) => {
      const next = new Set(prev);
      next.delete(`daemon:${daemon.id}`);
      return next;
    });
  };

  // 页头「添加代理」：定位到第一个有可用工具的守护进程；无可选目标时按钮禁用
  const addAgentTargetAvailable = daemons.some((d) => d.providers.some((p) => p.installed));
  const openAddAgentAnywhere = () => {
    const daemon = daemons.find((d) => d.providers.some((p) => p.installed));
    if (daemon) openAddAgentForDaemon(daemon);
  };

  // Web 端创建实例：server 记录后下发给该机器的 agentd,
  // 列表先显示"待上线",agentd 物化上线由 10s 轮询自动呈现
  const handleCreateAgent = async (data: { name: string; provider: string; personaKey: string; instructions: string; gitName: string; gitEmail: string }) => {
    if (!addAgentTarget || !currentWorkspaceId) return;
    setAddingAgent(true);
    setAddAgentError(null);
    try {
      await api.createDaemonAgent(currentWorkspaceId, addAgentTarget.daemon.id, {
        name: data.name,
        provider: data.provider,
        persona_key: data.personaKey,
        instructions: data.instructions,
        git_name: data.gitName,
        git_email: data.gitEmail,
      });
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

  // 重置 daemon token:旧 token 立即失效,新 token 仅展示一次(复用 token 内联提示条)
  const confirmRotateToken = async () => {
    if (!rotatingDaemon || !currentWorkspaceId) return;
    setRotatingBusy(true);
    setRotateError(null);
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const res: any = await api.rotateDaemonToken(currentWorkspaceId, rotatingDaemon.id);
      setRotatingDaemon(null);
      if (res?.daemon_token) {
        setNewDaemonToken({ daemonId: rotatingDaemon.id, name: rotatingDaemon.name, token: res.daemon_token });
        // 提示条挂在分组头部下方,确保该分组处于展开态可见
        setCollapsed((prev) => {
          const next = new Set(prev);
          next.delete(`daemon:${rotatingDaemon.id}`);
          return next;
        });
      }
    } catch (err) {
      setRotateError(err instanceof Error ? err.message : td("rotateFailed"));
    } finally {
      setRotatingBusy(false);
    }
  };

  // 详情区内的保存:成功返回 true(面板退回查看态),失败返回 false(停留编辑态)
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
      return true;
    } catch (err) {
      console.error("Update agent failed:", err);
      return false;
    } finally {
      setSaving(false);
    }
  };

  const confirmDelete = async (id: string) => {
    if (!currentWorkspaceId) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await api.deleteAgent(currentWorkspaceId, id);
      setAgents(agents.filter((a) => a.id !== id));
      setExpandedAgentId((prev) => (prev === id ? null : prev));
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : t("deleteFailed"));
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

  const countOnline = agents.filter((a) => a.status === "online").length;
  const countBusy = agents.filter((a) => a.status === "busy").length;
  const countWaiting = agents.filter((a) => a.status === "paused" || a.status === "pending").length;
  const countOffline = agents.filter((a) => a.status === "offline").length;

  const renderAgentCard = (agent: MappedAgent, detected?: Set<string>) => {
    const currentTask =
      tasks?.find(
        (task) => String(task.id) === agent.task?.split(" ")[0]
      ) || null;
    const expanded = expandedAgentId === agent.id;
    const hasActiveTask = !!currentTask || !!agent.task;
    return (
      <div
        key={agent.id}
        id={`agent-card-${agent.id}`}
        className={`relative rounded-[var(--radius-card)] border transition-colors-fast ${
          expanded
            ? "border-primary/50 bg-primary/5"
            : "border-line-strong/50 bg-surface-2/30 hover:bg-surface-2/60 hover:border-line-strong"
        } ${agent.status === "offline" ? "opacity-60" : ""}`}
      >
        {hasActiveTask && (
          <span aria-hidden="true" className="absolute left-0 top-3 bottom-3 w-0.5 rounded-r-full bg-accent-fg" />
        )}
        <div
          role="button"
          tabIndex={0}
          onClick={() => setExpandedAgentId(expanded ? null : agent.id)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              setExpandedAgentId(expanded ? null : agent.id);
            }
          }}
          aria-expanded={expanded}
          className="w-full text-left px-4 pt-4 pb-3.5 cursor-pointer focus-visible:outline-none"
        >
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-start gap-3 min-w-0">
              <AgentGlyph name={agent.name} status={agent.status} size="md" />
              <div className="min-w-0 pt-0.5">
                <h3 className="text-sm font-semibold text-fg truncate">{agent.name}</h3>
                <div className="mt-0.5 flex items-center gap-1.5 text-xs text-fg-subtle font-mono min-w-0">
                  <Terminal className="w-3 h-3 shrink-0" />
                  <span className="truncate">{providerLabel(agent.provider)}</span>
                  {detected && !detected.has(agent.provider) && (
                    <span className="inline-flex items-center gap-0.5 shrink-0 text-warning-fg text-[10px] px-1 rounded border border-warning/25 bg-warning/10">
                      <AlertCircle className="w-2.5 h-2.5" />
                      {td("providerMissing")}
                    </span>
                  )}
                </div>
              </div>
            </div>
            <StatusBadge tone={agentStatusTone(agent.status)} dot className="shrink-0 mt-0.5">
              {t(`status.${agent.status}`)}
            </StatusBadge>
          </div>
          <div className="mt-3 flex items-center gap-x-3.5 gap-y-1 flex-wrap text-xs text-fg-subtle">
            <span className="flex items-center gap-1 min-w-0">
              <span className="text-fg-faint shrink-0">{t("currentTask")}</span>
              <span className="text-fg-muted truncate max-w-[180px]">
                {currentTask?.title || agent.task || t("idle")}
              </span>
            </span>
            <span className="flex items-center gap-1 shrink-0" title={t("cardSkills")}>
              <Zap className="w-3 h-3 text-accent-fg shrink-0" />
              <span className="font-mono text-fg-muted">{(agent.skills || []).length}</span>
            </span>
            <span className="flex items-center gap-1 shrink-0" title={t("cardMcp")}>
              <Database className="w-3 h-3 text-success-fg shrink-0" />
              <span className="font-mono text-fg-muted">{(agent.mcpServers || []).length}</span>
            </span>
            <span
              className="flex items-center gap-1 shrink-0"
              title={t("detail.tokenUsageLabel")}
            >
              <Cpu className="w-3 h-3 text-fg-faint shrink-0" />
              <span className="font-mono text-fg-muted">
                {((agent.tokenUsage?.input || 0) + (agent.tokenUsage?.output || 0)).toLocaleString()}
              </span>
            </span>
          </div>
        </div>
        {expanded && (
          <AgentDetailSection
            agent={agent}
            onSaveEdit={handleEditSave}
            saving={saving}
            skills={skills}
            mcpServers={mcpServers}
            onEditStart={() => {
              setShowCreateDaemon(false);
              setAddAgentTarget(null);
            }}
            canEdit={canEdit}
            onDelete={confirmDelete}
            deleting={deleting}
            deleteError={deleteError}
          />
        )}
      </div>
    );
  };

  const agentGridCls = "grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4";

  return (
    <div className="h-full flex flex-col page-enter overflow-y-auto" data-tick={tick}>
      <PageHeader
        title={t("title")}
        description={t("subtitle", { count: agents.length })}
        meta={
          <>
            <MetaStat dot="bg-success-fg" label={t("meta.online")} value={countOnline} />
            <MetaStat dot="bg-accent-fg" label={t("meta.busy")} value={countBusy} />
            <MetaStat dot="bg-warning-fg" label={t("meta.waiting")} value={countWaiting} />
            <MetaStat dot="bg-fg-faint" label={t("meta.offline")} value={countOffline} />
          </>
        }
        actions={
          <>
            <Button
              variant="secondary"
              size="lg"
              onClick={openAddAgentAnywhere}
              disabled={!canEdit || !addAgentTargetAvailable}
              title={addAgentTargetAvailable ? undefined : td("noProviders")}
            >
              <Plus className="w-4 h-4" /> {t("addAgent")}
            </Button>
            <Button variant="primary" size="lg" onClick={openCreateDaemonForm} disabled={!canEdit}>
              <Monitor className="w-4 h-4" /> {td("addButton")}
            </Button>
          </>
        }
      />

      <div className="px-6 pb-8">
        {showCreateDaemon && (
          <CreateDaemonCard
            onSave={handleCreateDaemon}
            onClose={() => {
              setShowCreateDaemon(false);
              setCreateDaemonError(null);
            }}
            saving={creatingDaemon}
            error={createDaemonError}
          />
        )}

        {daemons.length === 0 && agents.length === 0 ? (
          <EmptyState
            icon={Server}
            title={td("empty.title")}
            description={td("empty.desc")}
            className="mt-8"
            action={
              <Button variant="primary" size="lg" onClick={openCreateDaemonForm} disabled={!canEdit}>
                <Monitor className="w-4 h-4" /> {td("addButton")}
              </Button>
            }
          />
        ) : (
          <div className="space-y-8">
            {groups.map(({ daemon, groupAgents }) => {
              const daemonCollapsed = collapsed.has(`daemon:${daemon.id}`);
              const detected = daemon.providers.filter((p) => p.installed);
              const detectedSet = new Set(detected.map((d) => d.provider));
              const addFormOpen = !!addAgentTarget && addAgentTarget.daemon.id === daemon.id;
              // 展开详情的代理提到分组卡片区顶部全宽展示，其余保持原顺序
              const expandedAgent = groupAgents.find((a) => a.id === expandedAgentId);
              const restAgents = groupAgents.filter((a) => a.id !== expandedAgentId);
              return (
                <section key={daemon.id} className="rounded-[var(--radius-card)] border border-line bg-surface-2/20">
                  <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3 border-b border-line">
                    <button
                      type="button"
                      onClick={() => toggleCollapse(`daemon:${daemon.id}`)}
                      aria-expanded={!daemonCollapsed}
                      className="flex items-center gap-3 text-left min-w-0 focus-visible:outline-none"
                    >
                      <span className="w-9 h-9 rounded-[var(--radius-control)] bg-primary/10 border border-primary/20 flex items-center justify-center shrink-0">
                        <MonitorPlay className="w-4 h-4 text-primary-fg" />
                      </span>
                      <span className="min-w-0">
                        <span className="flex items-center gap-2 flex-wrap">
                          <span className="text-sm font-semibold text-fg">{daemon.name}</span>
                          <StatusBadge
                            tone={daemon.status === "online" ? "success" : "neutral"}
                            dot
                            className="shrink-0"
                          >
                            {td(`status.${daemon.status}`)}
                          </StatusBadge>
                          <ChevronDown
                            className={`w-3.5 h-3.5 text-fg-faint transition-transform-fast ${daemonCollapsed ? "" : "rotate-180"}`}
                          />
                        </span>
                        <span className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-fg-faint">
                          <span>
                            {daemon.deviceName || td("noDevice")}
                            {daemon.version ? ` · v${daemon.version}` : ""}
                          </span>
                          <span className="flex items-center gap-1">
                            <Clock className="w-3 h-3" />
                            {td("heartbeat")} {formatRelative(td, daemon.lastHeartbeat)}
                          </span>
                          <span>{td("agentsCount", { count: groupAgents.length })}</span>
                          {detected.length > 0
                            ? detected.map((p) => (
                                <span key={p.provider} className="font-mono">
                                  {providerLabel(p.provider)}
                                  {p.version ? ` v${p.version}` : ""}
                                </span>
                              ))
                            : (
                                <span>{td("noProviders")}</span>
                              )}
                        </span>
                      </span>
                    </button>
                    <div className="flex items-center gap-2 shrink-0">
                      {deletingDaemon?.id === daemon.id ? (
                        <DaemonDeleteConfirm
                          daemon={deletingDaemon}
                          onConfirm={confirmDeleteDaemon}
                          onCancel={() => {
                            setDeletingDaemon(null);
                            setDeleteDaemonError(null);
                          }}
                          deleting={deletingDaemonBusy}
                          error={deleteDaemonError}
                        />
                      ) : rotatingDaemon?.id === daemon.id ? (
                        <DaemonRotateConfirm
                          daemon={rotatingDaemon}
                          onConfirm={confirmRotateToken}
                          onCancel={() => {
                            setRotatingDaemon(null);
                            setRotateError(null);
                          }}
                          rotating={rotatingBusy}
                          error={rotateError}
                        />
                      ) : (
                        <>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => openAddAgentForDaemon(daemon)}
                            disabled={!canEdit || detected.length === 0}
                            title={detected.length === 0 ? td("noProviders") : undefined}
                          >
                            <Plus className="w-3.5 h-3.5" /> {t("addAgent")}
                          </Button>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => {
                              // 与删除确认互斥,同一时刻只有一个确认态
                              setDeleteDaemonError(null);
                              setDeletingDaemon(null);
                              setRotateError(null);
                              setRotatingDaemon(daemon);
                            }}
                          >
                            <KeyRound className="w-3.5 h-3.5" /> {td("rotateButton")}
                          </Button>
                          <Button
                            variant="danger-ghost"
                            size="sm"
                            disabled={!canEdit}
                            onClick={() => {
                              setRotateError(null);
                              setRotatingDaemon(null);
                              setDeleteDaemonError(null);
                              setDeletingDaemon(daemon);
                            }}
                          >
                            <Trash2 className="w-3.5 h-3.5" /> {td("deleteButton")}
                          </Button>
                        </>
                      )}
                    </div>
                  </div>

                  {newDaemonToken?.daemonId === daemon.id && (
                    <DaemonTokenBanner
                      daemonName={newDaemonToken.name}
                      daemonToken={newDaemonToken.token}
                      onClose={() => setNewDaemonToken(null)}
                    />
                  )}

                  {!daemonCollapsed && (
                    <div className="p-4">
                      {addFormOpen && addAgentTarget && (
                        <div className="mb-4">
                          <AddAgentCard
                            target={addAgentTarget}
                            providers={detected.map((d) => ({ provider: d.provider, version: d.version }))}
                            onSave={handleCreateAgent}
                            onClose={() => {
                              setAddAgentTarget(null);
                              setAddAgentError(null);
                            }}
                            saving={addingAgent}
                            error={addAgentError}
                          />
                        </div>
                      )}
                      {groupAgents.length === 0 && !addFormOpen ? (
                        <p className="text-xs text-fg-faint py-1">{td("noAgents")}</p>
                      ) : (
                        <>
                          {expandedAgent && (
                            <div className="mb-4">{renderAgentCard(expandedAgent, detectedSet)}</div>
                          )}
                          {restAgents.length > 0 && (
                            <div className={agentGridCls}>
                              {restAgents.map((agent) => renderAgentCard(agent, detectedSet))}
                            </div>
                          )}
                        </>
                      )}
                    </div>
                  )}
                </section>
              );
            })}
            {ungrouped.length > 0 && (
              <section>
                <div className="flex items-center gap-2 pb-3 border-b border-line">
                  <AlertCircle className="w-4 h-4 text-warning-fg shrink-0" />
                  <h2 className="text-sm font-semibold text-fg-muted">{t("ungrouped")}</h2>
                </div>
                <div className="mt-4">
                  {(() => {
                    const expandedAgent = ungrouped.find((a) => a.id === expandedAgentId);
                    const restAgents = ungrouped.filter((a) => a.id !== expandedAgentId);
                    return (
                      <>
                        {expandedAgent && (
                          <div className="mb-4">{renderAgentCard(expandedAgent)}</div>
                        )}
                        {restAgents.length > 0 && (
                          <div className={agentGridCls}>
                            {restAgents.map((agent) => renderAgentCard(agent))}
                          </div>
                        )}
                      </>
                    );
                  })()}
                </div>
              </section>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
