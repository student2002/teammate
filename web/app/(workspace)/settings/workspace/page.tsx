"use client";
// 工作区设置页：工作区信息编辑、所有者查看与危险操作（删除工作区）。
// 内容单列限宽；危险区放页面底部、细边框中性底，danger 只出现在确认按钮上。

import { useState, useEffect } from "react";
import { Save, AlertTriangle, Trash2, Building2, User } from "lucide-react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import InlineConfirm from "@/lib/ui/InlineConfirm";

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
        const members = (await api.listMembers(workspaceId)) as {
          name?: string;
          email?: string;
          workspace_role?: string;
          role?: string;
        }[];
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

  const isDefault = !!(workspace as { is_default?: boolean })?.is_default;

  const handleDelete = async () => {
    try {
      setDeleting(true);
      await api.deleteWorkspace(workspaceId);
      // 从 store 中移除已删除的工作区
      const remaining = workspaces.filter((w) => w.id !== workspaceId);
      setWorkspaces(remaining);
      if (remaining.length > 0) {
        await switchWorkspace(remaining[0].id);
        router.push("/dashboard");
      } else {
        router.push("/login");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("deleteFailed"));
      setDeleting(false);
    }
  };

  return (
    <div className="h-full overflow-y-auto page-enter">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <Button variant="primary" onClick={handleSave}>
            <Save className="w-4 h-4 mr-1.5" />
            {saved ? t("saved") : t("saveSettings")}
          </Button>
        }
      />

      <div className="max-w-[720px] px-6 pb-8 space-y-5">
        {error && (
          <div className="p-3 bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] text-sm text-danger-fg">
            {error}
          </div>
        )}

        {/* 工作区信息 */}
        <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5">
          <div className="flex items-center gap-2 mb-4">
            <Building2 className="w-4 h-4 text-primary-fg" />
            <h2 className="text-sm font-bold text-fg">{t("info")}</h2>
          </div>

          <div className="space-y-4">
            <div>
              <label className="block text-xs font-medium text-fg-muted mb-1">{t("name")}</label>
              <input
                type="text"
                value={draft.name}
                onChange={(e) => handleChange("name", e.target.value)}
                className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-primary"
                placeholder={t("namePlaceholder")}
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-fg-muted mb-1">{t("description")}</label>
              <textarea
                value={draft.description}
                onChange={(e) => handleChange("description", e.target.value)}
                rows={3}
                className="w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 py-2 text-fg text-sm focus:outline-none focus:border-primary resize-none"
                placeholder={t("descriptionPlaceholder")}
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-fg-muted mb-1">{t("issuePrefix")}</label>
              <input
                type="text"
                value={draft.issuePrefix}
                readOnly
                className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg-subtle text-sm cursor-not-allowed"
              />
              <p className="text-[10px] text-fg-faint mt-1">{t("issuePrefixHint")}</p>
            </div>

            <div>
              <label className="block text-xs font-medium text-fg-muted mb-1">{t("owner")}</label>
              <div className="flex items-center gap-2 h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3">
                <User className="w-4 h-4 text-fg-faint shrink-0" />
                <span className="text-fg-muted text-sm">{ownerName || t("loadingOwner")}</span>
              </div>
              <p className="text-[10px] text-fg-faint mt-1">{t("ownerHint")}</p>
            </div>
          </div>
        </div>

        {/* 危险操作区：页面底部，细边框中性底 */}
        <div className="border border-line rounded-[var(--radius-card)] p-5">
          <div className="flex items-center gap-2 mb-3">
            <AlertTriangle className="w-4 h-4 text-danger-fg" />
            <h2 className="text-sm font-bold text-fg">{t("dangerZone")}</h2>
          </div>
          <p className="text-xs text-fg-subtle mb-3.5">{t("dangerHint")}</p>
          <div className="flex items-center justify-between gap-4 py-3.5 px-4 border border-line rounded-[var(--radius-control)] bg-surface-2/30">
            <div className="min-w-0">
              <div className="text-sm font-medium text-fg">{t("deleteWorkspace")}</div>
              <div className="text-[10px] text-fg-faint mt-0.5">
                {isDefault ? t("defaultCannotDelete") : t("deleteWorkspaceHint")}
              </div>
            </div>
            <InlineConfirm
              label={
                <span className="flex items-center gap-1.5">
                  <Trash2 className="w-3.5 h-3.5" />
                  {deleting ? t("deleting") : t("deleteWorkspace")}
                </span>
              }
              confirmLabel={t("deleteWorkspace")}
              cancelLabel={t("deleteCancel")}
              onConfirm={handleDelete}
              busy={deleting}
              disabled={isDefault}
              resetKey={workspaceId}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
