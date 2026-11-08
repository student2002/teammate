"use client";
// SegmentedControl：分段单选控件。radiogroup 语义 + aria-checked，键盘方向键由原生
// button Tab 序兜底（选项数少，不做 roving tabindex）。

export default function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  ariaLabel,
}: {
  options: Array<{ value: T; label: React.ReactNode }>;
  value: T;
  onChange: (value: T) => void;
  ariaLabel: string;
}) {
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className="inline-flex items-center bg-surface-2 border border-line rounded-[var(--radius-control)] p-0.5"
    >
      {options.map((opt) => {
        const active = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onChange(opt.value)}
            className={`h-7 px-3 text-xs rounded-[5px] transition-colors-fast focus-visible:outline-none ${
              active
                ? "bg-surface text-fg font-medium shadow-sm border border-line"
                : "text-fg-subtle hover:text-fg border border-transparent"
            }`}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}
