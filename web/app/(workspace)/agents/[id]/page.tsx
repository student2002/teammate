"use client";
// 代理详情页：单个 Agent 的身份指令、Git 身份、技能、MCP、Token 用量与执行历史。
// 与列表页共用 AgentGlyph/StatusBadge/SectionHeader 等组件，不维护第二套视觉。

import { useState, useEffect } from "react";
import { useRouter, useParams } from "next/navigation";
import { useTranslations } from "next-intl";
import {
  ArrowLeft,
  BookOpen,
  Zap,
  Database,
  Clock,
  BarChart3,
  Terminal,
  Cpu,
} from "lucide-react";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";
import type { MappedAgent } from "@/lib/types";
import { mapAgentFromApi } from "@/lib/mappers";
import StatusBadge from "@/lib/ui/StatusBadge";
import type { BadgeTone } from "@/lib/ui/StatusBadge";
import SectionHeader from "@/lib/ui/SectionHeader";
import AgentGlyph from "@/lib/product/AgentGlyph";
import { agentStatusTone, taskStateTone, providerLabel } from "@/lib/product/presentation";

interface HistoryItem {
  task: string;
  node: string;
  status: string;
}

// 历史条目状态 → 徽章语义色（失败用 danger；其余复用任务状态映射）
function historyTone(status: string): BadgeTone {
  return status === "failed" ? "danger" : taskStateTone(status);
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
        <span className="text-fg-subtle">{label}</span>
        <span className="text-fg font-medium font-mono">
          {value.toLocaleString()}{" "}
          <span className="text-fg-faint text-xs">({pct.toFixed(1)}%)</span>
        </span>
      </div>
      <div className="h-2 w-full rounded-full bg-surface-3/50 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all-normal ${color}`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

export default function AgentDetailPage() {
  const t = useTranslations("agentDetail");
  const ta = useTranslations("agents.detail");
  const ts = useTranslations("agents.status");
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
          <div className="w-8 h-8 border-2 border-primary-fg border-t-transparent rounded-full animate-spin" />
          <span className="text-sm text-fg-subtle">{t("loading")}</span>
        </div>
      </div>
    );
  }

  if (!agent) {
    return (
      <div className="h-full flex items-center justify-center">
        <p className="text-fg-faint text-lg">{t("notFound")}</p>
      </div>
    );
  }

  const { input, output } = agent.tokenUsage || { input: 0, output: 0 };
  const totalTokens = input + output;
  const historyItems = (agent.history as HistoryItem[]) || [];
  const skills = agent.skills || [];
  const mcpServers = agent.mcpServers || [];
  const gitIdentity =
    agent.gitName || agent.gitEmail ? `${agent.gitName} <${agent.gitEmail}>` : "—";

  return (
    <div className="h-full flex flex-col page-enter overflow-y-auto">
      <div className="px-6 pt-6">
        <button
          onClick={handleBack}
          className="inline-flex items-center gap-2 text-fg-subtle hover:text-fg transition-colors-fast mb-4 self-start group focus-visible:outline-none"
        >
          <ArrowLeft className="w-4 h-4 group-hover:-translate-x-0.5 transition-transform-fast" />
          <span className="text-sm font-medium">{t("backToList")}</span>
        </button>

        {/* 代理头部 */}
        <div className="rounded-[var(--radius-card)] border border-line-strong/50 bg-surface-2/30 p-5 flex items-start gap-4">
          <AgentGlyph name={agent.name} status={agent.status} size="lg" />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-3 flex-wrap">
              <h1 className="text-xl font-bold text-fg truncate">{agent.name}</h1>
              <StatusBadge tone={agentStatusTone(agent.status)} dot className="shrink-0">
                {ts(agent.status)}
              </StatusBadge>
            </div>
            <div className="mt-1 flex items-center gap-1.5 text-xs text-fg-subtle font-mono">
              <Terminal className="w-3 h-3 shrink-0" />
              <span className="truncate">{providerLabel(agent.provider)}</span>
            </div>
            {agent.task && (
              <p className="mt-2 text-sm text-fg-muted leading-relaxed">{agent.task}</p>
            )}
          </div>
        </div>
      </div>

      {/* 双栏布局 */}
      <div className="px-6 pb-6 pt-4 grid grid-cols-1 lg:grid-cols-2 gap-4 items-start flex-1">
        {/* 身份指令 / Git 身份 / 记忆标识 */}
        <div className="rounded-[var(--radius-card)] border border-line bg-surface-2/20 p-5">
          <SectionHeader
            title={
              <span className="flex items-center gap-2">
                <BookOpen className="w-4 h-4 text-fg-faint" />
                {t("identity")}
              </span>
            }
            className="mb-4"
          />
          <p className="text-sm text-fg-muted leading-relaxed whitespace-pre-wrap font-mono break-words">
            {agent.identityInstruction || t("identityEmpty")}
          </p>
          <div className="mt-4 pt-3 border-t border-line text-xs leading-relaxed">
            <span className="text-fg-faint">{ta("gitLabel")}：</span>
            <span className="font-mono text-fg-muted break-all">{gitIdentity}</span>
          </div>
          {agent.personaKey && (
            <div className="mt-2 text-xs leading-relaxed">
              <span className="text-fg-faint">{ta("personaLabel")}：</span>
              <span
                className="font-mono text-fg-muted break-all"
                title={ta("personaBadge", { key: agent.personaKey })}
              >
                {agent.personaKey}
              </span>
            </div>
          )}
        </div>

        {/* Token 用量下钻 */}
        <div className="rounded-[var(--radius-card)] border border-line bg-surface-2/20 p-5">
          <SectionHeader
            title={
              <span className="flex items-center gap-2">
                <BarChart3 className="w-4 h-4 text-fg-faint" />
                {t("tokenUsage")}
              </span>
            }
            className="mb-4"
          />

          <div className="flex items-baseline gap-2 mb-4">
            <span className="text-2xl font-bold text-fg font-mono">{totalTokens.toLocaleString()}</span>
            <span className="text-xs text-fg-faint">{t("tokensTotal")}</span>
          </div>

          <div className="space-y-3">
            <TokenBar
              label={t("inputTokens")}
              value={input}
              total={totalTokens}
              color="bg-primary"
            />
            <TokenBar
              label={t("outputTokens")}
              value={output}
              total={totalTokens}
              color="bg-accent"
            />
          </div>
        </div>

        {/* 能力：技能 + MCP */}
        <div className="rounded-[var(--radius-card)] border border-line bg-surface-2/20 p-5">
          <SectionHeader
            title={
              <span className="flex items-center gap-2">
                <Database className="w-4 h-4 text-fg-faint" />
                {t("knowledge")}
              </span>
            }
            className="mb-4"
          />

          <div className="mb-4">
            <div className="flex items-center gap-2 mb-2">
              <Zap className="w-3.5 h-3.5 text-accent-fg" />
              <span className="text-xs font-medium text-fg-subtle">{t("skills")}</span>
            </div>
            {skills.length > 0 ? (
              <div className="flex flex-wrap gap-1.5">
                {skills.map((skill) => (
                  <span
                    key={typeof skill === "string" ? skill : skill.id}
                    className="inline-flex items-center px-2 py-0.5 rounded border text-xs bg-accent/10 text-accent-fg border-accent/20"
                  >
                    {typeof skill === "string" ? skill : skill.name}
                  </span>
                ))}
              </div>
            ) : (
              <p className="text-fg-faint text-xs">{t("skillsEmpty")}</p>
            )}
          </div>

          <div>
            <div className="flex items-center gap-2 mb-2">
              <Cpu className="w-3.5 h-3.5 text-success-fg" />
              <span className="text-xs font-medium text-fg-subtle">{t("mcp")}</span>
            </div>
            {mcpServers.length > 0 ? (
              <div className="flex flex-wrap gap-1.5">
                {mcpServers.map((server) => (
                  <span
                    key={typeof server === "string" ? server : server.id}
                    className="inline-flex items-center px-2 py-0.5 rounded border text-xs bg-success/10 text-success-fg border-success/20"
                  >
                    {typeof server === "string" ? server : server.name}
                  </span>
                ))}
              </div>
            ) : (
              <p className="text-fg-faint text-xs">{t("mcpEmpty")}</p>
            )}
          </div>
        </div>

        {/* 执行历史 */}
        <div className="rounded-[var(--radius-card)] border border-line bg-surface-2/20 p-5">
          <SectionHeader
            title={
              <span className="flex items-center gap-2">
                <Clock className="w-4 h-4 text-fg-faint" />
                {t("history")}
              </span>
            }
            className="mb-4"
          />

          {historyItems.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-line">
                    <th className="text-left text-fg-subtle font-medium pb-3 pr-4">
                      {t("thTask")}
                    </th>
                    <th className="text-left text-fg-subtle font-medium pb-3 pr-4">
                      {t("thNode")}
                    </th>
                    <th className="text-left text-fg-subtle font-medium pb-3">
                      {t("thStatus")}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {historyItems.map((item, idx) => (
                    <tr key={idx} className="border-b border-line/50 last:border-0">
                      <td className="py-2.5 pr-4 text-fg-muted font-mono text-xs">
                        {item.task}
                      </td>
                      <td className="py-2.5 pr-4 text-fg-subtle text-xs">{item.node}</td>
                      <td className="py-2.5">
                        <StatusBadge tone={historyTone(item.status)}>
                          {item.status === "completed"
                            ? ta("historyCompleted")
                            : item.status === "manual_intervention"
                              ? ta("historyIntervention")
                              : item.status === "in_progress"
                                ? ta("historyInProgress")
                                : item.status}
                        </StatusBadge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="text-fg-faint text-sm">{t("historyEmpty")}</p>
          )}
        </div>
      </div>
    </div>
  );
}
