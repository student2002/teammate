"use client";
// 主题切换器：渲染注册表中的全部主题；持久化与防闪烁由 next-themes 承担。

import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { useLocale } from "next-intl";
import { Check, Contrast, Moon, Sparkles, Sun, Zap } from "lucide-react";
import { THEMES } from "./registry";
import type { ThemeIcon } from "./types";

const ICONS: Record<ThemeIcon, typeof Sun> = {
  moon: Moon,
  sun: Sun,
  sparkles: Sparkles,
  zap: Zap,
  contrast: Contrast,
};

type Locale = "zh-CN" | "en-US";

export default function ThemeSwitcher({
  placement = "top",
}: {
  /** 弹层相对按钮的方向：侧边栏底部用 top（向上弹），登录页用 bottom */
  placement?: "top" | "bottom";
}) {
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const { theme, setTheme } = useTheme();
  const locale = useLocale() as Locale;

  // next-themes 在挂载前不返回主题，固定渲染默认主题图标与名称避免水合不一致
  useEffect(() => setMounted(true), []);

  const active = THEMES.find((t) => t.id === theme) ?? THEMES[0];
  // 挂载前 theme 为 undefined，title 回退 THEMES[0] 与 SSR 输出保持一致
  const display = mounted ? active : THEMES[0];
  const ActiveIcon = ICONS[active.icon];

  return (
    <div className="relative shrink-0">
      <button
        onClick={() => setOpen(!open)}
        onBlur={() => setTimeout(() => setOpen(false), 150)}
        className="w-8 h-8 rounded-lg bg-surface-2 hover:bg-surface-3 flex items-center justify-center transition-colors-fast btn-press"
        title={display.name[locale] ?? display.name["en-US"] ?? display.id}
      >
        {mounted ? (
          <ActiveIcon className="w-4 h-4 text-primary-fg" />
        ) : (
          <Moon className="w-4 h-4 text-primary-fg" />
        )}
      </button>
      {open && (
        <div
          className={`absolute left-0 ${
            placement === "top" ? "bottom-full mb-1" : "top-full mt-1"
          } w-48 bg-surface-2 border border-line rounded-xl shadow-2xl overflow-hidden z-50 animate-scale-in`}
        >
          {THEMES.map((t) => {
            const Icon = ICONS[t.icon];
            const isActive = t.id === theme;
            return (
              <button
                key={t.id}
                onClick={() => {
                  setTheme(t.id);
                  setOpen(false);
                }}
                className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                  isActive
                    ? "bg-primary/10 text-primary-fg"
                    : "text-fg-muted hover:bg-surface-3"
                }`}
              >
                <Icon className="w-3.5 h-3.5 shrink-0" />
                <span className="flex-1 truncate">
                  {t.name[locale] ?? t.name["en-US"] ?? t.id}
                </span>
                <span className="flex items-center gap-0.5 shrink-0">
                  <span
                    className="w-2 h-2 rounded-full border border-line"
                    style={{ backgroundColor: t.preview.bg }}
                  />
                  <span
                    className="w-2 h-2 rounded-full border border-line"
                    style={{ backgroundColor: t.preview.surface }}
                  />
                  <span
                    className="w-2 h-2 rounded-full"
                    style={{ backgroundColor: t.preview.primary }}
                  />
                </span>
                {isActive && <Check className="w-3.5 h-3.5 shrink-0" />}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
