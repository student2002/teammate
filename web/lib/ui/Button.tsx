"use client";
// Button：全局统一按钮。primary 为主操作（每页最多一个）；danger 实底只在确认态使用
// （平时危险操作走 InlineConfirm 的文字态）；loading 时保持按钮占位不塌缩。

const variantClasses = {
  primary: "bg-primary-strong hover:bg-primary text-fg font-medium",
  secondary: "bg-surface-2 hover:bg-surface-3 text-fg border border-line",
  ghost: "text-fg-subtle hover:text-fg hover:bg-surface-2",
  "danger-ghost": "text-danger-fg hover:bg-danger/10",
  danger: "bg-danger-strong hover:bg-danger text-fg",
} as const;

const sizeClasses = {
  sm: "h-7 px-2.5 text-xs rounded-[var(--radius-control)] gap-1",
  md: "h-[34px] px-3.5 text-sm rounded-[var(--radius-control)] gap-1.5",
  lg: "h-9 px-5 text-sm rounded-[var(--radius-control)] gap-1.5",
} as const;

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: keyof typeof variantClasses;
  size?: keyof typeof sizeClasses;
  loading?: boolean;
}

export default function Button({
  variant = "secondary",
  size = "md",
  loading = false,
  disabled,
  className = "",
  children,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || loading}
      className={`inline-flex items-center justify-center whitespace-nowrap transition-colors-fast btn-press disabled:opacity-50 disabled:cursor-not-allowed disabled:pointer-events-none focus-visible:outline-none ${variantClasses[variant]} ${sizeClasses[size]} ${className}`}
      {...rest}
    >
      {loading && (
        <span className="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin shrink-0" />
      )}
      {children}
    </button>
  );
}
