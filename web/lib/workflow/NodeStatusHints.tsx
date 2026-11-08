"use client";
// 运行节点的失败/跳过原因提示与标题未解析引用警告标记。运行视图
// (workflows/view/page.tsx)与任务监控(TaskCockpit.tsx)的节点详情区共用,
// 保证两处同款渲染一致;文案统一来自 runFeedback i18n 命名空间。

import { useTranslations } from "next-intl";
import { TriangleAlert } from "lucide-react";

export interface NodeErrorInfo {
  code?: string;
  message?: string;
}

// 图接口里非失败节点/未失败尝试的 error 是空对象 {},按无错误内容处理
function errorText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

// 失败原因块:一行 code、一行 message;message 含 permission/403 时追加一行
// 权限排查指引。code/message 都为空时整体不渲染。
export function NodeErrorBlock({
  error,
  className = "text-[10px]",
}: {
  error?: NodeErrorInfo | null;
  className?: string;
}) {
  const t = useTranslations("runFeedback");
  const code = errorText(error?.code);
  const message = errorText(error?.message);
  if (!code && !message) return null;
  const permissionHint = /permission|403/i.test(message);
  return (
    <div
      className={`rounded-[var(--radius-control)] border border-danger/25 bg-danger/10 px-2.5 py-2 text-danger-fg space-y-0.5 ${className}`}
    >
      {code && <div className="font-semibold font-mono break-all">{code}</div>}
      {message && <div className="break-all">{message}</div>}
      {permissionHint && <div className="break-all">{t("errPermissionHint")}</div>}
    </div>
  );
}

// 跳过原因:上游分支未走到该节点,连线未被激活
export function NodeSkippedHint({ className = "text-[10px]" }: { className?: string }) {
  const t = useTranslations("runFeedback");
  return <div className={`${className} text-fg-faint`}>{t("skippedReason")}</div>;
}

// 标题包含未解析引用("{{ ")时的警告标记;不改动标题文本本身
export function UnresolvedRefMark({ title }: { title: string }) {
  const t = useTranslations("runFeedback");
  if (!title.includes("{{ ")) return null;
  const hint = t("unresolvedRefTitle");
  return (
    <span title={hint} className="inline-flex items-center shrink-0" aria-label={hint}>
      <TriangleAlert className="w-3.5 h-3.5 text-warning-fg" aria-hidden="true" />
    </span>
  );
}
