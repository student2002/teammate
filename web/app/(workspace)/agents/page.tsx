"use client";
// 代理列表页：注册与管理 Agent，绑定技能和 MCP 服务器，展示 API Token。

import React, { useState, useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import {
  Bot,
  Terminal,
  Plus,
  Zap,
  Database,
  X,
  Cpu,
  Activity,
  Clock,
  CheckCircle2,
  Save,
  Trash2,
  Wifi,
  WifiOff,
  ChevronDown,
  Check,
  Loader2,
  AlertTriangle,
  PlayCircle,
  PauseCircle,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import type { MappedAgent, Skill, McpServer, AgentStatus } from "@/lib/types";
import { mapAgentFromApi } from "@/lib/mappers";
import EmptyStateGuide from "@/lib/EmptyStateGuide";

// ==========================================
// AgentStatusBadge 组件
// ==========================================
const STATUS_STYLES: Record<string, string> = {
  online: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20",
  busy: "bg-blue-500/10 text-blue-400 border-blue-500/20",
  paused: "bg-amber-500/10 text-amber-400 border-amber-500/20",
  offline: "bg-slate-500/10 text-slate-400 border-slate-500/20",
};

const STATUS_LABELS: Record<string, string> = {
  online: "空闲在线",
  busy: "忙碌中",
  paused: "暂停接单",
  offline: "离线",
};

function AgentStatusBadge({ status }: { status: AgentStatus | string }) {
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
      ) : (
        <PauseCircle className="w-3 h-3 mr-1 opacity-50" />
      )}
      {STATUS_LABELS[status] || status}
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
      bg: "bg-indigo-500/10",
      border: "border-indigo-500/20",
      text: "text-indigo-300",
      focus: "focus:border-indigo-500",
      hover: "hover:border-indigo-500/40",
    },
    emerald: {
      bg: "bg-emerald-500/10",
      border: "border-emerald-500/20",
      text: "text-emerald-300",
      focus: "focus:border-emerald-500",
      hover: "hover:border-emerald-500/40",
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
        className={`w-full bg-slate-950 border border-slate-700 rounded-lg px-3 py-2 text-left text-sm ${c.hover} transition-colors-fast flex items-center justify-between min-h-[38px]`}
      >
        <div className="flex flex-wrap gap-1 flex-1">
          {selected.length === 0 ? (
            <span className="text-slate-500">{placeholder}</span>
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
                    className="ml-1 hover:text-white"
                  >
                    <X className="w-3 h-3" />
                  </button>
                </span>
              );
            })
          )}
        </div>
        <ChevronDown
          className={`w-4 h-4 text-slate-500 shrink-0 ml-2 transition-transform ${isOpen ? "rotate-180" : ""}`}
        />
      </button>
      {isOpen && (
        <>
          <div
            className="fixed inset-0 z-[60]"
            onClick={() => setIsOpen(false)}
          />
          <div
            className="animate-scale-in absolute top-full left-0 right-0 mt-1 bg-slate-900 border border-slate-700 rounded-lg shadow-xl z-[70] max-h-48 overflow-y-auto overscroll-contain"
            onWheel={(e) => e.stopPropagation()}
          >
            {options.length === 0 ? (
              <div className="px-3 py-2 text-sm text-slate-500">
                暂无可选项
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
                        : "text-slate-300 hover:bg-slate-800"
                    } ${!opt.enabled ? "opacity-40" : ""}`}
                  >
                    <span className="flex items-center gap-2">
                      {isSelected && <Check className="w-3.5 h-3.5" />}
                      <span>{opt.name}</span>
                      {!opt.enabled && (
                        <span className="text-[10px] text-slate-500">
                          (已禁用)
                        </span>
                      )}
                    </span>
                    <span className="text-[10px] text-slate-500">
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
  return (
    <div
      className="modal-overlay fixed inset-0 z-[60] flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-red-500/30 rounded-2xl w-[440px] shadow-2xl shadow-red-500/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 flex items-start gap-4">
          <div className="w-10 h-10 rounded-full bg-red-500/10 flex items-center justify-center shrink-0 mt-0.5">
            <AlertTriangle className="w-5 h-5 text-red-400" />
          </div>
          <div className="flex-1">
            <h3 className="text-lg font-bold text-white mb-1">删除代理</h3>
            <p className="text-sm text-slate-400">
              确定要删除代理{" "}
              <span className="text-white font-medium">{agentName}</span>{" "}
              吗？此操作不可恢复，关联的技能和 MCP 配置也将被清除。
            </p>
            {error && (
              <p className="text-sm text-red-400 mt-2 bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2">
                {error}
              </p>
            )}
          </div>
        </div>
        <div className="p-4 border-t border-slate-800 flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={deleting}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast disabled:opacity-50"
          >
            取消
          </button>
          <button
            onClick={onConfirm}
            disabled={deleting}
            className="btn-press flex items-center px-4 py-2 bg-red-600 hover:bg-red-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {deleting ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Trash2 className="w-4 h-4 mr-2" />
            )}
            确认删除
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// API Token 展示弹窗（创建后一次性展示）
// ==========================================
function yamlScalar(value: string) {
  const normalized = value || "";
  if (/^[A-Za-z0-9_./:@-]+(?: [A-Za-z0-9_./:@-]+)*$/.test(normalized)) {
    return normalized;
  }
  return JSON.stringify(normalized);
}

function TokenRevealModal({
  agentName,
  agentId,
  apiToken,
  provider,
  serverUrl,
  workspaceId,
  onClose,
}: {
  agentName: string;
  agentId: string;
  apiToken: string;
  provider: string;
  serverUrl?: string;
  workspaceId: string | null;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const toolName = provider || "claude";
  const configYaml = `agent:
    context_window: 100000
    id: ${yamlScalar(agentId)}
    name: ${yamlScalar(agentName)}
    provider: ${yamlScalar(toolName)}
server:
    api_token: ${yamlScalar(apiToken)}
    url: ${yamlScalar(serverUrl || "http://localhost:8080")}
tools:
    ${toolName}:
        path: ${yamlScalar(toolName)}
workspace:
    id: ${yamlScalar(workspaceId || "YOUR_WORKSPACE_ID")}`;

  return (
    <div
      className="modal-overlay fixed inset-0 z-[60] flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/98 backdrop-blur-xl border border-amber-500/30 rounded-2xl w-[620px] max-h-[85vh] overflow-y-auto shadow-2xl shadow-amber-500/10"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-amber-500/20 flex justify-between items-center">
          <h2 className="text-xl font-bold text-white flex items-center gap-2">
            <span className="text-amber-400">&#9888;</span>{" "}
            代理 API Token（仅显示一次）
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-white"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-5">
          <div className="bg-amber-500/5 border border-amber-500/20 rounded-lg p-4">
            <p className="text-sm text-amber-300 font-medium mb-1">重要提示</p>
            <p className="text-xs text-amber-200/70">
              此
              Token 仅在创建时展示一次，关闭后无法再次查看。请立即复制并保存到安全位置。
            </p>
          </div>

          <div>
            <label className="block text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2">
              守护进程配置（~/.teammate/config.yaml）
            </label>
            <div className="relative">
              <pre className="bg-slate-950 border border-slate-700 rounded-lg p-4 text-xs text-slate-300 font-mono overflow-x-auto whitespace-pre">
                {configYaml}
              </pre>
              <button
                onClick={() => handleCopy(configYaml)}
                className={`absolute top-2 right-2 px-2 py-1 rounded text-[10px] text-slate-300 ${copied ? "bg-emerald-600 text-white" : "bg-slate-700 hover:bg-slate-600"}`}
              >
                {copied ? "已复制" : "复制"}
              </button>
            </div>
          </div>

          <div className="bg-slate-800/50 border border-slate-700 rounded-lg p-4">
            <p className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-3">
              快速启动步骤
            </p>
            <ol className="space-y-2 text-sm text-slate-300">
              <li className="flex items-start gap-2">
                <span className="text-blue-400 font-bold shrink-0">1.</span>
                <span>
                  在代理机器上安装{" "}
                  <code className="text-blue-300 bg-blue-500/10 px-1.5 py-0.5 rounded">
                    teammate
                  </code>{" "}
                  CLI
                </span>
              </li>
              <li className="flex items-start gap-2">
                <span className="text-blue-400 font-bold shrink-0">2.</span>
                <span>
                  运行{" "}
                  <code className="text-blue-300 bg-blue-500/10 px-1.5 py-0.5 rounded">
                    teammate-agentd config init --profile {toolName}
                  </code>{" "}
                  生成配置文件
                </span>
              </li>
              <li className="flex items-start gap-2">
                <span className="text-blue-400 font-bold shrink-0">3.</span>
                <span>
                  将上方配置写入{" "}
                  <code className="text-blue-300 bg-blue-500/10 px-1.5 py-0.5 rounded">
                    ~/.teammate/config-{toolName}.yaml
                  </code>
                </span>
              </li>
              <li className="flex items-start gap-2">
                <span className="text-blue-400 font-bold shrink-0">4.</span>
                <span>
                  运行{" "}
                  <code className="text-blue-300 bg-blue-500/10 px-1.5 py-0.5 rounded">
                    teammate-agentd --profile {toolName}
                  </code>{" "}
                  启动守护进程
                </span>
              </li>
              <li className="flex items-start gap-2">
                <span className="text-blue-400 font-bold shrink-0">5.</span>
                <span>
                  守护进程将自动连接服务端、认领任务节点并执行编码
                </span>
              </li>
            </ol>
          </div>
        </div>
        <div className="p-5 border-t border-slate-800 flex justify-end">
          <button
            onClick={onClose}
            className="btn-press px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium"
          >
            我已保存 Token，关闭
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 代理注册表单弹窗
// ==========================================
function AgentRegisterForm({
  onSave,
  onClose,
  skills,
  mcpServers,
  saving,
}: {
  onSave: (data: {
    name: string;
    tool: string;
    identityInstruction: string;
    gitName: string;
    gitEmail: string;
    skills: string[];
    mcpServers: string[];
  }) => void;
  onClose: () => void;
  skills: Skill[];
  mcpServers: McpServer[];
  saving: boolean;
}) {
  const [name, setName] = useState("");
  const [tool, setTool] = useState("Claude Code");
  const [identity, setIdentity] = useState("");
  const [gitName, setGitName] = useState("");
  const [gitEmail, setGitEmail] = useState("");
  const [selectedSkills, setSelectedSkills] = useState<string[]>([]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([]);

  const enabledSkills = skills.filter((s) => (s as Skill & { enabled?: boolean }).enabled !== false);
  const enabledMcps = mcpServers.filter((m) => (m as McpServer & { enabled?: boolean }).enabled !== false);

  const handleSave = () => {
    if (!name.trim() || !gitName.trim() || !gitEmail.trim()) return;
    onSave({
      name: name.trim(),
      tool,
      identityInstruction:
        identity.trim() || `你是 ${name.trim()}，一个 AI 代理。`,
      gitName: gitName.trim(),
      gitEmail: gitEmail.trim(),
      skills: selectedSkills,
      mcpServers: selectedMcps,
    });
  };

  return (
    <div
      className="modal-overlay fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[560px] max-h-[85vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center sticky top-0 bg-slate-900 z-10">
          <h2 className="text-xl font-bold text-white">注册新 AI 代理</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-white"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-5 space-y-5">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                代理名称 <span className="text-red-400">*</span>
              </label>
              <input
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                placeholder="如: Code-Reviewer"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                编码工具
              </label>
              <select
                value={tool}
                onChange={(e) => setTool(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
              >
                <option>Claude Code</option>
                <option>OpenClaw</option>
                <option>OpenCode</option>
                <option>AtomCode</option>
                <option>MiMoCode</option>
                <option>自定义 CLI</option>
              </select>
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              身份指令
            </label>
            <textarea
              value={identity}
              onChange={(e) => setIdentity(e.target.value)}
              rows={3}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-blue-500 resize-none font-mono"
              placeholder="例: 你是一个前端专家，擅长 React 和 TypeScript..."
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                Git 用户名 <span className="text-red-400">*</span>
              </label>
              <input
                value={gitName}
                onChange={(e) => setGitName(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                placeholder="如: Claude Worker"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                Git 邮箱 <span className="text-red-400">*</span>
              </label>
              <input
                value={gitEmail}
                onChange={(e) => setGitEmail(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                placeholder="如: worker@example.com"
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2 flex items-center">
              <Zap className="w-4 h-4 mr-1.5 text-indigo-400" /> 关联技能
            </label>
            <MultiSelect
              options={enabledSkills.map((s) => ({
                id: s.id,
                name: s.name,
                enabled: true,
              }))}
              selected={selectedSkills}
              onChange={setSelectedSkills}
              placeholder="选择已配置的技能..."
              accentColor="indigo"
            />
            {enabledSkills.length === 0 && (
              <p className="text-xs text-amber-400 mt-1.5">
                请先在「Skill & MCP」菜单中配置技能
              </p>
            )}
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2 flex items-center">
              <Database className="w-4 h-4 mr-1.5 text-emerald-400" /> MCP
              服务器
            </label>
            <MultiSelect
              options={enabledMcps.map((m) => ({
                id: m.id,
                name: m.name,
                enabled: true,
              }))}
              selected={selectedMcps}
              onChange={setSelectedMcps}
              placeholder="选择已配置的 MCP 服务器..."
              accentColor="emerald"
            />
            {enabledMcps.length === 0 && (
              <p className="text-xs text-amber-400 mt-1.5">
                请先在「Skill & MCP」菜单中配置 MCP 服务器
              </p>
            )}
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
            onClick={handleSave}
            disabled={!name.trim() || !gitName.trim() || !gitEmail.trim() || saving}
            className="btn-press flex items-center px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 transition-colors-fast"
          >
            {saving ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Save className="w-4 h-4 mr-2" />
            )}
            创建代理并生成 API Token
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 代理编辑弹窗（技能/MCP 管理）
// ==========================================
function AgentEditForm({
  agent,
  onSave,
  onClose,
  skills,
  mcpServers,
  saving,
}: {
  agent: MappedAgent;
  onSave: (id: string, fields: Partial<MappedAgent>) => void;
  onClose: () => void;
  skills: Skill[];
  mcpServers: McpServer[];
  saving: boolean;
}) {
  const [identity, setIdentity] = useState(
    agent.identityInstruction || ""
  );
  const [gitName, setGitName] = useState(agent.gitName || "");
  const [gitEmail, setGitEmail] = useState(agent.gitEmail || "");
  const [selectedSkills, setSelectedSkills] = useState<string[]>([
    ...(agent.skills || []).map((s) => (typeof s === "string" ? s : s.id)),
  ]);
  const [selectedMcps, setSelectedMcps] = useState<string[]>([
    ...(agent.mcpServers || []).map((m) =>
      typeof m === "string" ? m : m.id
    ),
  ]);

  const enabledSkills = skills.filter(
    (s) => (s as Skill & { enabled?: boolean }).enabled !== false
  );
  const enabledMcps = mcpServers.filter(
    (m) => (m as McpServer & { enabled?: boolean }).enabled !== false
  );

  const availableSkills = enabledSkills;
  const availableMcps = enabledMcps;

  const handleSave = () => {
    onSave(agent.id, {
      identityInstruction: identity,
      gitName,
      gitEmail,
      skills: selectedSkills as unknown as Skill[],
      mcpServers: selectedMcps as unknown as McpServer[],
    });
  };

  return (
    <div
      className="modal-overlay fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[520px] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex justify-between items-center">
          <h2 className="text-xl font-bold text-white flex items-center">
            <Bot className="w-5 h-5 mr-2 text-blue-400" /> {agent.name}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>
        <div className="p-5 space-y-5">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              身份指令
            </label>
            <textarea
              value={identity}
              onChange={(e) => setIdentity(e.target.value)}
              rows={3}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-sm text-slate-300 focus:outline-none focus:border-blue-500 resize-none font-mono"
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                Git 用户名
              </label>
              <input
                value={gitName}
                onChange={(e) => setGitName(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                placeholder="如: Claude Worker"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-slate-300 mb-2">
                Git 邮箱
              </label>
              <input
                value={gitEmail}
                onChange={(e) => setGitEmail(e.target.value)}
                className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                placeholder="如: worker@example.com"
              />
            </div>
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2 flex items-center">
              <Zap className="w-4 h-4 mr-1.5 text-indigo-400" /> 技能
            </label>
            <MultiSelect
              options={availableSkills.map((s) => ({
                id: s.id,
                name: s.name,
                enabled: true,
              }))}
              selected={selectedSkills}
              onChange={setSelectedSkills}
              placeholder="选择已配置的技能..."
              accentColor="indigo"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2 flex items-center">
              <Database className="w-4 h-4 mr-1.5 text-emerald-400" /> MCP
              服务器
            </label>
            <MultiSelect
              options={availableMcps.map((m) => ({
                id: m.id,
                name: m.name,
                enabled: true,
              }))}
              selected={selectedMcps}
              onChange={setSelectedMcps}
              placeholder="选择已配置的 MCP 服务器..."
              accentColor="emerald"
            />
          </div>
        </div>
        <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg"
          >
            取消
          </button>
          <button
            onClick={handleSave}
            disabled={saving}
            className="flex items-center px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50"
          >
            {saving ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Save className="w-4 h-4 mr-2" />
            )}
            保存配置
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 代理详情弹窗
// ==========================================
interface HistoryItem {
  task: string;
  node: string;
  status: string;
}

function AgentDetailModal({
  agent,
  onClose,
  onEdit,
}: {
  agent: MappedAgent;
  onClose: () => void;
  onEdit: (agent: MappedAgent) => void;
}) {
  return (
    <div
      className="modal-overlay fixed inset-0 z-50 flex items-center justify-center"
      onClick={onClose}
    >
      <div
        className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[640px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-6 border-b border-slate-800 flex justify-between items-center sticky top-0 bg-slate-900 z-10">
          <div className="flex items-center">
            <div className="w-12 h-12 rounded-xl bg-slate-700/80 border border-slate-600 flex items-center justify-center mr-4">
              <Bot className="w-7 h-7 text-blue-400" />
            </div>
            <div>
              <h2 className="text-xl font-bold text-white">{agent.name}</h2>
              <div className="text-xs text-slate-400 flex items-center mt-1">
                <Terminal className="w-3 h-3 mr-1" /> {agent.tool}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => onEdit(agent)}
              className="p-2 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-blue-400 transition-colors-fast"
            >
              <Zap className="w-4 h-4" />
            </button>
            <button
              onClick={onClose}
              className="p-2 hover:bg-slate-700 rounded-lg text-slate-400 hover:text-white"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>
        <div className="p-6 space-y-6">
          <div>
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-2">
              身份指令
            </div>
            <div className="bg-slate-950 border border-slate-800 rounded-lg p-4 text-sm text-slate-300 leading-relaxed font-mono">
              {agent.identityInstruction}
            </div>
          </div>
          <div>
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3 flex items-center">
              <Zap className="w-3.5 h-3.5 mr-1 text-indigo-400" /> 关联技能
            </div>
            <div className="flex flex-wrap gap-2">
              {(agent.skills || []).map((s) => (
                <span
                  key={typeof s === "string" ? s : s.name}
                  className="px-3 py-1.5 bg-indigo-500/10 border border-indigo-500/20 text-indigo-300 text-xs rounded-lg"
                >
                  {typeof s === "string" ? s : s.name}
                </span>
              ))}
            </div>
          </div>
          <div>
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3 flex items-center">
              <Database className="w-3.5 h-3.5 mr-1 text-emerald-400" /> MCP
              服务器
            </div>
            <div className="space-y-2">
              {(agent.mcpServers || []).map((m) => (
                <div
                  key={typeof m === "string" ? m : m.name}
                  className="flex items-center text-sm text-slate-300 bg-slate-950 border border-slate-800 rounded-lg p-3"
                >
                  <div className="w-2 h-2 rounded-full bg-emerald-500 mr-3 status-glow-green animate-pulse-soft" />{" "}
                  {typeof m === "string" ? m : m.name}
                </div>
              ))}
            </div>
          </div>
          <div>
            <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3 flex items-center">
              <Cpu className="w-3.5 h-3.5 mr-1 text-blue-400" /> Token 用量
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="bg-slate-950 border border-slate-800 rounded-lg p-4">
                <div className="text-xs text-slate-400 mb-1">输入 Token</div>
                <div className="text-lg font-bold text-white">
                  {(agent.tokenUsage || { input: 0, output: 0 }).input.toLocaleString()}
                </div>
              </div>
              <div className="bg-slate-950 border border-slate-800 rounded-lg p-4">
                <div className="text-xs text-slate-400 mb-1">输出 Token</div>
                <div className="text-lg font-bold text-white">
                  {(agent.tokenUsage || { input: 0, output: 0 }).output.toLocaleString()}
                </div>
              </div>
            </div>
          </div>
          {((agent.history as HistoryItem[]) || []).length > 0 && (
            <div>
              <div className="text-xs font-semibold text-slate-500 uppercase tracking-wider mb-3 flex items-center">
                <Clock className="w-3.5 h-3.5 mr-1 text-slate-400" /> 近期执行历史
              </div>
              <div className="space-y-2">
                {(agent.history as HistoryItem[]).map((h, i) => (
                  <div
                    key={i}
                    className="flex items-center justify-between bg-slate-950 border border-slate-800 rounded-lg p-3"
                  >
                    <div className="flex items-center text-sm">
                      <span className="text-slate-300 font-mono mr-2">
                        {h.task}
                      </span>
                      <span className="text-slate-500">—</span>
                      <span className="text-slate-400 ml-2">{h.node}</span>
                    </div>
                    <span
                      className={`text-xs px-2 py-0.5 rounded-full ${
                        h.status === "completed"
                          ? "bg-emerald-500/10 text-emerald-400"
                          : h.status === "manual_intervention"
                            ? "bg-red-500/10 text-red-400"
                            : "bg-blue-500/10 text-blue-400"
                      }`}
                    >
                      {h.status === "completed"
                        ? "已完成"
                        : h.status === "manual_intervention"
                          ? "需介入"
                          : "进行中"}
                    </span>
                  </div>
                ))}
              </div>
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

const PROVIDER_MAP: Record<string, string> = {
  claude: "Claude Code",
  openclaw: "OpenClaw",
  opencode: "OpenCode",
  atomcode: "AtomCode",
  mimocode: "MiMoCode",
  copilot: "Copilot",
  hermes: "Hermes",
  gemini: "Gemini",
  pi: "Pi",
  cursor: "Cursor",
  kimi: "Kimi",
  kiro: "Kiro",
};

const TOOL_TO_PROVIDER: Record<string, string> = Object.fromEntries(
  Object.entries(PROVIDER_MAP).map(([k, v]) => [v, k])
);

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
  const router = useRouter();
  const agents = useAppStore((s) => s.agents);
  const setAgents = useAppStore((s) => s.setAgents);
  const tasks = useAppStore((s) => s.tasks);
  const skills = useAppStore((s) => s.skills);
  const mcpServers = useAppStore((s) => s.mcpServers);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  const [selectedAgent, setSelectedAgent] = useState<MappedAgent | null>(null);
  const [editingAgent, setEditingAgent] = useState<MappedAgent | null>(null);
  const [showRegister, setShowRegister] = useState(false);
  const [saving, setSaving] = useState(false);
  const [newAgentToken, setNewAgentToken] = useState<{
    name: string;
    id: string;
    apiToken: string;
    provider: string;
  } | null>(null);
  const [deletingAgent, setDeletingAgent] = useState<MappedAgent | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const pollingRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // 每 10 秒轮询代理状态以检测在线/离线变化
  useEffect(() => {
    if (!currentWorkspaceId) return;

    const refreshAgents = async () => {
      try {
        const agentList = await api.listAgents(currentWorkspaceId);
        if (agentList && Array.isArray(agentList)) {
          const mapped = agentList.map(mapAgentFromApi);
          setAgents(await hydrateAgentBindings(currentWorkspaceId, mapped));
        }
      } catch {
        // 静默忽略轮询错误
      }
    };

    pollingRef.current = setInterval(refreshAgents, 10000);
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [currentWorkspaceId, setAgents]);

  const handleRegister = async (data: {
    name: string;
    tool: string;
    identityInstruction: string;
    gitName: string;
    gitEmail: string;
    skills: string[];
    mcpServers: string[];
  }) => {
    if (!data.name.trim() || !currentWorkspaceId) return;
    setSaving(true);
    try {
      const provider = TOOL_TO_PROVIDER[data.tool] || data.tool || "claude";
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const created: any = await api.createAgent(currentWorkspaceId, {
        name: data.name,
        provider,
        instructions: data.identityInstruction || "",
        model: "",
        git_name: data.gitName,
        git_email: data.gitEmail,
      });
      await syncAgentBindings(
        currentWorkspaceId,
        created.id,
        [],
        data.skills,
        [],
        data.mcpServers
      );
      const mapped: MappedAgent = {
        id: created.id,
        name: created.name,
        tool: PROVIDER_MAP[created.provider] || created.provider || "自定义 CLI",
        status: created.status || "offline",
        task: null,
        identityInstruction: created.instructions || "",
        gitName: created.git_name || "",
        gitEmail: created.git_email || "",
        skills: skills.filter((s) => data.skills.includes(s.id)),
        mcpServers: mcpServers.filter((m) => data.mcpServers.includes(m.id)),
        tokenUsage: { input: 0, output: 0 },
        history: [],
      };
      setAgents([...agents, mapped]);
      setShowRegister(false);

      // 若返回 API Token 则展示（仅在创建时）
      if (created.api_token) {
        setNewAgentToken({
          name: created.name,
          id: created.id,
          apiToken: created.api_token,
          provider: created.provider || "claude",
        });
      }
    } catch (err) {
      console.error("Register agent failed:", err);
    } finally {
      setSaving(false);
    }
  };

  const handleEditSave = async (id: string, fields: Partial<MappedAgent>) => {
    if (!currentWorkspaceId) return;
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
      setEditingAgent(null);
    } catch (err) {
      console.error("Update agent failed:", err);
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
        err instanceof Error ? err.message : "删除失败，请重试"
      );
    } finally {
      setDeleting(false);
    }
  };

  const handleViewDetail = (agentId: string) => {
    router.push(`/agents/${agentId}`);
  };

  return (
    <div className="page-enter h-full flex flex-col p-8 overflow-y-auto">
      <div className="flex justify-between items-center mb-8 shrink-0">
        <div>
          <h1 className="text-2xl font-bold text-white mb-2">AI 代理集市</h1>
          <p className="text-sm text-slate-400">
            {agents.length} 个代理在线 ·
            点击卡片查看详情，或编辑技能和 MCP 配置。
          </p>
        </div>
        <button
          onClick={() => setShowRegister(true)}
          className="btn-press flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors-fast shadow-lg shadow-blue-500/20"
        >
          <Plus className="w-5 h-5 mr-2" /> 注册新代理
        </button>
      </div>

      {agents.length === 0 ? (
        <EmptyStateGuide page="agents" />
      ) : (
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {agents.map((agent) => {
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
              className="card-hover bg-slate-800/40 border border-slate-700/50 rounded-xl backdrop-blur-sm p-6 flex flex-col shadow-sm hover:border-slate-500 transition-all cursor-pointer hover:shadow-lg hover:shadow-blue-500/5"
            >
              <div className="flex justify-between items-start mb-6">
                <div className="flex items-center">
                  <div className="w-12 h-12 rounded-xl bg-gradient-to-br from-blue-500/20 to-indigo-500/20 border border-blue-500/20 flex items-center justify-center mr-4 shadow-inner">
                    <Bot className="w-7 h-7 text-blue-400" />
                  </div>
                  <div>
                    <h3 className="font-bold text-white text-lg leading-tight">
                      {agent.name}
                    </h3>
                    <div className="text-xs text-slate-400 flex items-center mt-1 font-mono">
                      <Terminal className="w-3 h-3 mr-1.5" /> {agent.tool}
                    </div>
                  </div>
                </div>
                <AgentStatusBadge status={agent.status} />
              </div>

              <div className="space-y-5 mb-6">
                <div>
                  <div className="text-xs font-semibold text-slate-500 mb-2 flex items-center">
                    <Zap className="w-3.5 h-3.5 mr-1 text-indigo-400" /> 技能
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {(agent.skills || []).map((s) => (
                      <span
                        key={typeof s === "string" ? s : s.name}
                        className="px-2 py-1 bg-indigo-500/10 border border-indigo-500/20 text-indigo-300 text-[11px] rounded uppercase tracking-wide font-medium"
                      >
                        {typeof s === "string" ? s : s.name}
                      </span>
                    ))}
                  </div>
                </div>
                <div>
                  <div className="text-xs font-semibold text-slate-500 mb-2 flex items-center">
                    <Database className="w-3.5 h-3.5 mr-1 text-emerald-400" />{" "}
                    MCP
                  </div>
                  <div className="flex flex-col gap-2">
                    {(agent.mcpServers || []).map((m) => (
                      <div
                        key={typeof m === "string" ? m : m.name}
                        className="flex items-center text-xs text-slate-300 bg-slate-900/50 border border-slate-700/50 rounded-lg p-2"
                      >
                        <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 mr-2.5 status-glow-green animate-pulse-soft" />{" "}
                        {typeof m === "string" ? m : m.name}
                      </div>
                    ))}
                  </div>
                </div>
                <div>
                  <div className="text-xs font-semibold text-slate-500 mb-2 flex items-center">
                    <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-emerald-400" />
                    完成节点
                  </div>
                  <div className="text-lg font-bold text-emerald-400">
                    {completedCount}
                  </div>
                </div>
              </div>

              <div className="mt-auto pt-4 border-t border-slate-700/50 flex justify-between items-center bg-slate-800/20 -mx-6 -mb-6 px-6 pb-6 rounded-b-xl">
                <div className="flex items-center gap-4">
                  <div className="text-xs text-slate-400">
                    当前:{" "}
                    <span className="text-white font-medium ml-1">
                      {currentTask?.title
                        ? currentTask.title.substring(0, 14) + "..."
                        : agent.task || "空闲"}
                    </span>
                  </div>
                  <div className="text-xs flex items-center gap-1">
                    {agent.status === "online" || agent.status === "busy" ? (
                      <>
                        <Wifi className="w-3.5 h-3.5 text-emerald-400" />
                        <span className="text-emerald-400">SSE: 已连接</span>
                      </>
                    ) : (
                      <>
                        <WifiOff className="w-3.5 h-3.5 text-red-400" />
                        <span className="text-red-400">SSE: 离线</span>
                      </>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleViewDetail(agent.id);
                    }}
                    className="text-slate-400 hover:text-white text-xs font-medium bg-slate-700/50 px-3 py-1.5 rounded-md border border-slate-600/50 transition-colors-fast"
                  >
                    详情
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      setEditingAgent(agent);
                    }}
                    className="text-blue-400 hover:text-blue-300 text-xs font-medium bg-blue-500/10 px-3 py-1.5 rounded-md border border-blue-500/20 transition-colors-fast"
                  >
                    配置能力
                  </button>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDelete(agent.id);
                    }}
                    className="text-red-400 hover:text-red-300 text-xs font-medium bg-red-500/10 px-3 py-1.5 rounded-md border border-red-500/20 transition-colors-fast"
                  >
                    删除
                  </button>
                </div>
              </div>
            </div>
          );
        })}
      </div>
        )}

      {selectedAgent && (
        <AgentDetailModal
          agent={selectedAgent}
          onClose={() => setSelectedAgent(null)}
          onEdit={(a) => {
            setSelectedAgent(null);
            setEditingAgent(a);
          }}
        />
      )}
      {editingAgent && (
        <AgentEditForm
          agent={editingAgent}
          onSave={handleEditSave}
          onClose={() => setEditingAgent(null)}
          skills={skills}
          mcpServers={mcpServers}
          saving={saving}
        />
      )}
      {showRegister && (
        <AgentRegisterForm
          onSave={handleRegister}
          onClose={() => setShowRegister(false)}
          skills={skills}
          mcpServers={mcpServers}
          saving={saving}
        />
      )}
      {deletingAgent && (
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
      )}
      {newAgentToken && (
        <TokenRevealModal
          agentName={newAgentToken.name}
          agentId={newAgentToken.id}
          apiToken={newAgentToken.apiToken}
          provider={newAgentToken.provider}
          workspaceId={currentWorkspaceId}
          onClose={async () => {
            setNewAgentToken(null);
            // 刷新代理列表以获取守护进程启动后的状态变化
            if (!currentWorkspaceId) return;
            try {
              const agentList = await api.listAgents(currentWorkspaceId);
              if (agentList && Array.isArray(agentList)) {
                const mapped = agentList.map(mapAgentFromApi);
                setAgents(mapped);
              }
            } catch {
              /* 忽略 */
            }
          }}
        />
      )}
    </div>
  );
}
