"use client";
// 项目设置页：成员/审查员管理及 Git 凭据配置。

import { Fragment, useState, useEffect } from "react";
import {
  Settings,
  Save,
  AlertTriangle,
  Plus,
  X,
  Users,
  Trash2,
  GitBranch,
  Eye,
  EyeOff,
  Pencil,
  Loader2,
} from "lucide-react";
import { useTranslations } from "next-intl";
import PageHeader from "@/lib/ui/PageHeader";
import Button from "@/lib/ui/Button";
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

// 新建工程的内联表单卡:空状态与主视图共用,原位展开不用弹窗
function NewProjectCard({
  onSave,
  onClose,
  saving,
  error,
}: {
  onSave: (name: string, repoUrl: string) => void;
  onClose: () => void;
  saving: boolean;
  error: string | null;
}) {
  const t = useTranslations("settings.project");
  const [name, setName] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const valid = name.trim() !== "" && repoUrl.trim() !== "";

  return (
    <div className="bg-surface-2/40 border border-primary/30 rounded-xl backdrop-blur-sm p-5 shadow-sm transition-all">
      <div className="flex justify-between items-center pb-3 border-b border-line">
        <h3 className="text-sm font-bold text-fg flex items-center">
          <Plus className="w-4 h-4 mr-2 text-primary-fg" />
          {t("newProject")}
        </h3>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-surface-3 rounded-lg text-fg-subtle hover:text-fg"
          title={t("cancel")}
        >
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="space-y-3.5 mt-4">
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-2">
            {t("projectName")} <span className="text-danger-fg">*</span>
          </label>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
            placeholder={t("projectNamePlaceholder")}
            autoFocus
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-2">
            {t("repoUrl")} <span className="text-danger-fg">*</span>
          </label>
          <input
            value={repoUrl}
            onChange={(e) => setRepoUrl(e.target.value)}
            className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
            placeholder="https://github.com/org/repo.git"
          />
        </div>
        {error && (
          <p className="text-sm text-danger-fg bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
            {error}
          </p>
        )}
      </div>
      <div className="mt-4 pt-4 border-t border-line flex justify-end gap-3">
        <button
          onClick={onClose}
          className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
        >
          {t("cancel")}
        </button>
        <button
          onClick={() => valid && onSave(name.trim(), repoUrl.trim())}
          disabled={!valid || saving}
          className="px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press transition-colors-fast"
        >
          {saving ? t("creating") : t("create")}
        </button>
      </div>
    </div>
  );
}

// Git 凭据的内联表单卡:新建挂在凭据列表上方,编辑挂在对应凭据行下方
function CredFormCard({
  editingCred,
  editingAgentName,
  form,
  onFormChange,
  agentsWithoutCred,
  hasMemberAgents,
  showPat,
  onToggleShowPat,
  onSave,
  onCancel,
  saving,
  error,
}: {
  editingCred: GitCredential | null;
  editingAgentName: string;
  form: { agent_id: string; username: string; pat: string };
  onFormChange: (patch: Partial<{ agent_id: string; username: string; pat: string }>) => void;
  agentsWithoutCred: Array<{ id: string; name: string }>;
  hasMemberAgents: boolean;
  showPat: boolean;
  onToggleShowPat: () => void;
  onSave: () => void;
  onCancel: () => void;
  saving: boolean;
  error: string | null;
}) {
  const t = useTranslations("settings.project");

  return (
    <div className="bg-surface-2/40 border border-primary/30 rounded-xl backdrop-blur-sm p-5 mt-2 shadow-sm transition-all">
      <div className="flex justify-between items-center pb-3 border-b border-line">
        <h3 className="text-sm font-bold text-fg flex items-center">
          <GitBranch className="w-4 h-4 mr-2 text-accent-fg" />
          {editingCred ? t("editCredTitle") : t("addCredTitle")}
        </h3>
        <button
          onClick={onCancel}
          className="p-1.5 hover:bg-surface-3 rounded-lg text-fg-subtle hover:text-fg"
          title={t("cancel")}
        >
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="space-y-3.5 mt-4">
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-2">
            {t("credAgent")} <span className="text-danger-fg">*</span>
          </label>
          {editingCred ? (
            <input
              value={editingAgentName}
              disabled
              className="w-full bg-surface-2 border border-line-strong rounded-lg px-4 py-2.5 text-fg-muted cursor-not-allowed"
            />
          ) : !hasMemberAgents ? (
            <p className="text-xs text-fg-faint py-2">{t("noMemberAgents")}</p>
          ) : agentsWithoutCred.length === 0 ? (
            <p className="text-xs text-fg-faint py-2">{t("allAgentsConfigured")}</p>
          ) : (
            <select
              value={form.agent_id}
              onChange={(e) => onFormChange({ agent_id: e.target.value })}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
            >
              <option value="">{t("credAgentPlaceholder")}</option>
              {agentsWithoutCred.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          )}
          <p className="text-[10px] text-fg-faint mt-1.5">{t("credAgentHint")}</p>
        </div>
        <div>
          <label className="block text-sm font-medium text-fg-muted mb-2">
            {t("credUsername")}
          </label>
          <input
            value={form.username}
            onChange={(e) => onFormChange({ username: e.target.value })}
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
              value={form.pat}
              onChange={(e) => onFormChange({ pat: e.target.value })}
              className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary pr-10"
              placeholder={editingCred ? t("patNewPlaceholder") : "ghp_xxxxxxxxxxxx"}
            />
            <button
              type="button"
              onClick={onToggleShowPat}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-fg-faint hover:text-fg-muted"
            >
              {showPat ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
            </button>
          </div>
          <p className="text-[10px] text-fg-faint mt-1.5">{t("patHelp")}</p>
        </div>
        {error && (
          <p className="text-sm text-danger-fg bg-danger/10 border border-danger/20 rounded-lg px-3 py-2">
            {error}
          </p>
        )}
      </div>
      <div className="mt-4 pt-4 border-t border-line flex justify-end gap-3">
        <button
          onClick={onCancel}
          className="px-4 py-2 text-sm bg-surface-2 hover:bg-surface-3 rounded-lg transition-colors-fast"
        >
          {t("cancel")}
        </button>
        <button
          onClick={onSave}
          disabled={!form.agent_id || saving || (!editingCred && !form.pat)}
          className="px-5 py-2 bg-primary-strong hover:bg-primary text-fg rounded-lg text-sm font-medium disabled:opacity-50 btn-press transition-colors-fast"
        >
          {saving ? t("saving") : editingCred ? t("update") : t("add")}
        </button>
      </div>
    </div>
  );
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
  const [error, setError] = useState<string | null>(null);
  const [showNewProject, setShowNewProject] = useState(false);
  const [creatingProject, setCreatingProject] = useState(false);
  const [createProjectError, setCreateProjectError] = useState<string | null>(null);
  const [deletingProject, setDeletingProject] = useState<string | null>(null);
  const [deletingProjectBusy, setDeletingProjectBusy] = useState(false);

  // Git 凭据状态
  const [gitCredentials, setGitCredentials] = useState<GitCredential[]>([]);
  const [loadingCreds, setLoadingCreds] = useState(false);
  const [newCredOpen, setNewCredOpen] = useState(false);
  const [editingCred, setEditingCred] = useState<GitCredential | null>(null);
  const [credForm, setCredForm] = useState({ agent_id: "", username: "git", pat: "" });
  const [savingCred, setSavingCred] = useState(false);
  const [showPat, setShowPat] = useState(false);
  const [credError, setCredError] = useState<string | null>(null);

  // 服务端的项目成员/审查员记录，用于基于差异的保存
  const [serverMembers, setServerMembers] = useState<ProjectMemberRecord[]>([]);
  const [serverReviewers, setServerReviewers] = useState<ProjectReviewerRecord[]>([]);
  const [loadingMembers, setLoadingMembers] = useState(false);

  // 开发人员卡保存的状态(开发+审查两组一起提交,错误显示在卡内)
  const [savingMembers, setSavingMembers] = useState(false);
  const [membersSaved, setMembersSaved] = useState(false);
  const [memberSaveError, setMemberSaveError] = useState<string | null>(null);

  const currentProject = projects.find((p) => p.id === selectedProjectId);

  useEffect(() => {
    if (currentProject) {
      setDraft({
        members: [...(currentProject.members || [])],
        reviewers: [...(currentProject.reviewers || [])],
      });
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

  // 新建工程表单的打开/收起;错误独立于页面共享 error 条,保证空状态分支也能看到
  const openNewProjectForm = () => {
    setCreateProjectError(null);
    setShowNewProject(true);
  };

  const closeNewProjectForm = () => {
    setShowNewProject(false);
    setCreateProjectError(null);
  };

  const handleCreateProject = async (name: string, repoUrl: string) => {
    if (!name.trim() || !repoUrl.trim()) return;
    if (!workspaceId) {
      setCreateProjectError(t("noWorkspace"));
      return;
    }
    try {
      setCreatingProject(true);
      setCreateProjectError(null);
      const created = (await api.createProject(workspaceId, {
        name: name.trim(),
        status: "active",
        repo_url: repoUrl.trim(),
      })) as { id: string; name?: string };
      const newProject: LocalProject = {
        id: created.id,
        name: created.name || name.trim(),
        members: [],
        reviewers: [],
      };
      setProjects((prev: LocalProject[]) => [...prev, newProject]);
      setSelectedProjectId(created.id);
      setShowNewProject(false);
    } catch (err) {
      setCreateProjectError(t("createFailed", { message: err instanceof Error ? err.message : t("unknownError") }));
    } finally {
      setCreatingProject(false);
    }
  };

  const handleDeleteProject = async () => {
    if (!deletingProject || !workspaceId) return;
    try {
      setDeletingProjectBusy(true);
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
    } finally {
      setDeletingProjectBusy(false);
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
          onClick={openNewProjectForm}
          className="flex items-center px-5 py-2.5 bg-primary-strong hover:bg-primary text-fg rounded-xl text-sm font-medium transition-colors-fast btn-press"
        >
          <Plus className="w-4 h-4 mr-2" /> {t("newProject")}
        </button>

        {showNewProject && (
          <div className="mt-6 w-full max-w-xl text-left">
            <NewProjectCard
              onSave={handleCreateProject}
              onClose={closeNewProjectForm}
              saving={creatingProject}
              error={createProjectError}
            />
          </div>
        )}

        <div className="mt-8 w-full max-w-2xl">
          <EmptyStateGuide page="project" />
        </div>
      </div>
    );
  }

  if (!draft || !currentProject) {
    return null;
  }

  const selfReviewAgents = draft.members.filter((id) =>
    draft.reviewers.includes(id)
  );

  // 对比草稿与服务端记录,把工程成员差异同步到服务端(新增勾选加入,取消勾选移除)
  const syncProjectMembers = async () => {
    const currentAgentMemberIds = serverMembers
      .filter((m) => m.member_type === "agent" && m.agent_id)
      .map((m) => m.agent_id!);
    const toAddMembers = draft!.members.filter(
      (id) => !currentAgentMemberIds.includes(id)
    );
    const toRemoveMembers = serverMembers
      .filter((m) => m.member_type === "agent" && m.agent_id && !draft!.members.includes(m.agent_id))
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
  };

  // 对比草稿与服务端记录,把审查员差异同步到服务端
  const syncProjectReviewers = async () => {
    const currentAgentReviewerIds = serverReviewers
      .filter((r) => r.member_type === "agent" && r.agent_id)
      .map((r) => r.agent_id!);
    const toAddReviewers = draft!.reviewers.filter(
      (id) => !currentAgentReviewerIds.includes(id)
    );
    const toRemoveReviewers = serverReviewers
      .filter((r) => r.member_type === "agent" && r.agent_id && !draft!.reviewers.includes(r.agent_id))
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
  };

  // 开发人员卡保存:一次同步开发人员与审查员两组,错误显示在卡内
  const handleSaveMembers = async () => {
    if (!workspaceId || !draft) {
      setMemberSaveError(t("noWorkspace"));
      return;
    }
    try {
      setSavingMembers(true);
      setMemberSaveError(null);
      await syncProjectMembers();
      await syncProjectReviewers();
      // 两组都已提交,分别拉取各自列表对齐基线(不走全量重载)
      const [membersRes, reviewersRes] = await Promise.all([
        api.listProjectMembers(workspaceId, selectedProjectId).catch(() => []),
        api.listProjectReviewers(workspaceId, selectedProjectId).catch(() => []),
      ]);
      const freshMembers = (Array.isArray(membersRes) ? membersRes : []) as ProjectMemberRecord[];
      const freshReviewers = (Array.isArray(reviewersRes) ? reviewersRes : []) as ProjectReviewerRecord[];
      setServerMembers(freshMembers);
      setServerReviewers(freshReviewers);
      const freshMemberIds = freshMembers
        .filter((m) => m.member_type === "agent" && m.agent_id)
        .map((m) => m.agent_id!);
      const freshReviewerIds = freshReviewers
        .filter((r) => r.member_type === "agent" && r.agent_id)
        .map((r) => r.agent_id!);
      setDraft((prev) =>
        prev ? { ...prev, members: freshMemberIds, reviewers: freshReviewerIds } : prev
      );
      setProjects((prev: LocalProject[]) =>
        prev.map((p) =>
          p.id === selectedProjectId
            ? { ...p, members: freshMemberIds, reviewers: freshReviewerIds }
            : p
        )
      );
      setMembersSaved(true);
      setTimeout(() => setMembersSaved(false), 2000);
    } catch (err) {
      setMemberSaveError(err instanceof Error ? err.message : t("saveFailed"));
    } finally {
      setSavingMembers(false);
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
  };

  const addAllMembers = () => {
    const allIds = agents.map((a) => a.id);
    setDraft((prev) => (prev ? { ...prev, members: allIds } : prev));
  };

  const clearMembers = () => {
    setDraft((prev) => (prev ? { ...prev, members: [] } : prev));
  };

  // ── Git 凭据处理器 ──
  // 可绑定凭据的 Agent：工程成员中尚未配置凭据的 Agent（每个 Agent 一张专属凭据）
  const memberAgents = agents.filter((a) => draft?.members.includes(a.id));
  const agentsWithoutCred = memberAgents.filter(
    (a) => !gitCredentials.some((c) => c.agent_id === a.id)
  );

  // 新建与编辑凭据的内联表单互斥;错误显示在表单卡内
  const openNewCred = () => {
    setEditingCred(null);
    setCredForm({ agent_id: "", username: "git", pat: "" });
    setShowPat(false);
    setCredError(null);
    setNewCredOpen(true);
  };

  const openEditCred = (cred: GitCredential) => {
    setNewCredOpen(false);
    setEditingCred(cred);
    setCredForm({ agent_id: cred.agent_id, username: cred.username, pat: "" });
    setShowPat(false);
    setCredError(null);
  };

  const closeCredForm = () => {
    setNewCredOpen(false);
    setEditingCred(null);
    setCredError(null);
  };

  const handleSaveCred = async () => {
    if (!credForm.agent_id) return;
    if (!editingCred && !credForm.pat) return;
    try {
      setSavingCred(true);
      setCredError(null);
      if (editingCred) {
        // 绑定关系不可变更，只更新用户名和令牌
        const data: Record<string, unknown> = {
          username: credForm.username.trim() || "git",
        };
        if (credForm.pat) data.pat = credForm.pat;
        await api.updateGitCredential(selectedProjectId, editingCred.id, data);
      } else {
        await api.createGitCredential(selectedProjectId, {
          agent_id: credForm.agent_id,
          username: credForm.username.trim() || "git",
          pat: credForm.pat,
        });
      }
      // 刷新凭据
      const res = await api.listGitCredentials(selectedProjectId);
      const creds = ((res as Record<string, unknown>)?.credentials ?? res) as GitCredential[];
      setGitCredentials(Array.isArray(creds) ? creds : []);
      setNewCredOpen(false);
      setEditingCred(null);
    } catch (err) {
      setCredError(t("saveCredFailed", { message: err instanceof Error ? err.message : t("unknownError") }));
    } finally {
      setSavingCred(false);
    }
  };

  return (
    <div className="h-full flex flex-col overflow-y-auto page-enter">
      <PageHeader
        title={t("title")}
        description={t("subtitle")}
        actions={
          <Button variant="secondary" onClick={openNewProjectForm}>
            <Plus className="w-4 h-4 mr-1.5" /> {t("newProject")}
          </Button>
        }
      />
      <div className="px-6 pb-8">

      {showNewProject && (
        <div className="mb-6">
          <NewProjectCard
            onSave={handleCreateProject}
            onClose={closeNewProjectForm}
            saving={creatingProject}
            error={createProjectError}
          />
        </div>
      )}

      {error && (
        <div className="mb-4 p-3 bg-danger/10 border border-danger/20 rounded-lg text-sm text-danger-fg">
          {error}
        </div>
      )}

      {/* 项目选择器 */}
      <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5 mb-6">
        <div className="flex items-center justify-between mb-2">
          <label className="block text-sm font-medium text-fg-muted">
            {t("currentProject")}
          </label>
          {currentProject &&
            (deletingProject ? (
              <div className="flex items-center gap-2 flex-wrap justify-end">
                <span className="flex items-center gap-1.5 text-xs text-danger-fg leading-relaxed">
                  <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
                  {t("deleteConfirmPrefix")}{" "}
                  <span className="font-medium">
                    {projects.find((p) => p.id === deletingProject)?.name}
                  </span>{" "}
                  {t("deleteConfirmSuffix")}
                </span>
                <div className="flex items-center gap-2">
                  <button
                    onClick={() => setDeletingProject(null)}
                    disabled={deletingProjectBusy}
                    className="px-3 py-1.5 text-xs font-medium bg-surface-3/50 border border-line-strong/50 rounded-md hover:bg-surface-3 transition-colors-fast disabled:opacity-50"
                  >
                    {t("cancel")}
                  </button>
                  <button
                    onClick={handleDeleteProject}
                    disabled={deletingProjectBusy}
                    className="btn-press flex items-center px-3 py-1.5 text-xs font-medium bg-danger-strong hover:bg-danger text-fg rounded-md transition-colors-fast disabled:opacity-50"
                  >
                    {deletingProjectBusy ? (
                      <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                    ) : (
                      <Trash2 className="w-3.5 h-3.5 mr-1" />
                    )}
                    {t("delete")}
                  </button>
                </div>
              </div>
            ) : (
              <button
                onClick={() => setDeletingProject(selectedProjectId)}
                className="flex items-center px-2.5 py-1 text-[10px] text-danger-fg hover:text-danger-fg hover:bg-danger/10 rounded-lg transition-colors-fast"
              >
                <Trash2 className="w-3 h-3 mr-1" /> {t("deleteProject")}
              </button>
            ))}
        </div>
        <select
          value={selectedProjectId}
          onChange={(e) => {
            // 切换工程后原工程的删除确认、凭据表单与成员保存态不再适用,一并清理
            setDeletingProject(null);
            closeCredForm();
            setMemberSaveError(null);
            setMembersSaved(false);
            setSelectedProjectId(e.target.value);
          }}
          className="w-full bg-bg border border-line-strong rounded-lg px-4 py-2.5 text-fg focus:outline-none focus:border-primary"
        >
          {projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name?.trim() ? p.name : t("unnamed")}
            </option>
          ))}
        </select>
      </div>

      {/* 开发人员与审查员(单列表双勾选) */}
      <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5 mb-6">
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
            <button
              onClick={handleSaveMembers}
              disabled={savingMembers}
              className={`btn-press flex items-center px-3 py-1 text-[10px] font-medium rounded-lg transition-colors-fast disabled:opacity-50 ${
                membersSaved
                  ? "bg-success-strong text-fg"
                  : "bg-primary-strong hover:bg-primary text-fg"
              }`}
            >
              {savingMembers ? (
                <Loader2 className="w-3 h-3 mr-1 animate-spin" />
              ) : (
                <Save className="w-3 h-3 mr-1" />
              )}
              {membersSaved ? t("saved") : t("saveMembers")}
            </button>
          </div>
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

        {memberSaveError && (
          <div className="mb-4 p-3 bg-danger/10 border border-danger/20 rounded-lg text-sm text-danger-fg">
            {memberSaveError}
          </div>
        )}
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
              const isReviewer = draft.reviewers.includes(agent.id);
              const isSelfReview = isMember && isReviewer;
              return (
                <div
                  key={agent.id}
                  className={`flex items-center gap-3 px-4 py-3 rounded-lg border transition-colors-normal ${
                    isMember
                      ? "bg-success/5 border-success/30 hover:bg-success/10"
                      : "bg-surface-2/30 border-line-strong/50 hover:bg-surface-2/50"
                  }`}
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm text-fg font-medium">
                        {agent.name}
                      </span>
                      <span className="text-[10px] text-fg-faint bg-surface-3/50 px-1.5 py-0.5 rounded">
                        {agent.tool}
                      </span>
                      {isSelfReview && (
                        <span className="inline-flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full bg-warning/10 text-warning-fg border border-warning/20">
                          <AlertTriangle className="w-3 h-3" />
                          {t("selfReviewBadge")}
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
                  <label className="flex items-center gap-1.5 cursor-pointer shrink-0">
                    <input
                      type="checkbox"
                      checked={isMember}
                      onChange={() => toggleMember(agent.id)}
                      className="w-4 h-4 rounded border-line-strong bg-bg text-success focus:ring-success/30"
                    />
                    <span
                      className={`text-xs font-medium ${
                        isMember ? "text-success-fg" : "text-fg-subtle"
                      }`}
                    >
                      {t("roleDev")}
                    </span>
                  </label>
                  <label className="flex items-center gap-1.5 cursor-pointer shrink-0">
                    <input
                      type="checkbox"
                      checked={isReviewer}
                      onChange={() => toggleReviewer(agent.id)}
                      className="w-4 h-4 rounded border-line-strong bg-bg text-warning focus:ring-warning/30"
                    />
                    <span
                      className={`text-xs font-medium ${
                        isReviewer ? "text-warning-fg" : "text-fg-subtle"
                      }`}
                    >
                      {t("roleReviewer")}
                    </span>
                  </label>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Git 凭据 */}
      <div className="bg-surface-2/40 border border-line rounded-[var(--radius-card)] p-5 mb-6">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-2">
            <GitBranch className="w-4 h-4 text-primary-fg" />
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

        {newCredOpen && (
          <div className="mb-4">
            <CredFormCard
              editingCred={null}
              editingAgentName=""
              form={credForm}
              onFormChange={(patch) => setCredForm((f) => ({ ...f, ...patch }))}
              agentsWithoutCred={agentsWithoutCred}
              hasMemberAgents={memberAgents.length > 0}
              showPat={showPat}
              onToggleShowPat={() => setShowPat((v) => !v)}
              onSave={handleSaveCred}
              onCancel={closeCredForm}
              saving={savingCred}
              error={credError}
            />
          </div>
        )}

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
            {gitCredentials.map((cred) => {
              const agentName =
                cred.agent_name ||
                agents.find((a) => a.id === cred.agent_id)?.name ||
                cred.agent_id;
              const editingThis = editingCred?.id === cred.id;
              return (
                <Fragment key={cred.id}>
                  <div className="flex items-center gap-3 px-4 py-3 rounded-lg border bg-surface-2/30 border-line-strong/50 hover:bg-surface-2/50 transition-colors-normal group card-hover">
                    <div className="w-8 h-8 rounded-lg bg-primary/10 border border-primary/20 flex items-center justify-center shrink-0">
                      <GitBranch className="w-4 h-4 text-primary-fg" />
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 text-sm text-fg font-medium">
                        <span className="truncate">{agentName}</span>
                        <span className="text-[10px] text-fg-faint bg-surface-3/50 px-1.5 py-0.5 rounded shrink-0">
                          {t("credOwnerBadge")}
                        </span>
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
                  {editingThis && (
                    <CredFormCard
                      editingCred={editingCred}
                      editingAgentName={
                        editingCred.agent_name ||
                        agents.find((a) => a.id === editingCred.agent_id)?.name ||
                        editingCred.agent_id
                      }
                      form={credForm}
                      onFormChange={(patch) => setCredForm((f) => ({ ...f, ...patch }))}
                      agentsWithoutCred={agentsWithoutCred}
                      hasMemberAgents={memberAgents.length > 0}
                      showPat={showPat}
                      onToggleShowPat={() => setShowPat((v) => !v)}
                      onSave={handleSaveCred}
                      onCancel={closeCredForm}
                      saving={savingCred}
                      error={credError}
                    />
                  )}
                </Fragment>
              );
            })}
          </div>
        )}
      </div>

      </div>
    </div>
  );
}
