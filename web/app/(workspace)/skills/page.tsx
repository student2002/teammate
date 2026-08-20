"use client";
// 技能管理页：技能的创建、编辑与删除。

import { useState } from "react";
import {
  Zap,
  Database,
  Plus,
  X,
  Edit2,
  Save,
  Trash2,
  Search,
  ToggleLeft,
  ToggleRight,
  Wifi,
  WifiOff,
  Tag,
  Server,
  AlertTriangle,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";
import EmptyStateGuide from "@/lib/EmptyStateGuide";
import type { Skill, McpAuthType, McpServer, McpServerType } from "@/lib/types";
import { mapMcpServerFromApi, mapSkillFromApi } from "@/lib/mappers";

type LocalSkill = Skill;
type LocalMcpServer = McpServer;

interface DeleteConfirmState {
  type: "skill" | "mcp";
  item: LocalSkill | LocalMcpServer;
  usingAgents: { id: string; name: string }[];
}

// ── Category / type colour map ──────────────────────────────────────────────
const categoryColors: Record<string, string> = {
  "前端": "bg-blue-500/10 text-blue-400 border-blue-500/20",
  "后端": "bg-orange-500/10 text-orange-400 border-orange-500/20",
  "数据": "bg-cyan-500/10 text-cyan-400 border-cyan-500/20",
  "安全": "bg-red-500/10 text-red-400 border-red-500/20",
  "运维": "bg-amber-500/10 text-amber-400 border-amber-500/20",
  "文档": "bg-violet-500/10 text-violet-400 border-violet-500/20",
  "性能": "bg-yellow-500/10 text-yellow-400 border-yellow-500/20",
  "质量": "bg-teal-500/10 text-teal-400 border-teal-500/20",
  "设计": "bg-pink-500/10 text-pink-400 border-pink-500/20",
  "存储": "bg-sky-500/10 text-sky-400 border-sky-500/20",
  "代码": "bg-indigo-500/10 text-indigo-400 border-indigo-500/20",
  "数据库": "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  "服务": "bg-purple-500/10 text-purple-400 border-purple-500/20",
  "CI/CD": "bg-lime-500/10 text-lime-400 border-lime-500/20",
  sse: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  http: "bg-sky-500/10 text-sky-400 border-sky-500/20",
  streamable_http: "bg-indigo-500/10 text-indigo-400 border-indigo-500/20",
};

const getCategoryColor = (cat: string) =>
  categoryColors[cat] || "bg-slate-500/10 text-slate-400 border-slate-500/20";

// ==========================================
// Skill 编辑/新增弹窗
// ==========================================
function SkillEditModal({
  skill,
  onSave,
  onClose,
}: {
  skill?: LocalSkill | null;
  onSave: (data: LocalSkill) => Promise<void>;
  onClose: () => void;
}) {
  const [name, setName] = useState(skill?.name || "");
  const [category, setCategory] = useState(skill?.category || "前端");
  const [description, setDescription] = useState(skill?.description || "");
  const [promptTemplate, setPromptTemplate] = useState(skill?.promptTemplate || "");
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
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[560px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-700/50 flex justify-between items-center">
          <h2 className="text-lg font-bold text-white flex items-center">
            <Zap className="w-5 h-5 mr-2 text-indigo-400" />
            {skill ? "编辑技能" : "新增技能"}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-white"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              技能名称 <span className="text-red-400">*</span>
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-indigo-500"
              placeholder="如: React 组件开发"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              分类
            </label>
            <select
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-indigo-500"
            >
              {categories.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              描述
            </label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-indigo-500 resize-none"
              placeholder="描述该技能的用途和范围"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              Prompt 模板
            </label>
            <textarea
              value={promptTemplate}
              onChange={(e) => setPromptTemplate(e.target.value)}
              rows={5}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-indigo-500 resize-y font-mono"
              placeholder="写入 agent 使用该技能时需要注入的具体说明"
            />
          </div>
        </div>
        <div className="p-5 border-t border-slate-700/50 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
          >
            取消
          </button>
          <button
            onClick={handleSave}
            disabled={!name.trim() || saving}
            className="flex items-center px-5 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast btn-press"
          >
            <Save className="w-4 h-4 mr-2" /> {saving ? "保存中" : "保存"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// MCP 编辑/新增弹窗
// ==========================================
function McpEditModal({
  mcp,
  onSave,
  onClose,
}: {
  mcp?: LocalMcpServer | null;
  onSave: (data: LocalMcpServer) => Promise<void>;
  onClose: () => void;
}) {
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
        setError("环境变量必须是 JSON 对象");
        return;
      }
      envVars = parsed as Record<string, unknown>;
    } catch {
      setError("环境变量不是合法 JSON");
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
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[560px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-700/50 flex justify-between items-center">
          <h2 className="text-lg font-bold text-white flex items-center">
            <Database className="w-5 h-5 mr-2 text-emerald-400" />
            {mcp ? "编辑 MCP 服务器" : "新增 MCP 服务器"}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-white"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              服务器名称 <span className="text-red-400">*</span>
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-emerald-500"
              placeholder="如: 文档检索 MCP"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              端点 URL <span className="text-red-400">*</span>
            </label>
            <input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-emerald-500 font-mono text-sm"
              placeholder="https://mcp.example.com/sse"
            />
          </div>
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-1.5">
                协议类型
              </label>
              <select
                value={type}
                onChange={(e) => setType(e.target.value as McpServerType)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-emerald-500"
              >
                <option value="sse">SSE</option>
                <option value="http">HTTP</option>
                <option value="streamable_http">Streamable HTTP</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-1.5">
                认证方式
              </label>
              <select
                value={authType}
                onChange={(e) => setAuthType(e.target.value as McpAuthType)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-emerald-500"
              >
                <option value="none">无</option>
                <option value="api_key">API Key</option>
                <option value="oauth">OAuth</option>
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-1.5">
                状态
              </label>
              <select
                value={status}
                onChange={(e) => setStatus(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-emerald-500"
              >
                <option value="active">启用</option>
                <option value="inactive">禁用</option>
              </select>
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              环境变量 JSON
            </label>
            <textarea
              value={envVarsText}
              onChange={(e) => setEnvVarsText(e.target.value)}
              rows={5}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-emerald-500 resize-y font-mono"
              placeholder={'{"API_KEY":"..."}'}
            />
            {error && <p className="text-xs text-red-400 mt-2">{error}</p>}
          </div>
        </div>
        <div className="p-5 border-t border-slate-700/50 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
          >
            取消
          </button>
          <button
            onClick={handleSave}
            disabled={!name.trim() || !url.trim() || saving}
            className="flex items-center px-5 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast btn-press"
          >
            <Save className="w-4 h-4 mr-2" /> {saving ? "保存中" : "保存"}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 主页面组件
// ==========================================
export default function SkillsPage() {
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

  const [activeTab, setActiveTab] = useState<"skills" | "mcp">("skills");
  const [searchQuery, setSearchQuery] = useState("");
  const [editingSkill, setEditingSkill] = useState<LocalSkill | null>(null);
  const [editingMcp, setEditingMcp] = useState<LocalMcpServer | null>(null);
  const [showAddSkill, setShowAddSkill] = useState(false);
  const [showAddMcp, setShowAddMcp] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState<DeleteConfirmState | null>(
    null
  );

  // 计算哪些 skill/mcp 正在被代理使用
  const usedSkillNames = new Set(agents.flatMap((a) => a.skills?.map((s) => s.name) || []));
  const usedMcpNames = new Set(agents.flatMap((a) => a.mcpServers?.map((m) => m.name) || []));

  const handleSaveSkill = async (skillData: LocalSkill) => {
    try {
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
    } catch (err) {
      alert("保存技能失败: " + (err instanceof Error ? err.message : String(err)));
      throw err;
    }
  };

  const handleSaveMcp = async (mcpData: LocalMcpServer) => {
    try {
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
    } catch (err) {
      alert("保存 MCP 服务器失败: " + (err instanceof Error ? err.message : String(err)));
      throw err;
    }
  };

  const handleDeleteSkill = async (id: string) => {
    const skill = skills.find((s) => s.id === id);
    if (!skill) return;
    const usingAgents = agents.filter((a) =>
      a.skills?.some((s) => s.name === skill.name)
    );
    if (usingAgents.length > 0) {
      setDeleteConfirm({
        type: "skill",
        item: skill,
        usingAgents: usingAgents.map((a) => ({ id: a.id, name: a.name })),
      });
      return;
    }
    try {
      await api.deleteSkill(workspaceId, id);
      setSkills((prev: LocalSkill[]) => prev.filter((s) => s.id !== id));
    } catch (err) {
      alert("删除技能失败: " + (err instanceof Error ? err.message : String(err)));
    }
  };

  const handleDeleteMcp = async (id: string) => {
    const mcp = mcpServers.find((m) => m.id === id);
    if (!mcp) return;
    const usingAgents = agents.filter((a) =>
      a.mcpServers?.some((m) => m.name === mcp.name)
    );
    if (usingAgents.length > 0) {
      setDeleteConfirm({
        type: "mcp",
        item: mcp,
        usingAgents: usingAgents.map((a) => ({ id: a.id, name: a.name })),
      });
      return;
    }
    try {
      await api.deleteMcpServer(workspaceId, id);
      setMcpServers((prev: LocalMcpServer[]) => prev.filter((m) => m.id !== id));
    } catch (err) {
      alert("删除 MCP 服务器失败: " + (err instanceof Error ? err.message : String(err)));
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
      alert("更新 MCP 状态失败: " + (err instanceof Error ? err.message : String(err)));
    }
  };

  // 过滤
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
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      {/* 头部 */}
      <div className="flex justify-between items-center mb-6 shrink-0">
        <div>
          <h1 className="text-2xl font-bold text-white mb-1">
            Skill &amp; MCP 配置
          </h1>
          <p className="text-sm text-slate-400">
            管理工作区可用的技能和 MCP 服务器，AI
            代理只能关联已配置的 Skill 和 MCP。
          </p>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-4 gap-4 mb-6 shrink-0">
        <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 backdrop-blur-sm card-hover">
          <div className="flex items-center justify-between mb-2">
            <Zap className="w-5 h-5 text-indigo-400" />
            <span className="text-xs text-slate-500">技能</span>
          </div>
          <div className="text-2xl font-bold text-white">{skills.length}</div>
          <div className="text-xs text-slate-500 mt-1">
            {skillCategories.length} 个分类
          </div>
        </div>
        <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 backdrop-blur-sm card-hover">
          <div className="flex items-center justify-between mb-2">
            <Database className="w-5 h-5 text-emerald-400" />
            <span className="text-xs text-slate-500">MCP</span>
          </div>
          <div className="text-2xl font-bold text-white">
            {mcpServers.length}
          </div>
          <div className="text-xs text-slate-500 mt-1">
            {mcpServers.filter((m) => m.status === "active").length} 已启用
          </div>
        </div>
        <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 backdrop-blur-sm card-hover">
          <div className="flex items-center justify-between mb-2">
            <Wifi className="w-5 h-5 text-blue-400" />
            <span className="text-xs text-slate-500">MCP 状态</span>
          </div>
          <div className="text-2xl font-bold text-white">
            {mcpServers.filter((m) => m.status === "active").length}
          </div>
          <div className="text-xs text-slate-500 mt-1">
            {mcpServers.filter((m) => m.status !== "active").length} 已禁用
          </div>
        </div>
        <div className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 backdrop-blur-sm card-hover">
          <div className="flex items-center justify-between mb-2">
            <Tag className="w-5 h-5 text-amber-400" />
            <span className="text-xs text-slate-500">分类</span>
          </div>
          <div className="text-2xl font-bold text-white">
            {skillCategories.length + mcpTypes.length}
          </div>
          <div className="text-xs text-slate-500 mt-1">技能 + MCP 类型</div>
        </div>
      </div>

      {/* Tab 切换 + 搜索 + 添加 */}
      <div className="flex items-center gap-4 mb-6 shrink-0">
        <div className="flex bg-slate-800/60 border border-slate-700/50 rounded-lg overflow-hidden">
          <button
            onClick={() => setActiveTab("skills")}
            className={`flex items-center px-4 py-2 text-sm font-medium transition-colors-fast ${
              activeTab === "skills"
                ? "bg-indigo-600/20 text-indigo-400"
                : "text-slate-400 hover:text-white"
            }`}
          >
            <Zap className="w-4 h-4 mr-2" /> 技能 ({skills.length})
          </button>
          <button
            onClick={() => setActiveTab("mcp")}
            className={`flex items-center px-4 py-2 text-sm font-medium transition-colors-fast ${
              activeTab === "mcp"
                ? "bg-emerald-600/20 text-emerald-400"
                : "text-slate-400 hover:text-white"
            }`}
          >
            <Database className="w-4 h-4 mr-2" /> MCP 服务器 (
            {mcpServers.length})
          </button>
        </div>

        <div className="flex-1 relative">
          <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
          <input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full bg-slate-800/60 border border-slate-700/50 rounded-lg pl-10 pr-4 py-2 text-sm text-white focus:outline-none focus:border-slate-500"
            placeholder={
              activeTab === "skills"
                ? "搜索技能名称或分类..."
                : "搜索 MCP 名称或类型..."
            }
          />
        </div>

        <button
          onClick={() =>
            activeTab === "skills" ? setShowAddSkill(true) : setShowAddMcp(true)
          }
          className="flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors-fast btn-press shadow-lg shadow-blue-500/20 text-sm shrink-0"
        >
          <Plus className="w-4 h-4 mr-2" />
          {activeTab === "skills" ? "新增技能" : "新增 MCP"}
        </button>
      </div>

      {/* Skills 列表 */}
      {activeTab === "skills" && (
        <div className="space-y-3">
          {filteredSkills.map((skill) => {
            const isUsed = usedSkillNames.has(skill.name);
            const usingAgentCount = agents.filter((a) =>
              a.skills?.some((s) => s.name === skill.name)
            ).length;
            return (
              <div
                key={skill.id}
                className="bg-slate-800/40 border border-slate-700/60 rounded-xl p-4 backdrop-blur-sm card-hover transition-colors-normal hover:shadow-lg hover:shadow-blue-500/5 hover:border-slate-500/60"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <div
                      className={`w-10 h-10 rounded-lg flex items-center justify-center shrink-0 ${getCategoryColor(skill.category)} border`}
                    >
                      <Zap className="w-5 h-5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-white">
                          {skill.name}
                        </span>
                        <span
                          className={`px-2 py-0.5 text-[10px] rounded-full border ${getCategoryColor(skill.category)}`}
                        >
                          {skill.category}
                        </span>
                        {isUsed && (
                          <span className="px-2 py-0.5 text-[10px] rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/20">
                            {usingAgentCount} 个代理使用
                          </span>
                        )}
                      </div>
                      <p className="text-xs text-slate-500 mt-1 truncate">
                        {skill.description}
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0 ml-4">
                    <button
                      onClick={() => setEditingSkill(skill)}
                      className="p-1.5 rounded-lg text-slate-400 hover:text-blue-400 hover:bg-blue-500/10 transition-colors-fast"
                    >
                      <Edit2 className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => handleDeleteSkill(skill.id)}
                      className="p-1.5 rounded-lg text-slate-400 hover:text-red-400 hover:bg-red-500/10 transition-colors-fast"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              </div>
            );
          })}
          {filteredSkills.length === 0 && (
            <div>
              <div className="text-center py-8 text-slate-500">
                <Zap className="w-10 h-10 mx-auto mb-3 opacity-30" />
                <p>没有找到匹配的技能</p>
              </div>
              <EmptyStateGuide page="skills" />
            </div>
          )}
        </div>
      )}

      {/* MCP 列表 */}
      {activeTab === "mcp" && (
        <div className="space-y-3">
          {filteredMcps.map((mcp) => {
            const isUsed = usedMcpNames.has(mcp.name);
            const usingAgentCount = agents.filter((a) =>
              a.mcpServers?.some((m) => m.name === mcp.name)
            ).length;
            return (
              <div
                key={mcp.id}
                className={`bg-slate-800/40 border rounded-xl p-4 backdrop-blur-sm card-hover transition-colors-normal hover:shadow-lg hover:shadow-blue-500/5 ${
                  mcp.status === "active"
                    ? "border-slate-700/60 hover:border-slate-500/60"
                    : "border-slate-700/30 opacity-60"
                }`}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3 flex-1 min-w-0">
                    <div
                      className={`w-10 h-10 rounded-lg flex items-center justify-center shrink-0 ${getCategoryColor(mcp.type)} border`}
                    >
                      <Server className="w-5 h-5" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-white">
                          {mcp.name}
                        </span>
                        <span
                          className={`px-2 py-0.5 text-[10px] rounded-full border ${getCategoryColor(mcp.type)}`}
                        >
                          {mcp.type}
                        </span>
                        {mcp.status === "active" ? (
                          <span className="flex items-center gap-1 px-2 py-0.5 text-[10px] rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                            <Wifi className="w-3 h-3" /> 启用
                          </span>
                        ) : (
                          <span className="flex items-center gap-1 px-2 py-0.5 text-[10px] rounded-full bg-slate-500/10 text-slate-500 border border-slate-500/20">
                            <WifiOff className="w-3 h-3" /> 禁用
                          </span>
                        )}
                        {isUsed && (
                          <span className="px-2 py-0.5 text-[10px] rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/20">
                            {usingAgentCount} 个代理使用
                          </span>
                        )}
                      </div>
                      <div className="flex items-center gap-3 mt-1">
                        <p className="text-xs text-slate-500 truncate">
                          {mcp.authType}
                        </p>
                        <code className="text-[10px] text-slate-600 font-mono shrink-0">
                          {mcp.url}
                        </code>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0 ml-4">
                    <button
                      onClick={() => handleToggleMcp(mcp.id)}
                      className={`p-1.5 rounded-lg transition-colors-fast ${mcp.status === "active" ? "text-emerald-400 hover:bg-emerald-500/10" : "text-slate-500 hover:bg-slate-700"}`}
                      title={mcp.status === "active" ? "点击禁用" : "点击启用"}
                    >
                      {mcp.status === "active" ? (
                        <ToggleRight className="w-6 h-6" />
                      ) : (
                        <ToggleLeft className="w-6 h-6" />
                      )}
                    </button>
                    <button
                      onClick={() => setEditingMcp(mcp)}
                      className="p-1.5 rounded-lg text-slate-400 hover:text-blue-400 hover:bg-blue-500/10 transition-colors-fast"
                    >
                      <Edit2 className="w-4 h-4" />
                    </button>
                    <button
                      onClick={() => handleDeleteMcp(mcp.id)}
                      className="p-1.5 rounded-lg text-slate-400 hover:text-red-400 hover:bg-red-500/10 transition-colors-fast"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              </div>
            );
          })}
          {filteredMcps.length === 0 && (
            <div className="text-center py-12 text-slate-500">
              <Database className="w-10 h-10 mx-auto mb-3 opacity-30" />
              <p>没有找到匹配的 MCP 服务器</p>
            </div>
          )}
        </div>
      )}

      {/* 删除确认弹窗 */}
      {deleteConfirm && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
          onClick={() => setDeleteConfirm(null)}
        >
          <div
            className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[440px] shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-slate-700/50 flex items-center">
              <AlertTriangle className="w-5 h-5 text-amber-400 mr-2" />
              <h2 className="text-lg font-bold text-white">无法删除</h2>
            </div>
            <div className="p-5">
              <p className="text-sm text-slate-300 mb-3">
                <span className="font-medium text-white">
                  {deleteConfirm.item.name}
                </span>{" "}
                正在被以下代理使用，请先移除代理的关联后再删除：
              </p>
              <div className="space-y-2">
                {deleteConfirm.usingAgents.map((a) => (
                  <div
                    key={a.id}
                    className="flex items-center gap-2 bg-slate-800/60 border border-slate-700/50 rounded-lg p-2.5"
                  >
                    <div className="w-6 h-6 rounded-md bg-blue-500/10 flex items-center justify-center">
                      <Zap className="w-3.5 h-3.5 text-blue-400" />
                    </div>
                    <span className="text-sm text-white font-medium">
                      {a.name}
                    </span>
                  </div>
                ))}
              </div>
            </div>
            <div className="p-5 border-t border-slate-700/50 flex justify-end">
              <button
                onClick={() => setDeleteConfirm(null)}
                className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
              >
                知道了
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 编辑弹窗 */}
      {editingSkill && (
        <SkillEditModal
          skill={editingSkill}
          onSave={handleSaveSkill}
          onClose={() => setEditingSkill(null)}
        />
      )}
      {editingMcp && (
        <McpEditModal
          mcp={editingMcp}
          onSave={handleSaveMcp}
          onClose={() => setEditingMcp(null)}
        />
      )}
      {showAddSkill && (
        <SkillEditModal
          onSave={handleSaveSkill}
          onClose={() => setShowAddSkill(false)}
        />
      )}
      {showAddMcp && (
        <McpEditModal
          onSave={handleSaveMcp}
          onClose={() => setShowAddMcp(false)}
        />
      )}
    </div>
  );
}
