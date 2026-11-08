"use client";
// 工作区布局：WorkspaceSidebar + ContextBar + 页面内容。
// 数据初始化与登录守卫在此层；创建工作区为 Context Bar 下方的内联表单区（无遮罩弹窗）。

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import WorkspaceSidebar from "./WorkspaceSidebar";
import ContextBar from "./ContextBar";
import Button from "@/lib/ui/Button";
import { useAppStore } from "@/lib/store";

export default function WorkspaceLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const t = useTranslations("nav");
  const router = useRouter();

  const user = useAppStore((s) => s.user);
  const loading = useAppStore((s) => s.loading);
  const loadInitialData = useAppStore((s) => s.loadInitialData);
  const createWorkspace = useAppStore((s) => s.createWorkspace);

  const [showCreateWs, setShowCreateWs] = useState(false);
  const [newWsName, setNewWsName] = useState("");
  const [newWsPrefix, setNewWsPrefix] = useState("");
  const [newWsDesc, setNewWsDesc] = useState("");
  const [creatingWs, setCreatingWs] = useState(false);
  const [wsError, setWsError] = useState<string | null>(null);

  useEffect(() => {
    loadInitialData();
  }, [loadInitialData]);

  useEffect(() => {
    if (!loading && !user) {
      router.push("/login");
    }
  }, [loading, user, router]);

  const openCreateWorkspace = () => {
    setNewWsName("");
    setNewWsPrefix("");
    setNewWsDesc("");
    setWsError(null);
    setShowCreateWs(true);
  };

  const closeCreateWorkspace = () => {
    setShowCreateWs(false);
    setWsError(null);
  };

  const handleCreateWorkspace = async () => {
    if (!newWsName.trim() || !newWsPrefix.trim()) return;
    try {
      setCreatingWs(true);
      setWsError(null);
      await createWorkspace(newWsName.trim(), newWsPrefix.trim().toUpperCase(), newWsDesc.trim());
      closeCreateWorkspace();
    } catch (err) {
      setWsError(err instanceof Error ? err.message : t("createFailed"));
    } finally {
      setCreatingWs(false);
    }
  };

  return (
    <div className="flex h-screen bg-bg text-fg-muted font-sans overflow-hidden selection:bg-primary/30">
      <WorkspaceSidebar onCreateWorkspace={openCreateWorkspace} />

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

        <ContextBar />

        {/* 创建工作区：Context Bar 下方的内联表单区，文档流推开页面内容 */}
        {showCreateWs && (
          <div className="shrink-0 border-b border-line bg-surface-2/40 px-5 py-3 animate-fade-in">
            {wsError && (
              <div className="mb-2.5 px-3 py-2 bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] text-sm text-danger-fg">
                {wsError}
              </div>
            )}
            <div className="flex flex-wrap items-end gap-3">
              <div>
                <label className="block text-xs font-medium text-fg-muted mb-1">
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
                  className="h-[34px] w-48 bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder={t("workspaceNamePlaceholder")}
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-fg-muted mb-1">
                  ISSUE_PREFIX <span className="text-danger-fg">*</span>
                </label>
                <input
                  value={newWsPrefix}
                  onChange={(e) => setNewWsPrefix(e.target.value.toUpperCase())}
                  maxLength={10}
                  className="h-[34px] w-28 bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg font-mono uppercase focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder="ACME"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-fg-muted mb-1">
                  {t("description")}
                </label>
                <input
                  value={newWsDesc}
                  onChange={(e) => setNewWsDesc(e.target.value)}
                  className="h-[34px] w-56 bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-sm text-fg focus:outline-none focus:border-primary transition-colors-fast"
                  placeholder={t("descriptionPlaceholder")}
                />
              </div>
              <Button variant="primary" loading={creatingWs} onClick={handleCreateWorkspace} disabled={!newWsName.trim() || !newWsPrefix.trim()}>
                {creatingWs ? t("creating") : t("create")}
              </Button>
              <Button variant="ghost" onClick={closeCreateWorkspace}>
                {t("cancel")}
              </Button>
            </div>
            <p className="mt-2 text-[11px] text-fg-faint">{t("issuePrefixHint")}</p>
          </div>
        )}

        {/* 页面内容：页面自己管理滚动 */}
        <div className="flex-1 overflow-y-auto page-enter">
          {children}
        </div>
      </div>
    </div>
  );
}
