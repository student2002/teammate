// StatusBadge：统一语义状态徽章。tone 由调用方从业务状态映射（映射函数位于
// lib/product/presentation.ts，S2 落地）；默认 4px 圆角方形，不用胶囊形。
// 状态不只靠颜色：dot 与文字并存。

const toneClasses = {
  neutral: "bg-surface-3/60 text-fg-subtle border-line",
  primary: "bg-primary/12 text-primary-fg border-primary/25",
  accent: "bg-accent/12 text-accent-fg border-accent/25",
  success: "bg-success/12 text-success-fg border-success/25",
  warning: "bg-warning/12 text-warning-fg border-warning/25",
  danger: "bg-danger/12 text-danger-fg border-danger/25",
} as const;

const toneDotClasses = {
  neutral: "bg-fg-faint",
  primary: "bg-primary-fg",
  accent: "bg-accent-fg",
  success: "bg-success-fg",
  warning: "bg-warning-fg",
  danger: "bg-danger-fg",
} as const;

export type BadgeTone = keyof typeof toneClasses;

export default function StatusBadge({
  tone = "neutral",
  dot = false,
  children,
  className = "",
}: {
  tone?: BadgeTone;
  dot?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded border px-1.5 py-0.5 text-xs leading-none whitespace-nowrap ${toneClasses[tone]} ${className}`}
    >
      {dot && <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${toneDotClasses[tone]}`} />}
      {children}
    </span>
  );
}
