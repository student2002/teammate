"use client";
// 侧边栏：品牌标识、工作区切换、分组导航（工作/编排/洞察/管理）、底部用户区。
// 展开 224px、收起 64px（仅图标 + title 提示）；收纳状态持久化，1280px 以下首次进入默认收起。

import { useState, useEffect } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useLocale, useTranslations } from "next-intl";
import {
  LayoutDashboard,
  Cpu,
  Workflow,
  Puzzle,
  Shield,
  Settings,
  BarChart3,
  Globe,
  LogOut,
  Plus,
  Brain,
  Check,
  Building2,
  Users,
  History,
  PanelLeftClose,
  PanelLeftOpen,
  ChevronDown,
  type LucideIcon,
} from "lucide-react";
import ThemeSwitcher from "@/lib/themes/ThemeSwitcher";
import BrandMark from "@/lib/product/BrandMark";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";

interface NavItem {
  href: string;
  icon: LucideIcon;
  labelKey: string;
  perm?: "canManageMembers" | "isAdmin";
}

const navGroups: Array<{ key: string; items: NavItem[] }> = [
  {
    key: "groupWork",
    items: [
      { href: "/dashboard", icon: LayoutDashboard, labelKey: "dashboard" },
      { href: "/history", icon: History, labelKey: "history" },
      { href: "/reviews", icon: Shield, labelKey: "reviews" },
    ],
  },
  {
    key: "groupOrchestration",
    items: [
      { href: "/workflows", icon: Workflow, labelKey: "workflows" },
      { href: "/agents", icon: Cpu, labelKey: "agents" },
      { href: "/skills", icon: Puzzle, labelKey: "skills" },
      { href: "/memory", icon: Brain, labelKey: "memory" },
    ],
  },
  {
    key: "groupInsight",
    items: [
      { href: "/tokens", icon: BarChart3, labelKey: "tokens" },
      { href: "/market", icon: Globe, labelKey: "market" },
    ],
  },
  {
    key: "groupManage",
    items: [
      { href: "/settings/project", icon: Settings, labelKey: "settings" },
      { href: "/members", icon: Users, labelKey: "memberManage", perm: "canManageMembers" },
      { href: "/settings/workspace", icon: Building2, labelKey: "workspaceSettings", perm: "isAdmin" },
    ],
  },
];

export default function WorkspaceSidebar({
  onCreateWorkspace,
}: {
  onCreateWorkspace: () => void;
}) {
  const t = useTranslations("nav");
  const pathname = usePathname();
  const router = useRouter();
  const locale = useLocale();
  const perm = usePermission();

  const user = useAppStore((s) => s.user);
  const setUser = useAppStore((s) => s.setUser);
  const currentWs = useAppStore((s) => s.currentWs);
  const workspaces = useAppStore((s) => s.workspaces);
  const switchWorkspace = useAppStore((s) => s.switchWorkspace);

  const [collapsed, setCollapsed] = useState(false);
  const [ready, setReady] = useState(false);
  const [showWsSwitcher, setShowWsSwitcher] = useState(false);

  // Escape 关闭工作区下拉（§11.3 键盘验收）
  useEffect(() => {
    if (!showWsSwitcher) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setShowWsSwitcher(false);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [showWsSwitcher]);

  // 收纳状态：首帧按展开渲染避免 SSR 水合不一致，挂载后恢复；无持久化记录时
  // 1280px 以下默认收起。过渡类延迟两层 rAF 再挂，防止刷新时重放收起动画
  useEffect(() => {
    const saved = localStorage.getItem("sidebar.collapsed");
    setCollapsed(saved === null ? window.innerWidth < 1280 : saved === "1");
    let raf2 = 0;
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => setReady(true));
    });
    return () => {
      cancelAnimationFrame(raf1);
      cancelAnimationFrame(raf2);
    };
  }, []);

  const toggleCollapsed = () => {
    const next = !collapsed;
    setCollapsed(next);
    localStorage.setItem("sidebar.collapsed", next ? "1" : "0");
    if (next) setShowWsSwitcher(false);
  };

  // 项目未启用 next-intl 中间件：只写 NEXT_LOCALE cookie 并刷新，由 i18n/request.ts
  // 按新 cookie 在服务端重渲染（显式传 locale 会被强制加 /en-US 前缀导致 404）
  const toggleLocale = () => {
    const next = locale === "zh-CN" ? "en-US" : "zh-CN";
    document.cookie = `NEXT_LOCALE=${next}; path=/; max-age=31536000; samesite=lax`;
    router.refresh();
  };

  const handleLogout = () => {
    localStorage.removeItem("token");
    setUser(null);
  };

  const handleSwitchWorkspace = async (id: string) => {
    try {
      await switchWorkspace(id);
      setShowWsSwitcher(false);
    } catch (err) {
      console.error("Failed to switch workspace:", err);
    }
  };

  const isActive = (href: string) =>
    pathname === href || pathname.startsWith(href + "/");

  const visibleGroups = navGroups
    .map((g) => ({
      ...g,
      items: g.items.filter((item) => !item.perm || perm[item.perm]),
    }))
    .filter((g) => g.items.length > 0);

  return (
    <div
      className={`${collapsed ? "w-16" : "w-56"} bg-surface border-r border-line flex flex-col shrink-0 relative z-20 ${ready ? "transition-all-normal" : ""}`}
    >
      {/* 品牌 + 收纳开关 */}
      <div className={`h-14 shrink-0 flex items-center border-b border-line ${collapsed ? "px-2 justify-center" : "px-4"}`}>
        <div className="w-8 h-8 rounded-[var(--radius-control)] bg-gradient-to-br from-primary to-accent-strong flex items-center justify-center text-fg shrink-0">
          <BrandMark size={18} />
        </div>
        {!collapsed && (
          <>
            <span className="ml-2.5 text-[15px] font-semibold text-fg tracking-wide truncate">
              Teammate
            </span>
            <button
              onClick={toggleCollapsed}
              className="ml-auto p-1.5 rounded-[var(--radius-control)] text-fg-faint hover:text-fg hover:bg-surface-2 transition-colors-fast"
              title={t("collapseSidebar")}
            >
              <PanelLeftClose className="w-4 h-4" />
            </button>
          </>
        )}
      </div>
      {collapsed && (
        <button
          onClick={toggleCollapsed}
          className="mx-auto mt-2 p-1.5 rounded-[var(--radius-control)] text-fg-faint hover:text-fg hover:bg-surface-2 transition-colors-fast"
          title={t("expandSidebar")}
        >
          <PanelLeftOpen className="w-4 h-4" />
        </button>
      )}

      {/* 工作区切换（纯文字内容，收起态隐藏） */}
      {!collapsed && (
        <div className="relative px-3 pt-3 shrink-0">
          <button
            onClick={() => setShowWsSwitcher(!showWsSwitcher)}
            className="w-full h-8 px-2 rounded-[var(--radius-control)] border border-line bg-surface-2/60 flex items-center justify-between gap-2 hover:border-line-strong transition-colors-fast"
          >
            <span className="text-xs text-fg-muted truncate">
              {t("workspace")} : {(currentWs?.issuePrefix || currentWs?.slug || "LOADING").toUpperCase()}
            </span>
            <ChevronDown className={`w-3 h-3 shrink-0 text-fg-faint transition-transform duration-200 ${showWsSwitcher ? "rotate-180" : ""}`} />
          </button>

          {showWsSwitcher && (
            <div className="absolute left-3 right-3 top-full mt-1 bg-surface-2 border border-line-strong rounded-[var(--radius-panel)] shadow-xl z-50 overflow-hidden animate-scale-in">
              {workspaces.map((ws) => (
                <button
                  key={ws.id}
                  onClick={() => handleSwitchWorkspace(ws.id)}
                  className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                    ws.id === currentWs?.id
                      ? "bg-primary/10 text-primary-fg"
                      : "text-fg-muted hover:bg-surface-3/50"
                  }`}
                >
                  <Building2 className="w-3.5 h-3.5 shrink-0" />
                  <span className="truncate flex-1">{ws.name}</span>
                  {ws.issuePrefix && (
                    <span className="text-[10px] text-fg-faint font-mono">{ws.issuePrefix}</span>
                  )}
                  {ws.id === currentWs?.id && <Check className="w-3.5 h-3.5 text-primary-fg shrink-0" />}
                </button>
              ))}
              {perm.canManageMembers && (
                <div className="border-t border-line-strong">
                  <button
                    onClick={() => {
                      setShowWsSwitcher(false);
                      onCreateWorkspace();
                    }}
                    className="w-full text-left px-3 py-2 text-xs text-primary-fg hover:bg-surface-3/50 flex items-center gap-2 transition-colors-fast"
                  >
                    <Plus className="w-3.5 h-3.5" />
                    {t("createWorkspace")}
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* 分组导航 */}
      <nav className={`flex-1 overflow-y-auto ${collapsed ? "px-2 py-3" : "px-3 py-3"}`}>
        {visibleGroups.map((group, gi) => (
          <div key={group.key} className={gi > 0 ? (collapsed ? "mt-3 pt-3 border-t border-line" : "mt-4") : ""}>
            {!collapsed && (
              <div className="px-2 mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-fg-faint">
                {t(group.key)}
              </div>
            )}
            <div className="space-y-0.5">
              {group.items.map((item) => {
                const active = isActive(item.href);
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    prefetch={false}
                    title={collapsed ? t(item.labelKey) : undefined}
                    className={`relative w-full flex items-center rounded-[var(--radius-control)] transition-colors-fast group ${
                      collapsed ? "justify-center h-9" : "px-2.5 h-9"
                    } ${
                      active
                        ? "bg-primary/10 text-primary-fg font-medium"
                        : "text-fg-subtle hover:text-fg hover:bg-surface-2/60"
                    }`}
                  >
                    {active && (
                      <span className="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-4 bg-primary-fg rounded-r-full" />
                    )}
                    <item.icon className={`w-4 h-4 shrink-0 ${collapsed ? "" : "mr-2.5"}`} />
                    {!collapsed && <span className="truncate">{t(item.labelKey)}</span>}
                  </Link>
                );
              })}
            </div>
          </div>
        ))}
      </nav>

      {/* 底部：语言 / 主题 / 用户 / 退出 */}
      <div className={`shrink-0 border-t border-line ${collapsed ? "px-2 py-3" : "px-3 py-3"}`}>
        {collapsed ? (
          <div className="flex flex-col items-center gap-2">
            <button
              onClick={toggleLocale}
              className="w-8 h-8 rounded-[var(--radius-control)] bg-surface-2 hover:bg-surface-3 flex items-center justify-center shrink-0 transition-colors-fast btn-press text-[10px] font-bold text-fg-muted"
              title={locale === "zh-CN" ? "Switch to English" : "切换到中文"}
            >
              {locale === "zh-CN" ? "EN" : "中"}
            </button>
            <ThemeSwitcher />
            <button
              onClick={handleLogout}
              className="p-1.5 text-fg-faint hover:text-danger-fg hover:bg-surface-2 rounded transition-colors-fast shrink-0"
              title={t("logout")}
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        ) : (
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center min-w-0">
              <button
                onClick={toggleLocale}
                className="w-8 h-8 rounded-[var(--radius-control)] bg-surface-2 hover:bg-surface-3 flex items-center justify-center mr-1.5 shrink-0 transition-colors-fast btn-press text-[10px] font-bold text-fg-muted"
                title={locale === "zh-CN" ? "Switch to English" : "切换到中文"}
              >
                {locale === "zh-CN" ? "EN" : "中"}
              </button>
              <ThemeSwitcher />
              <div className="ml-2.5 min-w-0">
                <div className="text-sm font-medium text-fg truncate leading-tight">
                  {user?.name || "Admin"}
                </div>
                <div className="text-[10px] text-fg-faint truncate leading-tight">
                  {perm.isViewer ? t("readOnly") : (user?.role || "Owner")}
                </div>
              </div>
            </div>
            <button
              onClick={handleLogout}
              className="p-1.5 text-fg-faint hover:text-danger-fg hover:bg-surface-2 rounded transition-colors-fast shrink-0"
              title={t("logout")}
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
