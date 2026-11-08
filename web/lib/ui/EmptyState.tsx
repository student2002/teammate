// EmptyState：空状态 = 图标(24~32px) + 标题 + 一句说明 + 可选主操作。
// 不在空状态中重复页面标题。

import type { LucideIcon } from "lucide-react";

export default function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className = "",
}: {
  icon?: LucideIcon;
  title: React.ReactNode;
  description?: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`flex flex-col items-center justify-center text-center py-10 ${className}`}>
      {Icon && <Icon className="w-8 h-8 text-fg-faint mb-3" strokeWidth={1.5} />}
      <p className="text-sm font-medium text-fg-muted">{title}</p>
      {description && (
        <p className="mt-1 text-xs text-fg-faint max-w-xs">{description}</p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}
