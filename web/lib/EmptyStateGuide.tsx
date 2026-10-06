"use client";
// 首次使用/空态引导卡片：插在主要数据页的空态位置，告诉新用户"这是干嘛的、怎么开始"，
// 并提供完整使用指南的外部链接。

import { useTranslations } from "next-intl";
import { Compass, ExternalLink, CheckCircle2 } from "lucide-react";

// 完整使用指南地址（首次登录教程文档）
export const GUIDE_URL = "http://zhenfeng.chat";

type PageKey = "project" | "agents" | "dashboard" | "history" | "reviews" | "memory" | "skills";

export default function EmptyStateGuide({
  page,
}: {
  page: PageKey;
}) {
  const t = useTranslations(`empty.${page}`);
  const tGlobal = useTranslations("empty");

  const steps = [
    t("step1"),
    t("step2"),
    t("step3", { defaultValue: "" }),
    t("step4", { defaultValue: "" }),
  ].filter(Boolean);

  return (
    <div className="bg-gradient-to-br from-surface-2/40 to-surface/40 border border-line-strong/60 rounded-xl p-6">
      <div className="flex items-start gap-3 mb-3">
        <div className="w-10 h-10 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center shrink-0">
          <Compass className="w-5 h-5 text-primary-fg" />
        </div>
        <div>
          <h3 className="text-base font-bold text-fg">{t("title")}</h3>
          <p className="text-xs text-fg-subtle mt-1 leading-relaxed">
            {t("desc")}
          </p>
        </div>
      </div>

      <ol className="space-y-2 my-4">
        {steps.map((step, i) => (
          <li key={i} className="flex items-start gap-2 text-sm text-fg-muted">
            <CheckCircle2 className="w-4 h-4 mt-0.5 text-success-fg shrink-0" />
            <span className="leading-relaxed">{step}</span>
          </li>
        ))}
      </ol>

      <a
        href={GUIDE_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center text-xs font-medium text-primary-fg hover:text-primary-fg transition-colors-fast"
      >
        <ExternalLink className="w-3.5 h-3.5 mr-1.5" />
        {tGlobal("viewGuide")}
      </a>
    </div>
  );
}
