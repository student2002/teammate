"use client";
// 工作区布局：侧边栏导航、全局搜索、通知中心、主题切换与工作区切换。

import { useState, useEffect } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  LayoutDashboard,
  Cpu,
  Workflow,
  Puzzle,
  Shield,
  Settings,
  BarChart3,
  Globe,
  Sun,
  Moon,
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
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import api from "@/lib/api";

const navItems = [
  { id: "dashboard", href: "/dashboard", icon: LayoutDashboard, label: "任务监控" },
  { id: "history", href: "/history", icon: History, label: "历史任务" },
  { id: "workflows", href: "/workflows", icon: Workflow, label: "工作流模板" },
  { id: "agents", href: "/agents", icon: Cpu, label: "AI 代理" },
  { id: "skills", href: "/skills", icon: Puzzle, label: "Skill & MCP" },
  { id: "memory", href: "/memory", icon: Brain, label: "共享记忆" },
  { id: "reviews", href: "/reviews", icon: Shield, label: "审查队列" },
  { id: "settings", href: "/settings/project", icon: Settings, label: "工程设置" },
  { id: "tokens", href: "/tokens", icon: BarChart3, label: "Token 统计" },
  { id: "market", href: "/market", icon: Globe, label: "社区市场" },
];

export default function WorkspaceLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  // 固定初始值避免 SSR 与 hydrate 首帧不一致；真实主题由下方 useEffect 从 localStorage 读取
  const [theme, setTheme] = useState("dark");
  const [showWsOverview, setShowWsOverview] = useState(false);
  const [showWsSwitcher, setShowWsSwitcher] = useState(false);
  const [showCreateWs, setShowCreateWs] = useState(false);
  const [newWsName, setNewWsName] = useState("");
  const [newWsPrefix, setNewWsPrefix] = useState("");
  const [newWsDesc, setNewWsDesc] = useState("");
  const [creatingWs, setCreatingWs] = useState(false);
  const [wsError, setWsError] = useState<string | null>(null);
  const [showNotifications, setShowNotifications] = useState(false);
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

  // 水合后读取持久化的主题，避免不一致
  useEffect(() => {
    const saved = localStorage.getItem("theme");
    if (saved) {
      setTheme(saved);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 尽快将主题同步到 document，防止闪烁
  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
  }, [theme]);

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
          tasks.forEach((t) => results.push({ id: String(t.id), type: "task", title: t.title, subtitle: `任务 #${t.id}` }));
        }
        if (Array.isArray(agents)) {
          agents.forEach((a) => results.push({ id: a.id, type: "agent", title: a.name, subtitle: "AI 代理" }));
        }
        setSearchResults(results);
      } catch {
        setSearchResults([]);
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [searchQuery, currentWs?.id]);

  const toggleTheme = () => {
    setTheme((t) => {
      const next = t === "dark" ? "light" : "dark";
      localStorage.setItem("theme", next);
      return next;
    });
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
      setWsError(err instanceof Error ? err.message : "创建工作区失败");
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
    <div
      data-theme={theme}
      className="flex h-screen bg-slate-950 text-slate-300 font-sans overflow-hidden selection:bg-blue-500/30"
    >
      {/* 侧边栏 */}
      <div className="w-64 bg-slate-900/95 backdrop-blur-sm border-r border-slate-800/80 flex flex-col shrink-0 relative z-20">
        {/* Logo */}
        <div className="h-16 flex items-center px-6 border-b border-slate-800">
          <div className="w-8 h-8 bg-gradient-to-br from-blue-500 to-indigo-600 rounded-lg flex items-center justify-center mr-3 font-bold text-white shadow-lg shadow-blue-500/25 animate-fade-in">
            T
          </div>
          <span className="text-lg font-semibold text-white tracking-wide animate-fade-in">
            Teammate
          </span>
        </div>

        <div className="p-4 flex-1 overflow-y-auto">
          {/* 工作区选择器 */}
          <div className="relative mb-4">
            <button
              onClick={() => setShowWsSwitcher(!showWsSwitcher)}
              className="w-full text-xs font-semibold text-slate-500 uppercase tracking-wider px-2 flex items-center justify-between hover:text-slate-300 transition-colors-fast"
            >
              <span className="truncate">
                工作区 : {(currentWs?.issuePrefix || currentWs?.slug || "LOADING").toUpperCase()}
              </span>
              <ChevronDown className={`w-3 h-3 shrink-0 ml-1 transition-transform duration-200 ${showWsSwitcher ? "rotate-180" : ""}`} />
            </button>

            {showWsSwitcher && (
              <div className="absolute left-0 right-0 top-full mt-1 mx-1 bg-slate-800 border border-slate-700 rounded-xl shadow-xl z-50 overflow-hidden animate-scale-in">
                {workspaces.map((ws) => (
                  <button
                    key={ws.id}
                    onClick={() => handleSwitchWorkspace(ws.id)}
                    className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                      ws.id === currentWs?.id
                        ? "bg-blue-500/10 text-blue-400"
                        : "text-slate-300 hover:bg-slate-700/50"
                    }`}
                  >
                    <Building2 className="w-3.5 h-3.5 shrink-0" />
                    <span className="truncate flex-1">{ws.name}</span>
                    {ws.issuePrefix && (
                      <span className="text-[10px] text-slate-500 font-mono">{ws.issuePrefix}</span>
                    )}
                    {ws.id === currentWs?.id && <Check className="w-3.5 h-3.5 text-blue-400 shrink-0" />}
                  </button>
                ))}
                <div className="border-t border-slate-700">
                  {perm.canManageMembers && (
                    <button
                      onClick={() => {
                        setShowCreateWs(true);
                        setShowWsSwitcher(false);
                      }}
                      className="w-full text-left px-3 py-2 text-xs text-blue-400 hover:bg-slate-700/50 flex items-center gap-2 transition-colors-fast"
                    >
                      <Plus className="w-3.5 h-3.5" />
                      新建工作区
                    </button>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* 工作区详情开关 */}
          <button
            onClick={() => setShowWsOverview(!showWsOverview)}
            className="w-full text-[10px] text-slate-600 mb-4 px-2 flex items-center hover:text-slate-400 transition-colors-fast"
          >
            <ChevronRight
              className={`w-2.5 h-2.5 mr-1 transition-transform duration-200 ${showWsOverview ? "rotate-90" : ""}`}
            />
            工作区详情
          </button>

          {showWsOverview && (
            <div className="mb-4 mx-2 p-3 bg-slate-800/60 border border-slate-700/50 rounded-xl space-y-2 text-xs animate-scale-in">
              <div className="flex justify-between">
                <span className="text-slate-500">工程</span>
                <span className="text-white font-medium">
                  {currentWs?.stats?.projects ?? projects.length}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">AI 代理</span>
                <span className="text-white font-medium">{agents.length}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">进行中任务</span>
                <span className="text-white font-medium">
                  {tasks.filter((t) => t.priorityState === "in_progress").length}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-slate-500">需人工介入</span>
                <span className="text-red-400 font-medium">
                  {tasks.filter((t) => t.priorityState === "manual_intervention").length}
                </span>
              </div>
              <div className="pt-2 border-t border-slate-700/50 space-y-1.5">
                {perm.canManageMembers && (
                  <Link
                    href="/members"
                    prefetch={false}
                    onClick={() => setShowWsOverview(false)}
                    className="w-full text-left text-slate-400 hover:text-white flex items-center py-1 transition-colors-fast"
                  >
                    <Settings className="w-3 h-3 mr-1.5" />
                    成员管理
                  </Link>
                )}
                {perm.isAdmin && (
                  <Link
                    href="/settings/workspace"
                    prefetch={false}
                    onClick={() => setShowWsOverview(false)}
                    className="w-full text-left text-slate-400 hover:text-white flex items-center py-1 transition-colors-fast"
                  >
                    <Settings className="w-3 h-3 mr-1.5" />
                    工作区设置
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
                      ? "bg-blue-500/15 text-blue-400 font-medium shadow-md shadow-blue-500/10 border border-blue-500/30"
                      : "hover:bg-slate-800/60 hover:text-white border border-transparent"
                  }`}
                >
                  {isActive && (
                    <span className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 bg-blue-400 rounded-r-full shadow-sm shadow-blue-400/50" />
                  )}
                  <item.icon className={`w-5 h-5 mr-3 transition-transform-fast group-hover:scale-110 ${isActive ? "text-blue-400" : ""}`} />
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>

        {/* 用户信息与退出登录 */}
        <div className="p-4 border-t border-slate-800 bg-slate-900">
          <div className="flex items-center justify-between">
            <div className="flex items-center min-w-0">
              <button
                onClick={toggleTheme}
                className="w-8 h-8 rounded-lg bg-slate-800 hover:bg-slate-700 flex items-center justify-center mr-3 shrink-0 transition-colors-fast btn-press"
                title={theme === "dark" ? "切换到白天模式" : "切换到夜间模式"}
              >
                {theme === "dark" ? (
                  <Sun className="w-4 h-4 text-amber-400" />
                ) : (
                  <Moon className="w-4 h-4 text-blue-400" />
                )}
              </button>
              <div className="min-w-0">
                <div className="text-sm font-medium text-white truncate">
                  {user?.name || "Admin"}
                </div>
                <div className="text-[10px] text-slate-500 truncate">
                  {perm.isViewer ? "只读" : (user?.role || "Owner")}
                </div>
              </div>
            </div>
            <button
              onClick={() => {
                localStorage.removeItem("token");
                setUser(null);
              }}
              className="p-1.5 text-slate-500 hover:text-red-400 hover:bg-slate-800 rounded transition-colors-fast shrink-0"
              title="退出登录"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>

      {/* 主内容区 */}
      <div className="flex-1 flex flex-col relative overflow-hidden bg-slate-950 z-10">
        {loading && (
          <div className="absolute inset-0 z-50 flex items-center justify-center bg-slate-950/80 backdrop-blur-sm animate-fade-in">
            <div className="flex flex-col items-center gap-3">
              <div className="w-8 h-8 border-2 border-blue-400 border-t-transparent rounded-full animate-spin" />
              <span className="text-sm text-slate-400">加载数据中...</span>
            </div>
          </div>
        )}

        {/* 顶栏 */}
        <div className="h-14 shrink-0 border-b border-slate-800/80 bg-slate-900/60 backdrop-blur-sm flex items-center px-5 gap-3 relative z-30">
          {/* 全局搜索 */}
          <div className="relative flex-1 max-w-md">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500 pointer-events-none" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => {
                  setSearchQuery(e.target.value);
                  setShowSearch(true);
                }}
                onFocus={() => searchQuery.trim() && setShowSearch(true)}
                onBlur={() => setTimeout(() => setShowSearch(false), 200)}
                placeholder="搜索任务、代理..."
                className="w-full bg-slate-800/60 border border-slate-700/50 rounded-lg pl-9 pr-4 py-1.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-blue-500/50 focus:bg-slate-800 transition-colors-fast"
              />
              {searchQuery && (
                <button
                  onClick={() => { setSearchQuery(""); setSearchResults([]); }}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500 hover:text-white transition-colors-fast"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
            {/* 搜索结果下拉框 */}
            {showSearch && searchResults.length > 0 && (
              <div className="absolute left-0 right-0 top-full mt-1 bg-slate-800 border border-slate-700 rounded-xl shadow-2xl overflow-hidden z-50 animate-scale-in">
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
                    className="w-full text-left px-4 py-2.5 flex items-center gap-3 hover:bg-slate-700/50 transition-colors-fast"
                  >
                    <span className={`w-6 h-6 rounded flex items-center justify-center text-xs font-bold shrink-0 ${
                      r.type === "task" ? "bg-blue-500/20 text-blue-400" : "bg-emerald-500/20 text-emerald-400"
                    }`}>
                      {r.type === "task" ? "T" : "A"}
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="text-sm text-white truncate">{r.title}</div>
                      {r.subtitle && <div className="text-[10px] text-slate-500">{r.subtitle}</div>}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="flex-1" />

          {/* 通知铃铛 */}
          <div className="relative">
            <button
              onClick={() => setShowNotifications(!showNotifications)}
              className="relative w-9 h-9 rounded-lg bg-slate-800/60 hover:bg-slate-700 flex items-center justify-center transition-colors-fast btn-press"
              title="通知"
            >
              <Bell className="w-4 h-4 text-slate-400" />
              {notifications.length > 0 && (
                <span className="absolute -top-1 -right-1 w-4 h-4 bg-red-500 rounded-full text-[10px] font-bold text-white flex items-center justify-center animate-scale-in">
                  {notifications.length > 9 ? "9+" : notifications.length}
                </span>
              )}
            </button>
            {/* 通知下拉框 */}
            {showNotifications && (
              <div className="absolute right-0 top-full mt-2 w-80 bg-slate-800 border border-slate-700 rounded-xl shadow-2xl overflow-hidden z-50 animate-scale-in">
                <div className="px-4 py-3 border-b border-slate-700 flex items-center justify-between">
                  <span className="text-sm font-semibold text-white">通知</span>
                  {notifications.length > 0 && (
                    <span className="text-[10px] text-slate-500">{notifications.length} 条未读</span>
                  )}
                </div>
                <div className="max-h-72 overflow-y-auto">
                  {notifications.length === 0 ? (
                    <div className="px-4 py-8 text-center text-sm text-slate-500">暂无通知</div>
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
                        className="w-full text-left px-4 py-3 hover:bg-slate-700/50 transition-colors-fast border-b border-slate-700/50 last:border-b-0"
                      >
                        <div className="flex items-start gap-2">
                          <span className={`w-2 h-2 rounded-full mt-1.5 shrink-0 ${
                            n.type === "manual_intervention" ? "bg-red-400" :
                            n.type === "mention" ? "bg-blue-400" :
                            n.type === "review" ? "bg-amber-400" : "bg-slate-500"
                          }`} />
                          <div className="min-w-0 flex-1">
                            <div className="text-sm text-white truncate">{n.title}</div>
                            {n.description && (
                              <div className="text-xs text-slate-400 mt-0.5 line-clamp-2">{n.description}</div>
                            )}
                            <div className="text-[10px] text-slate-600 mt-1">
                              {new Date(n.created_at).toLocaleString("zh-CN")}
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
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 backdrop-blur-sm modal-overlay"
          onClick={() => setShowCreateWs(false)}
        >
          <div
            className="bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[420px] shadow-2xl modal-content"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-slate-800 flex items-center justify-between">
              <h3 className="text-lg font-bold text-white">新建工作区</h3>
              <button
                onClick={() => setShowCreateWs(false)}
                className="text-slate-500 hover:text-white transition-colors-fast"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="p-5 space-y-4">
              {wsError && (
                <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-sm text-red-400 animate-fade-in">
                  {wsError}
                </div>
              )}
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  工作区名称 <span className="text-red-400">*</span>
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
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500 transition-colors-fast"
                  placeholder="如: Acme Corp"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  ISSUE_PREFIX <span className="text-red-400">*</span>
                </label>
                <input
                  value={newWsPrefix}
                  onChange={(e) => setNewWsPrefix(e.target.value.toUpperCase())}
                  maxLength={10}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white font-mono uppercase focus:outline-none focus:border-blue-500 transition-colors-fast"
                  placeholder="ACME"
                />
                <p className="text-[10px] text-slate-500 mt-1">
                  用于构造任务 ID 前缀（无 PROJECT_KEY 时生效），如 ACME-42
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  描述
                </label>
                <input
                  value={newWsDesc}
                  onChange={(e) => setNewWsDesc(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500 transition-colors-fast"
                  placeholder="可选，工作区描述"
                />
              </div>
            </div>
            <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
              <button
                onClick={() => setShowCreateWs(false)}
                className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
              >
                取消
              </button>
              <button
                onClick={handleCreateWorkspace}
                disabled={!newWsName.trim() || !newWsPrefix.trim() || creatingWs}
                className="px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press transition-colors-fast"
              >
                {creatingWs ? "创建中..." : "创建"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
