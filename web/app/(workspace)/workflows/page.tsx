"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import {
  Plus,
  ArrowRight,
  Trash2,
  Rocket,
  Edit2,
  Loader2,
  Workflow,
  LayoutTemplate,
  Database,
  Eye,
  X,
  BarChart3,
  Clock,
} from "lucide-react";
import ModalPortal from "@/lib/ModalPortal";
import { useAppStore } from "@/lib/store";
import { useTranslations } from "next-intl";
import { usePermission } from "@/lib/use-permission";
import api from "@/lib/api";
import type {
  MappedTemplate,
  Project,
} from "@/lib/types";

// ==========================================
// 运行模板（发起任务）
// ==========================================
interface TemplateRunViewProps {
  template: MappedTemplate;
  onCancel: () => void;
  onTaskCreated: (
    template: MappedTemplate,
    title: string,
    desc: string,
    taskType: string,
    priority: string,
    selectedProject: string,
    dueDate: string,
    labelList: string[],
    constraints: string
  ) => Promise<boolean | void>;
  projects: Project[];
}

function TemplateRunView({
  template,
  onCancel,
  onTaskCreated,
  projects,
}: TemplateRunViewProps) {
  const t = useTranslations("workflows.run");
  const [title, setTitle] = useState("");
  const [desc, setDesc] = useState("");
  const [constraints, setConstraints] = useState("");
  const [taskType, setTaskType] = useState("story");
  const [priority, setPriority] = useState("high");
  const [selectedProject, setSelectedProject] = useState(
    projects?.[0]?.id || ""
  );
  const [dueDate, setDueDate] = useState("");
  const [labels, setLabels] = useState("");
  const [isStarting, setIsStarting] = useState(false);

  const handleStart = async () => {
    if (!selectedProject) {
      return;
    }
    setIsStarting(true);
    try {
      const labelList = labels
        .split(",")
        .map((l) => l.trim())
        .filter(Boolean);
      const ok = await onTaskCreated(
        template,
        title,
        desc,
        taskType,
        priority,
        selectedProject,
        dueDate,
        labelList,
        constraints
      );
      if (!ok) {
        setIsStarting(false);
      }
    } catch {
      setIsStarting(false);
    }
  };

  return (
    <div className="page-enter flex flex-col h-full relative bg-bg">
      <div className="p-6 border-b border-line-strong/50 bg-surface/50 backdrop-blur-md z-10 flex justify-between items-center shrink-0">
        <div className="flex items-center">
          <button
            onClick={onCancel}
            className="p-2 mr-4 text-fg-subtle hover:text-fg hover:bg-surface-2 rounded-lg transition-colors-fast"
          >
            <ArrowRight className="w-5 h-5 rotate-180" />
          </button>
          <div>
            <h1 className="text-2xl font-bold text-fg mb-1 flex items-center">
              {t("title")}{" "}
              <span className="ml-3 text-sm text-accent-fg bg-accent/10 border border-accent/20 px-2 py-0.5 rounded-full font-normal">
                {t("basedOn", { name: template.name })}
              </span>
            </h1>
            <p className="text-xs text-fg-subtle">
              {t("subtitle")}
            </p>
          </div>
        </div>
        <button
          onClick={handleStart}
          disabled={isStarting || !title || !desc || !selectedProject}
          className={`flex items-center px-8 py-2.5 rounded-lg font-bold transition-colors-normal shadow-lg btn-press ${
            isStarting
              ? "bg-accent/50 text-fg cursor-wait"
              : "bg-accent-strong hover:bg-accent text-fg shadow-accent/20"
          } disabled:opacity-50 disabled:cursor-not-allowed`}
        >
          {isStarting ? (
            <Loader2 className="w-5 h-5 mr-2 animate-spin" />
          ) : (
            <Rocket className="w-5 h-5 mr-2" />
          )}
          {isStarting ? t("triggering") : t("triggerButton")}
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-8 flex justify-center">
        <div className="w-full max-w-3xl">
          {/* 任务表单 */}
          <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-6">
            <h3 className="text-lg font-bold text-fg flex items-center mb-6">
              <div className="w-8 h-8 rounded-full bg-accent-strong/50 border border-accent text-accent-fg flex items-center justify-center mr-3 text-sm">
                1
              </div>
              {t("stepOneHeader", { title: t("stepOneTitle") })}
            </h3>

            <div className="space-y-5">
              <div>
                <label className="block text-sm font-semibold text-fg-muted mb-2">
                  {t("titleLabel")} <span className="text-danger-fg">*</span>
                </label>
                <input
                  type="text"
                  value={title}
                  onChange={(e) => setTitle(e.target.value)}
                  className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg text-lg focus:outline-none focus:border-accent"
                  placeholder={t("titlePlaceholder")}
                />
              </div>

              <div className="grid grid-cols-3 gap-4">
                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("projectLabel")} <span className="text-danger-fg">*</span>
                  </label>
                  <select
                    value={selectedProject}
                    onChange={(e) => setSelectedProject(e.target.value)}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-accent"
                  >
                    {!projects?.length && (
                      <option value="">
                        {t("noProjects")}
                      </option>
                    )}
                    {projects?.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.name}
                      </option>
                    ))}
                  </select>
                  {!projects?.length && (
                    <p className="text-[10px] text-warning-fg mt-1">
                      {t("goCreateProject")}
                    </p>
                  )}
                </div>
                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("taskTypeLabel")}
                  </label>
                  <select
                    value={taskType}
                    onChange={(e) => setTaskType(e.target.value)}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-accent"
                  >
                    <option value="story">
                      {"\uD83D\uDCD6"} {t("typeStory")}
                    </option>
                    <option value="bug">{"\uD83D\uDC1B"} {t("typeBug")}</option>
                    <option value="task">
                      {"\uD83D\uDCCB"} {t("typeTask")}
                    </option>
                  </select>
                </div>
                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("priorityLabel")}
                  </label>
                  <select
                    value={priority}
                    onChange={(e) => setPriority(e.target.value)}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg focus:outline-none focus:border-accent"
                  >
                    <option value="urgent">
                      {"\uD83D\uDD34"} {t("prioUrgent")}
                    </option>
                    <option value="high">
                      {"\uD83D\uDFE0"} {t("prioHigh")}
                    </option>
                    <option value="medium">
                      {"\uD83D\uDFE1"} {t("prioMedium")}
                    </option>
                    <option value="low">{"\uD83D\uDFE2"} {t("prioLow")}</option>
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("dueDateLabel")}
                  </label>
                  <input
                    type="date"
                    value={dueDate}
                    onChange={(e) => setDueDate(e.target.value)}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg text-sm focus:outline-none focus:border-accent"
                  />
                </div>
                <div>
                  <label className="block text-sm font-semibold text-fg-muted mb-2">
                    {t("labelsLabel")}
                  </label>
                  <input
                    type="text"
                    value={labels}
                    onChange={(e) => setLabels(e.target.value)}
                    className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg text-sm focus:outline-none focus:border-accent"
                    placeholder={t("labelsPlaceholder")}
                  />
                  <p className="text-[10px] text-fg-faint mt-1">
                    {t("labelsHint")}
                  </p>
                </div>
              </div>

              <div>
                <label className="block text-sm font-semibold text-fg-muted mb-2">
                  {t("constraintsLabel")}
                </label>
                <textarea
                  value={constraints}
                  onChange={(e) => setConstraints(e.target.value)}
                  rows={3}
                  className="w-full bg-surface border border-warning/30 rounded-lg px-4 py-3 text-warning-fg text-sm focus:outline-none focus:border-warning resize-none font-mono"
                  placeholder={t("constraintsPlaceholder")}
                />
                <p className="text-[10px] text-fg-faint mt-1">
                  {t("constraintsHint")}
                </p>
              </div>

              <div>
                <label className="block text-sm font-semibold text-fg-muted mb-2">
                  {t("descRequiredLabel")}<span className="text-danger-fg">*</span>
                </label>
                <textarea
                  value={desc}
                  onChange={(e) => setDesc(e.target.value)}
                  rows={10}
                  className="w-full bg-surface border border-line-strong rounded-lg px-4 py-3 text-fg-muted text-sm focus:outline-none focus:border-accent resize-none font-mono"
                  placeholder={t("descPlaceholder")}
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================

// ==========================================
// 模板使用统计弹窗
// ==========================================
interface TemplateStatsDialogProps {
  template: MappedTemplate;
  onClose: () => void;
}

function TemplateStatsDialog({
  template,
  onClose,
}: TemplateStatsDialogProps) {
  const t = useTranslations("workflows.stats");
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center modal-overlay"
      onClick={onClose}
    >
      <div
        className="modal-content bg-surface/95 backdrop-blur-xl border border-line-strong/60 rounded-2xl w-[680px] max-h-[80vh] overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-line flex justify-between items-center sticky top-0 bg-surface z-10">
          <h2 className="text-lg font-bold text-fg flex items-center">
            <BarChart3 className="w-5 h-5 mr-2 text-primary-fg" />{" "}
            {t("title")}
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-surface-3 rounded-lg"
          >
            <X className="w-5 h-5 text-fg-subtle" />
          </button>
        </div>
        <div className="p-6 space-y-6">
          <div className="flex items-center gap-4">
            <div className="w-12 h-12 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center">
              <Workflow className="w-6 h-6 text-primary-fg" />
            </div>
            <div>
              <h3 className="text-xl font-bold text-fg">
                {template.name}
              </h3>
              <p className="text-xs text-fg-subtle">
                {template.description?.substring(0, 60)}
              </p>
            </div>
          </div>

          <div className="grid grid-cols-3 gap-4">
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4 text-center">
              <div className="text-xs text-fg-faint mb-1">{t("refCount")}</div>
              <div className="text-2xl font-bold text-fg">-</div>
              <div className="text-[10px] text-fg-faint mt-1">{t("tasksUnit")}</div>
            </div>
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4 text-center">
              <div className="text-xs text-fg-faint mb-1">{t("avgCompletion")}</div>
              <div className="text-2xl font-bold text-fg flex items-center justify-center">
                <Clock className="w-5 h-5 mr-1 text-primary-fg" /> -
              </div>
              <div className="text-[10px] text-fg-faint mt-1">{t("endToEnd")}</div>
            </div>
            <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-4 text-center">
              <div className="text-xs text-fg-faint mb-1">{t("nodeCountLabel")}</div>
              <div className="text-2xl font-bold text-fg">
                {template.nodes.length}
              </div>
              <div className="text-[10px] text-fg-faint mt-1">{t("stepsUnit")}</div>
            </div>
          </div>

          <div className="bg-surface-2/40 border border-line-strong/60 rounded-xl p-6 text-center">
            <BarChart3 className="w-8 h-8 text-fg-faint mx-auto mb-3" />
            <p className="text-fg-subtle text-sm">{t("noStats")}</p>
            <p className="text-fg-faint text-xs mt-1">
              {t("noStatsHint")}
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

// ==========================================
// 模板列表
// ==========================================
interface TemplateListViewProps {
  templates: MappedTemplate[];
  onCreate: () => void;
  onEdit: (template: MappedTemplate) => void;
  onRun: (template: MappedTemplate) => void;
  onDelete: (templateId: string) => void;
}

function TemplateListView({
  templates,
  onCreate,
  onEdit,
  onRun,
  onDelete,
}: TemplateListViewProps) {
  const t = useTranslations("workflows.list");
  const { canEdit } = usePermission();
  const router = useRouter();
  const builtIn = templates.filter((t) => t.isBuiltIn);
  const custom = templates.filter((t) => !t.isBuiltIn);
  const [statsTemplate, setStatsTemplate] = useState<MappedTemplate | null>(
    null
  );

  const TemplateCard = ({ template }: { template: MappedTemplate }) => (
    <div
      className={`card-hover bg-surface-2/40 border border-line-strong/60 rounded-xl p-6 flex flex-col transition-all hover:bg-surface-2/50 shadow-sm backdrop-blur-sm hover:shadow-lg hover:shadow-primary/5 ${template.isBuiltIn ? "" : "hover:border-primary/30"}`}
    >
      <div className="flex justify-between items-start mb-3">
        <h3 className="text-lg font-bold text-fg flex items-center">
          <Workflow
            className={`w-5 h-5 mr-2 ${template.isBuiltIn ? "text-fg-subtle" : "text-primary-fg"}`}
          />
          {template.name}
        </h3>
        {template.isBuiltIn && (
          <span className="px-2 py-0.5 bg-surface-3 text-fg-muted text-xs rounded border border-line-strong">
            {t("builtInTag")}
          </span>
        )}
      </div>
      <p className="text-sm text-fg-subtle mb-4 flex-1 line-clamp-2">
        {template.description}
      </p>

      <div className="flex flex-wrap gap-2 mb-5">
        <span className="text-[10px] px-2 py-0.5 bg-surface-3/50 text-fg-subtle rounded">
          {t("nodeCount", { count: template.nodes.length })}
        </span>
        {template.nodes.filter((n) => n.nodeType === "review").length > 0 && (
          <span className="text-[10px] px-2 py-0.5 bg-purple-500/10 text-purple-400 rounded border border-purple-500/20">
            {t("reviewCount", { count: template.nodes.filter((n) => n.nodeType === "review").length })}
          </span>
        )}
        {template.nodes.filter((n) => n.nodeType === "manual").length > 0 && (
          <span className="text-[10px] px-2 py-0.5 bg-warning/10 text-warning-fg rounded border border-warning/20">
            {t("manualCount", { count: template.nodes.filter((n) => n.nodeType === "manual").length })}
          </span>
        )}
      </div>

      <div className="flex items-center text-xs border-t border-line-strong/50 pt-4 mt-auto">
        <div className="flex items-center space-x-3 ml-auto">
          <button
            onClick={() => router.push(`/workflows/view?templateId=${template.id}`)}
            className="flex items-center text-fg-subtle hover:text-fg-muted transition-colors-fast"
          >
            <Eye className="w-3.5 h-3.5 mr-1" /> {t("preview")}
          </button>
          <button
            onClick={() => setStatsTemplate(template)}
            className="flex items-center text-fg-subtle hover:text-success-fg transition-colors-fast"
          >
            <BarChart3 className="w-3.5 h-3.5 mr-1" /> {t("stats")}
          </button>
          {!template.isBuiltIn && (
            <>
              <button
                onClick={() => onEdit(template)}
                disabled={!canEdit}
                className="flex items-center text-fg-subtle hover:text-primary-fg font-medium transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:text-fg-subtle"
              >
                <Edit2 className="w-3.5 h-3.5 mr-1" /> {t("edit")}
              </button>
              <button
                onClick={() => onDelete(template.id)}
                disabled={!canEdit}
                className="flex items-center text-fg-subtle hover:text-danger-fg transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:text-fg-subtle"
              >
                <Trash2 className="w-3.5 h-3.5 mr-1" /> {t("delete")}
              </button>
            </>
          )}
          <button
            onClick={() => onRun(template)}
            className="flex items-center px-3 py-1.5 bg-accent-strong/10 hover:bg-accent-strong/20 text-accent-fg border border-accent/30 rounded-lg font-medium transition-colors-fast btn-press"
          >
            <Rocket className="w-3.5 h-3.5 mr-1.5" /> {t("run")}
          </button>
        </div>
      </div>
    </div>
  );

  return (
    <div className="page-enter h-full flex flex-col p-8 overflow-y-auto">
      <div className="flex justify-between items-center mb-10 shrink-0">
        <div>
          <h1 className="text-3xl font-bold text-fg mb-1">
            {t("page.title")}
          </h1>
          <p className="text-sm text-fg-subtle">
            {t("page.subtitle", {
              total: templates.length,
              builtIn: builtIn.length,
            })}
          </p>
        </div>
        <button
          onClick={onCreate}
          disabled={!canEdit}
          className="flex items-center px-5 py-2.5 bg-primary-strong hover:bg-primary text-fg rounded-lg font-medium transition-colors-normal shadow-lg shadow-primary/20 btn-press disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Plus className="w-5 h-5 mr-2" /> {t("page.createButton")}
        </button>
      </div>

      {/* 自定义模板*/}
      <div className="mb-10">
        <h2 className="text-sm font-semibold text-fg-faint uppercase tracking-wider mb-4 flex items-center">
          <LayoutTemplate className="w-4 h-4 mr-2" /> {t("customTemplates")}
        </h2>
        {custom.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {custom.map((t) => (
              <TemplateCard key={t.id} template={t} />
            ))}
          </div>
        ) : (
          <div className="bg-surface/50 border border-dashed border-line-strong rounded-xl p-10 text-center">
            <LayoutTemplate className="w-12 h-12 text-fg-faint mx-auto mb-4" />
            <p className="text-fg-subtle mb-4">
              {t("noCustomTemplates")}
            </p>
            <button
              onClick={onCreate}
              className="text-primary-fg hover:text-primary-fg font-medium text-sm"
            >
              {t("createFirst")}
            </button>
          </div>
        )}
      </div>

      {/* 内置模板 */}
      <div>
        <h2 className="text-sm font-semibold text-fg-faint uppercase tracking-wider mb-4 flex items-center">
          <Database className="w-4 h-4 mr-2" /> {t("builtInTemplates")}
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {builtIn.map((t) => (
            <TemplateCard key={t.id} template={t} />
          ))}
        </div>
      </div>

      {statsTemplate && (
        <ModalPortal>
        <TemplateStatsDialog
          template={statsTemplate}
          onClose={() => setStatsTemplate(null)}
        />
        </ModalPortal>
      )}
    </div>
  );
}

// ==========================================
// 工作流模板管理主视图 (Next.js Page)
// ==========================================
export default function WorkflowsPage() {
  const router = useRouter();
  const projects = useAppStore((s) => s.projects);
  const templates = useAppStore((s) => s.templates);
  const setTemplates = useAppStore((s) => s.setTemplates);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);

  const [subView, setSubView] = useState<"list" | "run">("list");
  const [runTemplate, setRunTemplate] = useState<MappedTemplate | null>(null);

  const wsId = currentWorkspaceId;

  const handleCreateNew = () => {
    router.push("/workflows/editor");
  };

  const handleEdit = (template: MappedTemplate) => {
    if (template.isBuiltIn) return;
    router.push(`/workflows/editor?templateId=${template.id}`);
  };

  const handleRun = (template: MappedTemplate) => {
    setRunTemplate(JSON.parse(JSON.stringify(template)));
    setSubView("run");
  };

  const handleDelete = async (templateId: string) => {
    try {
      if (wsId) {
        await api.deleteWorkflow(wsId, templateId);
      }
      setTemplates(templates.filter((t) => t.id !== templateId));
    } catch (err) {
      console.error("Delete template failed:", err);
      // Still update local state on API failure
      setTemplates(templates.filter((t) => t.id !== templateId));
    }
  };

  const handleTaskCreated = async (
    template: MappedTemplate,
    title: string,
    desc: string,
    taskType: string,
    priority: string,
    selectedProject: string,
    dueDate: string,
    labelList: string[],
    constraints: string
  ): Promise<boolean> => {
    try {
      const payload = {
        title,
        description: desc,
        type: taskType,
        priority,
        due_date: dueDate,
        labels: labelList,
        constraints,
        workflow_template_id: template.id,
      };
      await api.createTask(selectedProject, payload);
      await loadProjectData(selectedProject);
      router.push("/dashboard");
      return true;
    } catch (err) {
      console.error("Create task failed:", err);
      return false;
    }
  };

  if (subView === "run" && runTemplate) {
    return (
      <TemplateRunView
        template={runTemplate}
        onCancel={() => setSubView("list")}
        onTaskCreated={handleTaskCreated}
        projects={projects}
      />
    );
  }
  return (
    <TemplateListView
      templates={templates}
      onCreate={handleCreateNew}
      onEdit={handleEdit}
      onRun={handleRun}
      onDelete={handleDelete}
    />
  );
}
