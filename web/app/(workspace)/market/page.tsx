"use client";
// 社区工作流市场页：浏览并导入社区工作流模板。

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
  Save,
  Workflow,
  Cpu,
  Zap,
  Database,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";
import type { MappedAgent, MappedTemplate, Skill, McpServer } from "@/lib/types";

// ── 社区工作流类型（来自 API）──
interface CommunityWorkflow {
  id: string;
  name: string;
  author: string;
  version: string;
  downloads: number;
  desc: string;
  skills: string[];
  mcps: string[];
  nodes: string[];
  recommendedAgentInstructions?: Record<string, string>;
}

interface PublishedWorkflow {
  id: string;
  name: string;
  desc: string;
  version: string;
  skills: string[];
  mcps: string[];
  nodes: string[];
  author: string;
  downloads: number;
}

// ==========================================
// 发布工作流弹窗（基于已有模板）
// ==========================================
function PublishDialog({
  onClose,
  onPublish,
  templates,
  agents,
  skills,
  mcpServers,
}: {
  onClose: () => void;
  onPublish: (workflow: PublishedWorkflow) => void;
  templates: MappedTemplate[];
  agents: MappedAgent[];
  skills: Skill[];
  mcpServers: McpServer[];
}) {
  const t = useTranslations("market.publish");
  const [selectedTemplateId, setSelectedTemplateId] = useState("");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [version, setVersion] = useState("v1.0.0");
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([]);
  const [nodes, setNodes] = useState<string[]>([]);

  const selectedTemplate = templates.find((t) => t.id === selectedTemplateId);

  const handleTemplateSelect = (tplId: string) => {
    setSelectedTemplateId(tplId);
    const tpl = templates.find((t) => t.id === tplId);
    if (!tpl) return;

    setName(tpl.name);
    setDesc(tpl.description || "");
    setNodes(tpl.nodes.map((n) => n.name));

    const autoSkills = new Set<string>();
    const autoMcps = new Set<string>();

    tpl.nodes.forEach((node) => {
      if (
        node.assigneeType === "specific_agent" &&
        node.assigneeId
      ) {
        const agent = agents.find((a) => a.id === node.assigneeId);
        if (agent) {
          agent.skills.forEach((s) => autoSkills.add(s.name));
          agent.mcpServers.forEach((m) => autoMcps.add(m.name));
        }
      }
      if (node.assigneeType === "any_agent") {
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
      nodes,
      author: t("defaultAuthor"),
      downloads: 0,
    });
  };

  const isValid = name.trim().length > 0 && nodes.length > 0;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[640px] max-h-[85vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center sticky top-0 bg-slate-900 z-10">
          <h2 className="text-lg font-bold text-white flex items-center">
            <Upload className="w-5 h-5 mr-2 text-blue-400" /> {t("title")}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>

        <div className="p-5 space-y-5">
          {/* 选择工作流模板 */}
          <div>
            <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              <Workflow className="w-3.5 h-3.5 inline mr-1 text-blue-400" />
              {t("templateLabel")} <span className="text-red-400">*</span>
            </label>
            <select
              value={selectedTemplateId}
              onChange={(e) => handleTemplateSelect(e.target.value)}
              className="w-full bg-slate-800/40 border border-slate-700/60 rounded-lg px-4 py-2.5 text-sm text-white focus:outline-none focus:border-blue-500 transition-colors-fast cursor-pointer animate-scale-in"
            >
              <option value="" className="bg-slate-800">
                {t("templatePlaceholder")}
              </option>
              {templates.map((tpl) => (
                <option key={tpl.id} value={tpl.id} className="bg-slate-800">
                  {tpl.name} {tpl.isBuiltIn ? t("builtIn") : t("custom")} —{" "}
                  {t("nodeCount", { count: tpl.nodes.length })}
                </option>
              ))}
            </select>
          </div>

          {/* 工作流名称 */}
          <div>
            <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              {t("nameLabel")} <span className="text-red-400">*</span>
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("namePlaceholder")}
              className="w-full bg-slate-800/40 border border-slate-700/60 rounded-lg px-4 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition-colors-fast"
            />
          </div>

          {/* 描述 */}
          <div>
            <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              {t("descLabel")}
            </label>
            <textarea
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
              placeholder={t("descPlaceholder")}
              rows={3}
              className="w-full bg-slate-800/40 border border-slate-700/60 rounded-lg px-4 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition-colors-fast resize-none"
            />
          </div>

          {/* 版本 */}
          <div>
            <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              {t("versionLabel")}
            </label>
            <input
              value={version}
              onChange={(e) => setVersion(e.target.value)}
              placeholder="v1.0.0"
              className="w-full bg-slate-800/40 border border-slate-700/60 rounded-lg px-4 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 transition-colors-fast"
            />
          </div>

          {/* 节点预览（只读） */}
          {nodes.length > 0 && (
            <div>
              <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
                {t("nodeFlowLabel")}
              </label>
              <div className="flex items-center gap-2 flex-wrap">
                {nodes.map((n, i) => (
                  <React.Fragment key={i}>
                    <span className="px-3 py-1.5 bg-slate-800 border border-slate-700 rounded-lg text-sm text-slate-300">
                      {n}
                    </span>
                    {i < nodes.length - 1 && (
                      <span className="text-slate-600 text-xs">→</span>
                    )}
                  </React.Fragment>
                ))}
              </div>
            </div>
          )}

          {/* 依赖技能（下拉多选） */}
          <div>
            <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              <Zap className="w-3.5 h-3.5 inline mr-1 text-indigo-400" />
              {t("skillsLabel")}{" "}
              {selectedTemplate && (
                <span className="text-blue-400 normal-case tracking-normal font-normal">
                  {t("autoDetected")}
                </span>
              )}
            </label>
            <div className="bg-slate-800/40 border border-slate-700/60 rounded-lg p-3 max-h-40 overflow-y-auto">
              {skills.length === 0 ? (
                <div className="text-xs text-slate-500 text-center py-2">
                  {t("noSkills")}
                </div>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {skills.map((s) => (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => toggleSkill(s.name)}
                      className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors-fast ${
                        selectedSkills.includes(s.name)
                          ? "bg-indigo-500/20 border-indigo-500/40 text-indigo-300"
                          : "bg-slate-700/30 border-slate-600/50 text-slate-400 hover:border-slate-500"
                      }`}
                    >
                      {s.name}
                    </button>
                  ))}
                </div>
              )}
            </div>
            {selectedSkills.length > 0 && (
              <div className="mt-2 text-xs text-slate-500">
                {t("skillsSelected", { count: selectedSkills.length })}
              </div>
            )}
          </div>

          {/* 依赖 MCP 服务器（下拉多选） */}
          <div>
            <label className="block text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              <Database className="w-3.5 h-3.5 inline mr-1 text-emerald-400" />
              {t("mcpsLabel")}{" "}
              {selectedTemplate && (
                <span className="text-blue-400 normal-case tracking-normal font-normal">
                  {t("autoDetected")}
                </span>
              )}
            </label>
            <div className="bg-slate-800/40 border border-slate-700/60 rounded-lg p-3 max-h-40 overflow-y-auto">
              {mcpServers.length === 0 ? (
                <div className="text-xs text-slate-500 text-center py-2">
                  {t("noMcps")}
                </div>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {mcpServers.map((m) => (
                    <button
                      key={m.id}
                      type="button"
                      onClick={() => toggleMcp(m.name)}
                      className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors-fast ${
                        selectedMcps.includes(m.name)
                          ? "bg-emerald-500/20 border-emerald-500/40 text-emerald-300"
                          : "bg-slate-700/30 border-slate-600/50 text-slate-400 hover:border-slate-500"
                      }`}
                    >
                      {m.name}
                    </button>
                  ))}
                </div>
              )}
            </div>
            {selectedMcps.length > 0 && (
              <div className="mt-2 text-xs text-slate-500">
                {t("mcpsSelected", { count: selectedMcps.length })}
              </div>
            )}
          </div>

          {/* 代理信息提示 */}
          {selectedTemplate && (
            <div className="bg-blue-500/5 border border-blue-500/20 rounded-lg p-3 text-xs text-blue-300">
              <Cpu className="w-3.5 h-3.5 inline mr-1" />
              {t("autoDetectHint")}
            </div>
          )}

          {/* 发布按钮 */}
          <div className="flex justify-end gap-3 pt-2">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
            >
              {t("cancel")}
            </button>
            <button
              onClick={handlePublish}
              disabled={!isValid}
              className="flex items-center px-5 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-slate-700 disabled:text-slate-500 text-white rounded-lg text-sm font-medium transition-colors-fast btn-press"
            >
              <Save className="w-4 h-4 mr-2" /> {t("publish")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 导入预览弹窗
// ==========================================
function ImportDialog({
  workflow,
  onClose,
  workspaceId,
}: {
  workflow: CommunityWorkflow;
  onClose: () => void;
  workspaceId: string;
}) {
  const t = useTranslations("market.import");
  const [step, setStep] = useState<"preview" | "importing" | "done">(
    "preview"
  );
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
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[580px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center sticky top-0 bg-slate-900 z-10">
          <h2 className="text-lg font-bold text-white">{t("title")}</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>

        {step === "preview" && (
          <div className="p-5 space-y-5">
            {error && (
              <div className="p-3 bg-red-500/10 border border-red-500/30 rounded-lg text-sm text-red-400">
                {error}
              </div>
            )}
            <div className="flex items-start gap-4">
              <div className="w-12 h-12 rounded-xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center shrink-0">
                <Download className="w-6 h-6 text-indigo-400" />
              </div>
              <div>
                <h3 className="text-xl font-bold text-white">
                  {workflow.name}
                </h3>
                <div className="text-xs text-slate-400 mt-1">
                  {t("authorVersion", {
                    author: workflow.author,
                    version: workflow.version,
                    downloads: workflow.downloads,
                  })}
                </div>
                <p className="text-sm text-slate-300 mt-3">{workflow.desc}</p>
              </div>
            </div>

            {/* 节点序列 */}
            <div>
              <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3">
                {t("nodeSequenceLabel")}
              </div>
              <div className="flex items-center gap-2 flex-wrap">
                {workflow.nodes.map((n, i) => (
                  <React.Fragment key={i}>
                    <span className="px-3 py-1.5 bg-slate-800 border border-slate-700 rounded-lg text-sm text-slate-300">
                      {n}
                    </span>
                    {i < workflow.nodes.length - 1 && (
                      <span className="text-slate-600 text-xs">→</span>
                    )}
                  </React.Fragment>
                ))}
              </div>
            </div>

            {/* 依赖分析 */}
            <div className="grid grid-cols-2 gap-4">
              <div className="bg-slate-800/30 border border-slate-700 rounded-xl p-4">
                <div className="text-xs font-semibold text-slate-500 mb-3">
                  {t("requiredSkills")}
                </div>
                <ul className="space-y-2">
                  {workflow.skills.map((s) => (
                    <li
                      key={s}
                      className="flex items-center justify-between text-sm"
                    >
                      <span className="text-slate-300">{s}</span>
                      <span className="text-[10px] text-amber-400 bg-amber-500/10 px-1.5 py-0.5 rounded">
                        {t("notInstalled")}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
              <div className="bg-slate-800/30 border border-slate-700 rounded-xl p-4">
                <div className="text-xs font-semibold text-slate-500 mb-3">
                  {t("requiredMcps")}
                </div>
                <ul className="space-y-2">
                  {workflow.mcps.map((m) => (
                    <li
                      key={m}
                      className="flex items-center justify-between text-sm"
                    >
                      <span className="text-slate-300">{m}</span>
                      <span className="text-[10px] text-amber-400 bg-amber-500/10 px-1.5 py-0.5 rounded">
                        {t("placeholder")}
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>

            {/* 推荐的代理身份指令 */}
            {workflow.recommendedAgentInstructions && (
              <div className="bg-slate-800/30 border border-indigo-500/20 rounded-xl p-4">
                <div className="text-xs font-semibold text-slate-500 mb-3 flex items-center">
                  <BookOpen className="w-3.5 h-3.5 mr-1 text-indigo-400" />{" "}
                  {t("recommendedInstructions")}
                </div>
                <div className="space-y-2">
                  {Object.entries(workflow.recommendedAgentInstructions).map(
                    ([nodeName, instruction]) => (
                      <div
                        key={nodeName}
                        className="bg-slate-950/50 border border-slate-700/50 rounded-lg p-3"
                      >
                        <div className="text-[10px] text-indigo-400 font-semibold mb-1">
                          {nodeName}
                        </div>
                        <div className="text-xs text-slate-400 leading-relaxed">
                          {instruction}
                        </div>
                      </div>
                    )
                  )}
                </div>
              </div>
            )}

            <div className="bg-slate-800/30 border border-slate-700 rounded-xl p-4 text-xs text-slate-400">
              <AlertTriangle className="w-3.5 h-3.5 inline mr-1 text-amber-400" />
              {t("mcpPlaceholderHint")}
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <button
                onClick={onClose}
                className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
              >
                {t("cancel")}
              </button>
              <button
                onClick={handleImport}
                className="flex items-center px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors-fast btn-press"
              >
                <Download className="w-4 h-4 mr-2" /> {t("confirmImport")}
              </button>
            </div>
          </div>
        )}

        {step === "importing" && (
          <div className="p-8 flex flex-col items-center">
            <Loader2 className="w-10 h-10 text-blue-400 animate-spin mb-4" />
            <h3 className="text-lg font-bold text-white mb-2">{t("importingTitle")}</h3>
            <p className="text-sm text-slate-400 mb-6">
              {t("importingDesc")}
            </p>
          </div>
        )}

        {step === "done" && (
          <div className="p-8 flex flex-col items-center">
            <div className="w-14 h-14 rounded-full bg-emerald-500/20 border border-emerald-500/30 flex items-center justify-center mb-4">
              <CheckCircle2 className="w-8 h-8 text-emerald-400" />
            </div>
            <h3 className="text-lg font-bold text-white mb-2">{t("successTitle")}</h3>
            <div className="text-sm text-slate-400 text-center mb-6">
              {t("successDesc", { name: workflow.name })}
              <br />
              {t("successHint")}
            </div>
            <button
              onClick={onClose}
              className="px-6 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors-fast btn-press"
            >
              {t("finish")}
            </button>
          </div>
        )}
      </div>
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
        let nodesArr: string[] = [];

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

        // 从 workflow_definition 解析节点
        try {
          const nodes = workflowDef.nodes;
          if (Array.isArray(nodes)) nodesArr = (nodes as Record<string, unknown>[]).map((n) => (n.name as string) || "");
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
          author: (w.author as string) || t("unknownAuthor"),
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
      // API 请求失败，展示空列表
      setCommunityWorkflows([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadCommunityWorkflows();
  }, [loadCommunityWorkflows]);

  const allWorkflows: CommunityWorkflow[] = communityWorkflows;

  const filtered = allWorkflows.filter(
    (w) =>
      w.name.toLowerCase().includes(search.toLowerCase()) ||
      w.desc.toLowerCase().includes(search.toLowerCase()) ||
      w.skills.some((s) => s.toLowerCase().includes(search.toLowerCase()))
  );

  const handlePublish = async (workflow: PublishedWorkflow) => {
    try {
      await api.createCommunityWorkflow({
        name: workflow.name,
        description: workflow.desc,
        author: workflow.author,
        version: workflow.version,
        workflow_definition: {
          name: workflow.name,
          nodes: workflow.nodes.map((n) => ({ name: n })),
        },
        required_skills: workflow.skills,
        required_mcp_servers: workflow.mcps,
      });
      // 发布成功后重新拉取列表，让新模板出现在社区市场
      await loadCommunityWorkflows();
    } catch (err) {
      console.error("Publish community workflow failed:", err);
      alert(t("publishFailed"));
    }
    setPublishing(false);
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      <div className="flex justify-between items-center mb-8 shrink-0">
        <div>
          <h1 className="text-2xl font-bold text-white mb-2">
            {t("title")}
          </h1>
          <p className="text-sm text-slate-400">
            {t("subtitle")}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <div className="relative">
            <Search className="w-4 h-4 text-slate-500 absolute left-3 top-2.5" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t("searchPlaceholder")}
              className="bg-slate-900 border border-slate-700 rounded-lg pl-9 pr-4 py-2 text-sm text-white focus:outline-none focus:border-blue-500 w-64 transition-colors-fast"
            />
          </div>
          <button
            onClick={() => setPublishing(true)}
            className="flex items-center px-4 py-2 bg-blue-600/10 hover:bg-blue-600/20 text-blue-400 border border-blue-500/30 rounded-lg text-sm font-medium transition-colors-fast btn-press"
          >
            <Upload className="w-4 h-4 mr-1.5" /> {t("publishButton")}
          </button>
        </div>
      </div>

      {loading ? (
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="w-8 h-8 text-blue-400 animate-spin" />
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex-1 flex items-center justify-center text-slate-500">
          {t("noMatch")}
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {filtered.map((wf) => (
            <div
              key={wf.id}
              className="bg-slate-900/50 border border-slate-700/60 hover:border-blue-500/40 transition-colors-normal rounded-xl backdrop-blur-sm hover:shadow-lg hover:shadow-blue-500/5 p-6 flex flex-col card-hover"
            >
              <div className="flex justify-between items-start mb-3">
                <h3 className="text-lg font-bold text-white">{wf.name}</h3>
                <span className="text-xs text-slate-400 bg-slate-800 px-2 py-1 rounded">
                  {wf.version}
                </span>
              </div>
              <div className="text-sm text-slate-400 mb-4 line-clamp-2 min-h-[40px]">
                {wf.desc}
              </div>

              {/* 统 badges */}
              <div className="flex items-center gap-2 mb-4 text-[10px]">
                {wf.nodes && (
                  <span className="text-slate-400 bg-slate-800 px-2 py-0.5 rounded">
                    {t("nodeCount", { count: wf.nodes.length })}
                  </span>
                )}
                {wf.recommendedAgentInstructions && (
                  <span className="text-indigo-400 bg-indigo-500/10 px-2 py-0.5 rounded border border-indigo-500/20 flex items-center">
                    <BookOpen className="w-3 h-3 mr-0.5" />{" "}
                    {t("instructionCount", { count: Object.keys(wf.recommendedAgentInstructions).length })}
                  </span>
                )}
              </div>

              {/* 节点预览 */}
              <div className="flex items-center gap-1.5 mb-4 text-xs">
                {wf.nodes.map((n, i) => (
                  <React.Fragment key={i}>
                    <span className="px-2 py-1 bg-slate-800 border border-slate-700 rounded text-slate-300">
                      {n}
                    </span>
                    {i < wf.nodes.length - 1 && (
                      <span className="text-slate-600">→</span>
                    )}
                  </React.Fragment>
                ))}
              </div>

              <div className="grid grid-cols-2 gap-4 mb-6 text-xs">
                <div className="bg-slate-950 p-3 rounded-lg border border-slate-800">
                  <div className="text-slate-500 mb-2 font-semibold">
                    {t("dependentSkills")}
                  </div>
                  <ul className="space-y-1">
                    {wf.skills.map((s) => (
                      <li
                        key={s}
                        className="text-slate-300 flex items-center"
                      >
                        <span className="w-1 h-1 bg-indigo-500 rounded-full mr-2" />
                        {s}
                      </li>
                    ))}
                  </ul>
                </div>
                <div className="bg-slate-950 p-3 rounded-lg border border-slate-800">
                  <div className="text-slate-500 mb-2 font-semibold">
                    {t("dependentMcps")}
                  </div>
                  <ul className="space-y-1">
                    {wf.mcps.map((m) => (
                      <li
                        key={m}
                        className="text-slate-300 flex items-center"
                      >
                        <span className="w-1 h-1 bg-emerald-500 rounded-full mr-2" />
                        {m}
                      </li>
                    ))}
                  </ul>
                </div>
              </div>

              <div className="mt-auto flex items-center justify-between pt-4 border-t border-slate-800">
                <div className="text-xs text-slate-500 flex items-center">
                  <User className="w-3.5 h-3.5 mr-1" /> {wf.author}
                  <span className="mx-2">·</span>
                  <Download className="w-3.5 h-3.5 mr-1" /> {wf.downloads}
                </div>
                <button
                  onClick={() => setImporting(wf)}
                  className="flex items-center px-4 py-1.5 bg-blue-600/10 hover:bg-blue-600/20 text-blue-400 border border-blue-500/30 rounded-lg text-sm font-medium transition-colors-fast btn-press"
                >
                  <Download className="w-4 h-4 mr-1.5" /> {t("import")}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {importing && (
        <ImportDialog
          workflow={importing}
          onClose={() => setImporting(null)}
          workspaceId={workspaceId}
        />
      )}
      {publishing && (
        <PublishDialog
          onClose={() => setPublishing(false)}
          onPublish={handlePublish}
          templates={templates}
          agents={agents}
          skills={skills}
          mcpServers={mcpServers}
        />
      )}
    </div>
  );
}
