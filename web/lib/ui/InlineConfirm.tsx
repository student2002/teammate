"use client";
// InlineConfirm：危险操作原位二次确认。默认只显示文字/图标按钮，进入确认态后才出现
// danger 实底；确认与取消并排原位展开，不用遮罩。
// busy 时禁用确认按钮；完成后由调用方通过状态变化收起（resetKey 变化时回到初始态）。

import { useEffect, useState } from "react";
import Button from "./Button";

export default function InlineConfirm({
  label,
  onConfirm,
  confirmLabel,
  cancelLabel,
  busy = false,
  disabled = false,
  resetKey,
  className = "",
}: {
  label: React.ReactNode;
  onConfirm: () => void;
  confirmLabel: string;
  cancelLabel: string;
  busy?: boolean;
  disabled?: boolean;
  /** 变化时回到未确认态（如操作成功后的刷新标识） */
  resetKey?: string | number;
  className?: string;
}) {
  const [arming, setArming] = useState(false);

  useEffect(() => {
    setArming(false);
  }, [resetKey]);

  if (!arming) {
    return (
      <Button
        variant="danger-ghost"
        size="sm"
        disabled={disabled}
        onClick={() => setArming(true)}
        className={className}
      >
        {label}
      </Button>
    );
  }

  return (
    <span role="group" className={`inline-flex items-center gap-1.5 ${className}`}>
      <Button variant="danger" size="sm" loading={busy} onClick={onConfirm}>
        {confirmLabel}
      </Button>
      <Button variant="ghost" size="sm" onClick={() => setArming(false)}>
        {cancelLabel}
      </Button>
    </span>
  );
}
