"use client";
// 字段选择器(§13.3):列出 trigger/task/上游节点输出/vars 的受限路径,
// 点击后经 onPick 回传,由调用方插入 {{path}};同时允许手工输入路径。
import { Braces } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslations } from "next-intl";

export function FieldPicker({
  paths,
  onPick,
}: {
  paths: string[];
  onPick: (path: string) => void;
}) {
  const t = useTranslations("workflows.editor");
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const boxRef = useRef<HTMLDivElement>(null);
  const hits = paths.filter((p) => p.toLowerCase().includes(q.trim().toLowerCase()));

  // 点击面板外部关闭下拉
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  return (
    <div className="relative" ref={boxRef}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title={t("fieldPicker.title")}
        className="h-[26px] px-1.5 flex items-center border border-line rounded-[var(--radius-control)] text-fg-subtle hover:text-fg hover:bg-surface-2 transition-colors-fast"
      >
        <Braces className="w-3 h-3" />
      </button>
      {open && (
        <div className="absolute right-0 top-full mt-1 z-30 w-64 bg-surface border border-line rounded-[var(--radius-panel)] shadow-2xl overflow-hidden">
          <div className="px-2 py-1.5 border-b border-line">
            <input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={t("fieldPicker.filter")}
              className="w-full bg-transparent text-xs text-fg placeholder-fg-faint outline-none"
            />
          </div>
          <div className="max-h-52 overflow-y-auto">
            {hits.map((p) => (
              <button
                key={p}
                type="button"
                onClick={() => {
                  onPick(p);
                  setOpen(false);
                  setQ("");
                }}
                className="w-full text-left px-2.5 py-1.5 text-[11px] font-mono text-fg-muted hover:bg-surface-2 hover:text-fg truncate"
              >
                {p}
              </button>
            ))}
            {hits.length === 0 && <div className="px-2.5 py-2 text-[11px] text-fg-faint">{t("fieldPicker.noHit")}</div>}
          </div>
        </div>
      )}
    </div>
  );
}
