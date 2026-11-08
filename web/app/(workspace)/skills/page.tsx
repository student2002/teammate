"use client";
// 技能管理页:Skill 与 MCP 使用同一实体卡骨架;新增/编辑在卡片区内内联展开(文档流推开),
// 同一时刻只展开一个编辑表单;删除在被占用时展示占用代理列表,未占用时走 InlineConfirm 原位确认;
// 所有错误以表单内/列表上的 danger 提示条展示,不使用遮罩弹窗和 alert。

import { useState } from "react";
import { useTranslations } from "next-intl";
import {
  Zap,
  Database,
  Server,
  Plus,
  X,
  Edit2,
  Trash2,
  Search,
  ToggleLeft,
  ToggleRight,
  Wifi,
  Tag,
  AlertTriangle,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
import api, { ApiError } from "@/lib/api";
import EmptyStateGuide from "@/lib/EmptyStateGuide";
import Button from "@/lib/ui/Button";
import PageHeader from "@/lib/ui/PageHeader";
import SectionHeader from "@/lib/ui/SectionHeader";
import StatusBadge from "@/lib/ui/StatusBadge";
import EmptyState from "@/lib/ui/EmptyState";
import InlineConfirm from "@/lib/ui/InlineConfirm";
import type { Skill, McpAuthType, McpServer, McpServerType } from "@/lib/types";
import { mapMcpServerFromApi, mapSkillFromApi } from "@/lib/mappers";

type LocalSkill = Skill;
type LocalMcpServer = McpServer;
type UsingAgent = { id: string; name: string };

// 编辑表单状态:全页同一时刻只存在一个(kind 互斥)
type EditFormState =
  | { kind: "skill-add" }
  | { kind: "skill-edit"; skill: LocalSkill }
  | { kind: "mcp-add" }
  | { kind: "mcp-edit"; mcp: LocalMcpServer };

// 删除被占用时的原位展示状态(非弹窗)
type BlockedDeleteState = {
  type: "skill" | "mcp";
  id: string;
  usingAgents: UsingAgent[];
};

// 分类数据值为中文(存库),显示层通过此映射转为翻译 key
const categoryKeyMap: Record<string, string> = {
  "前端": "frontend",
  "后端": "backend",
  "数据": "data",
  "安全": "security",
  "运维": "ops",
  "文档": "docs",
  "性能": "performance",
  "质量": "quality",
  "设计": "design",
  "存储": "storage",
  "代码": "code",
  "数据库": "database",
  "服务": "service",
};

// 表单区内的 danger 提示条(alert 的替代)
function FormErrorStrip({ message }: { message: string }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-2 bg-danger/10 border border-danger/30 rounded-[var(--radius-control)] px-3 py-2.5 text-sm text-danger-fg"
    >
      <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
      <span className="min-w-0 break-all">{message}</span>
    </div>
  );
}

const inputCls =
  "w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg focus:outline-none focus:border-primary transition-colors-fast";
const textareaCls =
  "w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 py-2 text-sm text-fg-muted focus:outline-none focus:border-primary transition-colors-fast";
const labelCls = "block text-sm font-medium text-fg-muted mb-1.5";

// ==========================================
// Skill 内联编辑/新增表单(无遮罩,嵌入卡片或区块顶部)
// ==========================================
function SkillForm({
  skill,
  onSave,
  onClose,
}: {
  skill?: LocalSkill | null;
  onSave: (data: LocalSkill) => Promise<void>;
  onClose: () => void;
}) {
  const t = useTranslations("skills.skillForm");
  const tCat = useTranslations("skills.category");
  const tErr = useTranslations("skills");
  const [name, setName] = useState(skill?.name || "");
  const [category, setCategory] = useState(skill?.category || "前端");
  const [description, setDescription] = useState(skill?.description || "");
  const [promptTemplate, setPromptTemplate] = useState(skill?.promptTemplate || "");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const categories = [
    "前端",
    "后端",
    "数据",
    "安全",
    "运维",
    "文档",
    "性能",
    "质量",
  ];

  const handleSave = async () => {
    if (!name.trim() || saving) return;
    setSaving(true);
    setError("");
    try {
      await onSave({
        id: skill?.id || "",
        workspaceId: skill?.workspaceId || "",
        name: name.trim(),
        category,
        description: description.trim(),
        promptTemplate: promptTemplate.trim(),
      });
      onClose();
    } catch (err) {
      setError(tErr("errSaveSkill") + (err instanceof Error ? err.message : String(err)));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <label className={labelCls}>
          {t("nameLabel")} <span className="text-danger-fg">*</span>
        </label>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={inputCls}
          placeholder={t("namePlaceholder")}
        />
      </div>
      <div>
        <label className={labelCls}>{t("categoryLabel")}</label>
        <select
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          className={`${inputCls} cursor-pointer`}
        >
          {categories.map((c) => (
            <option key={c} value={c}>
              {c ? tCat(categoryKeyMap[c] || c) : tCat("uncategorized")}
            </option>
          ))}
        </select>
      </div>
      <div>
        <label className={labelCls}>{t("descLabel")}</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={2}
          className={`${textareaCls} resize-none`}
          placeholder={t("descPlaceholder")}
        />
      </div>
      <div>
        <label className={labelCls}>{t("promptLabel")}</label>
        <textarea
          value={promptTemplate}
          onChange={(e) => setPromptTemplate(e.target.value)}
          rows={5}
          className={`${textareaCls} resize-y font-mono`}
          placeholder={t("promptPlaceholder")}
        />
      </div>
      {error && <FormErrorStrip message={error} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button variant="primary" onClick={handleSave} disabled={!name.trim()} loading={saving}>
          {saving ? t("saving") : t("save")}
        </Button>
      </div>
    </div>
  );
}

// ==========================================
// MCP 内联编辑/新增表单(无遮罩,嵌入卡片或区块顶部)
// ==========================================
function McpForm({
  mcp,
  onSave,
  onClose,
}: {
  mcp?: LocalMcpServer | null;
  onSave: (data: LocalMcpServer) => Promise<void>;
  onClose: () => void;
}) {
  const t = useTranslations("skills.mcpForm");
  const tErr = useTranslations("skills");
  const [name, setName] = useState(mcp?.name || "");
  const [url, setUrl] = useState(mcp?.url || "");
  const [type, setType] = useState<McpServerType>(mcp?.type || "sse");
  const [authType, setAuthType] = useState<McpAuthType>(mcp?.authType || "none");
  const [status, setStatus] = useState(mcp?.status || "active");
  const [envVarsText, setEnvVarsText] = useState(
    JSON.stringify(mcp?.envVars || {}, null, 2)
  );
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (!name.trim() || !url.trim() || saving) return;
    let envVars: Record<string, unknown> = {};
    try {
      const parsed = envVarsText.trim() ? JSON.parse(envVarsText) : {};
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        setError(t("errEnvObject"));
        return;
      }
      envVars = parsed as Record<string, unknown>;
    } catch {
      setError(t("errEnvJson"));
      return;
    }

    setSaving(true);
    setError("");
    try {
      await onSave({
        id: mcp?.id || "",
        workspaceId: mcp?.workspaceId || "",
        name: name.trim(),
        url: url.trim(),
        type,
        authType,
        envVars,
        status,
      });
      onClose();
    } catch (err) {
      setError(tErr("errSaveMcp") + (err instanceof Error ? err.message : String(err)));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div>
        <label className={labelCls}>
          {t("nameLabel")} <span className="text-danger-fg">*</span>
        </label>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={inputCls}
          placeholder={t("namePlaceholder")}
        />
      </div>
      <div>
        <label className={labelCls}>
          {t("urlLabel")} <span className="text-danger-fg">*</span>
        </label>
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          className={`${inputCls} font-mono`}
          placeholder="https://mcp.example.com/sse"
        />
      </div>
      <div className="grid grid-cols-3 gap-4">
        <div>
          <label className={labelCls}>{t("typeLabel")}</label>
          <select
            value={type}
            onChange={(e) => setType(e.target.value as McpServerType)}
            className={`${inputCls} cursor-pointer`}
          >
            <option value="sse">SSE</option>
            <option value="http">HTTP</option>
            <option value="streamable_http">Streamable HTTP</option>
          </select>
        </div>
        <div>
          <label className={labelCls}>{t("authLabel")}</label>
          <select
            value={authType}
            onChange={(e) => setAuthType(e.target.value as McpAuthType)}
            className={`${inputCls} cursor-pointer`}
          >
            <option value="none">{t("authNone")}</option>
            <option value="api_key">API Key</option>
            <option value="oauth">OAuth</option>
          </select>
        </div>
        <div>
          <label className={labelCls}>{t("statusLabel")}</label>
          <select
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            className={`${inputCls} cursor-pointer`}
          >
            <option value="active">{t("statusActive")}</option>
            <option value="inactive">{t("statusInactive")}</option>
          </select>
        </div>
      </div>
      <div>
        <label className={labelCls}>{t("envLabel")}</label>
        <textarea
          value={envVarsText}
          onChange={(e) => setEnvVarsText(e.target.value)}
          rows={5}
          className={`${textareaCls} resize-y font-mono`}
          placeholder={'{"API_KEY":"..."}'}
        />
      </div>
      {error && <FormErrorStrip message={error} />}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose}>
          {t("cancel")}
        </Button>
        <Button
          variant="primary"
          onClick={handleSave}
          disabled={!name.trim() || !url.trim()}
          loading={saving}
        >
          {saving ? t("saving") : t("save")}
        </Button>
      </div>
    </div>
  );
}

// ==========================================
// 删除被占用时的原位提示(展示占用代理名单)
// ==========================================
function UsingAgentsPanel({
  itemName,
  usingAgents,
  onAcknowledge,
}: {
  itemName: string;
  usingAgents: UsingAgent[];
  onAcknowledge: () => void;
}) {
  const t = useTranslations("skills");
  return (
    <div>
      <div className="flex items-center gap-2 text-sm font-semibold text-warning-fg mb-1.5">
        <AlertTriangle className="w-4 h-4" /> {t("cannotDelete")}
      </div>
      <p className="text-xs text-fg-subtle mb-3">
        <span className="font-medium text-fg">{itemName}</span> {t("inUseByAgents")}
      </p>
      <div className="flex flex-wrap gap-2">
        {usingAgents.map((a) => (
          <span
            key={a.id}
            className="inline-flex items-center gap-1.5 bg-surface-2 border border-line rounded-[var(--radius-control)] px-2 py-1 text-xs text-fg-muted"
          >
            <Zap className="w-3 h-3 text-primary-fg shrink-0" /> {a.name}
          </span>
        ))}
      </div>
      <div className="mt-3">
        <Button variant="secondary" size="sm" onClick={onAcknowledge}>
          {t("acknowledge")}
        </Button>
      </div>
    </div>
  );
}

// ==========================================
// 主页面组件
// ==========================================
export default function SkillsPage() {
  const t = useTranslations("skills");
  const tCat = useTranslations("skills.category");
  const tCommon = useTranslations("common");
  const skills = useAppStore((s) => s.skills) as unknown as LocalSkill[];
  const setSkills = useAppStore((s) => s.setSkills) as unknown as (
    v: LocalSkill[] | ((prev: LocalSkill[]) => LocalSkill[])
  ) => void;
  const mcpServers = useAppStore((s) => s.mcpServers) as unknown as LocalMcpServer[];
  const setMcpServers = useAppStore((s) => s.setMcpServers) as unknown as (
    v: LocalMcpServer[] | ((prev: LocalMcpServer[]) => LocalMcpServer[])
  ) => void;
  const agents = useAppStore((s) => s.agents);
  const workspaceId = useAppStore((s) => s.currentWorkspaceId) || "";

  const [searchQuery, setSearchQuery] = useState("");
  const [formState, setFormState] = useState<EditFormState | null>(null);
  const [blockedDelete, setBlockedDelete] = useState<BlockedDeleteState | null>(null);
  const [listError, setListError] = useState("");

  // 计算某个 skill/mcp 正在被哪些代理使用
  const skillUsingAgents = (skill: LocalSkill) =>
    agents.filter((a) => a.skills?.some((s) => s.name === skill.name));
  const mcpUsingAgents = (mcp: LocalMcpServer) =>
    agents.filter((a) => a.mcpServers?.some((m) => m.name === mcp.name));

  const handleSaveSkill = async (skillData: LocalSkill) => {
    const payload = {
      name: skillData.name,
      category: skillData.category,
      description: skillData.description,
      prompt_template: skillData.promptTemplate,
    };
    const saved = skillData.id
      ? await api.updateSkill(workspaceId, skillData.id, payload)
      : await api.createSkill(workspaceId, payload);
    const mapped = mapSkillFromApi(saved);
    setSkills((prev: LocalSkill[]) => {
      const exists = prev.some((s) => s.id === mapped.id);
      return exists
        ? prev.map((s) => (s.id === mapped.id ? mapped : s))
        : [...prev, mapped];
    });
  };

  const handleSaveMcp = async (mcpData: LocalMcpServer) => {
    const payload = {
      name: mcpData.name,
      url: mcpData.url,
      type: mcpData.type,
      auth_type: mcpData.authType,
      env_vars: mcpData.envVars,
      status: mcpData.status,
    };
    const saved = mcpData.id
      ? await api.updateMcpServer(workspaceId, mcpData.id, payload)
      : await api.createMcpServer(workspaceId, payload);
    const mapped = mapMcpServerFromApi(saved);
    setMcpServers((prev: LocalMcpServer[]) => {
      const exists = prev.some((m) => m.id === mapped.id);
      return exists
        ? prev.map((m) => (m.id === mapped.id ? mapped : m))
        : [...prev, mapped];
    });
  };

  const handleDeleteSkill = async (id: string) => {
    const skill = skills.find((s) => s.id === id);
    if (!skill) return;
    const usingAgents = skillUsingAgents(skill);
    if (usingAgents.length > 0) {
      setBlockedDelete({
        type: "skill",
        id,
        usingAgents: usingAgents.map((a) => ({ id: a.id, name: a.name })),
      });
      return;
    }
    setListError("");
    try {
      await api.deleteSkill(workspaceId, id);
      setSkills((prev: LocalSkill[]) => prev.filter((s) => s.id !== id));
    } catch (err) {
      setListError(t("errDeleteSkill") + (err instanceof Error ? err.message : String(err)));
    }
  };

  const handleDeleteMcp = async (id: string) => {
    const mcp = mcpServers.find((m) => m.id === id);
    if (!mcp) return;
    const usingAgents = mcpUsingAgents(mcp);
    if (usingAgents.length > 0) {
      setBlockedDelete({
        type: "mcp",
        id,
        usingAgents: usingAgents.map((a) => ({ id: a.id, name: a.name })),
      });
      return;
    }
    setListError("");
    try {
      await api.deleteMcpServer(workspaceId, id);
      setMcpServers((prev: LocalMcpServer[]) => prev.filter((m) => m.id !== id));
    } catch (err) {
      // 服务端权威判定:仍被绑定时以 409 返回占用代理名单
      if (err instanceof ApiError && err.status === 409) {
        const names = Array.isArray((err.body as { agents?: unknown } | null)?.agents)
          ? ((err.body as { agents: string[] }).agents)
          : [];
        if (names.length > 0) {
          setBlockedDelete({
            type: "mcp",
            id,
            usingAgents: names.map((name, i) => ({ id: `bound-${i}-${name}`, name })),
          });
          return;
        }
      }
      setListError(t("errDeleteMcp") + (err instanceof Error ? err.message : String(err)));
    }
  };

  const handleToggleMcp = async (id: string) => {
    const mcp = mcpServers.find((m) => m.id === id);
    if (!mcp) return;
    const nextStatus = mcp.status === "active" ? "inactive" : "active";
    try {
      const saved = await api.updateMcpServer(workspaceId, id, { status: nextStatus });
      const mapped = mapMcpServerFromApi(saved);
      setMcpServers((prev: LocalMcpServer[]) =>
        prev.map((m) => (m.id === mapped.id ? mapped : m))
      );
    } catch (err) {
      setListError(t("errToggleMcp") + (err instanceof Error ? err.message : String(err)));
    }
  };

  // 过滤(单个搜索框同时作用于两个区块)
  const filteredSkills = skills.filter(
    (s) =>
      s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      s.category.toLowerCase().includes(searchQuery.toLowerCase())
  );
  const filteredMcps = mcpServers.filter(
    (m) =>
      m.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      m.type.toLowerCase().includes(searchQuery.toLowerCase())
  );

  // 分类统计
  const skillCategories = [...new Set(skills.map((s) => s.category))];
  const mcpTypes = [...new Set(mcpServers.map((m) => m.type))];

  return (
    <div className="h-full flex flex-col overflow-y-auto page-enter">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <Button variant="primary" size="lg" onClick={() => setFormState({ kind: "skill-add" })}>
            <Plus className="w-4 h-4 mr-1.5" /> {t("addSkill")}
          </Button>
        }
      />

      {/* 统计卡片 */}
      <div className="px-6 mb-5 grid grid-cols-2 xl:grid-cols-4 gap-4 shrink-0">
        <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-4">
          <div className="flex items-center justify-between mb-2">
            <Zap className="w-4 h-4 text-fg-subtle" />
            <span className="text-xs text-fg-faint">{t("skillsLabel")}</span>
          </div>
          <div className="text-2xl font-bold text-fg">{skills.length}</div>
          <div className="text-xs text-fg-faint mt-1">
            {t("categoryCount", { count: skillCategories.length })}
          </div>
        </div>
        <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-4">
          <div className="flex items-center justify-between mb-2">
            <Database className="w-4 h-4 text-fg-subtle" />
            <span className="text-xs text-fg-faint">MCP</span>
          </div>
          <div className="text-2xl font-bold text-fg">{mcpServers.length}</div>
          <div className="text-xs text-fg-faint mt-1">
            {t("enabledCount", { count: mcpServers.filter((m) => m.status === "active").length })}
          </div>
        </div>
        <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-4">
          <div className="flex items-center justify-between mb-2">
            <Wifi className="w-4 h-4 text-fg-subtle" />
            <span className="text-xs text-fg-faint">{t("mcpStatusLabel")}</span>
          </div>
          <div className="text-2xl font-bold text-fg">
            {mcpServers.filter((m) => m.status === "active").length}
          </div>
          <div className="text-xs text-fg-faint mt-1">
            {t("disabledCount", { count: mcpServers.filter((m) => m.status !== "active").length })}
          </div>
        </div>
        <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-4">
          <div className="flex items-center justify-between mb-2">
            <Tag className="w-4 h-4 text-fg-subtle" />
            <span className="text-xs text-fg-faint">{t("categoryTab")}</span>
          </div>
          <div className="text-2xl font-bold text-fg">
            {skillCategories.length + mcpTypes.length}
          </div>
          <div className="text-xs text-fg-faint mt-1">{t("skillPlusMcpTypes")}</div>
        </div>
      </div>

      {/* 搜索(两个区块共用) */}
      <div className="px-6 mb-5 shrink-0">
        <div className="relative max-w-xl">
          <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
          <input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] pl-9 pr-3 text-sm text-fg focus:outline-none focus:border-primary transition-colors-fast"
            placeholder={t("searchPlaceholder")}
          />
        </div>
      </div>

      {/* 删除/启停失败提示条 */}
      {listError && (
        <div
          role="alert"
          className="mx-6 mb-5 flex items-start justify-between gap-3 bg-danger/10 border border-danger/30 rounded-[var(--radius-card)] px-4 py-3 text-sm text-danger-fg shrink-0"
        >
          <span className="flex items-start gap-2 min-w-0">
            <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
            <span className="min-w-0 break-all">{listError}</span>
          </span>
          <button
            onClick={() => setListError("")}
            aria-label={tCommon("close")}
            className="text-danger-fg hover:text-fg transition-colors-fast shrink-0"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      )}

      {/* Skill 区块 */}
      <section className="px-6 pb-6">
        <SectionHeader title={t("tabSkills", { count: skills.length })} className="mb-3" />

        {formState?.kind === "skill-add" && (
          <div className="bg-surface border border-line rounded-[var(--radius-card)] p-4 mb-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-fg mb-4">
              <Zap className="w-4 h-4 text-primary-fg" /> {t("skillForm.addTitle")}
            </h3>
            <SkillForm onSave={handleSaveSkill} onClose={() => setFormState(null)} />
          </div>
        )}

        <div className="space-y-3">
          {filteredSkills.map((skill) => {
            const usingAgents = skillUsingAgents(skill);
            const editing =
              formState?.kind === "skill-edit" && formState.skill.id === skill.id;
            const blocked =
              blockedDelete?.type === "skill" && blockedDelete.id === skill.id;
            return (
              <div
                key={skill.id}
                className="bg-surface border border-line rounded-[var(--radius-card)] transition-colors-fast hover:border-line-strong"
              >
                <div className="p-4 flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <div className="w-9 h-9 rounded-[var(--radius-control)] bg-surface-2 border border-line flex items-center justify-center shrink-0">
                      <Zap className="w-4 h-4 text-fg-subtle" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium text-fg">{skill.name}</span>
                        <StatusBadge tone="neutral">
                          {skill.category
                            ? tCat(categoryKeyMap[skill.category] || skill.category)
                            : tCat("uncategorized")}
                        </StatusBadge>
                        {usingAgents.length > 0 && (
                          <StatusBadge tone="neutral">
                            {t("usedByAgents", { count: usingAgents.length })}
                          </StatusBadge>
                        )}
                      </div>
                      {skill.description && (
                        <p className="text-xs text-fg-faint mt-1 truncate">{skill.description}</p>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      onClick={() => setFormState({ kind: "skill-edit", skill })}
                      title={t("skillForm.editTitle")}
                      aria-label={t("skillForm.editTitle")}
                      className="p-1.5 rounded-[var(--radius-control)] text-fg-subtle hover:text-primary-fg hover:bg-primary/10 transition-colors-fast"
                    >
                      <Edit2 className="w-4 h-4" />
                    </button>
                    {usingAgents.length > 0 ? (
                      // 被占用的实体删除必然失败:单击直接展开占用代理名单,不进入确认态
                      <Button
                        variant="danger-ghost"
                        size="sm"
                        onClick={() => handleDeleteSkill(skill.id)}
                      >
                        <Trash2 className="w-3 h-3" /> {tCommon("delete")}
                      </Button>
                    ) : (
                      <InlineConfirm
                        label={
                          <>
                            <Trash2 className="w-3 h-3" /> {tCommon("delete")}
                          </>
                        }
                        onConfirm={() => handleDeleteSkill(skill.id)}
                        confirmLabel={tCommon("confirm")}
                        cancelLabel={tCommon("cancel")}
                      />
                    )}
                  </div>
                </div>

                {editing && (
                  <div className="border-t border-line px-4 py-4">
                    <h3 className="flex items-center gap-2 text-sm font-semibold text-fg mb-4">
                      <Zap className="w-4 h-4 text-primary-fg" /> {t("skillForm.editTitle")}
                    </h3>
                    <SkillForm
                      skill={skill}
                      onSave={handleSaveSkill}
                      onClose={() => setFormState(null)}
                    />
                  </div>
                )}

                {blocked && blockedDelete && (
                  <div className="border-t border-line px-4 py-4">
                    <UsingAgentsPanel
                      itemName={skill.name}
                      usingAgents={blockedDelete.usingAgents}
                      onAcknowledge={() => setBlockedDelete(null)}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {filteredSkills.length === 0 && (
          <div>
            <EmptyState icon={Zap} title={t("noSkillMatch")} />
            <div className="max-w-2xl mx-auto">
              <EmptyStateGuide page="skills" />
            </div>
          </div>
        )}
      </section>

      {/* MCP 区块 */}
      <section className="px-6 pb-8">
        <SectionHeader
          title={t("tabMcp", { count: mcpServers.length })}
          className="mb-3"
          actions={
            <Button variant="secondary" size="sm" onClick={() => setFormState({ kind: "mcp-add" })}>
              <Plus className="w-3.5 h-3.5" /> {t("addMcp")}
            </Button>
          }
        />

        {formState?.kind === "mcp-add" && (
          <div className="bg-surface border border-line rounded-[var(--radius-card)] p-4 mb-3">
            <h3 className="flex items-center gap-2 text-sm font-semibold text-fg mb-4">
              <Server className="w-4 h-4 text-primary-fg" /> {t("mcpForm.addTitle")}
            </h3>
            <McpForm onSave={handleSaveMcp} onClose={() => setFormState(null)} />
          </div>
        )}

        <div className="space-y-3">
          {filteredMcps.map((mcp) => {
            const usingAgents = mcpUsingAgents(mcp);
            const editing = formState?.kind === "mcp-edit" && formState.mcp.id === mcp.id;
            const blocked = blockedDelete?.type === "mcp" && blockedDelete.id === mcp.id;
            return (
              <div
                key={mcp.id}
                className={`bg-surface border rounded-[var(--radius-card)] transition-colors-fast hover:border-line-strong ${
                  mcp.status === "active" ? "border-line" : "border-line opacity-60"
                }`}
              >
                <div className="p-4 flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <div className="w-9 h-9 rounded-[var(--radius-control)] bg-surface-2 border border-line flex items-center justify-center shrink-0">
                      <Server className="w-4 h-4 text-fg-subtle" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium text-fg">{mcp.name}</span>
                        <StatusBadge tone="primary">{mcp.type}</StatusBadge>
                        {mcp.status === "active" ? (
                          <StatusBadge tone="success" dot>{t("statusActive")}</StatusBadge>
                        ) : (
                          <StatusBadge tone="neutral" dot>{t("statusInactive")}</StatusBadge>
                        )}
                        {usingAgents.length > 0 && (
                          <StatusBadge tone="neutral">
                            {t("usedByAgents", { count: usingAgents.length })}
                          </StatusBadge>
                        )}
                      </div>
                      <div className="flex items-center gap-3 mt-1 min-w-0">
                        <span className="text-xs text-fg-faint shrink-0">{mcp.authType}</span>
                        <code className="text-[10px] text-fg-faint font-mono truncate">
                          {mcp.url}
                        </code>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      onClick={() => handleToggleMcp(mcp.id)}
                      title={mcp.status === "active" ? t("clickToDisable") : t("clickToEnable")}
                      aria-label={mcp.status === "active" ? t("clickToDisable") : t("clickToEnable")}
                      className={`p-1.5 rounded-[var(--radius-control)] transition-colors-fast ${
                        mcp.status === "active"
                          ? "text-success-fg hover:bg-success/10"
                          : "text-fg-faint hover:bg-surface-2"
                      }`}
                    >
                      {mcp.status === "active" ? (
                        <ToggleRight className="w-5 h-5" />
                      ) : (
                        <ToggleLeft className="w-5 h-5" />
                      )}
                    </button>
                    <button
                      onClick={() => setFormState({ kind: "mcp-edit", mcp })}
                      title={t("mcpForm.editTitle")}
                      aria-label={t("mcpForm.editTitle")}
                      className="p-1.5 rounded-[var(--radius-control)] text-fg-subtle hover:text-primary-fg hover:bg-primary/10 transition-colors-fast"
                    >
                      <Edit2 className="w-4 h-4" />
                    </button>
                    {usingAgents.length > 0 ? (
                      // 被占用的实体删除必然失败:单击直接展开占用代理名单,不进入确认态
                      <Button
                        variant="danger-ghost"
                        size="sm"
                        onClick={() => handleDeleteMcp(mcp.id)}
                      >
                        <Trash2 className="w-3 h-3" /> {tCommon("delete")}
                      </Button>
                    ) : (
                      <InlineConfirm
                        label={
                          <>
                            <Trash2 className="w-3 h-3" /> {tCommon("delete")}
                          </>
                        }
                        onConfirm={() => handleDeleteMcp(mcp.id)}
                        confirmLabel={tCommon("confirm")}
                        cancelLabel={tCommon("cancel")}
                      />
                    )}
                  </div>
                </div>

                {editing && (
                  <div className="border-t border-line px-4 py-4">
                    <h3 className="flex items-center gap-2 text-sm font-semibold text-fg mb-4">
                      <Server className="w-4 h-4 text-primary-fg" /> {t("mcpForm.editTitle")}
                    </h3>
                    <McpForm
                      mcp={mcp}
                      onSave={handleSaveMcp}
                      onClose={() => setFormState(null)}
                    />
                  </div>
                )}

                {blocked && blockedDelete && (
                  <div className="border-t border-line px-4 py-4">
                    <UsingAgentsPanel
                      itemName={mcp.name}
                      usingAgents={blockedDelete.usingAgents}
                      onAcknowledge={() => setBlockedDelete(null)}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>

        {filteredMcps.length === 0 && <EmptyState icon={Database} title={t("noMcpMatch")} />}
      </section>
    </div>
  );
}
