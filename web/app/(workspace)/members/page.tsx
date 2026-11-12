"use client";
// 工作区成员页：邀请成员、角色管理与移除成员。邀请在页头下方内联展开；
// 角色修改在该成员行下方原位展开；移除用原位二次确认；错误一律表单内提示。

import { useState, useEffect } from "react";
import { useTranslations, useLocale } from "next-intl";
import { Plus, User, Mail, Shield, Trash2, Loader2 } from "lucide-react";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import api from "@/lib/api";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
import InlineConfirm from "@/lib/ui/InlineConfirm";

// ── 类型 ──
interface Member {
  id: string;
  name: string;
  email: string;
  role: string;
  /** 加入时间，取自后端 workspace_joined_at */
  joinedAt?: string;
}

// 角色显示名称 — 后端使用小写，界面显示使用首字母大写
const roleLabels: Record<string, string> = {
  owner: "Owner",
  admin: "Admin",
  member: "Member",
  viewer: "Viewer",
};

const roleTone: Record<string, string> = {
  owner: "bg-warning/10 text-warning-fg border-warning/20",
  admin: "bg-primary/10 text-primary-fg border-primary/20",
  member: "bg-fg-faint/10 text-fg-muted border-line-strong/20",
  viewer: "bg-fg-faint/10 text-fg-subtle border-line-strong/20",
};

const roleLevelMap: Record<string, number> = { owner: 4, admin: 3, member: 2, viewer: 1 };

export default function MembersPage() {
  const t = useTranslations("members");
  const tInvite = useTranslations("members.invite");
  const tChange = useTranslations("members.changeRole");
  const locale = useLocale();
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const { canManageMembers, level: currentUserLevel, isViewer } = usePermission();

  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [showInvite, setShowInvite] = useState(false);
  const [editingMemberId, setEditingMemberId] = useState<string | null>(null);

  useEffect(() => {
    if (!currentWorkspaceId) {
      setLoading(false);
      return;
    }
    api
      .listMembers(currentWorkspaceId)
      .then((data: unknown) => {
        const raw = Array.isArray(data) ? data : (data as { members?: Member[] }).members || [];
        // API 返回 workspace_role，映射为 Member 接口的 role
        const list = (raw as Record<string, unknown>[]).map((m) => ({
          ...m,
          role: (m as Record<string, unknown>).workspace_role || (m as Record<string, unknown>).role,
          joinedAt:
            (m as Record<string, unknown>).workspace_joined_at ||
            (m as Record<string, unknown>).joined_at ||
            (m as Record<string, unknown>).created_at ||
            "",
        }));
        setMembers(list as Member[]);
      })
      .catch(() => {
        setLoading(false);
      })
      .finally(() => setLoading(false));
  }, [currentWorkspaceId]);

  // ============ 邀请（内联表单） ============
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteName, setInviteName] = useState("");
  const [inviteRole, setInviteRole] = useState("member");
  const [inviting, setInviting] = useState(false);
  const [inviteError, setInviteError] = useState<string | null>(null);
  const [inviteSuccess, setInviteSuccess] = useState<string | null>(null);

  const handleInvite = async () => {
    if (!inviteEmail.trim() || !currentWorkspaceId) return;
    setInviting(true);
    setInviteError(null);
    setInviteSuccess(null);
    try {
      // 邀请接口返回的是 invitation(含待接受的令牌),不是成员对象;
      // 被邀请人接受邀请后才进入成员列表,因此这里只做成功提示
      await api.createMember(currentWorkspaceId, {
        name: inviteName.trim() || inviteEmail.split("@")[0],
        email: inviteEmail.trim(),
        role: inviteRole, // 小写，与后端一致
      });
      setInviteSuccess(tInvite("success"));
      setInviteEmail("");
      setInviteName("");
      setInviteRole("member");
    } catch (err) {
      setInviteError(tInvite("errInvite") + (err instanceof Error ? err.message : String(err)));
    } finally {
      setInviting(false);
    }
  };

  // ============ 角色修改（行内展开） ============
  const [changeRole, setChangeRole] = useState("member");
  const [changing, setChanging] = useState(false);
  const [changeError, setChangeError] = useState<string | null>(null);

  const openRoleEditor = (m: Member) => {
    setEditingMemberId(m.id);
    setChangeRole(m.role);
    setChangeError(null);
  };

  const handleChangeRole = async (m: Member) => {
    if (!currentWorkspaceId || changeRole === m.role) {
      setEditingMemberId(null);
      return;
    }
    setChanging(true);
    setChangeError(null);
    try {
      const result = await api.updateMemberRole(currentWorkspaceId, m.id, changeRole);
      setMembers((prev) => prev.map((item) => (item.id === (result as Member).id ? (result as Member) : item)));
      setEditingMemberId(null);
    } catch (err) {
      setChangeError(tChange("errChange") + (err instanceof Error ? err.message : String(err)));
    } finally {
      setChanging(false);
    }
  };

  // ============ 移除（原位确认） ============
  const handleRemove = async (id: string) => {
    if (!currentWorkspaceId) return;
    try {
      await api.deleteMember(currentWorkspaceId, id);
      setMembers((prev) => prev.filter((m) => m.id !== id));
    } catch (err) {
      console.error("Remove member failed:", err);
    }
  };

  const editingMember = members.find((m) => m.id === editingMemberId) || null;
  // 只展示不高于当前用户级别的角色（不能提升到超过自己的级别）
  const availableRoles = editingMember
    ? ["admin", "member", "viewer"].filter(
        (r) => r !== editingMember.role && roleLevelMap[r] <= currentUserLevel
      )
    : [];

  return (
    <div className="h-full flex flex-col overflow-y-auto page-enter">
      <PageHeader
        title={t("title")}
        meta={<span className="text-xs text-fg-faint">{t("memberCount", { count: members.length })}</span>}
        actions={
          canManageMembers && (
            <Button
              variant="primary"
              onClick={() => {
                setShowInvite(!showInvite);
                setInviteSuccess(null);
                setInviteError(null);
              }}
            >
              <Plus className="w-4 h-4 mr-1.5" /> {t("add")}
            </Button>
          )
        }
      />

      {/* 邀请成员：页头下方内联表单 */}
      {showInvite && (
        <div className="mx-6 mb-4 border border-line rounded-[var(--radius-card)] bg-surface-2/40 p-4 space-y-3 animate-fade-in shrink-0">
          <div className="text-sm font-semibold text-fg flex items-center gap-2">
            <Mail className="w-4 h-4 text-primary-fg" />
            {tInvite("title")}
          </div>
          {inviteError && (
            <div className="px-3 py-2 bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] text-sm text-danger-fg">
              {inviteError}
            </div>
          )}
          {inviteSuccess && (
            <div className="px-3 py-2 bg-success/10 border border-success/20 rounded-[var(--radius-control)] text-sm text-success-fg">
              {inviteSuccess}
            </div>
          )}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-fg-muted mb-1">
                {tInvite("emailLabel")} <span className="text-danger-fg">*</span>
              </label>
              <input
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
                placeholder="colleague@company.com"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-fg-muted mb-1">{tInvite("nameLabel")}</label>
              <input
                value={inviteName}
                onChange={(e) => setInviteName(e.target.value)}
                className="w-full h-[34px] bg-bg border border-line-strong rounded-[var(--radius-control)] px-3 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
                placeholder={tInvite("namePlaceholder")}
              />
            </div>
          </div>
          <div>
            <label className="block text-xs font-medium text-fg-muted mb-1.5">{tInvite("roleLabel")}</label>
            <div className="flex gap-2 flex-wrap">
              {[
                { id: "admin", desc: tInvite("roleAdminDesc") },
                { id: "member", desc: tInvite("roleMemberDesc") },
                { id: "viewer", desc: tInvite("roleViewerDesc") },
              ].map((r) => (
                <button
                  key={r.id}
                  onClick={() => setInviteRole(r.id)}
                  className={`flex-1 min-w-[140px] p-2.5 rounded-[var(--radius-control)] border text-left transition-colors-fast focus-visible:outline-none ${
                    inviteRole === r.id
                      ? "bg-primary/10 border-primary/50 text-primary-fg"
                      : "bg-surface-2/50 border-line text-fg-subtle hover:border-line-strong"
                  }`}
                >
                  <div className="text-xs font-bold">{roleLabels[r.id]}</div>
                  <div className="text-[10px] mt-0.5 opacity-70">{r.desc}</div>
                </button>
              ))}
            </div>
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setShowInvite(false)}>
              {tInvite("cancel")}
            </Button>
            <Button variant="primary" size="sm" loading={inviting} onClick={handleInvite} disabled={!inviteEmail.trim()}>
              {tInvite("submit")}
            </Button>
          </div>
        </div>
      )}

      {/* 成员列表 */}
      <div className="px-6 pb-4 flex-1 min-h-0">
        {loading ? (
          <div className="flex items-center justify-center py-12 text-fg-subtle">
            <Loader2 className="w-5 h-5 mr-2 animate-spin" /> {t("loading")}
          </div>
        ) : (
          <div className="rounded-[var(--radius-card)] border border-line overflow-hidden">
            <div className="grid grid-cols-12 gap-3 text-xs text-fg-faint uppercase tracking-wider px-4 py-2.5 bg-surface-2/80">
              <div className="col-span-4">{t("thName")}</div>
              <div className="col-span-4">{t("thEmail")}</div>
              <div className="col-span-2">{t("thRole")}</div>
              <div className="col-span-1">{t("thJoinedAt")}</div>
              <div className="col-span-1" />
            </div>
            <div className="divide-y divide-line">
              {members.map((m) => (
                <div key={m.id}>
                  <div className="grid grid-cols-12 gap-3 items-center px-4 py-3 transition-colors-fast hover:bg-surface-2/40">
                    <div className="col-span-4 flex items-center gap-3">
                      <div className="w-8 h-8 rounded-full bg-surface-3 flex items-center justify-center shrink-0">
                        <User className="w-4 h-4 text-fg-subtle" />
                      </div>
                      <span className="text-sm text-fg font-medium truncate">{m.name}</span>
                    </div>
                    <div className="col-span-4 text-sm text-fg-subtle truncate">{m.email}</div>
                    <div className="col-span-2">
                      <button
                        onClick={() => canManageMembers && m.role !== "owner" && openRoleEditor(m)}
                        className={`text-[10px] px-2 py-0.5 rounded border ${roleTone[m.role] || ""} ${
                          canManageMembers && m.role !== "owner"
                            ? "cursor-pointer hover:opacity-80 focus-visible:outline-none"
                            : "cursor-default"
                        }`}
                      >
                        {roleLabels[m.role] || m.role}
                      </button>
                    </div>
                    <div className="col-span-1">
                      <span className="text-[10px] text-fg-subtle">
                        {m.joinedAt ? new Date(m.joinedAt).toLocaleDateString(locale) : "-"}
                      </span>
                    </div>
                    <div className="col-span-1 text-right">
                      {canManageMembers && m.role !== "owner" && (
                        <InlineConfirm
                          label={<Trash2 className="w-4 h-4" />}
                          confirmLabel={t("confirmRemoveConfirm")}
                          cancelLabel={t("cancelRemove")}
                          onConfirm={() => handleRemove(m.id)}
                        />
                      )}
                    </div>
                  </div>
                  {/* 角色修改：行下方原位展开 */}
                  {editingMember?.id === m.id && (
                    <div className="px-4 pb-3 animate-fade-in">
                      <div className="border border-primary/25 rounded-[var(--radius-control)] bg-surface-2/40 p-3.5 space-y-2.5">
                        <div className="text-sm font-semibold text-fg">{tChange("title")}</div>
                        <p className="text-xs text-fg-muted">{tChange("desc", { name: editingMember.name })}</p>
                        {changeError && (
                          <div className="px-3 py-2 bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] text-sm text-danger-fg">
                            {changeError}
                          </div>
                        )}
                        {availableRoles.length === 0 ? (
                          <p className="text-sm text-fg-faint">{tChange("noRoles")}</p>
                        ) : (
                          <div className="flex gap-2 flex-wrap">
                            {availableRoles.map((r) => (
                              <button
                                key={r}
                                onClick={() => setChangeRole(r)}
                                className={`px-3 py-2 rounded-[var(--radius-control)] border text-left transition-colors-fast focus-visible:outline-none ${
                                  changeRole === r
                                    ? "bg-primary/10 border-primary/50 text-primary-fg"
                                    : "bg-surface-2/50 border-line text-fg-subtle hover:border-line-strong"
                                }`}
                              >
                                <div className="text-xs font-bold">{roleLabels[r]}</div>
                              </button>
                            ))}
                          </div>
                        )}
                        <div className="flex justify-end gap-2">
                          <Button variant="ghost" size="sm" onClick={() => setEditingMemberId(null)}>
                            {tChange("cancel")}
                          </Button>
                          <Button
                            variant="primary"
                            size="sm"
                            loading={changing}
                            onClick={() => handleChangeRole(editingMember)}
                            disabled={availableRoles.length === 0}
                          >
                            {tChange("confirm")}
                          </Button>
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="mt-5 pt-3 border-t border-line text-[10px] text-fg-faint flex items-center">
          <Shield className="w-3 h-3 mr-1" /> {isViewer ? t("footerViewer") : t("footerManage")}
        </div>
      </div>
    </div>
  );
}
