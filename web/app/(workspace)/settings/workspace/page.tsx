"use client";
// 工作区设置页：工作区信息编辑、所有者查看与危险操作（删除工作区）。

import { useState, useEffect } from "react";
import { Settings, Save, AlertTriangle, Trash2, Building2, User } from "lucide-react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";

interface WorkspaceDraft {
  name: string;
  description: string;
  issuePrefix: string;
}

export default function WorkspaceSettingsPage() {
  const t = useTranslations("settings.workspace");
  const router = useRouter();
  const workspace = useAppStore((s) => s.currentWs);
  const workspaceId = useAppStore((s) => s.currentWorkspaceId) || "";
  const workspaces = useAppStore((s) => s.workspaces);
  const switchWorkspace = useAppStore((s) => s.switchWorkspace);
  const setWorkspaces = useAppStore((s) => s.setWorkspaces);
  const [deleting, setDeleting] = useState(false);
  const [ownerName, setOwnerName] = useState("");

  const [draft, setDraft] = useState<WorkspaceDraft>({
    name: workspace?.name || "",
    description: workspace?.description || "",
    issuePrefix: (workspace as { issue_prefix?: string })?.issue_prefix || "",
  });
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 工作区数据加载时同步草稿（如页面刷新后）
  useEffect(() => {
    if (workspace) {
      setDraft({
        name: workspace.name || "",
        description: workspace.description || "",
        issuePrefix: (workspace as { issue_prefix?: string })?.issue_prefix || "",
      });
    }
  }, [workspace]);

  // 从成员列表中获取所有者
  useEffect(() => {
    if (!workspaceId) return;
    (async () => {
      try {
        const members = (await api.listMembers(workspaceId)) as { name?: string; email?: string; workspace_role?: string; role?: string }[];
        const owner = (members || []).find((m) => m.workspace_role === "owner" || m.role === "owner");
        if (owner) setOwnerName(owner.name || owner.email || "");
      } catch {
        // 忽略
      }
    })();
  }, [workspaceId]);

  const handleSave = async () => {
    try {
      setError(null);
      await api.updateWorkspace(workspaceId, {
        name: draft.name,
        description: draft.description,
      });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("saveFailed"));
    }
  };

  const handleChange = (field: keyof WorkspaceDraft, value: string) => {
    setDraft((prev) => ({ ...prev, [field]: value }));
    setSaved(false);
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto">
      {/* 页头 */}
      <div className="flex items-center justify-between mb-8">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-accent/10 border border-accent/20 flex items-center justify-center">
            <Settings className="w-5 h-5 text-accent-fg" />
          </div>
          <div>
            <h1 className="text-xl font-bold text-fg">{t("title")}</h1>
            <p className="text-xs text-fg-subtle mt-0.5">
              {t("subtitle")}
            </p>
          </div>
        </div>
        <button
          onClick={handleSave}
          className={`flex items-center px-5 py-2.5 rounded-xl text-sm font-medium transition-all ${
            saved
              ? "bg-success-strong text-fg"
              : "bg-primary-strong hover:bg-primary text-fg"
          }`}
        >
          <Save className="w-4 h-4 mr-2" />
          {saved ? t("saved") : t("saveSettings")}
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-danger/10 border border-danger/20 rounded-lg text-sm text-danger-fg">
          {error}
        </div>
      )}

      {/* 工作区信息 */}
      <div className="bg-surface-2/40 backdrop-blur-sm border border-line-strong/60 rounded-xl p-5 mb-6">
        <div className="flex items-center gap-2 mb-5">
          <Building2 className="w-4 h-4 text-primary-fg" />
          <h2 className="text-sm font-bold text-fg">{t("info")}</h2>
        </div>

        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("name")}
            </label>
            <input
              type="text"
              value={draft.name}
              onChange={(e) => handleChange("name", e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
              placeholder={t("namePlaceholder")}
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("description")}
            </label>
            <textarea
              value={draft.description}
              onChange={(e) => handleChange("description", e.target.value)}
              rows={3}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary resize-none"
              placeholder={t("descriptionPlaceholder")}
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("issuePrefix")}
            </label>
            <input
              type="text"
              value={draft.issuePrefix}
              readOnly
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg-subtle cursor-not-allowed"
            />
            <p className="text-[10px] text-fg-faint mt-1.5">
              {t("issuePrefixHint")}
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium text-fg-muted mb-1.5">
              {t("owner")}
            </label>
            <div className="flex items-center gap-2 bg-bg border border-line-strong rounded-lg px-4 py-2.5">
              <User className="w-4 h-4 text-fg-faint shrink-0" />
              <span className="text-fg-muted">{ownerName || t("loadingOwner")}</span>
            </div>
            <p className="text-[10px] text-fg-faint mt-1.5">
              {t("ownerHint")}
            </p>
          </div>
        </div>
      </div>

      {/* 危险操作区 */}
      <div className="bg-danger/5 backdrop-blur-sm border border-danger/20 rounded-xl p-5">
        <div className="flex items-center gap-2 mb-4">
          <AlertTriangle className="w-4 h-4 text-danger-fg" />
          <h2 className="text-sm font-bold text-danger-fg">{t("dangerZone")}</h2>
        </div>
        <p className="text-xs text-fg-subtle mb-4">
          {t("dangerHint")}
        </p>
        <div className="flex items-center justify-between p-4 bg-surface-2/30 border border-line-strong/50 rounded-lg">
          <div>
            <div className="text-sm font-medium text-fg">{t("deleteWorkspace")}</div>
            <div className="text-[10px] text-fg-faint mt-0.5">
              {(workspace as { is_default?: boolean })?.is_default
                ? t("defaultCannotDelete")
                : t("deleteWorkspaceHint")}
            </div>
          </div>
          <div>
            <button
              onClick={async () => {
                if (!confirm(t("deleteConfirm"))) return;
                try {
                  setDeleting(true);
                  await api.deleteWorkspace(workspaceId);
                  // 从 store 中移除已删除的工作区
                  const remaining = workspaces.filter((w) => w.id !== workspaceId);
                  setWorkspaces(remaining);
                  if (remaining.length > 0) {
                    // 切换到其他工作区
                    await switchWorkspace(remaining[0].id);
                    router.push("/dashboard");
                  } else {
                    // 没有剩余工作区，跳转登录页
                    router.push("/login");
                  }
                } catch (err) {
                  setError(err instanceof Error ? err.message : t("deleteFailed"));
                  setDeleting(false);
                }
              }}
              disabled={deleting || !!(workspace as { is_default?: boolean })?.is_default}
              className={`flex items-center px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                (workspace as { is_default?: boolean })?.is_default
                  ? "bg-danger-strong/10 text-danger-fg/30 cursor-not-allowed"
                  : deleting
                    ? "bg-danger-strong/20 text-danger-fg/50 cursor-wait"
                    : "bg-danger-strong/20 text-danger-fg hover:bg-danger-strong/30 hover:text-danger-fg"
              }`}
            >
              <Trash2 className="w-4 h-4 mr-2" />
              {deleting ? t("deleting") : t("deleteWorkspace")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
