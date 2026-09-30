"use client";
// 项目设置页：成员/审查员管理及 Git 凭据配置。

import { useState, useEffect } from "react";
import {
  Settings,
  Save,
  AlertTriangle,
  Plus,
  X,
  Shield,
  Users,
  Trash2,
  GitBranch,
  Eye,
  EyeOff,
  Pencil,
} from "lucide-react";
import ModalPortal from "@/lib/ModalPortal";
import { useTranslations } from "next-intl";
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
}

interface ProjectDraft {
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
  const t = useTranslations("settings.project");
  const projects = useAppStore((s) => s.projects) as unknown as LocalProject[];
  const setProjects = useAppStore((s) => s.setProjects) as unknown as (
    v: LocalProject[] | ((prev: LocalProject[]) => LocalProject[])
  ) => void;
  const agents = useAppStore((s) => s.agents);
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
      setError(t("noWorkspace"));
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
      };
      setProjects((prev: LocalProject[]) => [...prev, newProject]);
      setSelectedProjectId(created.id);
      setShowNewProject(false);
      setNewProjectName("");
      setNewRepoUrl("");
      setNewGitUsername("git");
      setNewPat("");
    } catch (err) {
      setError(t("createFailed", { message: err instanceof Error ? err.message : t("unknownError") }));
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
      setError(t("deleteFailed", { message: err instanceof Error ? err.message : t("unknownError") }));
    }
  };

  if (!currentProject && projects.length === 0) {
    return (
      <div className="h-full flex flex-col items-center justify-center text-fg-subtle p-8">
        <Settings className="w-12 h-12 mb-4 text-fg-faint" />
        <p className="text-lg font-medium text-fg-muted mb-2">{t("noProjects")}</p>
        <p className="text-sm text-fg-faint mb-6">
          {t("noProjectsDesc")}
        </p>
        <button
          onClick={() => setShowNewProject(true)}
          className="flex items-center px-5 py-2.5 bg-primary-strong hover:bg-primary text-fg rounded-xl text-sm font-medium transition-colors-fast btn-press"
        >
          <Plus className="w-4 h-4 mr-2" /> {t("newProject")}
        </button>
        <div className="mt-8 w-full max-w-2xl">
          <EmptyStateGuide page="project" />
        </div>

        {showNewProject && (
          <ModalPortal>
          <div
            className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
            onClick={() => setShowNewProject(false)}
          >
            <div
              className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[520px] max-h-[80vh] overflow-y-auto shadow-2xl"
              onClick={(e) => e.stopPropagation()}
            >
              <div className="p-5 border-b border-line">
                <h3 className="text-lg font-bold text-fg">{t("newProject")}</h3>
              </div>
              <div className="p-5 space-y-4">
                <div>
                  <label className="block text-sm font-medium text-fg-muted mb-2">
                    {t("projectName")} <span className="text-danger-fg">*</span>
                  </label>
                  <input
                    value={newProjectName}
                    onChange={(e) => setNewProjectName(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                    placeholder={t("projectNamePlaceholder")}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-fg-muted mb-2">
                    {t("repoUrl")} <span className="text-danger-fg">*</span>
                  </label>
                  <input
                    value={newRepoUrl}
                    onChange={(e) => setNewRepoUrl(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                    placeholder="https://github.com/org/repo.git"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-fg-muted mb-2">
                    {t("gitUsername")}
                  </label>
                  <input
                    value={newGitUsername}
                    onChange={(e) => setNewGitUsername(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                    placeholder="git"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-fg-muted mb-2">
                    Personal Access Token <span className="text-danger-fg">*</span>
                  </label>
                  <div className="relative">
                    <input
                      type={showNewPat ? "text" : "password"}
                      value={newPat}
                      onChange={(e) => setNewPat(e.target.value)}
                      className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary pr-10"
                      placeholder="ghp_xxxxxxxxxxxx"
                    />
                    <button
                      type="button"
                      onClick={() => setShowNewPat((v) => !v)}
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg-muted"
                    >
                      {showNewPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
              </div>
              <div className="p-5 border-t border-line flex justify-end gap-3">
                <button
                  onClick={() => setShowNewProject(false)}
                  className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg"
                >
                  {t("cancel")}
                </button>
                <button
                  onClick={handleCreateProject}
                  disabled={
                    !newProjectName.trim() ||
                    !newRepoUrl.trim() ||
                    !newPat.trim() ||
                    creatingProject
                  }
                  className="px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
                >
                  {creatingProject ? t("creating") : t("create")}
                </button>
              </div>
            </div>
          </div>
          </ModalPortal>
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
      setError(t("noWorkspace"));
      return;
    }
    try {
      setError(null);

      // 1. 对比并同步项目成员
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

      // 2. 对比并同步项目审查员
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

      // 3. 从后端重新加载成员以同步状态
      await loadProjectMembers(selectedProjectId);

      setProjects((prev: LocalProject[]) =>
        prev.map((p) =>
          p.id === selectedProjectId
            ? { ...p, members: draft.members, reviewers: draft.reviewers }
            : p
        )
      );
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("saveFailed"));
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
          setError(t("patRequired"));
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
      setError(t("saveCredFailed", { message: err instanceof Error ? err.message : t("unknownError") }));
    } finally {
      setSavingCred(false);
    }
  };

  return (
    <div className="h-full flex flex-col p-8 overflow-y-auto page-enter">
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
        <div className="flex items-center gap-3">
          <button
            onClick={() => setShowNewProject(true)}
            className="flex items-center px-4 py-2.5 bg-surface-2 hover:bg-surface-3 text-fg-muted hover:text-fg rounded-xl text-sm font-medium transition-colors-fast border border-line-strong"
          >
            <Plus className="w-4 h-4 mr-2" /> {t("newProject")}
          </button>
          <button
            onClick={handleSave}
            className={`flex items-center px-5 py-2.5 rounded-xl text-sm font-medium transition-colors-normal btn-press ${
              saved
                ? "bg-success-strong text-fg"
                : "bg-primary-strong hover:bg-primary text-fg"
            }`}
          >
            <Save className="w-4 h-4 mr-2" />
            {saved ? t("saved") : t("saveSettings")}
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-danger/10 border border-danger/20 rounded-lg text-sm text-danger-fg">
          {error}
        </div>
      )}

      {/* 项目选择器 */}
      <div className="bg-surface-2/40 backdrop-blur-sm border border-line-strong/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center justify-between mb-2">
          <label className="block text-sm font-medium text-fg-muted">
            {t("currentProject")}
          </label>
          {currentProject && (
            <button
              onClick={() => setDeletingProject(selectedProjectId)}
              className="flex items-center px-2.5 py-1 text-[10px] text-danger-fg hover:text-danger-fg hover:bg-danger/10 rounded-lg transition-colors-fast"
            >
              <Trash2 className="w-3 h-3 mr-1" /> {t("deleteProject")}
            </button>
          )}
        </div>
        <select
          value={selectedProjectId}
          onChange={(e) => setSelectedProjectId(e.target.value)}
          className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
        >
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name?.trim() ? p.name : t("unnamed")}
            </option>
          ))}
        </select>
      </div>

      {/* 成员管理 */}
      <div className="bg-surface-2/40 backdrop-blur-sm border border-line-strong/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <Users className="w-4 h-4 text-success-fg" />
            <h2 className="text-sm font-bold text-fg">{t("members")}</h2>
            <span className="text-[10px] text-fg-faint bg-surface-3/50 px-2 py-0.5 rounded-full">
              {draft.members.length} / {agents.length}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={addAllMembers}
              className="flex items-center px-2.5 py-1 text-[10px] bg-surface-3/50 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors-fast"
            >
              <Plus className="w-3 h-3 mr-1" /> {t("addAll")}
            </button>
            <button
              onClick={clearMembers}
              className="flex items-center px-2.5 py-1 text-[10px] bg-surface-3/50 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors-fast"
            >
              <X className="w-3 h-3 mr-1" /> {t("clear")}
            </button>
          </div>
        </div>
        {loadingMembers ? (
          <p className="text-sm text-fg-faint py-4 text-center">{t("loadingMembers")}</p>
        ) : agents.length === 0 ? (
          <p className="text-sm text-fg-faint py-4 text-center">
            {t("noAgentsHint")}
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
                      ? "bg-success/5 border-success/30 hover:bg-success/10"
                      : "bg-surface-2/30 border-line-strong/50 hover:bg-surface-2/50"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={isMember}
                    onChange={() => toggleMember(agent.id)}
                    className="w-4 h-4 rounded border-line-strong bg-bg text-success focus:ring-success/30"
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-fg font-medium">
                        {agent.name}
                      </span>
                      <span className="text-[10px] text-fg-faint bg-surface-3/50 px-1.5 py-0.5 rounded">
                        {agent.tool}
                      </span>
                      {isMember && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-success/10 text-success-fg border border-success/20">
                          {t("joined")}
                        </span>
                      )}
                    </div>
                  </div>
                  <span
                    className={`text-[10px] px-2 py-0.5 rounded-full ${
                      agent.status === "online" || agent.status === "busy"
                        ? "bg-success/10 text-success-fg"
                        : agent.status === "paused"
                          ? "bg-warning/10 text-warning-fg"
                          : "bg-fg-faint/10 text-fg-subtle"
                    }`}
                  >
                    {agent.status === "online"
                      ? t("statusOnline")
                      : agent.status === "busy"
                        ? t("statusBusy")
                        : agent.status === "paused"
                          ? t("statusPaused")
                          : t("statusOffline")}
                  </span>
                </label>
              );
            })}
          </div>
        )}
      </div>

      {/* 审查员 */}
      <div className="bg-surface-2/40 backdrop-blur-sm border border-line-strong/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center gap-2 mb-4">
          <Shield className="w-4 h-4 text-warning-fg" />
          <h2 className="text-sm font-bold text-fg">{t("reviewers")}</h2>
          <span className="text-[10px] text-fg-faint bg-surface-3/50 px-2 py-0.5 rounded-full">
            {t("reviewerCount", { count: draft.reviewers.length })}
          </span>
        </div>

        {/* 自我审查警告条 */}
        {selfReviewAgents.length > 0 && (
          <div className="flex items-start gap-2 mb-4 p-3 bg-warning/5 border border-warning/20 rounded-lg">
            <AlertTriangle className="w-4 h-4 text-warning-fg shrink-0 mt-0.5" />
            <div className="text-xs text-warning-fg">
              <span className="font-medium">{t("selfReviewRisk")}</span>
              {t("selfReviewDesc")}{" "}
              {selfReviewAgents
                .map((id) => agents.find((a) => a.id === id)?.name)
                .filter(Boolean)
                .join("、")}
            </div>
          </div>
        )}

        {agents.length === 0 ? (
          <p className="text-sm text-fg-faint py-4 text-center">
            {t("noAgentsHint")}
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
                      ? "bg-warning/5 border-warning/30 hover:bg-warning/10"
                      : "bg-surface-2/30 border-line-strong/50 hover:bg-surface-2/50"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={isReviewer}
                    onChange={() => toggleReviewer(agent.id)}
                    className="w-4 h-4 rounded border-line-strong bg-bg text-warning focus:ring-warning/30"
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm text-fg font-medium">
                        {agent.name}
                      </span>
                      {isSelfReview && (
                        <span className="inline-flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full bg-warning/10 text-warning-fg border border-warning/20">
                          <AlertTriangle className="w-3 h-3" />
                          {t("selfReviewBadge")}
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
      <div className="bg-surface-2/40 backdrop-blur-sm border border-line-strong/60 rounded-xl p-5 mb-6 card-hover">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <GitBranch className="w-4 h-4 text-violet-400" />
            <h2 className="text-sm font-bold text-fg">{t("gitCredentials")}</h2>
            <span className="text-[10px] text-fg-faint bg-surface-3/50 px-2 py-0.5 rounded-full">
              {t("credCount", { count: gitCredentials.length })}
            </span>
          </div>
          <button
            onClick={openNewCred}
            className="flex items-center px-2.5 py-1 text-[10px] bg-surface-3/50 hover:bg-surface-3 text-fg-muted rounded-lg transition-colors-fast"
          >
            <Plus className="w-3 h-3 mr-1" /> {t("addCredential")}
          </button>
        </div>
        <p className="text-[10px] text-fg-faint mb-4">
          {t("gitCredHint")}
        </p>

        {loadingCreds ? (
          <p className="text-sm text-fg-faint py-4 text-center">{t("loading")}</p>
        ) : gitCredentials.length === 0 ? (
          <div className="py-8 text-center">
            <GitBranch className="w-8 h-8 text-fg-faint mx-auto mb-3" />
            <p className="text-sm text-fg-subtle mb-1">{t("noRepoYet")}</p>
            <p className="text-xs text-fg-faint">{t("noRepoDesc")}</p>
          </div>
        ) : (
          <div className="space-y-2">
            {gitCredentials.map((cred) => (
              <div
                key={cred.id}
                className="flex items-center gap-3 px-4 py-3 rounded-lg border bg-surface-2/30 border-line-strong/50 hover:bg-surface-2/50 transition-colors-normal group card-hover"
              >
                <div className="w-8 h-8 rounded-lg bg-violet-500/10 border border-violet-500/20 flex items-center justify-center shrink-0">
                  <GitBranch className="w-4 h-4 text-violet-400" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="text-sm text-fg font-medium truncate">
                    {cred.repo_url}
                  </div>
                  <div className="flex items-center gap-3 mt-0.5">
                    <span className="text-[10px] text-fg-faint">
                      {t("credUser", { username: cred.username })}
                    </span>
                    <span className="text-[10px] text-fg-faint">
                      {t("credToken", { token: cred.pat_masked || "****" })}
                    </span>
                  </div>
                </div>
                <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button
                    onClick={() => openEditCred(cred)}
                    className="p-1.5 rounded-lg hover:bg-surface-3/50 text-fg-subtle hover:text-fg transition-colors-fast"
                    title={t("edit")}
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
        <ModalPortal>
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
          onClick={() => setShowNewProject(false)}
        >
          <div
            className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[520px] max-h-[80vh] overflow-y-auto shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-line">
              <h3 className="text-lg font-bold text-fg">{t("newProject")}</h3>
            </div>
            <div className="p-5 space-y-4">
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("projectName")} <span className="text-danger-fg">*</span>
                </label>
                <input
                  value={newProjectName}
                  onChange={(e) => setNewProjectName(e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                  placeholder={t("projectNamePlaceholder")}
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("repoUrl")} <span className="text-danger-fg">*</span>
                </label>
                <input
                  value={newRepoUrl}
                  onChange={(e) => setNewRepoUrl(e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                  placeholder="https://github.com/org/repo.git"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("gitUsername")}
                </label>
                <input
                  value={newGitUsername}
                  onChange={(e) => setNewGitUsername(e.target.value)}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                  placeholder="git"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  Personal Access Token <span className="text-danger-fg">*</span>
                </label>
                <div className="relative">
                  <input
                    type={showNewPat ? "text" : "password"}
                    value={newPat}
                    onChange={(e) => setNewPat(e.target.value)}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary pr-10"
                    placeholder="ghp_xxxxxxxxxxxx"
                  />
                  <button
                    type="button"
                    onClick={() => setShowNewPat((v) => !v)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg-muted"
                  >
                    {showNewPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>
            </div>
            <div className="p-5 border-t border-line flex justify-end gap-3">
              <button
                onClick={() => setShowNewProject(false)}
                className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg"
              >
                {t("cancel")}
              </button>
              <button
                onClick={handleCreateProject}
                disabled={
                  !newProjectName.trim() ||
                  !newRepoUrl.trim() ||
                  !newPat.trim() ||
                  creatingProject
                }
                className="px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
              >
                {creatingProject ? t("creating") : t("create")}
              </button>
            </div>
          </div>
        </div>
        </ModalPortal>
      )}

      {/* 删除确认弹窗 */}
      {deletingProject && (
        <ModalPortal>
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
          onClick={() => setDeletingProject(null)}
        >
          <div
            className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[400px] shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-line">
              <h3 className="text-lg font-bold text-fg flex items-center gap-2">
                <AlertTriangle className="w-5 h-5 text-danger-fg" /> {t("deleteConfirmTitle")}
              </h3>
            </div>
            <div className="p-5">
              <p className="text-sm text-fg-muted">
                {t("deleteConfirmPrefix")}{" "}
                <span className="text-fg font-medium">
                  {projects.find((p) => p.id === deletingProject)?.name}
                </span>{" "}
                {t("deleteConfirmSuffix")}
              </p>
            </div>
            <div className="p-5 border-t border-line flex justify-end gap-3">
              <button
                onClick={() => setDeletingProject(null)}
                className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg"
              >
                {t("cancel")}
              </button>
              <button
                onClick={handleDeleteProject}
                className="px-5 py-2 bg-danger-strong hover:bg-danger text-fg rounded-lg text-sm font-medium btn-press"
              >
                {t("delete")}
              </button>
            </div>
          </div>
        </div>
        </ModalPortal>
      )}

      {/* Git 凭据弹窗 */}
      {showCredModal && (
        <ModalPortal>
        <div
          className="fixed inset-0 z-[60] flex items-center justify-center modal-overlay"
          onClick={() => setShowCredModal(false)}
        >
          <div
            className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[520px] max-h-[80vh] overflow-y-auto shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-5 border-b border-line">
              <h3 className="text-lg font-bold text-fg">
                {editingCred ? t("editCredTitle") : t("addCredTitle")}
              </h3>
            </div>
            <div className="p-5 space-y-4">
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("credRepoUrl")} <span className="text-danger-fg">*</span>
                </label>
                <input
                  value={credForm.repo_url}
                  onChange={(e) => setCredForm((f) => ({ ...f, repo_url: e.target.value }))}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                  placeholder="https://github.com/org/repo.git"
                />
                <p className="text-[10px] text-fg-faint mt-1.5">
                  {t("credRepoHint")}
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("credUsername")}
                </label>
                <input
                  value={credForm.username}
                  onChange={(e) => setCredForm((f) => ({ ...f, username: e.target.value }))}
                  className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
                  placeholder="git"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-fg-muted mb-2">
                  {t("patLabel")}{" "}
                  {!editingCred && <span className="text-danger-fg">*</span>}
                  {editingCred && (
                    <span className="text-fg-faint font-normal">{t("patUnchanged")}</span>
                  )}
                </label>
                <div className="relative">
                  <input
                    type={showPat ? "text" : "password"}
                    value={credForm.pat}
                    onChange={(e) => setCredForm((f) => ({ ...f, pat: e.target.value }))}
                    className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary pr-10"
                    placeholder={editingCred ? t("patNewPlaceholder") : "ghp_xxxxxxxxxxxx"}
                  />
                  <button
                    type="button"
                    onClick={() => setShowPat((v) => !v)}
                    className="absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg-muted"
                  >
                    {showPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
                <p className="text-[10px] text-fg-faint mt-1.5">
                  {t("patHelp")}
                </p>
              </div>
            </div>
            <div className="p-5 border-t border-line flex justify-end gap-3">
              <button
                onClick={() => setShowCredModal(false)}
                className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg"
              >
                {t("cancel")}
              </button>
              <button
                onClick={handleSaveCred}
                disabled={!credForm.repo_url.trim() || savingCred || (!editingCred && !credForm.pat)}
                className="px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press"
              >
                {savingCred ? t("saving") : editingCred ? t("update") : t("add")}
              </button>
            </div>
          </div>
        </div>
        </ModalPortal>
      )}
    </div>
  );
}
