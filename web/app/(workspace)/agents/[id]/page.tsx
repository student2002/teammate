"use client";
// 代理详情页：展示单个 Agent 的身份指令、知识库、技能、MCP 服务与 Token 用量。

import { useState, useEffect } from "react";
import { useRouter, useParams } from "next/navigation";
import {
  ArrowLeft,
  Bot,
  Terminal,
  Zap,
  Database,
  Clock,
  Cpu,
  Activity,
  BookOpen,
  BarChart3,
  CheckCircle2,
  XCircle,
  AlertTriangle,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import type { MappedAgent } from "@/lib/types";
import { mapAgentFromApi } from "@/lib/mappers";

// ==========================================
// 状态配置
// ==========================================
const STATUS_CONFIG: Record<
  string,
  {
    label: string;
    icon: typeof Activity;
    badge: string;
  }
> = {
  running: {
    label: "运行中",
    icon: Activity,
    badge: "bg-emerald-500/20 text-emerald-400 border-emerald-500/30",
  },
  idle: {
    label: "空闲",
    icon: Clock,
    badge: "bg-slate-500/20 text-slate-400 border-slate-500/30",
  },
  error: {
    label: "异常",
    icon: AlertTriangle,
    badge: "bg-red-500/20 text-red-400 border-red-500/30",
  },
  completed: {
    label: "已完成",
    icon: CheckCircle2,
    badge: "bg-blue-500/20 text-blue-400 border-blue-500/30",
  },
};

interface HistoryItem {
  task: string;
  node: string;
  status: string;
}

const HISTORY_STATUS_ICON: Record<string, React.ReactNode> = {
  success: <CheckCircle2 className="w-4 h-4 text-emerald-400" />,
  failed: <XCircle className="w-4 h-4 text-red-400" />,
  running: <Activity className="w-4 h-4 text-blue-400 animate-pulse" />,
};

// ==========================================
// 子组件
// ==========================================
function StatusBadge({ status }: { status: string }) {
  const config = STATUS_CONFIG[status] || STATUS_CONFIG.idle;
  const Icon = config.icon;
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium border ${config.badge}`}
    >
      <Icon className="w-3 h-3" />
      {config.label}
    </span>
  );
}

function TokenBar({
  label,
  value,
  total,
  color,
}: {
  label: string;
  value: number;
  total: number;
  color: string;
}) {
  const pct = total > 0 ? (value / total) * 100 : 0;
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-sm">
        <span className="text-slate-400">{label}</span>
        <span className="text-white font-medium">
          {value.toLocaleString()}{" "}
          <span className="text-slate-500 text-xs">({pct.toFixed(1)}%)</span>
        </span>
      </div>
      <div className="h-3 w-full rounded-full bg-slate-700/50 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-500 ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

// ==========================================
// 主页面组件
// ==========================================
export default function AgentDetailPage() {
  const router = useRouter();
  const params = useParams();
  const agentId = params.id as string;

  const agents = useAppStore((s) => s.agents);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  const [agent, setAgent] = useState<MappedAgent | null>(null);
  const [loading, setLoading] = useState(true);

  // 从 store 或 API 加载代理数据
  useEffect(() => {
    const loadAgent = async () => {
      // 先尝试从 store 中查找
      const found = agents.find((a) => a.id === agentId);
      if (found) {
        setAgent(found);
        setLoading(false);
        return;
      }

      // 否则从 API 获取
      if (!currentWorkspaceId) {
        setLoading(false);
        return;
      }
      try {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const apiAgent: any = await api.getAgent(currentWorkspaceId, agentId);
        const mapped = mapAgentFromApi(apiAgent);
        setAgent(mapped);
      } catch (err) {
        console.error("Failed to load agent:", err);
      } finally {
        setLoading(false);
      }
    };

    if (agentId) {
      loadAgent();
    }
  }, [agentId, agents, currentWorkspaceId]);

  // 当 agents 变化时，从 store 同步代理数据
  useEffect(() => {
    const found = agents.find((a) => a.id === agentId);
    if (found) {
      setAgent(found);
    }
  }, [agentId, agents]);

  const handleBack = () => {
    router.push("/agents");
  };

  if (loading) {
    return (
      <div className="h-full flex items-center justify-center">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-blue-400 border-t-transparent rounded-full animate-spin" />
          <span className="text-sm text-slate-400">加载代理信息...</span>
        </div>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="h-full flex items-center justify-center">
        <p className="text-slate-500 text-lg">未找到该代理</p>
      </div>
    );
  }

  const { input, output } = agent.tokenUsage || { input: 0, output: 0 };
  const totalTokens = input + output;

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto">
      {/* 返回按钮 */}
      <button
        onClick={handleBack}
        className="inline-flex items-center gap-2 text-slate-400 hover:text-white transition-colors mb-6 self-start group"
      >
        <ArrowLeft className="w-4 h-4 group-hover:-translate-x-0.5 transition-transform" />
        <span className="text-sm font-medium">返回列表</span>
      </button>

      {/* 代理头部 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-6 mb-6">
        <div className="flex items-start gap-5">
          <div className="w-16 h-16 rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center shrink-0">
            <Bot className="w-8 h-8 text-white" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-3 mb-1">
              <h1 className="text-2xl font-bold text-white truncate">
                {agent.name}
              </h1>
              <StatusBadge status={agent.status} />
            </div>
            <div className="flex items-center gap-2 text-slate-400 text-sm">
              <Terminal className="w-3.5 h-3.5" />
              <span>{agent.tool}</span>
            </div>
            {agent.task && (
              <p className="mt-2 text-slate-300 text-sm leading-relaxed">
                {agent.task}
              </p>
            )}
          </div>
        </div>
      </div>

      {/* 双栏布局 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 flex-1">
        {/* 左栏 */}
        <div className="flex flex-col gap-6">
          {/* 身份指令 */}
          <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <BookOpen className="w-4 h-4 text-indigo-400" />
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">
                身份指令
              </h2>
            </div>
            <p className="text-slate-300 text-sm leading-relaxed whitespace-pre-wrap">
              {agent.identityInstruction || "未设置身份指令"}
            </p>
          </div>

          {/* 知识库 */}
          <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <Database className="w-4 h-4 text-indigo-400" />
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">
                知识库
              </h2>
            </div>

            {/* 技能 */}
            <div className="mb-5">
              <div className="flex items-center gap-2 mb-2.5">
                <Zap className="w-3.5 h-3.5 text-amber-400" />
                <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">
                  技能
                </span>
              </div>
              {agent.skills && agent.skills.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                  {agent.skills.map((skill) => (
                    <span
                      key={typeof skill === "string" ? skill : skill.name}
                      className="inline-flex items-center px-2.5 py-1 rounded-lg bg-amber-500/10 text-amber-300 text-xs font-medium border border-amber-500/20"
                    >
                      {typeof skill === "string" ? skill : skill.name}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="text-slate-500 text-xs">暂无技能</p>
              )}
            </div>

            {/* MCP 服务 */}
            <div>
              <div className="flex items-center gap-2 mb-2.5">
                <Cpu className="w-3.5 h-3.5 text-cyan-400" />
                <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">
                  MCP 服务
                </span>
              </div>
              {agent.mcpServers && agent.mcpServers.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                  {agent.mcpServers.map((server) => (
                    <span
                      key={typeof server === "string" ? server : server.name}
                      className="inline-flex items-center px-2.5 py-1 rounded-lg bg-cyan-500/10 text-cyan-300 text-xs font-medium border border-cyan-500/20"
                    >
                      {typeof server === "string" ? server : server.name}
                    </span>
                  ))}
                </div>
              ) : (
                <p className="text-slate-500 text-xs">暂无 MCP 服务</p>
              )}
            </div>
          </div>
        </div>

        {/* 右栏 */}
        <div className="flex flex-col gap-6">
          {/* Token 用量下钻 */}
          <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-6">
            <div className="flex items-center gap-2 mb-4">
              <BarChart3 className="w-4 h-4 text-indigo-400" />
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">
                Token 用量
              </h2>
            </div>

            <div className="flex items-baseline gap-2 mb-5">
              <span className="text-3xl font-bold text-white">
                {totalTokens.toLocaleString()}
              </span>
              <span className="text-slate-500 text-sm">tokens 总计</span>
            </div>

            <div className="space-y-4">
              <TokenBar
                label="输入 Token"
                value={input}
                total={totalTokens}
                color="bg-blue-500"
              />
              <TokenBar
                label="输出 Token"
                value={output}
                total={totalTokens}
                color="bg-indigo-500"
              />
            </div>
          </div>

          {/* 执行历史 */}
          <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-6 flex-1">
            <div className="flex items-center gap-2 mb-4">
              <Clock className="w-4 h-4 text-indigo-400" />
              <h2 className="text-sm font-semibold text-white uppercase tracking-wider">
                执行历史
              </h2>
            </div>

            {agent.history && (agent.history as HistoryItem[]).length > 0 ? (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-700/60">
                      <th className="text-left text-slate-400 font-medium pb-3 pr-4">
                        任务
                      </th>
                      <th className="text-left text-slate-400 font-medium pb-3 pr-4">
                        节点
                      </th>
                      <th className="text-left text-slate-400 font-medium pb-3">
                        状态
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {(agent.history as HistoryItem[]).map((item, idx) => (
                      <tr
                        key={idx}
                        className="border-b border-slate-700/30 last:border-0"
                      >
                        <td className="py-3 pr-4 text-slate-300 font-mono text-xs">
                          {item.task}
                        </td>
                        <td className="py-3 pr-4 text-slate-400 text-xs">
                          {item.node}
                        </td>
                        <td className="py-3">
                          <span className="inline-flex items-center gap-1.5">
                            {HISTORY_STATUS_ICON[item.status] || (
                              <Activity className="w-4 h-4 text-slate-500" />
                            )}
                            <span className="text-xs text-slate-400 capitalize">
                              {item.status}
                            </span>
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="text-slate-500 text-sm">暂无执行记录</p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
