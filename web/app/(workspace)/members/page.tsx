"use client";
// 工作区成员页：邀请成员、角色管理与移除成员。

import React, { useState, useEffect } from "react";
import Link from "next/link";
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
        "邀请成员失败: " + (err instanceof Error ? err.message : String(err))
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
        className="bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[460px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex items-center">
          <Mail className="w-5 h-5 text-blue-400 mr-3" />
          <h2 className="text-lg font-bold text-white flex-1">邀请成员</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              邮箱地址 <span className="text-red-400">*</span>
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white text-sm focus:outline-none focus:border-blue-500 transition-colors-fast"
              placeholder="colleague@company.com"
              onKeyDown={(e) => e.key === "Enter" && handleInvite()}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              姓名
            </label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white text-sm focus:outline-none focus:border-blue-500 transition-colors-fast"
              placeholder="可选，默认取邮箱前缀"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-2">
              角色
            </label>
            <div className="flex gap-2">
              {[
                { id: "admin", desc: "管理所有配置" },
                { id: "member", desc: "创建和编辑任务" },
                { id: "viewer", desc: "只读访问" },
              ].map((r) => (
                <button
                  key={r.id}
                  onClick={() => setRole(r.id)}
                  className={`flex-1 p-3 rounded-lg border text-left transition-colors-normal ${
                    role === r.id
                      ? "bg-blue-500/10 border-blue-500/50 text-blue-300"
                      : "bg-slate-800/50 border-slate-700 text-slate-400 hover:border-slate-500"
                  }`}
                >
                  <div className="text-xs font-bold">{roleLabels[r.id]}</div>
                  <div className="text-[10px] mt-0.5 opacity-70">{r.desc}</div>
                </button>
              ))}
            </div>
          </div>
        </div>
        <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
          >
            取消
          </button>
          <button
            onClick={handleInvite}
            disabled={!email.trim() || sending}
            className="flex items-center px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {sending ? (
              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
            ) : (
              <Mail className="w-4 h-4 mr-2" />
            )}
            添加成员
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
        "修改角色失败: " + (err instanceof Error ? err.message : String(err))
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
        className="bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[400px] shadow-2xl modal-content"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-slate-800 flex items-center">
          <Shield className="w-5 h-5 text-blue-400 mr-3" />
          <h2 className="text-lg font-bold text-white flex-1">修改角色</h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-slate-700 rounded-lg"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>
        <div className="p-5 space-y-4">
          <p className="text-sm text-slate-300">
            修改 <span className="text-white font-medium">{member.name}</span>{" "}
            的角色
          </p>
          {availableRoles.length === 0 ? (
            <p className="text-sm text-slate-500">无可用的角色选项</p>
          ) : (
            <div className="flex gap-2">
              {availableRoles.map((r) => (
                <button
                  key={r}
                  onClick={() => setRole(r)}
                  className={`flex-1 p-3 rounded-lg border text-left transition-colors-normal ${
                    role === r
                      ? "bg-blue-500/10 border-blue-500/50 text-blue-300"
                      : "bg-slate-800/50 border-slate-700 text-slate-400 hover:border-slate-500"
                  }`}
                >
                  <div className="text-xs font-bold">{roleLabels[r]}</div>
                </button>
              ))}
            </div>
          )}
        </div>
        <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg transition-colors-fast"
          >
            取消
          </button>
          <button
            onClick={handleChange}
            disabled={saving || availableRoles.length === 0}
            className="flex items-center px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
          >
            {saving && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
            确认
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
        alert("加载成员列表失败: " + err.message);
      })
      .finally(() => setLoading(false));
  }, [currentWorkspaceId]);

  const handleInvited = (invited: Member) => {
    setMembers((prev) => [...prev, invited]);
  };

  const handleRemove = async (id: string) => {
    const m = members.find((m) => m.id === id);
    if (!m || m.role === "owner") return;
    if (!confirm(`确定移除成员 ${m.name} 吗？`)) return;
    try {
      await api.deleteMember(currentWorkspaceId!, id);
      setMembers((prev) => prev.filter((m) => m.id !== id));
    } catch (err) {
      alert(
        "移除成员失败: " + (err instanceof Error ? err.message : String(err))
      );
    }
  };

  const handleRoleChanged = (updated: Member) => {
    setMembers((prev) =>
      prev.map((m) => (m.id === updated.id ? updated : m))
    );
  };

  const roleColors: Record<string, string> = {
    owner: "bg-amber-500/10 text-amber-400 border-amber-500/20",
    admin: "bg-blue-500/10 text-blue-400 border-blue-500/20",
    member: "bg-slate-500/10 text-slate-300 border-slate-500/20",
    viewer: "bg-slate-500/10 text-slate-400 border-slate-500/20",
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      {/* 页头 */}
      <div className="flex justify-between items-center mb-8 shrink-0">
        <div className="flex items-center gap-4">
          <Link
            href="/dashboard"
            className="p-2 hover:bg-slate-800 rounded-lg transition-colors-fast"
          >
            <ArrowLeft className="w-5 h-5 text-slate-400" />
          </Link>
          <div>
            <h2 className="text-2xl font-bold text-white flex items-center">
              <User className="w-6 h-6 mr-2 text-blue-400" /> 成员管理
            </h2>
            <p className="text-xs text-slate-400 mt-1">
              {members.length} 位成员
            </p>
          </div>
        </div>
        {canManageMembers && (
          <button
            onClick={() => setShowInvite(true)}
            className="flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium transition-colors-fast btn-press"
          >
            <Plus className="w-4 h-4 mr-1.5" /> 添加成员
          </button>
        )}
      </div>

      {/* 成员列表 */}
      <div className="flex-1 space-y-2">
        {loading ? (
          <div className="flex items-center justify-center py-12 text-slate-400">
            <Loader2 className="w-5 h-5 mr-2 animate-spin" /> 加载中...
          </div>
        ) : (
          <>
            <div className="grid grid-cols-12 gap-3 text-xs text-slate-500 uppercase tracking-wider px-4 py-2">
              <div className="col-span-4">姓名</div>
              <div className="col-span-4">邮箱</div>
              <div className="col-span-2">角色</div>
              <div className="col-span-1">加入时间</div>
              <div className="col-span-1" />
            </div>
            {members.map((m) => (
              <div
                key={m.id}
                className="grid grid-cols-12 gap-3 items-center bg-slate-800/30 border border-slate-700/50 rounded-lg px-4 py-3 hover:bg-slate-800/50 transition-colors-fast card-hover"
              >
                <div className="col-span-4 flex items-center gap-3">
                  <div className="w-8 h-8 rounded-full bg-slate-700 flex items-center justify-center">
                    <User className="w-4 h-4 text-slate-400" />
                  </div>
                  <span className="text-sm text-white font-medium">
                    {m.name}
                  </span>
                </div>
                <div className="col-span-4 text-sm text-slate-400">
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
                  <span className="text-[10px] text-slate-400">
                    {m.joinedAt ? new Date(m.joinedAt).toLocaleDateString("zh-CN") : "-"}
                  </span>
                </div>
                <div className="col-span-1 text-right">
                  {canManageMembers && m.role !== "owner" && (
                    <button
                      onClick={() => handleRemove(m.id)}
                      className="p-1 text-slate-500 hover:text-red-400 transition-colors-fast"
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

      <div className="mt-6 pt-4 border-t border-slate-800 text-[10px] text-slate-500 flex items-center">
        {isViewer ? (
          <>
            <Shield className="w-3 h-3 mr-1" /> 只读模式 · Viewer 无法管理成员
          </>
        ) : (
          <>
            <Shield className="w-3 h-3 mr-1" /> Owner 不可移除 · 点击角色标签可修改角色
          </>
        )}
      </div>

      {showInvite && currentWorkspaceId && (
        <InviteMemberDialog
          onClose={() => setShowInvite(false)}
          onInvited={handleInvited}
          workspaceId={currentWorkspaceId}
        />
      )}

      {editMember && currentWorkspaceId && (
        <ChangeRoleDialog
          member={editMember}
          onClose={() => setEditMember(null)}
          onChanged={handleRoleChanged}
          workspaceId={currentWorkspaceId}
          currentUserLevel={currentUserLevel}
        />
      )}
    </div>
  );
}
