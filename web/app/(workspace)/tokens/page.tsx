"use client";
// Token 用量页：展示任务与 Agent 的 Token 消耗统计。统计口径不变；
// 图表系列色只使用主题语义变量（数据系列区分靠变量顺序，不引入裸色）。

import { Terminal } from "lucide-react";
import { useTranslations } from "next-intl";
import { useAppStore } from "@/lib/store";
import PageHeader from "@/lib/ui/PageHeader";
import EmptyState from "@/lib/ui/EmptyState";
import type { MappedAgent, MappedTask } from "@/lib/types";

// 图表数据系列色：全部来自主题契约变量，随主题切换
const SERIES_COLORS = [
  "var(--primary)",
  "var(--accent)",
  "var(--success)",
  "var(--warning)",
  "var(--danger)",
  "var(--primary-fg)",
];

interface AgentTokenItem {
  name: string;
  input: number;
  output: number;
  total: number;
}

interface TopAgentItem {
  name: string;
  tokens: number;
  percentage: number;
  color: string;
}

interface ComputedStats {
  totalTokens: number;
  totalInput: number;
  totalOutput: number;
  topAgents: TopAgentItem[];
  inputPct: number;
  outputPct: number;
  taskTotal: number;
  taskCompleted: number;
  taskInProgress: number;
}

function computeStats(agents: MappedAgent[], tasks: MappedTask[]): ComputedStats {
  let totalInput = 0;
  let totalOutput = 0;
  const agentTokenMap: AgentTokenItem[] = [];

  if (agents && agents.length > 0) {
    for (const agent of agents) {
      const input = agent.tokenUsage?.input || 0;
      const output = agent.tokenUsage?.output || 0;
      totalInput += input;
      totalOutput += output;
      agentTokenMap.push({
        name: agent.name || agent.id,
        input,
        output,
        total: input + output,
      });
    }
  }

  const totalTokens = totalInput + totalOutput;

  agentTokenMap.sort((a, b) => b.total - a.total);
  const topAgents = agentTokenMap.slice(0, 6).map((a, i) => ({
    name: a.name,
    tokens: a.total,
    percentage: totalTokens > 0 ? Math.round((a.total / totalTokens) * 100) : 0,
    color: SERIES_COLORS[i % SERIES_COLORS.length],
  }));

  const inputPct = totalTokens > 0 ? Math.round((totalInput / totalTokens) * 100) : 0;
  const outputPct = totalTokens > 0 ? 100 - inputPct : 0;

  let taskTotal = 0;
  let taskCompleted = 0;
  let taskInProgress = 0;

  if (tasks && tasks.length > 0) {
    taskTotal = tasks.length;
    taskCompleted = tasks.filter((t) => t.priorityState === "succeeded").length;
    taskInProgress = tasks.filter((t) => t.priorityState === "running").length;
  }

  return {
    totalTokens,
    totalInput,
    totalOutput,
    topAgents,
    inputPct,
    outputPct,
    taskTotal,
    taskCompleted,
    taskInProgress,
  };
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}

export default function TokenStatsPage() {
  const t = useTranslations("tokens");
  const agents = useAppStore((s) => s.agents);
  const tasks = useAppStore((s) => s.tasks);

  const hasData = (agents && agents.length > 0) || (tasks && tasks.length > 0);

  if (!hasData) {
    return (
      <div className="h-full flex items-center justify-center page-enter">
        <EmptyState icon={Terminal} title={t("emptyTitle")} description={`${t("empty")} ${t("emptyHint")}`} />
      </div>
    );
  }

  const stats = computeStats(agents, tasks);

  return (
    <div className="h-full flex flex-col overflow-y-auto page-enter">
      <PageHeader title={t("title")} description={t("subtitle")} />

      <div className="px-6 pb-6 flex-1 min-h-0 flex flex-col">
        {/* 总览卡片 */}
        <div className="grid grid-cols-3 gap-5 mb-5 shrink-0">
          <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5">
            <div className="text-fg-subtle text-sm font-medium mb-1">{t("totalTokens")}</div>
            <div className="text-3xl font-bold text-fg">{stats.totalTokens.toLocaleString()}</div>
            <div className="text-xs text-fg-faint mt-2">
              {t("inputOutput", {
                input: formatTokens(stats.totalInput),
                output: formatTokens(stats.totalOutput),
              })}
            </div>
          </div>
          <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5">
            <div className="text-fg-subtle text-sm font-medium mb-1">{t("taskProgress")}</div>
            <div className="text-3xl font-bold text-fg">
              {stats.taskCompleted} / {stats.taskTotal}
            </div>
            <div className="text-xs text-fg-faint mt-2">{t("inProgressCount", { count: stats.taskInProgress })}</div>
          </div>
          <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5">
            <div className="text-fg-subtle text-sm font-medium mb-1">{t("agentCount")}</div>
            <div className="text-3xl font-bold text-fg">{agents ? agents.length : 0}</div>
            <div className="text-xs text-fg-faint mt-2">{t("registeredAgents")}</div>
          </div>
        </div>

        <div className="grid grid-cols-3 gap-5 flex-1 min-h-0">
          {/* Agent 排名 */}
          <div className="col-span-2 bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5 flex flex-col">
            <h3 className="font-semibold text-fg mb-5">{t("agentRanking")}</h3>
            {stats.topAgents.length > 0 ? (
              <div className="flex-1 flex flex-col justify-center space-y-4">
                {stats.topAgents.map((item) => (
                  <div key={item.name}>
                    <div className="flex justify-between text-sm mb-1.5">
                      <span className="text-fg-muted font-medium">{item.name}</span>
                      <span className="text-fg-subtle">{formatTokens(item.tokens)}</span>
                    </div>
                    <div className="w-full bg-surface h-2.5 rounded-full overflow-hidden">
                      <div
                        className="h-full rounded-full transition-all"
                        style={{
                          width: `${item.percentage}%`,
                          backgroundColor: item.color,
                        }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex-1 flex items-center justify-center">
                <p className="text-fg-faint text-sm">{t("noAgentData")}</p>
              </div>
            )}
            <div className="mt-4 pt-3 border-t border-line text-[10px] text-fg-faint flex items-center">
              <Terminal className="w-3 h-3 mr-1" /> {t("basedOnReports")}
            </div>
          </div>

          {/* 右侧汇总 */}
          <div className="flex flex-col gap-5">
            {/* 输入/输出比例 */}
            <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5">
              <h3 className="font-semibold text-fg mb-4">{t("inputOutputRatio")}</h3>
              <div className="flex items-center gap-4">
                <div className="relative w-24 h-24 shrink-0">
                  <svg viewBox="0 0 36 36" className="w-full h-full -rotate-90">
                    <circle cx="18" cy="18" r="15.5" fill="none" strokeWidth="3" className="chart-ring-bg" />
                    <circle
                      cx="18"
                      cy="18"
                      r="15.5"
                      fill="none"
                      strokeWidth="3"
                      className="chart-ring-fg"
                      strokeDasharray={`${stats.inputPct * 0.97} ${stats.outputPct * 0.97}`}
                      strokeLinecap="round"
                    />
                  </svg>
                  <div className="absolute inset-0 flex items-center justify-center flex-col">
                    <span className="text-lg font-bold text-fg">{stats.inputPct}%</span>
                  </div>
                </div>
                <div className="space-y-2 text-xs">
                  <div className="flex items-center">
                    <div className="w-2.5 h-2.5 rounded-full bg-primary mr-2" /> {t("inputPct", { pct: stats.inputPct })}
                  </div>
                  <div className="flex items-center">
                    <div className="w-2.5 h-2.5 rounded-full bg-fg-faint mr-2" /> {t("outputPct", { pct: stats.outputPct })}
                  </div>
                </div>
              </div>
            </div>

            {/* 任务状态分布 */}
            <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5 flex-1">
              <h3 className="font-semibold text-fg mb-4">{t("taskStatusDist")}</h3>
              {stats.taskTotal > 0 ? (
                <div className="space-y-3">
                  <div className="flex justify-between text-sm">
                    <span className="text-success-fg">{t("completed")}</span>
                    <span className="text-fg-subtle">{stats.taskCompleted}</span>
                  </div>
                  <div className="w-full bg-surface h-2.5 rounded-full overflow-hidden">
                    <div
                      className="h-full rounded-full bg-success"
                      style={{
                        width: `${stats.taskTotal > 0 ? (stats.taskCompleted / stats.taskTotal) * 100 : 0}%`,
                      }}
                    />
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-accent-fg">{t("inProgress")}</span>
                    <span className="text-fg-subtle">{stats.taskInProgress}</span>
                  </div>
                  <div className="w-full bg-surface h-2.5 rounded-full overflow-hidden">
                    <div
                      className="h-full rounded-full bg-accent"
                      style={{
                        width: `${stats.taskTotal > 0 ? (stats.taskInProgress / stats.taskTotal) * 100 : 0}%`,
                      }}
                    />
                  </div>
                </div>
              ) : (
                <p className="text-fg-faint text-sm">{t("noTaskData")}</p>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
