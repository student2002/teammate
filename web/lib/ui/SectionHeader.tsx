// SectionHeader：页面内区块标题（14px/600），可选右侧操作。

export default function SectionHeader({
  title,
  actions,
  className = "",
}: {
  title: React.ReactNode;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`flex items-center justify-between gap-3 ${className}`}>
      <h2 className="text-sm font-semibold text-fg">{title}</h2>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
