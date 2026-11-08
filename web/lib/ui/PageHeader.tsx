// PageHeader：一级页面统一头部。标题 24px/700，说明与 meta 在下，actions 右对齐；
// 无 actions 时不渲染占位，不留空白。

export default function PageHeader({
  title,
  description,
  meta,
  actions,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  meta?: React.ReactNode;
  actions?: React.ReactNode;
}) {
  return (
    <div className="flex items-start justify-between gap-4 px-6 pt-6 pb-4">
      <div className="min-w-0">
        <h1 className="text-2xl font-bold text-fg leading-tight">{title}</h1>
        {description && <p className="mt-1.5 text-sm text-fg-subtle">{description}</p>}
        {meta && <div className="mt-2.5 flex items-center gap-2 flex-wrap">{meta}</div>}
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0 pt-1">{actions}</div>}
    </div>
  );
}
