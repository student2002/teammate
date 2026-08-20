"use client";
// 项目设置页：默认模板、成员/审查员管理及 Git 凭据配置。

import { useState, useEffect } from "react";
import {
  Settings,
  Save,
  AlertTriangle,
  Plus,
  X,
  Shield,
  Users,
  FileOutput,
  Trash2,
  GitBranch,
  Eye,
  EyeOff,
  Pencil,
} from "lucide-react";
import { useAppStore } from "@/lib/store";
import api from "@/lib/api";
import type { GitCredential } from "@/lib/types";
import EmptyStateGuide from "@/lib/EmptyStateGuide";

// ── 扩展的本地类型 ────
interface LocalProject {
  id: string;
  name: string;
  members: string[];
  reviewers: string[];
  defaultTemplateId: string;
}

interface ProjectDraft {
  defaultTemplateId: string;
  members: string[];
  reviewers: string[];
}

interface ProjectMemberRecord {
  id: string;
  member_type: string;
  agent_id?: string;
  member_id?: string;
  role: string;
}

interface ProjectReviewerRecord {
  id: string;
  member_type: string;
  agent_id?: string;
  member_id?: string;
}

export default function ProjectSettingsPage() {
  const projects = useAppStore((s) => s.projects) as unknown as LocalProject[];
  const setProjects = useAppStore((s) => s.setProjects) as unknown as (
    v: LocalProject[] | ((prev: LocalProject[]) => LocalProject[])
  ) => void;
  const agents = useAppStore((s) => s.agents);
  const templates = useAppStore((s) => s.templates);
  const workspaceId = useAppStore((s) => s.currentWorkspaceId) || "";

  const [selectedProjectId, setSelectedProjectId] = useState(
    projects[0]?.id || ""
  );
  const [draft, setDraft] = useState<ProjectDraft | null>(null);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showNewProject, setShowNewProject] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const [newRepoUrl, setNewRepoUrl] = useState("");
  const [newGitUsername, setNewGitUsername] = useState("git");
  const [newPat, setNewPat] = useState("");
  const [showNewPat, setShowNewPat] = useState(false);
  const [creatingProject, setCreatingProject] = useState(false);
  const [deletingProject, setDeletingProject] = useState<string | null>(null);

  // Git 凭据状态
  const [gitCredentials, setGitCredentials] = useState<GitCredential[]>([]);
  const [loadingCreds, setLoadingCreds] = useState(false);
  const [showCredModal, setShowCredModal] = useState(false);
  const [editingCred, setEditingCred] = useState<GitCredential | null>(null);
  const [credForm, setCredForm] = useState({ repo_url: "", username: "git", pat: "" });
  const [savingCred, setSavingCred] = useState(false);
  const [showPat, setShowPat] = useState(false);

  // 服务端的项目成员/审查员记录，用于基于差异的保存
  const [serverMembers, setServerMembers] = useState<ProjectMemberRecord[]>([]);
  const [serverReviewers, setServerReviewers] = useState<ProjectReviewerRecord[]>([]);
  const [loadingMembers, setLoadingMembers] = useState(false);

  const currentProject = projects.find((p) => p.id === selectedProjectId);

  useEffect(() => {
    if (currentProject) {
      setDraft({
        defaultTemplateId: currentProject.defaultTemplateId,
        members: [...(currentProject.members || [])],
        reviewers: [...(currentProject.reviewers || [])],
      });
      setSaved(false);
      // 从后端加载真实的项目成员和审查员
      loadProjectMembers(currentProject.id);
    }
  }, [selectedProjectId]); // eslint-disable-line react-hooks/exhaustive-deps

  const loadProjectMembers = async (projectId: string) => {
    setLoadingMembers(true);
    try {
      const [membersRes, reviewersRes] = await Promise.all([
        api.listProjectMembers(workspaceId, projectId).catch(() => []),
        api.listProjectReviewers(workspaceId, projectId).catch(() => []),
      ]);
      const members = (Array.isArray(membersRes) ? membersRes : []) as ProjectMemberRecord[];
      const reviewers = (Array.isArray(reviewersRes) ? reviewersRes : []) as ProjectReviewerRecord[];
      setServerMembers(members);
      setServerReviewers(reviewers);
      // 从成员记录中提取 Agent ID 用于草稿
      const agentMemberIds = members
        .filter((m) => m.member_type === "agent" && m.agent_id)
        .map((m) => m.agent_id!);
      const agentReviewerIds = reviewers
        .filter((r) => r.member_type === "agent" && r.agent_id)
        .map((r) => r.agent_id!);
      setDraft((prev) =>
        prev
          ? { ...prev, members: agentMemberIds, reviewers: agentReviewerIds }
          : prev
      );
    } catch {
      // 忽略
    } finally {
      setLoadingMembers(false);
    }
  };

  // 项目变化时自动选中第一个项目
  useEffect(() => {
    if (projects.length > 0 && !projects.find((p) => p.id === selectedProjectId)) {
      setSelectedProjectId(projects[0].id);
    }
  }, [projects]); // eslint-disable-line react-hooks/exhaustive-deps

  // 项目变化时加载 Git 凭据
  useEffect(() => {
    if (!selectedProjectId) {
      setGitCredentials([]);
      return;
    }
    setLoadingCreds(true);
    api.listGitCredentials(selectedProjectId)
      .then((res) => {
        const creds = ((res as Record<string, unknown>)?.credentials ?? res) as GitCredential[];
        setGitCredentials(Array.isArray(creds) ? creds : []);
      })
      .catch(() => setGitCredentials([]))
      .finally(() => setLoadingCreds(false));
  }, [selectedProjectId]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleCreateProject = async () => {
    if (!newProjectName.trim() || !newRepoUrl.trim() || !newPat.trim()) return;
    if (!workspaceId) {
      setError("请先选择工作空间");
      return;
    }
    try {
      setCreatingProject(true);
      const created = (await api.createProject(workspaceId, {
        name: newProjectName.trim(),
        status: "active",
        repo_url: newRepoUrl.trim(),
      })) as { id: string; name?: string };
      // 创建 Git 凭据
      await api.createGitCredential(created.id, {
        repo_url: newRepoUrl.trim(),
        username: newGitUsername.trim() || "git",
        pat: newPat,
      });
      const newProject: LocalProject = {
        id: created.id,
        name: created.name || newProjectName.trim(),
        members: [],
        reviewers: [],
        defaultTemplateId: "",
      };
      setProjects((prev: LocalProject[]) => [...prev, newProject]);
      setSelectedProjectId(created.id);
      setShowNewProject(false);
      setNewProjectName("");
      setNewRepoUrl("");
      setNewGitUsername("git");
      setNewPat("");
    } catch (err) {
      setError("创建工程失败: " + (err instanceof Error ? err.message : "未知错误"));
    } finally {
      setCreatingProject(false);
    }
  };

  const handleDeleteProject = async () => {
    if (!deletingProject || !workspaceId) return;
    try {
      await api.deleteProject(workspaceId, deletingProject);
      setProjects((prev: LocalProject[]) =>
        prev.filter((p) => p.id !== deletingProject)
      );
      if (selectedProjectId === deletingProject) {
        setSelectedProjectId(
          projects.find((p) => p.id !== deletingProject)?.id || ""
        );
      }
      setDeletingProject(null);
    } catch (err) {
      setError("删除工程失败: " + (err instanceof Error ? err.message : "未知错误"));
    }
  };

  if (!currentProject && projects.length === 0) {
    return (
      <div className="h-full flex flex-col items-center justify-center text-slate-400 p-8">
        <Settings className="w-12 h-12 mb-4 text-slate-600" />
        <p className="text-lg font-medium text-slate-300 mb-2">暂无工程</p>
        <p className="text-sm text-slate-500 mb-6">
          请先创建一个工程来管理项目设置
        </p>
        <button
          onClick={() => setShowNewProject(true)}
          className="flex items-center px-5 py-2.5 bg-blue-600 hover:bg-blue-500 text-white rounded-xl text-sm font-medium transition-colors-fast btn-press"
        >
          <Plus className="w-4 h-4 mr-2" /> 新建工程
        </button>
        <div className="mt-8 w-full max-w-2xl">
          <EmptyStateGuide page="project" />
        </div>

        {showNewProject && (
          <div
            className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
            onClick={() => setShowNewProject(false)}
          >
            <div
              className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[520px] max-h-[80vh] overflow-y-auto shadow-2xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="p-5 border-b border-slate-800">
                <h3 className="text-lg font-bold text-white">新建工程</h3>
              </div>
              <div className="p-5 space-y-4">
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    工程名称 <span className="text-red-400">*</span>
                  </label>
                  <input
                    value={newProjectName}
                    onChange={(e) => setNewProjectName(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                    placeholder="如: 支付中心"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Git 仓库地址 <span className="text-red-400">*</span>
                  </label>
                  <input
                    value={newRepoUrl}
                    onChange={(e) => setNewRepoUrl(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                    placeholder="https://github.com/org/repo.git"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Git 用户名
                  </label>
                  <input
                    value={newGitUsername}
                    onChange={(e) => setNewGitUsername(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                    placeholder="git"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-slate-300 mb-2">
                    Personal Access Token <span className="text-red-400">*</span>
                  </label>
                  <div className="relative">
                    <input
                      type={showNewPat ? "text" : "password"}
                      value={newPat}
                      onChange={(e) => setNewPat(e.target.value)}
                      className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500 pr-10"
                      placeholder="ghp_xxxxxxxxxxxx"
                    />
                    <button
                      type="button"
                      onClick={() => setShowNewPat((v) => !v)}
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                    >
                      {showNewPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
              </div>
              <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
                <button
                  onClick={() => setShowNewProject(false)}
                  className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg"
                >
                  取消
                </button>
                <button
                  onClick={handleCreateProject}
                  disabled={
                    !newProjectName.trim() ||
                    !newRepoUrl.trim() ||
                    !newPat.trim() ||
                    creatingProject
                  }
                  className="px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
                >
                  {creatingProject ? "创建中..." : "创建"}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    );
  }

  if (!draft || !currentProject) {
    return null;
  }

  const selfReviewAgents = draft.members.filter((id) =>
    draft.reviewers.includes(id)
  );

  const handleSave = async () => {
    if (!workspaceId || !draft) {
      setError("请先选择工作空间");
      return;
    }
    try {
      setError(null);

      // 1. 通过 updateProject 保存默认模板
      await api.updateProject(workspaceId, selectedProjectId, {
        defaultTemplateId: draft.defaultTemplateId,
      });

      // 2. 对比并同步项目成员
      const currentAgentMemberIds = serverMembers
        .filter((m) => m.member_type === "agent" && m.agent_id)
        .map((m) => m.agent_id!);
      const toAddMembers = draft.members.filter(
        (id) => !currentAgentMemberIds.includes(id)
      );
      const toRemoveMembers = serverMembers
        .filter((m) => m.member_type === "agent" && m.agent_id && !draft.members.includes(m.agent_id))
        .map((m) => m.id);

      for (const agentId of toAddMembers) {
        await api.addProjectMember(workspaceId, selectedProjectId, {
          member_type: "agent",
          agent_id: agentId,
          role: "developer",
        });
      }
      for (const recordId of toRemoveMembers) {
        await api.removeProjectMember(workspaceId, selectedProjectId, recordId);
      }

      // 3. 对比并同步项目审查员
      const currentAgentReviewerIds = serverReviewers
        .filter((r) => r.member_type === "agent" && r.agent_id)
        .map((r) => r.agent_id!);
      const toAddReviewers = draft.reviewers.filter(
        (id) => !currentAgentReviewerIds.includes(id)
      );
      const toRemoveReviewers = serverReviewers
        .filter((r) => r.member_type === "agent" && r.agent_id && !draft.reviewers.includes(r.agent_id))
        .map((r) => r.id);

      for (const agentId of toAddReviewers) {
        await api.addProjectReviewer(workspaceId, selectedProjectId, {
          member_type: "agent",
          agent_id: agentId,
        });
      }
      for (const recordId of toRemoveReviewers) {
        await api.removeProjectReviewer(workspaceId, selectedProjectId, recordId);
      }

      // 4. 从后端重新加载成员以同步状态
      await loadProjectMembers(selectedProjectId);

      setProjects((prev: LocalProject[]) =>
        prev.map((p) =>
          p.id === selectedProjectId
            ? { ...p, defaultTemplateId: draft.defaultTemplateId, members: draft.members, reviewers: draft.reviewers }
            : p
        )
      );
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存失败");
    }
  };

  const toggleMember = (agentId: string) => {
    setDraft((prev) =>
      prev
        ? {
            ...prev,
            members: prev.members.includes(agentId)
              ? prev.members.filter((id) => id !== agentId)
              : [...prev.members, agentId],
          }
        : prev
    );
    setSaved(false);
  };

  const toggleReviewer = (agentId: string) => {
    setDraft((prev) =>
      prev
        ? {
            ...prev,
            reviewers: prev.reviewers.includes(agentId)
              ? prev.reviewers.filter((id) => id !== agentId)
              : [...prev.reviewers, agentId],
          }
        : prev
    );
    setSaved(false);
  };

  const addAllMembers = () => {
    const allIds = agents.map((a) => a.id);
    setDraft((prev) => (prev ? { ...prev, members: allIds } : prev));
    setSaved(false);
  };

  const clearMembers = () => {
    setDraft((prev) => (prev ? { ...prev, members: [] } : prev));
    setSaved(false);
  };

  // ── Git 凭据处理器 ──
  const openNewCred = () => {
    setEditingCred(null);
    setCredForm({ repo_url: "", username: "git", pat: "" });
    setShowPat(false);
    setShowCredModal(true);
  };

  const openEditCred = (cred: GitCredential) => {
    setEditingCred(cred);
    setCredForm({ repo_url: cred.repo_url, username: cred.username, pat: "" });
    setShowPat(false);
    setShowCredModal(true);
  };

  const handleSaveCred = async () => {
    if (!credForm.repo_url.trim()) return;
    try {
      setSavingCred(true);
      if (editingCred) {
        const data: Record<string, unknown> = {
          repo_url: credForm.repo_url.trim(),
          username: credForm.username.trim() || "git",
        };
        if (credForm.pat) data.pat = credForm.pat;
        await api.updateGitCredential(selectedProjectId, editingCred.id, data);
      } else {
        if (!credForm.pat) {
          setError("创建凭据时必须填写 PAT");
          setSavingCred(false);
          return;
        }
        await api.createGitCredential(selectedProjectId, {
          repo_url: credForm.repo_url.trim(),
          username: credForm.username.trim() || "git",
          pat: credForm.pat,
        });
      }
      // 刷新凭据
      const res = await api.listGitCredentials(selectedProjectId);
      const creds = ((res as Record<string, unknown>)?.credentials ?? res) as GitCredential[];
      setGitCredentials(Array.isArray(creds) ? creds : []);
      setShowCredModal(false);
    } catch (err) {
      setError("保存凭据失败: " + (err instanceof Error ? err.message : "未知错误"));
    } finally {
      setSavingCred(false);
    }
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
      {/* 页头 */}
      <div className="flex items-center justify-between mb-8">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center">
            <Settings className="w-5 h-5 text-indigo-400" />
          </div>
          <div>
            <h1 className="text-xl font-bold text-white">工程设置</h1>
            <p className="text-xs text-slate-400 mt-0.5">
              管理工程的默认模板、成员和审查策略
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setShowNewProject(true)}
            className="flex items-center px-4 py-2.5 bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white rounded-xl text-sm font-medium transition-colors-fast border border-slate-700"
          >
            <Plus className="w-4 h-4 mr-2" /> 新建工程
          </button>
          <button
            onClick={handleSave}
            className={`flex items-center px-5 py-2.5 rounded-xl text-sm font-medium transition-colors-normal btn-press ${
              saved
                ? "bg-emerald-600 text-white"
                : "bg-blue-600 hover:bg-blue-500 text-white"
            }`}
          >
            <Save className="w-4 h-4 mr-2" />
            {saved ? "已保存" : "保存设置"}
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-sm text-red-400">
          {error}
        </div>
      )}

      {/* 项目选择器 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center justify-between mb-2">
          <label className="block text-sm font-medium text-slate-300">
            当前工程
          </label>
          {currentProject && (
            <button
              onClick={() => setDeletingProject(selectedProjectId)}
              className="flex items-center px-2.5 py-1 text-[10px] text-red-400 hover:text-red-300 hover:bg-red-500/10 rounded-lg transition-colors-fast"
            >
              <Trash2 className="w-3 h-3 mr-1" /> 删除此工程
            </button>
          )}
        </div>
        <select
          value={selectedProjectId}
          onChange={(e) => setSelectedProjectId(e.target.value)}
          className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
        >
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name?.trim() ? p.name : "(未命名)"}
            </option>
          ))}
        </select>
      </div>

      {/* 默认模板 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center gap-2 mb-4">
          <FileOutput className="w-4 h-4 text-blue-400" />
          <h2 className="text-sm font-bold text-white">默认工作流模板</h2>
        </div>
        <select
          value={draft.defaultTemplateId}
          onChange={(e) => {
            setDraft((prev) =>
              prev
                ? { ...prev, defaultTemplateId: e.target.value }
                : prev
            );
            setSaved(false);
          }}
          className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
        >
          {templates.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name} {t.isBuiltIn ? "(内置)" : ""}
            </option>
          ))}
        </select>
        <p className="text-[10px] text-slate-500 mt-2">
          新建任务时将自动使用此模板，可在任务创建后手动切换
        </p>
      </div>

      {/* 成员管理 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <Users className="w-4 h-4 text-emerald-400" />
            <h2 className="text-sm font-bold text-white">工程成员</h2>
            <span className="text-[10px] text-slate-500 bg-slate-700/50 px-2 py-0.5 rounded-full">
              {draft.members.length} / {agents.length}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={addAllMembers}
              className="flex items-center px-2.5 py-1 text-[10px] bg-slate-700/50 hover:bg-slate-700 text-slate-300 rounded-lg transition-colors-fast"
            >
              <Plus className="w-3 h-3 mr-1" /> 全部添加
            </button>
            <button
              onClick={clearMembers}
              className="flex items-center px-2.5 py-1 text-[10px] bg-slate-700/50 hover:bg-slate-700 text-slate-300 rounded-lg transition-colors-fast"
            >
              <X className="w-3 h-3 mr-1" /> 清空
            </button>
          </div>
        </div>
        {loadingMembers ? (
          <p className="text-sm text-slate-500 py-4 text-center">加载成员中...</p>
        ) : agents.length === 0 ? (
          <p className="text-sm text-slate-500 py-4 text-center">
            暂无 AI 代理，请先在「AI 代理」页面创建
          </p>
        ) : (
          <div className="space-y-2">
            {agents.map((agent) => {
              const isMember = draft.members.includes(agent.id);
              return (
                <label
                  key={agent.id}
                  className={`flex items-center gap-3 px-4 py-3 rounded-lg border cursor-pointer transition-colors-normal ${
                    isMember
                      ? "bg-emerald-500/5 border-emerald-500/30 hover:bg-emerald-500/10"
                      : "bg-slate-800/30 border-slate-700/50 hover:bg-slate-800/50"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={isMember}
                    onChange={() => toggleMember(agent.id)}
                    className="w-4 h-4 rounded border-slate-600 bg-slate-950 text-emerald-500 focus:ring-emerald-500/30"
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-white font-medium">
                        {agent.name}
                      </span>
                      <span className="text-[10px] text-slate-500 bg-slate-700/50 px-1.5 py-0.5 rounded">
                        {agent.tool}
                      </span>
                      {isMember && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                          已加入
                        </span>
                      )}
                    </div>
                  </div>
                  <span
                    className={`text-[10px] px-2 py-0.5 rounded-full ${
                      agent.status === "online" || agent.status === "busy"
                        ? "bg-emerald-500/10 text-emerald-400"
                        : agent.status === "paused"
                          ? "bg-amber-500/10 text-amber-400"
                          : "bg-slate-500/10 text-slate-400"
                    }`}
                  >
                    {agent.status === "online"
                      ? "在线"
                      : agent.status === "busy"
                        ? "忙碌"
                        : agent.status === "paused"
                          ? "暂停"
                          : "离线"}
                  </span>
                </label>
              );
            })}
          </div>
        )}
      </div>

      {/* 审查员 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-4 h-4 text-amber-400" />
          <h2 className="text-sm font-bold text-white">审查员</h2>
          <span className="text-[10px] text-slate-500 bg-slate-700/50 px-2 py-0.5 rounded-full">
            {draft.reviewers.length} 位
          </span>
        </div>

        {/* 自我审查警告条 */}
        {selfReviewAgents.length > 0 && (
          <div className="flex items-start gap-2 mb-4 p-3 bg-amber-500/5 border border-amber-500/20 rounded-lg">
            <AlertTriangle className="w-4 h-4 text-amber-400 shrink-0 mt-0.5" />
            <div className="text-xs text-amber-300">
              <span className="font-medium">自我审查风险：</span>
              以下代理同时为工程成员和审查员，可能导致自我审查：{" "}
              {selfReviewAgents
                .map((id) => agents.find((a) => a.id === id)?.name)
                .filter(Boolean)
                .join("、")}
            </div>
          </div>
        )}

        {agents.length === 0 ? (
          <p className="text-sm text-slate-500 py-4 text-center">
            暂无 AI 代理，请先在「AI 代理」页面创建
          </p>
        ) : (
          <div className="space-y-2">
            {agents.map((agent) => {
              const isReviewer = draft.reviewers.includes(agent.id);
              const isSelfReview =
                isReviewer && draft.members.includes(agent.id);
              return (
                <label
                  key={agent.id}
                  className={`flex items-center gap-3 px-4 py-3 rounded-lg border cursor-pointer transition-colors-normal ${
                    isReviewer
                      ? "bg-amber-500/5 border-amber-500/30 hover:bg-amber-500/10"
                      : "bg-slate-800/30 border-slate-700/50 hover:bg-slate-800/50"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={isReviewer}
                    onChange={() => toggleReviewer(agent.id)}
                    className="w-4 h-4 rounded border-slate-600 bg-slate-950 text-amber-500 focus:ring-amber-500/30"
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-white font-medium">
                        {agent.name}
                      </span>
                      {isSelfReview && (
                        <span className="inline-flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/20">
                          <AlertTriangle className="w-3 h-3" />
                          自我审查风险
                        </span>
                      )}
                    </div>
                  </div>
                </label>
              );
            })}
          </div>
        )}
      </div>

      {/* Git 凭据 */}
      <div className="bg-slate-800/40 backdrop-blur-sm border border-slate-700/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <GitBranch className="w-4 h-4 text-violet-400" />
            <h2 className="text-sm font-bold text-white">Git 仓库凭据</h2>
            <span className="text-[10px] text-slate-500 bg-slate-700/50 px-2 py-0.5 rounded-full">
              {gitCredentials.length} 个
            </span>
          </div>
          <button
            onClick={openNewCred}
            className="flex items-center px-2.5 py-1 text-[10px] bg-slate-700/50 hover:bg-slate-700 text-slate-300 rounded-lg transition-colors-fast"
          >
            <Plus className="w-3 h-3 mr-1" /> 添加凭据
          </button>
        </div>
        <p className="text-[10px] text-slate-500 mb-4">
          配置 Git 仓库地址和访问令牌，AI 代理执行任务时将自动克隆仓库并在任务分支上工作
        </p>

        {loadingCreds ? (
          <p className="text-sm text-slate-500 py-4 text-center">加载中...</p>
        ) : gitCredentials.length === 0 ? (
          <div className="py-8 text-center">
            <GitBranch className="w-8 h-8 text-slate-600 mx-auto mb-3" />
            <p className="text-sm text-slate-400 mb-1">尚未配置 Git 仓库</p>
            <p className="text-xs text-slate-500">添加凭据后，AI 代理将能自动同步代码</p>
          </div>
        ) : (
          <div className="space-y-2">
            {gitCredentials.map((cred) => (
              <div
                key={cred.id}
                className="flex items-center gap-3 px-4 py-3 rounded-lg border bg-slate-800/30 border-slate-700/50 hover:bg-slate-800/50 transition-colors-normal group card-hover"
              >
                <div className="w-8 h-8 rounded-lg bg-violet-500/10 border border-violet-500/20 flex items-center justify-center shrink-0">
                  <GitBranch className="w-4 h-4 text-violet-400" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-sm text-white font-medium truncate">
                    {cred.repo_url}
                  </div>
                  <div className="flex items-center gap-3 mt-0.5">
                    <span className="text-[10px] text-slate-500">
                      用户: {cred.username}
                    </span>
                    <span className="text-[10px] text-slate-500">
                      令牌: {cred.pat_masked || "****"}
                    </span>
                  </div>
                </div>
                <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button
                    onClick={() => openEditCred(cred)}
                    className="p-1.5 rounded-lg hover:bg-slate-700/50 text-slate-400 hover:text-white transition-colors-fast"
                    title="编辑"
                  >
                    <Pencil className="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* 新建项目弹窗 */}
      {showNewProject && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
          onClick={() => setShowNewProject(false)}
        >
          <div
            className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[520px] max-h-[80vh] overflow-y-auto shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-slate-800">
              <h3 className="text-lg font-bold text-white">新建工程</h3>
            </div>
            <div className="p-5 space-y-4">
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  工程名称 <span className="text-red-400">*</span>
                </label>
                <input
                  value={newProjectName}
                  onChange={(e) => setNewProjectName(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                  placeholder="如: 支付中心"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  Git 仓库地址 <span className="text-red-400">*</span>
                </label>
                <input
                  value={newRepoUrl}
                  onChange={(e) => setNewRepoUrl(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                  placeholder="https://github.com/org/repo.git"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  Git 用户名
                </label>
                <input
                  value={newGitUsername}
                  onChange={(e) => setNewGitUsername(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                  placeholder="git"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  Personal Access Token <span className="text-red-400">*</span>
                </label>
                <div className="relative">
                  <input
                    type={showNewPat ? "text" : "password"}
                    value={newPat}
                    onChange={(e) => setNewPat(e.target.value)}
                    className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500 pr-10"
                    placeholder="ghp_xxxxxxxxxxxx"
                  />
                  <button
                    type="button"
                    onClick={() => setShowNewPat((v) => !v)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                  >
                    {showNewPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>
            </div>
            <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
              <button
                onClick={() => setShowNewProject(false)}
                className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg"
              >
                取消
              </button>
              <button
                onClick={handleCreateProject}
                disabled={
                  !newProjectName.trim() ||
                  !newRepoUrl.trim() ||
                  !newPat.trim() ||
                  creatingProject
                }
                className="px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
              >
                {creatingProject ? "创建中..." : "创建"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 删除确认弹窗 */}
      {deletingProject && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
          onClick={() => setDeletingProject(null)}
        >
          <div
            className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[400px] shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-slate-800">
              <h3 className="text-lg font-bold text-white flex items-center gap-2">
                <AlertTriangle className="w-5 h-5 text-red-400" /> 确认删除
              </h3>
            </div>
            <div className="p-5">
              <p className="text-sm text-slate-300">
                确定要删除工程{" "}
                <span className="text-white font-medium">
                  {projects.find((p) => p.id === deletingProject)?.name}
                </span>{" "}
                吗？此操作不可撤销，工程下的所有任务数据将被删除。
              </p>
            </div>
            <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
              <button
                onClick={() => setDeletingProject(null)}
                className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg"
              >
                取消
              </button>
              <button
                onClick={handleDeleteProject}
                className="px-5 py-2 bg-red-600 hover:bg-red-500 text-white rounded-lg text-sm font-medium btn-press"
              >
                删除
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Git 凭据弹窗 */}
      {showCredModal && (
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
          onClick={() => setShowCredModal(false)}
        >
          <div
            className="modal-content bg-slate-900/95 backdrop-blur-xl border border-slate-700/60 rounded-2xl w-[520px] max-h-[80vh] overflow-y-auto shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-slate-800">
              <h3 className="text-lg font-bold text-white">
                {editingCred ? "编辑 Git 凭据" : "添加 Git 凭据"}
              </h3>
            </div>
            <div className="p-5 space-y-4">
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  仓库地址 <span className="text-red-400">*</span>
                </label>
                <input
                  value={credForm.repo_url}
                  onChange={(e) => setCredForm((f) => ({ ...f, repo_url: e.target.value }))}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                  placeholder="如: https://github.com/org/repo.git"
                />
                <p className="text-[10px] text-slate-500 mt-1.5">
                  支持 HTTP/HTTPS 协议的 Git 仓库地址
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  用户名
                </label>
                <input
                  value={credForm.username}
                  onChange={(e) => setCredForm((f) => ({ ...f, username: e.target.value }))}
                  className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500"
                  placeholder="git"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-slate-300 mb-2">
                  Personal Access Token{" "}
                  {!editingCred && <span className="text-red-400">*</span>}
                  {editingCred && (
                    <span className="text-slate-500 font-normal">（留空则不修改）</span>
                  )}
                </label>
                <div className="relative">
                  <input
                    type={showPat ? "text" : "password"}
                    value={credForm.pat}
                    onChange={(e) => setCredForm((f) => ({ ...f, pat: e.target.value }))}
                    className="w-full bg-slate-950 border border-slate-700 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:border-blue-500 pr-10"
                    placeholder={editingCred ? "输入新令牌以替换" : "ghp_xxxxxxxxxxxx"}
                  />
                  <button
                    type="button"
                    onClick={() => setShowPat((v) => !v)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                  >
                    {showPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
                <p className="text-[10px] text-slate-500 mt-1.5">
                  在 GitHub/GitLab 的 Settings → Developer Settings → Personal Access Tokens 中生成
                </p>
              </div>
            </div>
            <div className="p-5 border-t border-slate-800 flex justify-end gap-3">
              <button
                onClick={() => setShowCredModal(false)}
                className="px-4 py-2 text-sm bg-slate-800 hover:bg-slate-700 rounded-lg"
              >
                取消
              </button>
              <button
                onClick={handleSaveCred}
                disabled={!credForm.repo_url.trim() || savingCred || (!editingCred && !credForm.pat)}
                className="px-5 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
              >
                {savingCred ? "保存中..." : editingCred ? "更新" : "添加"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
