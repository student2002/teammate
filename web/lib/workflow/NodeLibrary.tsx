"use client";
// 左侧节点库(§13.1):按类别分组;开始/完成固定存在于模板,在库中置灰不可添加。
import { useTranslations } from "next-intl";
import { Play } from "lucide-react";
import { CATEGORY_TONE, LIBRARY_GROUPS, NODE_CATALOG, TONE_VAR } from "./catalog";
import type { WorkflowTypeKey } from "./types";

export function NodeLibrary({ onAdd }: { onAdd: (typeKey: WorkflowTypeKey) => void }) {
  const t = useTranslations("workflows.editor");
  return (
    <div className="w-52 shrink-0 border-r border-line bg-surface overflow-y-auto p-2 space-y-3">
      <div className="px-1 pt-1 text-[10px] font-semibold text-fg-faint uppercase tracking-wider">{t("library.title")}</div>
      {/* 开始节点固定存在,触发方式在 Inspector 中切换,不在库中列出具体触发类型 */}
      <div>
        <div className="px-1 pb-1 text-[10px] text-fg-faint">{t("library.groups.trigger")}</div>
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-[var(--radius-control)] text-left text-xs border border-line opacity-45 cursor-not-allowed text-fg-faint">
          <Play className="w-3.5 h-3.5 shrink-0" style={{ color: TONE_VAR[CATEGORY_TONE.trigger] }} />
          <span className="truncate">{t("startTitle")}</span>
        </div>
      </div>
      {LIBRARY_GROUPS.map((group) => (
        <div key={group.category}>
          <div className="px-1 pb-1 text-[10px] text-fg-faint">{t(`library.groups.${group.category}`)}</div>
          <div className="space-y-1">
            {group.typeKeys.map((typeKey) => {
              const d = NODE_CATALOG[typeKey];
              const Icon = d.icon;
              return (
                <button
                  key={typeKey}
                  onClick={() => onAdd(typeKey)}
                  className="w-full flex items-center gap-2 px-2 py-1.5 rounded-[var(--radius-control)] text-left text-xs border border-line text-fg-muted hover:bg-surface-2 hover:text-fg transition-colors-fast"
                >
                  <Icon className="w-3.5 h-3.5 shrink-0" style={{ color: TONE_VAR[CATEGORY_TONE[d.category]] }} />
                  <span className="truncate">{t(`types.${typeKey}.label`)}</span>
                </button>
              );
            })}
          </div>
        </div>
      ))}
      <p className="px-1 pb-1 text-[9px] leading-relaxed text-fg-faint">{t("library.fixedHint")}</p>
    </div>
  );
}
