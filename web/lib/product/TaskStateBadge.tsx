"use client";
// TaskStateBadge：任务级状态徽章（语义 tone + 状态点 + 文字），全站统一。

import { useTranslations } from "next-intl";
import StatusBadge from "@/lib/ui/StatusBadge";
import { taskStateLabelKey, taskStateTone } from "./presentation";

export default function TaskStateBadge({ state, className = "" }: { state: string; className?: string }) {
  const t = useTranslations("dashboard");
  return (
    <StatusBadge tone={taskStateTone(state)} dot className={className}>
      {t(`status.${taskStateLabelKey(state)}`)}
    </StatusBadge>
  );
}
