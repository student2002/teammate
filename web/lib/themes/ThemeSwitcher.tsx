"use client";
// 主题切换器：渲染内置注册表 + 运行时上传的自定义主题；持久化与防闪烁由 next-themes 承担。
// 自定义主题：面板底部内联「导入主题」入口(选 theme.json → 校验 → 预览 → 安装 → 自动切换)，
// 动态主题行带删除；CSS 变量块由 lib/themes/dynamic 注入，data-theme 属性即生效。

import { useEffect, useRef, useState } from "react";
import { useTheme } from "next-themes";
import { useLocale, useTranslations } from "next-intl";
import { Check, Contrast, Moon, Plus, Sparkles, Sun, Trash2, Upload, X, Zap } from "lucide-react";
import { THEMES } from "./registry";
import type { ThemeIcon } from "./types";
import { validateThemeFile, injectCustomThemesCss, type ThemeFileValidation } from "./dynamic";
import api from "@/lib/api";
import { useAppStore } from "@/lib/store";

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
  const t = useTranslations("themeImporter");
  const [open, setOpen] = useState(false);
  const [mounted, setMounted] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const { theme, setTheme } = useTheme();
  const locale = useLocale() as Locale;

  const customThemes = useAppStore((s) => s.customThemes);
  const setCustomThemes = useAppStore((s) => s.setCustomThemes);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);

  // 导入流程状态：选中待装文件 → 校验预览 → 安装中/错误
  const [importing, setImporting] = useState(false);
  const [candidate, setCandidate] = useState<ThemeFileValidation | null>(null);
  const [installing, setInstalling] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const refreshCustomThemes = async () => {
    if (!currentWorkspaceId) return;
    try {
      const res = await api.listCustomThemes(currentWorkspaceId);
      const list = ((res as Record<string, unknown>)?.themes ?? []) as Array<{
        id: string; name_zh: string; name_en: string; icon: string; vars: Record<string, string>;
      }>;
      setCustomThemes(list);
      injectCustomThemesCss(list);
      // 对账:持久化的主题 id 已不存在(如他端删除)时回落深色,避免变量悬空白屏样式
      const ids = new Set<string>([...THEMES.map((b) => b.id), ...list.map((ct) => ct.id)]);
      if (theme && theme !== "system" && !ids.has(theme)) setTheme("dark");
    } catch {
      // 读取失败时保持现状,不阻塞主题使用
    }
  };

  // 挂载时拉取自定义主题并注入(切换器多处渲染,store 去重后各处一致)
  useEffect(() => {
    setMounted(true);
    refreshCustomThemes();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentWorkspaceId]);

  // 点击外部关闭：用 pointerdown 指针事件判断，不能依赖按钮失焦——按下菜单项到松开
  // 超过一帧就会先触发 blur，菜单在 click 之前卸载，导致选择丢失
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  const pickFile = (file: File | undefined) => {
    setInstallError(null);
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => {
      try {
        const parsed = JSON.parse(String(reader.result));
        const result = validateThemeFile(parsed);
        setCandidate(result);
        if (!result.ok) return;
      } catch {
        setCandidate({ ok: false, error: t("parseError") });
      }
    };
    reader.readAsText(file);
  };

  const install = async () => {
    if (!candidate?.ok || !candidate.payload || !currentWorkspaceId) return;
    setInstalling(true);
    setInstallError(null);
    try {
      await api.createCustomTheme(currentWorkspaceId, candidate.payload as unknown as Record<string, unknown>);
      await refreshCustomThemes();
      setTheme(candidate.payload.id);
      setCandidate(null);
      setImporting(false);
      if (fileRef.current) fileRef.current.value = "";
    } catch (err) {
      const msg = err instanceof Error ? err.message : "";
      setInstallError(msg.includes("already exists") ? t("conflict") : t("installFailed"));
    } finally {
      setInstalling(false);
    }
  };

  const removeTheme = async (id: string) => {
    if (!currentWorkspaceId) return;
    try {
      await api.deleteCustomTheme(currentWorkspaceId, id);
      const next = customThemes.filter((ct) => ct.id !== id);
      setCustomThemes(next);
      injectCustomThemesCss(next);
      setDeletingId(null);
      if (theme === id) setTheme("dark");
    } catch {
      setInstallError(t("deleteFailed"));
    }
  };

  const nameOfCustom = (ct: { name_zh: string; name_en: string; id: string }) =>
    (locale === "zh-CN" ? ct.name_zh : ct.name_en) || ct.id;

  const active =
    THEMES.find((b) => b.id === theme) ??
    customThemes.find((ct) => ct.id === theme) ??
    THEMES[0];
  const display = mounted ? active : THEMES[0];
  const ActiveIcon = ICONS[(display as { icon: ThemeIcon }).icon] ?? Moon;

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        onClick={() => setOpen(!open)}
        className="w-8 h-8 rounded-lg bg-surface-2 hover:bg-surface-3 flex items-center justify-center transition-colors-fast btn-press"
        title={"name" in display ? display.name[locale] ?? display.name["en-US"] ?? display.id : nameOfCustom(display)}
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
          } w-56 bg-surface-2 border border-line rounded-xl shadow-2xl overflow-hidden z-50 animate-scale-in`}
        >
          <div className="max-h-72 overflow-y-auto">
            {THEMES.map((bt) => {
              const Icon = ICONS[bt.icon];
              const isActive = bt.id === theme;
              return (
                <button
                  key={bt.id}
                  onClick={() => {
                    setTheme(bt.id);
                    setOpen(false);
                  }}
                  className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                    isActive ? "bg-primary/10 text-primary-fg" : "text-fg-muted hover:bg-surface-3"
                  }`}
                >
                  <Icon className="w-3.5 h-3.5 shrink-0" />
                  <span className="flex-1 truncate">{bt.name[locale] ?? bt.name["en-US"] ?? bt.id}</span>
                  <span className="flex items-center gap-0.5 shrink-0">
                    <span className="w-2 h-2 rounded-full border border-line" style={{ backgroundColor: bt.preview.bg }} />
                    <span className="w-2 h-2 rounded-full border border-line" style={{ backgroundColor: bt.preview.surface }} />
                    <span className="w-2 h-2 rounded-full" style={{ backgroundColor: bt.preview.primary }} />
                  </span>
                  {isActive && <Check className="w-3.5 h-3.5 shrink-0" />}
                </button>
              );
            })}
            {customThemes.map((ct) => {
              const Icon = ICONS[ct.icon as ThemeIcon] ?? Sparkles;
              const isActive = ct.id === theme;
              const confirming = deletingId === ct.id;
              return (
                <div
                  key={ct.id}
                  className={`w-full px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                    isActive ? "bg-primary/10 text-primary-fg" : "text-fg-muted hover:bg-surface-3"
                  }`}
                >
                  <button
                    onClick={() => {
                      setTheme(ct.id);
                      setOpen(false);
                    }}
                    className="flex items-center gap-2 flex-1 min-w-0 text-left"
                  >
                    <Icon className="w-3.5 h-3.5 shrink-0" />
                    <span className="flex-1 truncate">{nameOfCustom(ct)}</span>
                    <span className="flex items-center gap-0.5 shrink-0">
                      <span className="w-2 h-2 rounded-full border border-line" style={{ backgroundColor: ct.vars["--bg"] }} />
                      <span className="w-2 h-2 rounded-full border border-line" style={{ backgroundColor: ct.vars["--surface"] }} />
                      <span className="w-2 h-2 rounded-full" style={{ backgroundColor: ct.vars["--primary"] }} />
                    </span>
                    {isActive && <Check className="w-3.5 h-3.5 shrink-0" />}
                  </button>
                  {confirming ? (
                    <span className="flex items-center gap-1 shrink-0">
                      <button onClick={() => removeTheme(ct.id)} className="p-1 rounded text-danger-fg hover:bg-danger/10" title={t("deleteConfirm")}>
                        <Trash2 className="w-3 h-3" />
                      </button>
                      <button onClick={() => setDeletingId(null)} className="p-1 rounded text-fg-subtle hover:bg-surface-3" title={t("cancel")}>
                        <X className="w-3 h-3" />
                      </button>
                    </span>
                  ) : (
                    <button
                      onClick={() => setDeletingId(ct.id)}
                      className="p-1 rounded text-fg-faint hover:text-danger-fg hover:bg-danger/10 shrink-0"
                      title={t("delete")}
                    >
                      <Trash2 className="w-3 h-3" />
                    </button>
                  )}
                </div>
              );
            })}
          </div>

          {/* 导入主题:内联展开,无弹窗 */}
          <div className="border-t border-line">
            {!importing ? (
              <button
                onClick={() => {
                  setImporting(true);
                  setCandidate(null);
                  setInstallError(null);
                }}
                className="w-full text-left px-3 py-2 text-xs flex items-center gap-2 text-fg-subtle hover:bg-surface-3 hover:text-fg-muted transition-colors-fast"
              >
                <Plus className="w-3.5 h-3.5" /> {t("import")}
              </button>
            ) : (
              <div className="px-3 py-2.5 space-y-2">
                <div className="text-[10px] text-fg-faint flex items-center gap-1">
                  <Upload className="w-3 h-3" /> {t("pickFile")}
                </div>
                <input
                  ref={fileRef}
                  type="file"
                  accept=".json,application/json"
                  onChange={(e) => pickFile(e.target.files?.[0])}
                  className="w-full text-[11px] text-fg-subtle file:mr-2 file:px-2 file:py-1 file:text-[11px] file:rounded file:border-0 file:bg-surface-3 file:text-fg-muted"
                />
                {candidate && !candidate.ok && (
                  <p className="text-[10px] text-danger-fg bg-danger/10 border border-danger/20 rounded px-2 py-1.5">{candidate.error}</p>
                )}
                {candidate?.ok && candidate.payload && (
                  <div className="flex items-center gap-2 bg-surface-3/60 border border-line rounded-md px-2 py-1.5">
                    <span className="flex items-center gap-1">
                      <span className="w-3 h-3 rounded-full border border-line" style={{ backgroundColor: candidate.preview?.bg }} />
                      <span className="w-3 h-3 rounded-full border border-line" style={{ backgroundColor: candidate.preview?.surface }} />
                      <span className="w-3 h-3 rounded-full" style={{ backgroundColor: candidate.preview?.primary }} />
                    </span>
                    <span className="text-[11px] text-fg-muted truncate flex-1">
                      {locale === "zh-CN" ? candidate.payload.name_zh : candidate.payload.name_en}
                    </span>
                    <button
                      onClick={install}
                      disabled={installing}
                      className="px-2 py-1 text-[10px] bg-primary-strong hover:bg-primary text-fg rounded disabled:opacity-50"
                    >
                      {installing ? t("installing") : t("install")}
                    </button>
                  </div>
                )}
                {installError && <p className="text-[10px] text-danger-fg">{installError}</p>}
                <button onClick={() => setImporting(false)} className="text-[10px] text-fg-faint hover:text-fg-subtle">
                  {t("cancel")}
                </button>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
