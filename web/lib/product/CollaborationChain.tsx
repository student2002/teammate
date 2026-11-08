"use client";
// CollaborationChain：协作链——任务节点接力视图，本产品的主要视觉标志。
// 圆点=执行/控制节点，菱形=human.input 人工节点；连线只表示节点顺序。
// compact（看板卡/表格内）最多 5 节点，超出显示首/当前/末，省略数量标在对应连线段上；
// full（任务工作区）完整展示并带状态文字。
// 入口数据在页面边界整理一次，组件按下标直读，不做数组修复。

import { useTranslations } from "next-intl";
import type { TaskNodeStatus } from "@/lib/types";
import { compactChainIndices, nodeVisual, taskStateLabelKey } from "./presentation";

export interface CollaborationChainProps {
  names: string[];
  statuses: TaskNodeStatus[];
  agents: string[];
  currentIndex: number;
  /** 与 names 同序的节点 type_key（human.input 画菱形）；缺省按圆点 */
  typeKeys?: string[];
  density?: "compact" | "full";
}

export default function CollaborationChain({
  names,
  statuses,
  agents,
  currentIndex,
  typeKeys,
  density = "compact",
}: CollaborationChainProps) {
  const t = useTranslations("dashboard");
  const count = names.length;
  if (count === 0) return null;

  const compact = density === "compact";
  const { indices } = compact
    ? compactChainIndices(count, currentIndex)
    : { indices: Array.from({ length: count }, (_, i) => i) };

  const labelOf = (i: number) => names[i] || t("chain.unnamedNode");
  // 已完成节点不再需要执行者，显示完成状态而非误导性的"待认领"
  const agentOf = (i: number) => {
    const a = agents[i];
    if (a) return a;
    if (statuses[i] === "succeeded") return t("status.succeeded");
    return t("chain.unassigned");
  };

  return (
    <div
      className={`flex items-start ${compact ? "" : "overflow-x-auto pb-1"}`}
      role="list"
      aria-label={t("chain.ariaLabel")}
    >
      {indices.map((i, pos) => {
        const visual = nodeVisual(statuses[i], i === currentIndex, typeKeys?.[i]);
        const isCurrent = i === currentIndex && statuses[i] !== "succeeded";
        const nextIdx = pos < indices.length - 1 ? indices[pos + 1] : null;
        // 两个保留节点之间被省略的节点数（compact 且 count>5 时才会 >0）
        const skipped = nextIdx !== null ? nextIdx - i - 1 : 0;
        const gapActive = nextIdx !== null && (isCurrent || statuses[i] === "succeeded") && statuses[nextIdx] !== "waiting";
        return (
          <div key={i} role="listitem" className="flex items-start min-w-0 flex-1 last:flex-none">
            {/* 节点列 */}
            <div className={`flex flex-col items-center ${compact ? "w-14 min-w-14" : "w-20 min-w-20"} px-0.5`}>
              <div className="flex items-center justify-center h-3.5">
                {visual.shape === "diamond" ? (
                  <span className={`w-2.5 h-2.5 rotate-45 rounded-[2px] border-2 ${visual.className}`} />
                ) : (
                  <span className={`w-3 h-3 rounded-full border-2 ${visual.className}`} />
                )}
              </div>
              <span
                className={`mt-1.5 text-[10px] leading-tight text-center truncate w-full ${isCurrent ? "text-fg font-medium" : "text-fg-subtle"}`}
                title={labelOf(i)}
              >
                {labelOf(i)}
              </span>
              <span className="text-[9px] leading-tight text-fg-faint truncate w-full text-center" title={agentOf(i)}>
                {agentOf(i)}
              </span>
              {!compact && (
                <span className="text-[9px] leading-tight text-fg-faint mt-0.5">
                  {t(`status.${taskStateLabelKey(statuses[i] || "waiting")}`)}
                </span>
              )}
            </div>
            {/* 连线段：到下一保留节点；中点标注被省略的节点数 */}
            {nextIdx !== null && (
              <div className="flex flex-col items-center justify-start h-3.5 pt-[5px] flex-1 min-w-3 relative">
                <div className={`h-px w-full ${gapActive ? "bg-accent/70" : "bg-line-strong/70"}`} />
                {skipped > 0 && (
                  <span className="absolute -top-0.5 left-1/2 -translate-x-1/2 text-[8px] leading-none text-fg-faint bg-surface px-0.5">
                    +{skipped}
                  </span>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
