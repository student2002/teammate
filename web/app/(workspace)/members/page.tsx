"use client";
// 工作区成员页：邀请成员、角色管理与移除成员。

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { useTranslations, useLocale } from "next-intl";
import {
  Plus,
  X,
  User,
  Mail,
  Shield,
  Trash2,
  Loader2,
  ArrowLeft,
} from "lucide-react";
import ModalPortal from "@/lib/ModalPortal";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import api from "@/lib/api";

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

// ==========================================
// 邀请成员弹窗
// ==========================================
function InviteMemberDialog({
  onClose,
  onInvited,
  workspaceId,
}: {
  onClose: () => void;
  onInvited: (member: Member) => void;
  workspaceId: string;
}) {
  const t = useTranslations("members.invite");
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [role, setRole] = useState("member");
  const [sending, setSending] = useState(false);

  const handleInvite = async () => {
    if (!email.trim()) return;
    setSending(true);
    try {
      const result = await api.createMember(workspaceId, {
        name: name.trim() || email.split("@")[0],
        email: email.trim(),
        role, // 小写，与后端一致
      });
      onInvited(result as Member);
      setEmail("");
      setName("");
    } catch (err) {
      alert(
        t("errInvite") + (err instanceof Error ? err.message : String(err))
      );
    } finally {
      setSending(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[460px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex items-center">
          <Mail className="w-5 h-5 text-primary-fg mr-3" />
          <h2 className="text-lg font-bold text-fg flex-1">{t("title")}</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg"
          >
            <X className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("emailLabel")} <span className="text-danger-fg">*</span>
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
              placeholder="colleague@company.com"
              onKeyDown={(e) => e.key === "Enter" && handleInvite()}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("nameLabel")}
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg text-sm focus:outline-none focus:border-primary transition-colors-fast"
              placeholder={t("namePlaceholder")}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-fg-muted mb-2">
              {t("roleLabel")}
            </label>
            <div className="flex gap-2">
              {[
                { id: "admin", desc: t("roleAdminDesc") },
                { id: "member", desc: t("roleMemberDesc") },
                { id: "viewer", desc: t("roleViewerDesc") },
              ].map((r) => (
                <button
                  key={r.id}
                  onClick={() => setRole(r.id)}
                  className={`flex-1 p-3 rounded-lg border text-left transition-colors-normal ${
                    role === r.id
                      ? "bg-primary/10 border-primary/50 text-primary-fg"
                      : "bg-surface-2/50 border-line-strong text-fg-subtle hover:border-line-strong"
                  }`}
                >
                  <div className="text-xs font-bold">{roleLabels[r.id]}</div>
                  <div className="text-[10px] mt-0.5 opacity-70">{r.desc}</div>
                </button>
              ))}
            </div>
          </div>
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
          >
            {t("cancel")}
          </button>
          <button
            onClick={handleInvite}
            disabled={!email.trim() || sending}
            className="flex items-center px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {sending ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Mail className="w-4 h-4 mr-2" />
            )}
            {t("submit")}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 角色变更弹窗
// ==========================================
function ChangeRoleDialog({
  member,
  onClose,
  onChanged,
  workspaceId,
  currentUserLevel,
}: {
  member: Member;
  onClose: () => void;
  onChanged: (member: Member) => void;
  workspaceId: string;
  currentUserLevel: number;
}) {
  const t = useTranslations("members.changeRole");
  const [role, setRole] = useState(member.role);
  const [saving, setSaving] = useState(false);

  const handleChange = async () => {
    if (role === member.role) {
      onClose();
      return;
    }
    setSaving(true);
    try {
      const result = await api.updateMemberRole(workspaceId, member.id, role);
      onChanged(result as Member);
      onClose();
    } catch (err) {
      alert(
        t("errChange") + (err instanceof Error ? err.message : String(err))
      );
    } finally {
      setSaving(false);
    }
  };

  // 只展示不高于当前用户级别的角色（不能提升到超过自己的级别）
  const roleLevelMap: Record<string, number> = { owner: 4, admin: 3, member: 2, viewer: 1 };
  const availableRoles = ["admin", "member", "viewer"].filter(
    (r) => r !== member.role && roleLevelMap[r] <= currentUserLevel
  );

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[400px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex items-center">
          <Shield className="w-5 h-5 text-primary-fg mr-3" />
          <h2 className="text-lg font-bold text-fg flex-1">{t("title")}</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg"
          >
            <X className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <p className="text-sm text-fg-muted">
            {t("desc", { name: member.name })}
          </p>
          {availableRoles.length === 0 ? (
            <p className="text-sm text-fg-faint">{t("noRoles")}</p>
          ) : (
            <div className="flex gap-2">
              {availableRoles.map((r) => (
                <button
                  key={r}
                  onClick={() => setRole(r)}
                  className={`flex-1 p-3 rounded-lg border text-left transition-colors-normal ${
                    role === r
                      ? "bg-primary/10 border-primary/50 text-primary-fg"
                      : "bg-surface-2/50 border-line-strong text-fg-subtle hover:border-line-strong"
                  }`}
                >
                  <div className="text-xs font-bold">{roleLabels[r]}</div>
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="p-5 border-t border-line flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
          >
            {t("cancel")}
          </button>
          <button
            onClick={handleChange}
            disabled={saving || availableRoles.length === 0}
            className="flex items-center px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {saving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
            {t("confirm")}
          </button>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 成员管理页面
// ==========================================
export default function MembersPage() {
  const t = useTranslations("members");
  const locale = useLocale();
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const { canManageMembers, level: currentUserLevel, isViewer } = usePermission();

  const [members, setMembers] = useState<Member[]>([]);
  const [loading, setLoading] = useState(true);
  const [showInvite, setShowInvite] = useState(false);
  const [editMember, setEditMember] = useState<Member | null>(null);

  useEffect(() => {
    if (!currentWorkspaceId) {
      setLoading(false);
      return;
    }
    api
      .listMembers(currentWorkspaceId)
      .then((data: unknown) => {
        const raw = Array.isArray(data)
          ? data
          : (data as { members?: Member[] }).members || [];
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
      .catch((err: Error) => {
        alert(t("errLoad") + err.message);
      })
      .finally(() => setLoading(false));
  }, [currentWorkspaceId]);

  const handleInvited = (invited: Member) => {
    setMembers((prev) => [...prev, invited]);
  };

  const handleRemove = async (id: string) => {
    const m = members.find((m) => m.id === id);
    if (!m || m.role === "owner") return;
    if (!confirm(t("confirmRemove", { name: m.name }))) return;
    try {
      await api.deleteMember(currentWorkspaceId!, id);
      setMembers((prev) => prev.filter((m) => m.id !== id));
    } catch (err) {
      alert(
        t("errRemove") + (err instanceof Error ? err.message : String(err))
      );
    }
  };

  const handleRoleChanged = (updated: Member) => {
    setMembers((prev) =>
      prev.map((m) => (m.id === updated.id ? updated : m))
    );
  };

  const roleColors: Record<string, string> = {
    owner: "bg-warning/10 text-warning-fg border-warning/20",
    admin: "bg-primary/10 text-primary-fg border-primary/20",
    member: "bg-fg-faint/10 text-fg-muted border-line-strong/20",
    viewer: "bg-fg-faint/10 text-fg-subtle border-line-strong/20",
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      {/* 页头 */}
      <div className="flex justify-between items-center mb-8 shrink-0">
        <div className="flex items-center gap-4">
          <Link
            href="/dashboard"
            className="p-2 hover:bg-surface-2 rounded-lg transition-colors-fast"
          >
            <ArrowLeft className="w-5 h-5 text-fg-subtle" />
          </Link>
          <div>
            <h2 className="text-2xl font-bold text-fg flex items-center">
              <User className="w-6 h-6 mr-2 text-primary-fg" /> {t("title")}
            </h2>
            <p className="text-xs text-fg-subtle mt-1">
              {t("memberCount", { count: members.length })}
            </p>
          </div>
        </div>
        {canManageMembers && (
          <button
            onClick={() => setShowInvite(true)}
            className="flex items-center px-4 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium transition-colors-fast btn-press"
          >
            <Plus className="w-4 h-4 mr-1.5" /> {t("add")}
          </button>
        )}
      </div>

      {/* 成员列表 */}
      <div className="flex-1 space-y-2">
        {loading ? (
          <div className="flex items-center justify-center py-12 text-fg-subtle">
            <Loader2 className="w-5 h-5 mr-2 animate-spin" /> {t("loading")}
          </div>
        ) : (
          <>
            <div className="grid grid-cols-12 gap-3 text-xs text-fg-faint uppercase tracking-wider px-4 py-2">
              <div className="col-span-4">{t("thName")}</div>
              <div className="col-span-4">{t("thEmail")}</div>
              <div className="col-span-2">{t("thRole")}</div>
              <div className="col-span-1">{t("thJoinedAt")}</div>
              <div className="col-span-1" />
            </div>
            {members.map((m) => (
              <div
                key={m.id}
                className="grid grid-cols-12 gap-3 items-center bg-surface-2/30 border border-line-strong/50 rounded-lg px-4 py-3 hover:bg-surface-2/50 transition-colors-fast card-hover"
              >
                <div className="col-span-4 flex items-center gap-3">
                  <div className="w-8 h-8 rounded-full bg-surface-3 flex items-center justify-center">
                    <User className="w-4 h-4 text-fg-subtle" />
                  </div>
                  <span className="text-sm text-fg font-medium">
                    {m.name}
                  </span>
                </div>
                <div className="col-span-4 text-sm text-fg-subtle">
                  {m.email}
                </div>
                <div className="col-span-2">
                  <button
                    onClick={() =>
                      canManageMembers && m.role !== "owner" && setEditMember(m)
                    }
                    className={`text-[10px] px-2 py-0.5 rounded-full border ${
                      roleColors[m.role] || ""
                    } ${canManageMembers && m.role !== "owner" ? "cursor-pointer hover:opacity-80" : "cursor-default"}`}
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
                    <button
                      onClick={() => handleRemove(m.id)}
                      className="p-1 text-fg-faint hover:text-danger-fg transition-colors-fast"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </>
        )}
      </div>

      <div className="mt-6 pt-4 border-t border-line text-[10px] text-fg-faint flex items-center">
        {isViewer ? (
          <>
            <Shield className="w-3 h-3 mr-1" /> {t("footerViewer")}
          </>
        ) : (
          <>
            <Shield className="w-3 h-3 mr-1" /> {t("footerManage")}
          </>
        )}
      </div>

      {showInvite && currentWorkspaceId && (
        <ModalPortal>
        <InviteMemberDialog
          onClose={() => setShowInvite(false)}
          onInvited={handleInvited}
          workspaceId={currentWorkspaceId}
        />
        </ModalPortal>
      )}

      {editMember && currentWorkspaceId && (
        <ModalPortal>
        <ChangeRoleDialog
          member={editMember}
          onClose={() => setEditMember(null)}
          onChanged={handleRoleChanged}
          workspaceId={currentWorkspaceId}
          currentUserLevel={currentUserLevel}
        />
        </ModalPortal>
      )}
    </div>
  );
}
