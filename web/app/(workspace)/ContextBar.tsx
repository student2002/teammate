"use client";
// Context Bar：当前项目切换、全局搜索、在线 Agent 数、通知入口。
// 项目切换只写 store 的 currentProjectId，各页面（dashboard/reviews/history）自行响应
// 重新加载——与页面内既有筛选器的数据语义一致。任务列表加载失败但保留了上次数据时，
// 在条下显示紧凑 warning 提示。

import { useState, useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { useTranslations, useFormatter } from "next-intl";
import { Folder, ChevronDown, Search, X, Bell, Bot, AlertTriangle } from "lucide-react";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";

type NotificationItem = { id: string; type: string; title: string; description: string; created_at: string; task_id?: number };

export default function ContextBar() {
  const t = useTranslations("nav");
  const { dateTime } = useFormatter();
  const router = useRouter();

  const user = useAppStore((s) => s.user);
  const currentWs = useAppStore((s) => s.currentWs);
  const projects = useAppStore((s) => s.projects);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const setCurrentProjectId = useAppStore((s) => s.setCurrentProjectId);
  const agents = useAppStore((s) => s.agents);
  const loadError = useAppStore((s) => s.loadError);
  const loadProjectData = useAppStore((s) => s.loadProjectData);

  const [showProjectMenu, setShowProjectMenu] = useState(false);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [showNotifications, setShowNotifications] = useState(false);
  const notifAnchorRef = useRef<HTMLDivElement | null>(null);
  const projectAnchorRef = useRef<HTMLDivElement | null>(null);

  const [showSearch, setShowSearch] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchResults, setSearchResults] = useState<Array<{ id: string; type: string; title: string; subtitle?: string }>>([]);

  const currentProject = projects.find((p) => p.id === currentProjectId);
  const onlineAgents = agents.filter((a) => a.status === "online" || a.status === "busy").length;

  // 定时获取通知
  useEffect(() => {
    if (!currentWs?.id || !user) return;
    const fetchNotifications = async () => {
      try {
        const data = await api.listNotifications(currentWs.id) as NotificationItem[];
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
          api.searchTasks(currentWs.id, searchQuery) as Promise<Array<{ id: number; title: string; project_id?: string }>>,
          api.searchAgents(currentWs.id, searchQuery) as Promise<Array<{ id: string; name: string }>>,
        ]);
        const results: Array<{ id: string; type: string; title: string; subtitle?: string }> = [];
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
  }, [searchQuery, currentWs?.id, t]);

  // 点击浮层外部或按 Escape 关闭
  useEffect(() => {
    if (!showNotifications && !showProjectMenu) return;
    const onPointerDown = (e: MouseEvent) => {
      if (notifAnchorRef.current && !notifAnchorRef.current.contains(e.target as Node)) {
        setShowNotifications(false);
      }
      if (projectAnchorRef.current && !projectAnchorRef.current.contains(e.target as Node)) {
        setShowProjectMenu(false);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setShowNotifications(false);
        setShowProjectMenu(false);
      }
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [showNotifications, showProjectMenu]);

  const selectProject = (id: string) => {
    setCurrentProjectId(id || null);
    setShowProjectMenu(false);
  };

  const retryLoadTasks = () => {
    if (currentProjectId) loadProjectData(currentProjectId);
  };

  return (
    <>
      <div className="h-14 shrink-0 border-b border-line bg-surface/80 backdrop-blur-sm flex items-center px-5 gap-3 relative z-30">
        {/* 当前项目 */}
        <div className="relative shrink-0" ref={projectAnchorRef}>
          <button
            onClick={() => setShowProjectMenu(!showProjectMenu)}
            className="h-8 px-2.5 rounded-[var(--radius-control)] border border-line bg-surface-2/60 flex items-center gap-1.5 hover:border-line-strong transition-colors-fast"
            title={t("projectSelector")}
          >
            <Folder className="w-3.5 h-3.5 text-fg-faint" />
            <span className="text-xs text-fg-muted max-w-[140px] truncate">
              {currentProject?.name || t("selectProject")}
            </span>
            <ChevronDown className={`w-3 h-3 text-fg-faint transition-transform duration-200 ${showProjectMenu ? "rotate-180" : ""}`} />
          </button>
          {showProjectMenu && (
            <div className="absolute left-0 top-full mt-1 min-w-[200px] bg-surface-2 border border-line-strong rounded-[var(--radius-panel)] shadow-xl z-50 overflow-hidden animate-scale-in">
              {projects.length === 0 ? (
                <div className="px-3 py-2.5 text-xs text-fg-faint">{t("noProjects")}</div>
              ) : (
                projects.map((p) => (
                  <button
                    key={p.id}
                    onClick={() => selectProject(p.id)}
                    className={`w-full text-left px-3 py-2 text-xs flex items-center gap-2 transition-colors-fast ${
                      p.id === currentProjectId
                        ? "bg-primary/10 text-primary-fg"
                        : "text-fg-muted hover:bg-surface-3/50"
                    }`}
                  >
                    <Folder className="w-3.5 h-3.5 shrink-0" />
                    <span className="truncate flex-1">{p.name}</span>
                  </button>
                ))
              )}
            </div>
          )}
        </div>

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
              className="w-full h-8 bg-surface-2/60 border border-line-strong/50 rounded-[var(--radius-control)] pl-9 pr-8 text-sm text-fg placeholder-fg-faint focus:outline-none focus:border-primary/50 focus:bg-surface-2 transition-colors-fast"
            />
            {searchQuery && (
              <button
                onClick={() => { setSearchQuery(""); setSearchResults([]); }}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg transition-colors-fast"
                title={t("cancel")}
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
          {showSearch && searchResults.length > 0 && (
            <div className="absolute left-0 right-0 top-full mt-1 bg-surface-2 border border-line-strong rounded-[var(--radius-panel)] shadow-xl overflow-hidden z-50 animate-scale-in">
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

        {/* 在线 Agent */}
        <div
          className="hidden md:flex items-center gap-1.5 text-xs text-fg-subtle shrink-0"
          title={t("onlineAgentsCount", { count: onlineAgents })}
        >
          <Bot className="w-4 h-4 text-fg-faint" />
          <span className="font-mono">{onlineAgents}</span>
          <span className="text-fg-faint">{t("onlineLabel")}</span>
        </div>

        {/* 通知 */}
        <div className="relative shrink-0" ref={notifAnchorRef}>
          <button
            onClick={() => setShowNotifications(!showNotifications)}
            className="relative w-9 h-9 rounded-[var(--radius-control)] bg-surface-2/60 hover:bg-surface-3 flex items-center justify-center transition-colors-fast btn-press"
            title={t("notifications")}
          >
            <Bell className="w-4 h-4 text-fg-subtle" />
            {notifications.length > 0 && (
              <span className="absolute -top-1 -right-1 w-4 h-4 bg-danger rounded-full text-[10px] font-bold text-fg flex items-center justify-center animate-scale-in">
                {notifications.length > 9 ? "9+" : notifications.length}
              </span>
            )}
          </button>
          {showNotifications && (
            <div className="absolute right-0 top-full mt-2 w-80 bg-surface-2 border border-line-strong rounded-[var(--radius-panel)] shadow-xl overflow-hidden z-50 animate-scale-in">
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

      {/* 任务数据加载失败（保留 last-good 数据）的紧凑提示条 */}
      {loadError && (
        <div className="shrink-0 px-5 py-1.5 bg-warning/10 border-b border-warning/20 flex items-center gap-2 text-xs text-warning-fg animate-fade-in">
          <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
          <span className="flex-1">{t("loadFailedStale")}</span>
          <button
            onClick={retryLoadTasks}
            className="underline underline-offset-2 hover:text-fg transition-colors-fast"
          >
            {t("retry")}
          </button>
        </div>
      )}
    </>
  );
}
