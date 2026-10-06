"use client";
// 工作区布局：侧边栏导航、全局搜索、通知中心、主题切换与工作区切换。

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useTranslations, useFormatter, useLocale } from "next-intl";
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
  ChevronRight,
  ChevronDown,
  Check,
  Building2,
  X,
  Bell,
  Search,
  History,
} from "lucide-react";
import ModalPortal from "@/lib/ModalPortal";
import ThemeSwitcher from "@/lib/themes/ThemeSwitcher";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import api from "@/lib/api";

const navItems = [
  { id: "dashboard", href: "/dashboard", icon: LayoutDashboard, labelKey: "dashboard" },
  { id: "history", href: "/history", icon: History, labelKey: "history" },
  { id: "workflows", href: "/workflows", icon: Workflow, labelKey: "workflows" },
  { id: "agents", href: "/agents", icon: Cpu, labelKey: "agents" },
  { id: "skills", href: "/skills", icon: Puzzle, labelKey: "skills" },
  { id: "memory", href: "/memory", icon: Brain, labelKey: "memory" },
  { id: "reviews", href: "/reviews", icon: Shield, labelKey: "reviews" },
  { id: "settings", href: "/settings/project", icon: Settings, labelKey: "settings" },
  { id: "tokens", href: "/tokens", icon: BarChart3, labelKey: "tokens" },
  { id: "market", href: "/market", icon: Globe, labelKey: "market" },
];

export default function WorkspaceLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const t = useTranslations("nav");
  const { dateTime } = useFormatter();
  const locale = useLocale();
  const pathname = usePathname();
  const [showWsOverview, setShowWsOverview] = useState(false);
  const [showWsSwitcher, setShowWsSwitcher] = useState(false);
  const [showCreateWs, setShowCreateWs] = useState(false);
  const [newWsName, setNewWsName] = useState("");
  const [newWsPrefix, setNewWsPrefix] = useState("");
  const [newWsDesc, setNewWsDesc] = useState("");
  const [creatingWs, setCreatingWs] = useState(false);
  const [wsError, setWsError] = useState<string | null>(null);
  const [showNotifications, setShowNotifications] = useState(false);
  const notifAnchorRef = useRef<HTMLDivElement | null>(null);

  // 点击通知浮层外部时关闭它
  useEffect(() => {
    if (!showNotifications) return;
    const onPointerDown = (e: MouseEvent) => {
      if (notifAnchorRef.current && !notifAnchorRef.current.contains(e.target as Node)) {
        setShowNotifications(false);
      }
    };
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [showNotifications]);
  const [notifications, setNotifications] = useState<Array<{id: string; type: string; title: string; description: string; created_at: string; task_id?: number}>>([]);
  const [showSearch, setShowSearch] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<Array<{id: string; type: string; title: string; subtitle?: string}>>([]);

  const user = useAppStore((s) => s.user);
  const setUser = useAppStore((s) => s.setUser);
  const currentWs = useAppStore((s) => s.currentWs);
  const workspaces = useAppStore((s) => s.workspaces);
  const switchWorkspace = useAppStore((s) => s.switchWorkspace);
  const createWorkspace = useAppStore((s) => s.createWorkspace);
  const agents = useAppStore((s) => s.agents);
  const tasks = useAppStore((s) => s.tasks);
  const projects = useAppStore((s) => s.projects);
  const loading = useAppStore((s) => s.loading);
  const loadInitialData = useAppStore((s) => s.loadInitialData);
  const router = useRouter();
  const perm = usePermission();

  useEffect(() => {
    loadInitialData();
  }, [loadInitialData]);

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login");
    }
  }, [loading, user, router]);

  // 定时获取通知
  useEffect(() => {
    if (!currentWs?.id || !user) return;
    const fetchNotifications = async () => {
      try {
        const data = await api.listNotifications(currentWs.id) as Array<{id: string; type: string; title: string; description: string; created_at: string; task_id?: number}>;
        setNotifications(data || []);
      } catch {}
    };
    fetchNotifications();
    const interval = setInterval(fetchNotifications, 30000);
    return () => clearInterval(interval);
  }, [currentWs?.id, user]);

  // 搜索逻辑
  useEffect(() => {
    if (!searchQuery.trim() || !currentWs?.id) {
      setSearchResults([]);
      return;
    }
    const timer = setTimeout(async () => {
      try {
        const [tasks, agents] = await Promise.all([
          api.searchTasks(currentWs.id, searchQuery) as Promise<Array<{id: number; title: string; project_id?: string}>>,
          api.searchAgents(currentWs.id, searchQuery) as Promise<Array<{id: string; name: string}>>,
        ]);
        const results: Array<{id: string; type: string; title: string; subtitle?: string}> = [];
        if (Array.isArray(tasks)) {
          tasks.forEach((task) => results.push({ id: String(task.id), type: "task", title: task.title, subtitle: `${t("searchTaskPrefix")} #${task.id}` }));
        }
        if (Array.isArray(agents)) {
          agents.forEach((a) => results.push({ id: a.id, type: "agent", title: a.name, subtitle: t("searchAgentLabel") }));
        }
        setSearchResults(results);
      } catch {
        setSearchResults([]);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [searchQuery, currentWs?.id]);

  const toggleLocale = () => {
    const next = locale === "zh-CN" ? "en-US" : "zh-CN";
    // 项目未启用 next-intl 中间件：给导航 API 显式传 locale 会被强制加上
    // /en-US 前缀（无对应路由 → 404），因此只写 NEXT_LOCALE cookie 并刷新，
    // 由 i18n/request.ts 按新 cookie 在服务端重渲染
    document.cookie = `NEXT_LOCALE=${next}; path=/; max-age=31536000; samesite=lax`;
    router.refresh();
  };

  const handleCreateWorkspace = async () => {
    if (!newWsName.trim() || !newWsPrefix.trim()) return;
    try {
      setCreatingWs(true);
      setWsError(null);
      await createWorkspace(newWsName.trim(), newWsPrefix.trim().toUpperCase(), newWsDesc.trim());
      setShowCreateWs(false);
      setShowWsSwitcher(false);
      setNewWsName("");
      setNewWsPrefix("");
      setNewWsDesc("");
    } catch (err) {
      setWsError(err instanceof Error ? err.message : t("createFailed"));
    } finally {
      setCreatingWs(false);
    }
  };

  const handleSwitchWorkspace = async (id: string) => {
    try {
      await switchWorkspace(id);
      setShowWsSwitcher(false);
      setShowWsOverview(false);
    } catch (err) {
      console.error("Failed to switch workspace:", err);
    }
  };

  return (
    <div className="flex h-screen bg-bg text-fg-muted font-sans overflow-hidden selection:bg-primary/30">
      {/* 侧边栏 */}
      <div className="w-64 bg-surface/95 backdrop-blur-sm border-r border-line/80 flex flex-col shrink-0 relative z-20">
        {/* Logo */}
        <div className="h-16 flex items-center px-6 border-b border-line">
          <div className="w-8 h-8 bg-gradient-to-br from-primary to-accent-strong rounded-lg flex items-center justify-center mr-3 font-bold text-fg shadow-lg shadow-primary/25 animate-fade-in">
            T
          </div>
          <span className="text-lg font-semibold text-fg tracking-wide animate-fade-in">
            Teammate
          </span>
        </div>

        <div className="p-4 flex-1 overflow-y-auto">
          {/* 工作区选择器 */}
          <div className="relative mb-4">
            <button
              onClick={() => setShowWsSwitcher(!showWsSwitcher)}
              className="w-full text-xs font-semibold text-fg-faint uppercase tracking-wider px-2 flex items-center justify-between hover:text-fg-muted transition-colors-fast"
            >
              <span className="truncate">
                {t("workspace")} : {(currentWs?.issuePrefix || currentWs?.slug || "LOADING").toUpperCase()}
              </span>
              <ChevronDown className={`w-3 h-3 shrink-0 ml-1 transition-transform duration-200 ${showWsSwitcher ? "rotate-180" : ""}`} />
            </button>

            {showWsSwitcher && (
              <div className="absolute left-0 right-0 top-full mt-1 mx-1 bg-surface-2 border border-line-strong rounded-xl shadow-xl z-50 overflow-hidden animate-scale-in">
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
                <div className="border-t border-line-strong">
                  {perm.canManageMembers && (
                    <button
                      onClick={() => {
                        setShowCreateWs(true);
                        setShowWsSwitcher(false);
                      }}
                      className="w-full text-left px-3 py-2 text-xs text-primary-fg hover:bg-surface-3/50 flex items-center gap-2 transition-colors-fast"
                    >
                      <Plus className="w-3.5 h-3.5" />
                      {t("createWorkspace")}
                    </button>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* 工作区详情开关 */}
          <button
            onClick={() => setShowWsOverview(!showWsOverview)}
            className="w-full text-[10px] text-fg-faint mb-4 px-2 flex items-center hover:text-fg-subtle transition-colors-fast"
          >
            <ChevronRight
              className={`w-2.5 h-2.5 mr-1 transition-transform duration-200 ${showWsOverview ? "rotate-90" : ""}`}
            />
            {t("workspaceDetails")}
          </button>

          {showWsOverview && (
            <div className="mb-4 mx-2 p-3 bg-surface-2/60 border border-line-strong/50 rounded-xl space-y-2 text-xs animate-scale-in">
              <div className="flex justify-between">
                <span className="text-fg-faint">{t("projectCount")}</span>
                <span className="text-fg font-medium">
                  {currentWs?.stats?.projects ?? projects.length}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-fg-faint">{t("agentCount")}</span>
                <span className="text-fg font-medium">{agents.length}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-fg-faint">{t("activeTaskCount")}</span>
                <span className="text-fg font-medium">
                  {tasks.filter((t) => t.priorityState === "in_progress").length}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-fg-faint">{t("manualInterventionCount")}</span>
                <span className="text-danger-fg font-medium">
                  {tasks.filter((t) => t.priorityState === "manual_intervention").length}
                </span>
              </div>
              <div className="pt-2 border-t border-line-strong/50 space-y-1.5">
                {perm.canManageMembers && (
                  <Link
                    href="/members"
                    prefetch={false}
                    onClick={() => setShowWsOverview(false)}
                    className="w-full text-left text-fg-subtle hover:text-fg flex items-center py-1 transition-colors-fast"
                  >
                    <Settings className="w-3 h-3 mr-1.5" />
                    {t("memberManage")}
                  </Link>
                )}
                {perm.isAdmin && (
                  <Link
                    href="/settings/workspace"
                    prefetch={false}
                    onClick={() => setShowWsOverview(false)}
                    className="w-full text-left text-fg-subtle hover:text-fg flex items-center py-1 transition-colors-fast"
                  >
                    <Settings className="w-3 h-3 mr-1.5" />
                    {t("workspaceSettings")}
                  </Link>
                )}
              </div>
            </div>
          )}

          {/* 导航 */}
          <nav className="space-y-1">
            {navItems.map((item) => {
              const isActive =
                pathname === item.href ||
                pathname.startsWith(item.href + "/");
              return (
                <Link
                  key={item.id}
                  href={item.href}
                  prefetch={false}
                  className={`w-full flex items-center px-3 py-2.5 rounded-lg transition-all-normal group relative ${
                    isActive
                      ? "bg-primary/15 text-primary-fg font-medium shadow-md shadow-primary/10 border border-primary/30"
                      : "hover:bg-surface-2/60 hover:text-fg border border-transparent"
                  }`}
                >
                  {isActive && (
                    <span className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 bg-primary-fg rounded-r-full shadow-sm shadow-primary-fg/50" />
                  )}
                  <item.icon className={`w-5 h-5 mr-3 transition-transform-fast group-hover:scale-110 ${isActive ? "text-primary-fg" : ""}`} />
                  {t(item.labelKey)}
                </Link>
              );
            })}
          </nav>
        </div>

        {/* 用户信息与退出登录 */}
        <div className="p-4 border-t border-line bg-surface">
          <div className="flex items-center justify-between">
            <div className="flex items-center min-w-0">
              <button
                onClick={toggleLocale}
                className="w-8 h-8 rounded-lg bg-surface-2 hover:bg-surface-3 flex items-center justify-center mr-2 shrink-0 transition-colors-fast btn-press text-[10px] font-bold text-fg-muted"
                title={locale === "zh-CN" ? "Switch to English" : "切换到中文"}
              >
                {locale === "zh-CN" ? "EN" : "中"}
              </button>
              <div className="mr-3">
                <ThemeSwitcher />
              </div>
              <div className="min-w-0">
                <div className="text-sm font-medium text-fg truncate">
                  {user?.name || "Admin"}
                </div>
                <div className="text-[10px] text-fg-faint truncate">
                  {perm.isViewer ? t("readOnly") : (user?.role || "Owner")}
                </div>
              </div>
            </div>
            <button
              onClick={() => {
                localStorage.removeItem("token");
                setUser(null);
              }}
              className="p-1.5 text-fg-faint hover:text-danger-fg hover:bg-surface-2 rounded transition-colors-fast shrink-0"
              title={t("logout")}
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>

      {/* 主内容区 */}
      <div className="flex-1 flex flex-col relative overflow-hidden bg-bg z-10">
        {loading && (
          <div className="absolute inset-0 z-50 flex items-center justify-center bg-bg/80 backdrop-blur-sm animate-fade-in">
            <div className="flex flex-col items-center gap-3">
              <div className="w-8 h-8 border-2 border-primary-fg border-t-transparent rounded-full animate-spin" />
              <span className="text-sm text-fg-subtle">{t("loadingData")}</span>
            </div>
          </div>
        )}

        {/* 顶栏 */}
        <div className="h-14 shrink-0 border-b border-line/80 bg-surface/60 backdrop-blur-sm flex items-center px-5 gap-3 relative z-30">
          {/* 全局搜索 */}
          <div className="relative flex-1 max-w-md">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-fg-faint pointer-events-none" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value);
                  setShowSearch(true);
                }}
                onFocus={() => searchQuery.trim() && setShowSearch(true)}
                onBlur={() => setTimeout(() => setShowSearch(false), 200)}
                placeholder={t("searchPlaceholder")}
                className="w-full bg-surface-2/60 border border-line-strong/50 rounded-lg pl-9 pr-4 py-1.5 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary/50 focus:bg-surface-2 transition-colors-fast"
              />
              {searchQuery && (
                <button
                  onClick={() => { setSearchQuery(""); setSearchResults([]); }}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg transition-colors-fast"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
            {/* 搜索结果下拉框 */}
            {showSearch && searchResults.length > 0 && (
              <div className="absolute left-0 right-0 top-full mt-1 bg-surface-2 border border-line-strong rounded-xl shadow-2xl overflow-hidden z-50 animate-scale-in">
                {searchResults.map((r) => (
                  <button
                    key={`${r.type}-${r.id}`}
                    onClick={() => {
                      if (r.type === "task") {
                        router.push(`/dashboard?task=${r.id}`);
                      } else if (r.type === "agent") {
                        router.push(`/agents?id=${r.id}`);
                      }
                      setShowSearch(false);
                      setSearchQuery("");
                      setSearchResults([]);
                    }}
                    className="w-full text-left px-4 py-2.5 flex items-center gap-3 hover:bg-surface-3/50 transition-colors-fast"
                  >
                    <span className={`w-6 h-6 rounded flex items-center justify-center text-xs font-bold shrink-0 ${
                      r.type === "task" ? "bg-primary/20 text-primary-fg" : "bg-success/20 text-success-fg"
                    }`}>
                      {r.type === "task" ? "T" : "A"}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="text-sm text-fg truncate">{r.title}</div>
                      {r.subtitle && <div className="text-[10px] text-fg-faint">{r.subtitle}</div>}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="flex-1" />

          {/* 通知铃铛 */}
          <div className="relative" ref={notifAnchorRef}>
            <button
              onClick={() => setShowNotifications(!showNotifications)}
              className="relative w-9 h-9 rounded-lg bg-surface-2/60 hover:bg-surface-3 flex items-center justify-center transition-colors-fast btn-press"
              title={t("notifications")}
            >
              <Bell className="w-4 h-4 text-fg-subtle" />
              {notifications.length > 0 && (
                <span className="absolute -top-1 -right-1 w-4 h-4 bg-danger rounded-full text-[10px] font-bold text-fg flex items-center justify-center animate-scale-in">
                  {notifications.length > 9 ? "9+" : notifications.length}
                </span>
              )}
            </button>
            {/* 通知下拉框 */}
            {showNotifications && (
              <div className="absolute right-0 top-full mt-2 w-80 bg-surface-2 border border-line-strong rounded-xl shadow-2xl overflow-hidden z-50 animate-scale-in">
                <div className="px-4 py-3 border-b border-line-strong flex items-center justify-between">
                  <span className="text-sm font-semibold text-fg">{t("notifications")}</span>
                  {notifications.length > 0 && (
                    <span className="text-[10px] text-fg-faint">{t("unreadCount", { count: notifications.length })}</span>
                  )}
                </div>
                <div className="max-h-72 overflow-y-auto">
                  {notifications.length === 0 ? (
                    <div className="px-4 py-8 text-center text-sm text-fg-faint">{t("noNotifications")}</div>
                  ) : (
                    notifications.map((n) => (
                      <button
                        key={n.id}
                        onClick={() => {
                          if (n.task_id) {
                            router.push(`/dashboard?task=${n.task_id}`);
                          }
                          setShowNotifications(false);
                        }}
                        className="w-full text-left px-4 py-3 hover:bg-surface-3/50 transition-colors-fast border-b border-line-strong/50 last:border-b-0"
                      >
                        <div className="flex items-start gap-2">
                          <span className={`w-2 h-2 rounded-full mt-1.5 shrink-0 ${
                            n.type === "manual_intervention" ? "bg-danger-fg" :
                            n.type === "mention" ? "bg-primary-fg" :
                            n.type === "review" ? "bg-warning-fg" : "bg-fg-faint"
                          }`} />
                          <div className="min-w-0 flex-1">
                            <div className="text-sm text-fg truncate">{n.title}</div>
                            {n.description && (
                              <div className="text-xs text-fg-subtle mt-0.5 line-clamp-2">{n.description}</div>
                            )}
                            <div className="text-[10px] text-fg-faint mt-1">
                              {dateTime(new Date(n.created_at), { dateStyle: "medium", timeStyle: "short" })}
                            </div>
                          </div>
                        </div>
                      </button>
                    ))
                  )}
                </div>
              </div>
            )}
          </div>
        </div>

        {/* 页面内容 */}
        <div className="flex-1 overflow-y-auto page-enter">
          {children}
        </div>
      </div>

      {/* 创建工作区弹窗 */}
      {showCreateWs && (
        <ModalPortal>
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center bg-bg/80 backdrop-blur-sm modal-overlay"
          onClick={() => setShowCreateWs(false)}
        >
          <div
            className="bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[420px] shadow-2xl modal-content"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-line flex items-center justify-between">
              <h3 className="text-lg font-bold text-fg">{t("createWorkspaceTitle")}</h3>
              <button
                onClick={() => setShowCreateWs(false)}
                className="text-fg-faint hover:text-fg transition-colors-fast"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="p-5 space-y-4">
              {wsError && (
                <div className="p-3 bg-danger/10 border border-danger/20 rounded-lg text-sm text-danger-fg animate-fade-in">
                  {wsError}
                </div>
              )}
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("workspaceName")} <span className="text-danger-fg">*</span>
                </label>
                <input
                  value={newWsName}
                  onChange={(e) => {
                    setNewWsName(e.target.value);
                    if (!newWsPrefix) {
                      const auto = e.target.value.replace(/[^a-zA-Z]/g, "").substring(0, 6).toUpperCase();
                      setNewWsPrefix(auto);
                    }
                  }}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder={t("workspaceNamePlaceholder")}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  ISSUE_PREFIX <span className="text-danger-fg">*</span>
                </label>
                <input
                  value={newWsPrefix}
                  onChange={(e) => setNewWsPrefix(e.target.value.toUpperCase())}
                  maxLength={10}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg font-mono uppercase focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder="ACME"
                />
                <p className="text-[10px] text-fg-faint mt-1">
                  {t("issuePrefixHint")}
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("description")}
                </label>
                <input
                  value={newWsDesc}
                  onChange={(e) => setNewWsDesc(e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder={t("descriptionPlaceholder")}
                />
              </div>
            </div>
            <div className="p-5 border-t border-line flex justify-end gap-3">
              <button
                onClick={() => setShowCreateWs(false)}
                className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
              >
                {t("cancel")}
              </button>
              <button
                onClick={handleCreateWorkspace}
                disabled={!newWsName.trim() || !newWsPrefix.trim() || creatingWs}
                className="px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press transition-colors-fast"
              >
                {creatingWs ? t("creating") : t("create")}
              </button>
            </div>
          </div>
        </div>
        </ModalPortal>
      )}
    </div>
  );
}
