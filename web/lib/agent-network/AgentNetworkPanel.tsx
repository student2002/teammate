"use client";
// AgentNetworkPanel.tsx: Dashboard 的协作网络面板。
// 负责折叠态(sessionStorage)、空态、WebGL 失败降级、小屏二维列表与选中 Agent 上下文;
// 3D 场景按需动态加载:折叠、小屏或无数据时不加载 Three.js chunk。

import { useEffect, useMemo, useState } from "react";
import dynamic from "next/dynamic";
import { useLocale, useTranslations } from "next-intl";
import { ChevronDown, ChevronUp, Network } from "lucide-react";
import type { AgentStatus, MappedAgent, MappedTask } from "@/lib/types";
import { useMediaQuery } from "@/lib/use-media-query";
import { buildAgentNetworkModel } from "./build-model";
import type { AgentNetworkNode } from "./types";

const AgentNetworkScene = dynamic(() => import("./AgentNetworkScene"), {
  ssr: false,
  loading: () => (
    <div className="absolute inset-0 flex items-center justify-center">
      <div className="w-6 h-6 border-2 border-primary-fg border-t-transparent rounded-full animate-spin" />
    </div>
  ),
});

const COLLAPSE_KEY = "teammate.networkCollapsed";

const STATUS_DOT: Record<AgentStatus, string> = {
  online: "bg-success-fg",
  busy: "bg-accent-fg",
  paused: "bg-warning-fg",
  pending: "bg-warning-fg",
  offline: "bg-fg-faint",
};

interface AgentNetworkPanelProps {
  agents: MappedAgent[];
  tasks: MappedTask[];
  projectId: string | null;
  /** 把指定 Agent 设为看板筛选(现有 filterAgent),不跳页、不弹窗 */
  onViewTasks: (agentName: string) => void;
}

export default function AgentNetworkPanel({
  agents,
  tasks,
  projectId,
  onViewTasks,
}: AgentNetworkPanelProps) {
  const t = useTranslations("agentNetwork");
  // 语言切换会重绘 3D 标签:标签文案画进 canvas 纹理,靠重挂载场景触发重建
  const locale = useLocale();
  const [collapsed, setCollapsed] = useState(false);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [webglFailed, setWebglFailed] = useState(false);
  const narrow = useMediaQuery("(max-width: 819px)");
  const compact = useMediaQuery("(max-width: 559px)");

  useEffect(() => {
    setCollapsed(window.sessionStorage.getItem(COLLAPSE_KEY) === "1");
  }, []);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      window.sessionStorage.setItem(COLLAPSE_KEY, next ? "1" : "0");
      return next;
    });
  };

  const model = useMemo(
    () => buildAgentNetworkModel(agents, tasks, projectId),
    [agents, tasks, projectId]
  );

  const statusText = useMemo<Record<AgentStatus, string>>(
    () => ({
      online: t("status.online"),
      offline: t("status.offline"),
      busy: t("status.busy"),
      paused: t("status.paused"),
      pending: t("status.pending"),
    }),
    [t]
  );

  const selectedNode: AgentNetworkNode | null = selectedAgentId
    ? model.nodes.find((node) => node.id === selectedAgentId) ?? null
    : null;
  const selectedAgent = selectedNode
    ? agents.find((agent) => agent.id === selectedNode.id) ?? null
    : null;
  const selectedName = selectedAgent?.name || selectedNode?.name || "";
  const activeTaskTitles = selectedNode
    ? tasks
        .filter((task) => selectedNode.activeTaskIds.includes(task.id))
        .map((task) => task.title)
    : [];

  const showScene = !compact && !webglFailed && model.nodes.length > 0;

  return (
    <section className="px-6 pb-3" aria-label={t("title")}>
      <button
        type="button"
        onClick={toggleCollapsed}
        aria-expanded={!collapsed}
        className="w-full h-12 px-3 flex items-center gap-2.5 rounded-[var(--radius-control)] border border-line bg-surface-2/40 hover:bg-surface-3/60 transition-colors-fast focus-visible:outline-none text-left"
      >
        <Network className="w-4 h-4 text-primary-fg shrink-0" />
        <span className="text-xs font-medium text-fg">{t("title")}</span>
        <span className="text-[11px] text-fg-faint">
          {t("agentsCount", { count: agents.length })}
        </span>
        <span className="flex-1" />
        <span className="text-[11px] text-fg-faint hidden sm:block">
          {collapsed ? t("expand") : t("collapse")}
        </span>
        {collapsed ? (
          <ChevronDown className="w-4 h-4 text-fg-faint shrink-0" />
        ) : (
          <ChevronUp className="w-4 h-4 text-fg-faint shrink-0" />
        )}
      </button>

      {!collapsed && (
        <div
          className={`mt-2 rounded-[var(--radius-card)] border border-line bg-surface-2/20 overflow-hidden animate-fade-in ${
            narrow ? "" : "flex"
          }`}
        >
          <div className={narrow ? "relative h-56" : "relative flex-1 min-w-0 h-[292px]"}>
            {model.nodes.length === 0 ? (
              <div className="h-full flex flex-col items-center justify-center gap-1.5 text-center px-6">
                <Network className="w-6 h-6 text-fg-faint" />
                <div className="text-sm text-fg-muted">{t("empty.title")}</div>
                <div className="text-xs text-fg-faint max-w-sm">{t("empty.description")}</div>
              </div>
            ) : showScene ? (
              <AgentNetworkScene
                key={locale}
                model={model}
                selectedAgentId={selectedAgentId}
                onSelectAgent={setSelectedAgentId}
                formatStatus={(status) => statusText[status]}
                ariaLabel={t("title")}
                onWebglError={() => setWebglFailed(true)}
              />
            ) : (
              <div className="h-full overflow-y-auto divide-y divide-line/50">
                {model.nodes.map((node) => {
                  const agent = agents.find((item) => item.id === node.id);
                  return (
                    <button
                      key={node.id}
                      type="button"
                      onClick={() => setSelectedAgentId(node.id)}
                      className={`w-full px-3 py-2 flex items-center gap-2.5 text-left transition-colors-fast focus-visible:outline-none ${
                        node.id === selectedAgentId ? "bg-primary/10" : "hover:bg-surface-3/50"
                      }`}
                    >
                      <span className={`w-2 h-2 rounded-full shrink-0 ${STATUS_DOT[node.status]}`} />
                      <span className="text-xs text-fg truncate">
                        {agent?.name || node.name}
                      </span>
                      <span className="text-[10px] text-fg-faint truncate hidden sm:block">
                        {agent?.tool || node.provider}
                      </span>
                      <span className="ml-auto text-[10px] text-fg-faint shrink-0">
                        {node.activeTaskIds.length > 0
                          ? t("fallback.taskCount", { count: node.activeTaskIds.length })
                          : ""}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>

          {model.nodes.length > 0 && (
            <aside
              className={
                narrow ? "border-t border-line p-3" : "w-60 shrink-0 border-l border-line p-3"
              }
            >
              {!selectedNode ? (
                <div className="flex flex-col justify-center gap-1.5 min-h-[248px]">
                  <div className="text-xs font-medium text-fg-muted">{t("context.none")}</div>
                  <p className="text-[11px] text-fg-faint leading-relaxed">
                    {t("context.noneHint")}
                  </p>
                </div>
              ) : (
                <div className="flex flex-col gap-2.5">
                  <div className="flex items-center gap-2 min-w-0">
                    <span
                      className={`w-2 h-2 rounded-full shrink-0 ${STATUS_DOT[selectedNode.status]}`}
                    />
                    <span className="text-sm font-medium text-fg truncate">{selectedName}</span>
                    <span className="text-[10px] text-fg-faint font-mono shrink-0">
                      {selectedAgent?.tool || selectedNode.provider}
                    </span>
                  </div>
                  <div className="text-xs text-fg-subtle">
                    {t("context.status")}
                    <span className="ml-1.5 text-fg">{statusText[selectedNode.status]}</span>
                  </div>
                  <div>
                    <div className="text-[11px] font-medium text-fg-muted mb-1">
                      {t("context.activeTasks")}
                    </div>
                    {activeTaskTitles.length === 0 ? (
                      <p className="text-[11px] text-fg-faint">{t("context.noActiveTasks")}</p>
                    ) : (
                      <ul className="space-y-1">
                        {activeTaskTitles.slice(0, 4).map((title, index) => (
                          <li key={index} className="text-[11px] text-fg-subtle truncate">
                            · {title}
                          </li>
                        ))}
                        {activeTaskTitles.length > 4 && (
                          <li className="text-[10px] text-fg-faint">
                            {t("context.moreTasks", { count: activeTaskTitles.length - 4 })}
                          </li>
                        )}
                      </ul>
                    )}
                  </div>
                  <div className="mt-auto flex flex-col gap-1.5">
                    <button
                      type="button"
                      onClick={() => onViewTasks(selectedName)}
                      className="h-7 px-2 text-[11px] bg-primary/15 hover:bg-primary/25 border border-primary/30 rounded-[var(--radius-control)] text-primary-fg transition-colors-fast focus-visible:outline-none"
                    >
                      {t("context.viewTasks")}
                    </button>
                    <button
                      type="button"
                      onClick={() => setSelectedAgentId(null)}
                      className="h-7 px-2 text-[11px] bg-surface-2 hover:bg-surface-3 border border-line rounded-[var(--radius-control)] text-fg-muted transition-colors-fast focus-visible:outline-none"
                    >
                      {t("context.clear")}
                    </button>
                  </div>
                </div>
              )}
            </aside>
          )}
        </div>
      )}
    </section>
  );
}
