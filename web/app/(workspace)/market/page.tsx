"use client";
// 社区工作流市场页:模板卡复用工作流模板卡的节点链预览(圆点=standard,菱形=review/manual);
// 发布与导入在页面级内联步骤区展开(文档流推开内容),不使用遮罩弹窗。

import React, { useState, useEffect, useCallback } from "react";
import { useTranslations } from "next-intl";
import {
  Download,
  Search,
  User,
  X,
  CheckCircle2,
  AlertTriangle,
  Loader2,
  BookOpen,
  Upload,
  Cpu,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";
import Button from "@/lib/ui/Button";
import PageHeader from "@/lib/ui/PageHeader";
import EmptyState from "@/lib/ui/EmptyState";
import StatusBadge from "@/lib/ui/StatusBadge";
import type { TemplateListItem } from "@/lib/workflow/types";
import { NODE_CATALOG } from "@/lib/workflow/catalog";
import type { MappedAgent, Skill, McpServer } from "@/lib/types";

// 节点分类 → 链上小图形颜色(human.input 人工节点画菱形)
const CATEGORY_DOT_CLS: Record<string, string> = {
  trigger: "bg-primary-fg",
  agent: "bg-accent-fg",
  human: "bg-warning-fg",
  end: "bg-success-fg",
};

function categoryOf(typeKey: string): string {
  return NODE_CATALOG[typeKey as keyof typeof NODE_CATALOG]?.category ?? "";
}

// ── 社区工作流类型(来自 API)──
interface CommunityNode {
  name: string;
  typeKey: string;
}

interface CommunityWorkflow {
  id: string;
  name: string;
  author: string;
  version: string;
  downloads: number;
  desc: string;
  skills: string[];
  mcps: string[];
  nodes: CommunityNode[];
  recommendedAgentInstructions?: Record<string, string>;
}

interface PublishedWorkflow {
  id: string;
  name: string;
  desc: string;
  version: string;
  skills: string[];
  mcps: string[];
  nodes: CommunityNode[];
  author: string;
  downloads: number;
}

// ==========================================
// 节点链预览(圆点/菱形 + 名称),与工作流模板卡同一视觉
// ==========================================
function MarketNodeChain({ nodes }: { nodes: CommunityNode[] }) {
  if (nodes.length === 0) return null;
  return (
    <div className="flex items-center overflow-x-auto py-1" role="list">
      {nodes.map((n, i) => {
        const diamond = n.typeKey === "human.input";
        const cls = CATEGORY_DOT_CLS[categoryOf(n.typeKey)] ?? "bg-fg-faint";
        return (
          <React.Fragment key={i}>
            {i > 0 && <span className="h-px w-4 shrink-0 bg-line-strong" aria-hidden="true" />}
            <div className="flex flex-col items-center gap-1 w-16 shrink-0" role="listitem" title={n.name}>
              {diamond ? (
                <span className={`w-2 h-2 rotate-45 rounded-[1px] shrink-0 ${cls}`} aria-hidden="true" />
              ) : (
                <span className={`w-2 h-2 rounded-full shrink-0 ${cls}`} aria-hidden="true" />
              )}
              <span className="text-[11px] leading-tight text-fg-muted truncate w-full text-center">
                {n.name || "—"}
              </span>
            </div>
          </React.Fragment>
        );
      })}
    </div>
  );
}

// ==========================================
// 发布工作流(页面内联步骤区,基于已有模板)
// ==========================================
function PublishPanel({
  onClose,
  onPublish,
  error,
  templates,
  agents,
  skills,
  mcpServers,
}: {
  onClose: () => void;
  onPublish: (workflow: PublishedWorkflow) => void;
  error: string | null;
  templates: TemplateListItem[];
  agents: MappedAgent[];
  skills: Skill[];
  mcpServers: McpServer[];
}) {
  const t = useTranslations("market.publish");
  const tCommon = useTranslations("common");
  const [selectedTemplateId, setSelectedTemplateId] = useState("");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [version, setVersion] = useState("v1.0.0");
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([]);
  const [nodes, setNodes] = useState<string[]>([]);

  const selectedTemplate = templates.find((tpl) => tpl.template.id === selectedTemplateId);

  // agent.task 节点的执行人配置(config.assignee)→ 依赖的技能/MCP 自动勾选
  const handleTemplateSelect = (tplId: string) => {
    setSelectedTemplateId(tplId);
    const tpl = templates.find((tpl) => tpl.template.id === tplId);
    if (!tpl) return;

    setName(tpl.template.name);
    setDesc(tpl.template.description || "");
    setNodes(tpl.nodes.map((n) => n.name));

    const autoSkills = new Set<string>();
    const autoMcps = new Set<string>();

    tpl.nodes.forEach((node) => {
      if (node.type_key !== "agent.task") return;
      const assignee = ((node.config ?? {}) as { assignee?: { mode?: string; agent_id?: string | null } }).assignee;
      if (assignee?.mode === "specific_agent" && assignee.agent_id) {
        const agent = agents.find((a) => a.id === assignee.agent_id);
        if (agent) {
          agent.skills.forEach((s) => autoSkills.add(s.name));
          agent.mcpServers.forEach((m) => autoMcps.add(m.name));
        }
      }
      if (assignee?.mode !== "specific_agent") {
        agents.forEach((agent) => {
          agent.skills.forEach((s) => autoSkills.add(s.name));
          agent.mcpServers.forEach((m) => autoMcps.add(m.name));
        });
      }
    });

    setSelectedSkills([...autoSkills]);
    setSelectedMcps([...autoMcps]);
  };

  const toggleSkill = (skillName: string) => {
    setSelectedSkills((prev) =>
      prev.includes(skillName)
        ? prev.filter((s) => s !== skillName)
        : [...prev, skillName]
    );
  };

  const toggleMcp = (mcpName: string) => {
    setSelectedMcps((prev) =>
      prev.includes(mcpName)
        ? prev.filter((m) => m !== mcpName)
        : [...prev, mcpName]
    );
  };

  const handlePublish = () => {
    if (!name.trim()) return;
    onPublish({
      id: `published-${Date.now()}`,
      name: name.trim(),
      desc: desc.trim(),
      version: version.trim() || "v1.0.0",
      skills: selectedSkills,
      mcps: selectedMcps,
      nodes: previewNodes,
      author: t("defaultAuthor"),
      downloads: 0,
    });
  };

  const isValid = name.trim().length > 0 && nodes.length > 0;

  // 节点流程预览:名称来自 nodes 状态,type_key 按选中模板的同名下标对齐
  const previewNodes: CommunityNode[] = nodes.map((nodeName, i) => ({
    name: nodeName,
    typeKey: String(selectedTemplate?.nodes[i]?.type_key ?? ""),
  }));

  return (
    <div className="bg-surface border border-line rounded-[var(--radius-card)] p-5">
      <div className="flex items-center justify-between gap-3 mb-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold text-fg">
          <Upload className="w-4 h-4 text-primary-fg" /> {t("title")}
        </h2>
        <button
          onClick={onClose}
          aria-label={tCommon("close")}
          className="p-1.5 rounded-[var(--radius-control)] text-fg-subtle hover:text-fg hover:bg-surface-2 transition-colors-fast"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      <div className="space-y-4">
        {/* 选择工作流模板 */}
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-1.5">
            {t("templateLabel")} <span className="text-danger-fg">*</span>
          </label>
          <select
            value={selectedTemplateId}
            onChange={(e) => handleTemplateSelect(e.target.value)}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg focus:outline-none focus:border-primary transition-colors-fast cursor-pointer"
          >
            <option value="">{t("templatePlaceholder")}</option>
            {templates.map((tpl) => (
              <option key={tpl.template.id} value={tpl.template.id}>
                {tpl.template.name} {tpl.template.is_builtin ? t("builtIn") : t("custom")} —{" "}
                {t("nodeCount", { count: tpl.nodes.length })}
              </option>
            ))}
          </select>
        </div>

        {/* 工作流名称 */}
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-1.5">
            {t("nameLabel")} <span className="text-danger-fg">*</span>
          </label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("namePlaceholder")}
            className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary transition-colors-fast"
          />
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* 描述 */}
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("descLabel")}
            </label>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder={t("descPlaceholder")}
              rows={3}
              className="w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 py-2 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary transition-colors-fast resize-none"
            />
          </div>

          {/* 版本 */}
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("versionLabel")}
            </label>
            <input
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder="v1.0.0"
              className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary transition-colors-fast"
            />
          </div>
        </div>

        {/* 节点预览(只读) */}
        {previewNodes.length > 0 && (
          <div>
            <div className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("nodeFlowLabel")}
            </div>
            <div className="border border-line rounded-[var(--radius-control)] bg-surface-2/40 px-3 py-2">
              <MarketNodeChain nodes={previewNodes} />
            </div>
          </div>
        )}

        {/* 依赖技能(多选) */}
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-1.5">
            {t("skillsLabel")}{" "}
            {selectedTemplate && (
              <span className="text-xs text-primary-fg font-normal">
                {t("autoDetected")}
              </span>
            )}
          </label>
          <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-3 max-h-40 overflow-y-auto">
            {skills.length === 0 ? (
              <div className="text-xs text-fg-faint text-center py-2">{t("noSkills")}</div>
            ) : (
              <div className="flex flex-wrap gap-2">
                {skills.map((s) => {
                  const selected = selectedSkills.includes(s.name);
                  return (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => toggleSkill(s.name)}
                      aria-pressed={selected}
                      className={`px-3 h-[28px] inline-flex items-center rounded-[var(--radius-control)] text-xs font-medium border transition-colors-fast ${
                        selected
                          ? "bg-primary/15 border-primary/40 text-primary-fg"
                          : "bg-surface-2 border-line text-fg-subtle hover:border-line-strong"
                      }`}
                    >
                      {s.name}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
          {selectedSkills.length > 0 && (
            <div className="mt-2 text-xs text-fg-faint">
              {t("skillsSelected", { count: selectedSkills.length })}
            </div>
          )}
        </div>

        {/* 依赖 MCP 服务器(多选) */}
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-1.5">
            {t("mcpsLabel")}{" "}
            {selectedTemplate && (
              <span className="text-xs text-primary-fg font-normal">
                {t("autoDetected")}
              </span>
            )}
          </label>
          <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-3 max-h-40 overflow-y-auto">
            {mcpServers.length === 0 ? (
              <div className="text-xs text-fg-faint text-center py-2">{t("noMcps")}</div>
            ) : (
              <div className="flex flex-wrap gap-2">
                {mcpServers.map((m) => {
                  const selected = selectedMcps.includes(m.name);
                  return (
                    <button
                      key={m.id}
                      type="button"
                      onClick={() => toggleMcp(m.name)}
                      aria-pressed={selected}
                      className={`px-3 h-[28px] inline-flex items-center rounded-[var(--radius-control)] text-xs font-medium border transition-colors-fast ${
                        selected
                          ? "bg-primary/15 border-primary/40 text-primary-fg"
                          : "bg-surface-2 border-line text-fg-subtle hover:border-line-strong"
                      }`}
                    >
                      {m.name}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
          {selectedMcps.length > 0 && (
            <div className="mt-2 text-xs text-fg-faint">
              {t("mcpsSelected", { count: selectedMcps.length })}
            </div>
          )}
        </div>

        {/* 代理信息提示 */}
        {selectedTemplate && (
          <div className="bg-primary/5 border border-primary/20 rounded-[var(--radius-control)] p-3 text-xs text-primary-fg flex items-start gap-2">
            <Cpu className="w-3.5 h-3.5 mt-0.5 shrink-0" />
            <span>{t("autoDetectHint")}</span>
          </div>
        )}

        {error && (
          <div
            role="alert"
            className="flex items-start gap-2 bg-danger/10 border border-danger/30 rounded-[var(--radius-control)] px-3 py-2.5 text-sm text-danger-fg"
          >
            <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <Button variant="secondary" onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button variant="primary" onClick={handlePublish} disabled={!isValid}>
            {t("publish")}
          </Button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 导入预览/执行(页面内联步骤区)
// ==========================================
function ImportPanel({
  workflow,
  onClose,
  workspaceId,
}: {
  workflow: CommunityWorkflow;
  onClose: () => void;
  workspaceId: string;
}) {
  const t = useTranslations("market.import");
  const tCommon = useTranslations("common");
  const [step, setStep] = useState<"preview" | "importing" | "done">("preview");
  const [error, setError] = useState<string | null>(null);

  const handleImport = () => {
    setStep("importing");
    setError(null);
    api.importCommunityWorkflow(workflow.id, workspaceId)
      .then(() => {
        setStep("done");
      })
      .catch((err: Error) => {
        setError(err.message || t("failed"));
        setStep("preview");
      });
  };

  return (
    <div className="bg-surface border border-line rounded-[var(--radius-card)] p-5">
      <div className="flex items-center justify-between gap-3 mb-4">
        <h2 className="text-sm font-semibold text-fg">{t("title")}</h2>
        <button
          onClick={onClose}
          aria-label={tCommon("close")}
          className="p-1.5 rounded-[var(--radius-control)] text-fg-subtle hover:text-fg hover:bg-surface-2 transition-colors-fast"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {step === "preview" && (
        <div className="space-y-4">
          {error && (
            <div
              role="alert"
              className="flex items-start gap-2 bg-danger/10 border border-danger/30 rounded-[var(--radius-control)] px-3 py-2.5 text-sm text-danger-fg"
            >
              <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}
          <div className="flex items-start gap-4">
            <div className="w-11 h-11 rounded-[var(--radius-control)] bg-accent/10 border border-accent/20 flex items-center justify-center shrink-0">
              <Download className="w-5 h-5 text-accent-fg" />
            </div>
            <div className="min-w-0">
              <h3 className="text-base font-bold text-fg">{workflow.name}</h3>
              <div className="text-xs text-fg-subtle mt-1">
                {t("authorVersion", {
                  author: workflow.author,
                  version: workflow.version,
                  downloads: workflow.downloads,
                })}
              </div>
              {workflow.desc && (
                <p className="text-sm text-fg-muted mt-2">{workflow.desc}</p>
              )}
            </div>
          </div>

          {/* 节点序列 */}
          {workflow.nodes.length > 0 && (
            <div>
              <div className="text-sm font-medium text-fg-muted mb-1.5">
                {t("nodeSequenceLabel")}
              </div>
              <div className="border border-line rounded-[var(--radius-control)] bg-surface-2/40 px-3 py-2">
                <MarketNodeChain nodes={workflow.nodes} />
              </div>
            </div>
          )}

          {/* 依赖分析 */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-4">
              <div className="text-xs font-semibold text-fg-faint mb-3">
                {t("requiredSkills")}
              </div>
              <ul className="space-y-2">
                {workflow.skills.map((s) => (
                  <li key={s} className="flex items-center justify-between gap-2 text-sm">
                    <span className="text-fg-muted min-w-0 truncate">{s}</span>
                    <StatusBadge tone="warning">{t("notInstalled")}</StatusBadge>
                  </li>
                ))}
              </ul>
            </div>
            <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-4">
              <div className="text-xs font-semibold text-fg-faint mb-3">
                {t("requiredMcps")}
              </div>
              <ul className="space-y-2">
                {workflow.mcps.map((m) => (
                  <li key={m} className="flex items-center justify-between gap-2 text-sm">
                    <span className="text-fg-muted min-w-0 truncate">{m}</span>
                    <StatusBadge tone="warning">{t("placeholder")}</StatusBadge>
                  </li>
                ))}
              </ul>
            </div>
          </div>

          {/* 推荐的代理身份指令 */}
          {workflow.recommendedAgentInstructions && (
            <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-4">
              <div className="text-xs font-semibold text-fg-faint mb-3 flex items-center gap-1.5">
                <BookOpen className="w-3.5 h-3.5 text-primary-fg" />
                {t("recommendedInstructions")}
              </div>
              <div className="space-y-2">
                {Object.entries(workflow.recommendedAgentInstructions).map(
                  ([nodeName, instruction]) => (
                    <div
                      key={nodeName}
                      className="bg-bg border border-line rounded-[var(--radius-control)] p-3"
                    >
                      <div className="text-[10px] text-primary-fg font-semibold mb-1">
                        {nodeName}
                      </div>
                      <div className="text-xs text-fg-subtle leading-relaxed">
                        {instruction}
                      </div>
                    </div>
                  )
                )}
              </div>
            </div>
          )}

          <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-3 text-xs text-fg-subtle flex items-start gap-2">
            <AlertTriangle className="w-3.5 h-3.5 mt-0.5 text-warning-fg shrink-0" />
            <span>{t("mcpPlaceholderHint")}</span>
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button variant="secondary" onClick={onClose}>
              {t("cancel")}
            </Button>
            <Button variant="primary" onClick={handleImport}>
              <Download className="w-4 h-4 mr-1.5" /> {t("confirmImport")}
            </Button>
          </div>
        </div>
      )}

      {step === "importing" && (
        <div className="py-8 flex flex-col items-center">
          <Loader2 className="w-8 h-8 text-primary-fg animate-spin mb-4" />
          <h3 className="text-base font-bold text-fg mb-2">{t("importingTitle")}</h3>
          <p className="text-sm text-fg-subtle">{t("importingDesc")}</p>
        </div>
      )}

      {step === "done" && (
        <div className="py-8 flex flex-col items-center">
          <div className="w-12 h-12 rounded-full bg-success/15 border border-success/30 flex items-center justify-center mb-4">
            <CheckCircle2 className="w-6 h-6 text-success-fg" />
          </div>
          <h3 className="text-base font-bold text-fg mb-2">{t("successTitle")}</h3>
          <div className="text-sm text-fg-subtle text-center mb-6">
            {t("successDesc", { name: workflow.name })}
            <br />
            {t("successHint")}
          </div>
          <Button variant="primary" onClick={onClose}>
            {t("finish")}
          </Button>
        </div>
      )}
    </div>
  );
}

// ==========================================
// 市场主页
// ==========================================
export default function MarketPage() {
  const t = useTranslations("market");
  const templates = useAppStore((s) => s.templates);
  const agents = useAppStore((s) => s.agents);
  const skills = useAppStore((s) => s.skills);
  const mcpServers = useAppStore((s) => s.mcpServers);
  const workspaceId = useAppStore((s) => s.currentWorkspaceId) || "";

  const [search, setSearch] = useState("");
  const [importing, setImporting] = useState<CommunityWorkflow | null>(null);
  const [publishing, setPublishing] = useState(false);
  const [publishError, setPublishError] = useState<string | null>(null);
  const [communityWorkflows, setCommunityWorkflows] = useState<CommunityWorkflow[]>([]);
  const [loading, setLoading] = useState(true);

  // 从 API 获取社区工作流
  const loadCommunityWorkflows = useCallback(async () => {
    setLoading(true);
    try {
      const data: unknown = await api.getCommunityWorkflows();
      const workflows = Array.isArray(data) ? data : [];
      const mapped: CommunityWorkflow[] = workflows.map((w: Record<string, unknown>) => {
        const workflowDef = w.workflow_definition as Record<string, unknown> || {};
        let skillsArr: string[] = [];
        let mcpsArr: string[] = [];
        let nodesArr: CommunityNode[] = [];

        // 从 JSON 解析 required_skills
        try {
          const raw = w.required_skills;
          if (typeof raw === "string") skillsArr = JSON.parse(raw);
          else if (Array.isArray(raw)) skillsArr = raw as string[];
        } catch { /* 忽略 */ }

        // 从 JSON 解析 required_mcp_servers
        try {
          const raw = w.required_mcp_servers;
          if (typeof raw === "string") mcpsArr = JSON.parse(raw);
          else if (Array.isArray(raw)) mcpsArr = raw as string[];
        } catch { /* 忽略 */ }

        // 从 workflow_definition 解析节点(名称 + type_key)
        try {
          const nodes = workflowDef.nodes;
          if (Array.isArray(nodes)) {
            nodesArr = (nodes as Record<string, unknown>[]).map((n) => ({
              name: (n.name as string) || "",
              typeKey: String(n.type_key ?? ""),
            }));
          }
        } catch { /* 忽略 */ }

        // 解析 recommended_agent_instructions
        let instructions: Record<string, string> | undefined;
        try {
          const raw = w.recommended_agent_instructions;
          if (typeof raw === "string") instructions = JSON.parse(raw);
          else if (raw && typeof raw === "object") instructions = raw as Record<string, string>;
        } catch { /* 忽略 */ }

        return {
          id: w.id as string,
          name: w.name as string,
          author: (w.author as string) || "",
          version: (w.version as string) || "v1.0.0",
          downloads: (w.downloads as number) || 0,
          desc: (w.description as string) || "",
          skills: skillsArr,
          mcps: mcpsArr,
          nodes: nodesArr,
          recommendedAgentInstructions: instructions,
        };
      });
      setCommunityWorkflows(mapped);
    } catch {
      // API 请求失败,展示空列表
      setCommunityWorkflows([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadCommunityWorkflows();
  }, [loadCommunityWorkflows]);

  const filtered = communityWorkflows.filter(
    (w) =>
      w.name.toLowerCase().includes(search.toLowerCase()) ||
      w.desc.toLowerCase().includes(search.toLowerCase()) ||
      w.skills.some((s) => s.toLowerCase().includes(search.toLowerCase()))
  );

  // 发布与导入两个内联区互斥展开
  const openPublish = () => {
    setImporting(null);
    setPublishError(null);
    setPublishing(true);
  };
  const openImport = (wf: CommunityWorkflow) => {
    setPublishing(false);
    setImporting(wf);
  };

  const handlePublish = async (workflow: PublishedWorkflow) => {
    try {
      await api.createCommunityWorkflow({
        name: workflow.name,
        description: workflow.desc,
        author: workflow.author,
        version: workflow.version,
        workflow_definition: {
          name: workflow.name,
          nodes: workflow.nodes.map((n) => ({ name: n.name, type_key: n.typeKey })),
        },
        required_skills: workflow.skills,
        required_mcp_servers: workflow.mcps,
      });
      // 发布成功后重新拉取列表,让新模板出现在社区市场
      await loadCommunityWorkflows();
      setPublishing(false);
    } catch (err) {
      console.error("Publish community workflow failed:", err);
      setPublishError(t("publishFailed"));
    }
  };

  return (
    <div className="h-full flex flex-col overflow-y-auto page-enter">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <>
            <div className="relative">
              <Search className="w-4 h-4 text-fg-faint absolute left-3 top-1/2 -translate-y-1/2" />
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder={t("searchPlaceholder")}
                className="h-[34px] w-56 bg-bg border border-line-strong rounded-[var(--radius-control)] pl-9 pr-3 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary transition-colors-fast"
              />
            </div>
            <Button variant="primary" size="lg" onClick={openPublish}>
              <Upload className="w-4 h-4 mr-1.5" /> {t("publishButton")}
            </Button>
          </>
        }
      />

      {/* 发布内联步骤区 */}
      {publishing && (
        <div className="px-6 pb-5 shrink-0">
          <PublishPanel
            onClose={() => setPublishing(false)}
            onPublish={handlePublish}
            error={publishError}
            templates={templates}
            agents={agents}
            skills={skills}
            mcpServers={mcpServers}
          />
        </div>
      )}

      {/* 导入内联步骤区 */}
      {importing && (
        <div className="px-6 pb-5 shrink-0">
          <ImportPanel
            key={importing.id}
            workflow={importing}
            onClose={() => setImporting(null)}
            workspaceId={workspaceId}
          />
        </div>
      )}

      {loading ? (
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="w-6 h-6 text-primary-fg animate-spin" />
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState icon={Search} title={t("noMatch")} className="flex-1 justify-center" />
      ) : (
        <div className="px-6 pb-8 grid grid-cols-1 lg:grid-cols-2 gap-5">
          {filtered.map((wf) => (
            <div
              key={wf.id}
              className="bg-surface border border-line rounded-[var(--radius-card)] p-5 flex flex-col transition-colors-fast hover:border-line-strong"
            >
              {/* 卡头:名称 + 版本 */}
              <div className="flex items-start justify-between gap-2 mb-1.5">
                <h3 className="text-[15px] font-semibold text-fg truncate" title={wf.name}>
                  {wf.name}
                </h3>
                <StatusBadge tone="neutral">{wf.version}</StatusBadge>
              </div>

              {wf.desc && (
                <p className="text-xs text-fg-subtle line-clamp-2 mb-3">{wf.desc}</p>
              )}

              {/* 统计标签 */}
              <div className="flex items-center gap-2 mb-3">
                {wf.nodes.length > 0 && (
                  <StatusBadge tone="neutral">
                    {t("nodeCount", { count: wf.nodes.length })}
                  </StatusBadge>
                )}
                {wf.recommendedAgentInstructions && (
                  <StatusBadge tone="neutral">
                    <BookOpen className="w-3 h-3" />
                    {t("instructionCount", { count: Object.keys(wf.recommendedAgentInstructions).length })}
                  </StatusBadge>
                )}
              </div>

              {/* 节点链预览 */}
              {wf.nodes.length > 0 && (
                <div className="border border-line rounded-[var(--radius-control)] bg-surface-2/40 px-3 py-2 mb-3">
                  <MarketNodeChain nodes={wf.nodes} />
                </div>
              )}

              {/* 依赖 */}
              <div className="grid grid-cols-2 gap-3 mb-4">
                <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-3 min-w-0">
                  <div className="text-[10px] font-semibold text-fg-faint mb-2">
                    {t("dependentSkills")}
                  </div>
                  {wf.skills.length === 0 ? (
                    <div className="text-[10px] text-fg-faint">—</div>
                  ) : (
                    <ul className="space-y-1">
                      {wf.skills.map((s) => (
                        <li key={s} className="text-xs text-fg-muted flex items-center min-w-0">
                          <span className="w-1 h-1 bg-fg-faint rounded-full mr-2 shrink-0" aria-hidden="true" />
                          <span className="truncate">{s}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
                <div className="bg-surface-2/40 border border-line rounded-[var(--radius-control)] p-3 min-w-0">
                  <div className="text-[10px] font-semibold text-fg-faint mb-2">
                    {t("dependentMcps")}
                  </div>
                  {wf.mcps.length === 0 ? (
                    <div className="text-[10px] text-fg-faint">—</div>
                  ) : (
                    <ul className="space-y-1">
                      {wf.mcps.map((m) => (
                        <li key={m} className="text-xs text-fg-muted flex items-center min-w-0">
                          <span className="w-1 h-1 bg-fg-faint rounded-full mr-2 shrink-0" aria-hidden="true" />
                          <span className="truncate">{m}</span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>

              {/* 底部:作者/下载量 + 导入 */}
              <div className="mt-auto border-t border-line pt-3 flex items-center justify-between gap-2">
                <div className="text-xs text-fg-faint flex items-center min-w-0">
                  <User className="w-3.5 h-3.5 mr-1 shrink-0" />
                  <span className="truncate">{wf.author || t("unknownAuthor")}</span>
                  <span className="mx-2 shrink-0">·</span>
                  <Download className="w-3.5 h-3.5 mr-1 shrink-0" />
                  {wf.downloads}
                </div>
                <Button variant="primary" size="sm" onClick={() => openImport(wf)}>
                  <Download className="w-3.5 h-3.5" /> {t("importAction")}
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
