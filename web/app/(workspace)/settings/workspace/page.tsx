"use client";
// 工作区设置页：工作区信息编辑、所有者查看与危险操作（删除工作区）。

import { useState, useEffect } from "react";
import { Settings, Save, AlertTriangle, Trash2, Building2, User } from "lucide-react";
import { useRouter } from "next/navigation";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";

interface WorkspaceDraft {
  name: string;
  description: string;
  issuePrefix: string;
}

export default function WorkspaceSettingsPage() {
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
      setError(err instanceof Error ? err.message : "保存失败");
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
          <div className="w-10 h-10 rounded-xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center">
            <Settings className="w-5 h-5 text-indigo-400" />
          </div>
          <div>
            <h1 className="text-xl font-bold text-white">工作空间设置</h1>
            <p className="text-xs text-slate-400 mt-0.5">
              管理工作空间基本信息与危险操作
            </p>
          </div>
        </div>
        <button
          onClick={handleSave}
          className={`flex items-center px-5 py-2.5 rounded-xl text-sm font-medium transition-all ${
            saved
              ? "bg-emerald-600 text-white"
              : "bg-blue-600 hover:bg-blue-500 text-white"
          }`}
        >
          <Save className="w-4 h-4 mr-2" />
          {saved ? "已保存" : "保存设置"}
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-sm text-red-400">
          {error}
        </div>
      )}

      {/* 工作区信息 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-5 mb-6">
        <div className="flex items-center gap-2 mb-5">
          <Building2 className="w-4 h-4 text-blue-400" />
          <h2 className="text-sm font-bold text-white">工作空间信息</h2>
        </div>

        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              工作空间名称
            </label>
            <input
              type="text"
              value={draft.name}
              onChange={(e) => handleChange("name", e.target.value)}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
              placeholder="输入工作空间名称"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              描述
            </label>
            <textarea
              value={draft.description}
              onChange={(e) => handleChange("description", e.target.value)}
              rows={3}
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500 resize-none"
              placeholder="工作空间用途说明"
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              任务 ID 前缀
            </label>
            <input
              type="text"
              value={draft.issuePrefix}
              readOnly
              className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-slate-400 cursor-not-allowed"
            />
            <p className="text-[10px] text-slate-500 mt-1.5">
              工作区级任务 ID 前缀，由系统自动生成
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              所有者
            </label>
            <div className="flex items-center gap-2 bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5">
              <User className="w-4 h-4 text-slate-500 shrink-0" />
              <span className="text-slate-300">{ownerName || "加载中..."}</span>
            </div>
            <p className="text-[10px] text-slate-500 mt-1.5">
              工作区创建者，拥有最高权限
            </p>
          </div>
        </div>
      </div>

      {/* 危险操作区 */}
      <div className="bg-red-500/5 backdrop-blur-sm border border-red-500/20 rounded-xl p-5">
        <div className="flex items-center gap-2 mb-4">
          <AlertTriangle className="w-4 h-4 text-red-400" />
          <h2 className="text-sm font-bold text-red-400">危险区域</h2>
        </div>
        <p className="text-xs text-slate-400 mb-4">
          以下操作不可逆，请谨慎执行。
        </p>
        <div className="flex items-center justify-between p-4 bg-slate-800/30 border border-slate-700/50 rounded-lg">
          <div>
            <div className="text-sm font-medium text-white">删除工作空间</div>
            <div className="text-[10px] text-slate-500 mt-0.5">
              {(workspace as { is_default?: boolean })?.is_default
                ? "默认工作区无法删除"
                : "永久删除此工作空间及其所有数据，此操作无法撤销"}
            </div>
          </div>
          <div>
            <button
              onClick={async () => {
                if (!confirm("确定要删除此工作空间吗？此操作不可撤销。")) return;
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
                  setError(err instanceof Error ? err.message : "删除失败");
                  setDeleting(false);
                }
              }}
              disabled={deleting || !!(workspace as { is_default?: boolean })?.is_default}
              className={`flex items-center px-4 py-2 rounded-lg text-sm font-medium transition-colors ${
                (workspace as { is_default?: boolean })?.is_default
                  ? "bg-red-600/10 text-red-400/30 cursor-not-allowed"
                  : deleting
                    ? "bg-red-600/20 text-red-400/50 cursor-wait"
                    : "bg-red-600/20 text-red-400 hover:bg-red-600/30 hover:text-red-300"
              }`}
            >
              <Trash2 className="w-4 h-4 mr-2" />
              {deleting ? "删除中..." : "删除工作空间"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
