"use client";
// TaskCommandDock.tsx: 全局任务指挥台。
// 桌面端为布局内右侧可折叠栏(收起 44px 命令栏/展开 360px),移动端为 ContextBar
// 下方的内联折叠区,均推开页面内容,不使用遮罩弹窗。
// 由确定性状态机驱动(requirement → project → workflow → review → submitting →
// succeeded/failed),不调用大模型,关键字段必须显式选择并在发布前确认;
// 发布使用现有 runWorkflowTemplate 接口,成功后刷新 Store 使 3D 网络自然更新。

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  ChevronsRight,
  Send,
} from "lucide-react";
import api, { ApiError } from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { usePermission } from "@/lib/use-permission";
import { useMediaQuery } from "@/lib/use-media-query";
import Button from "@/lib/ui/Button";
import TaskDraftReview from "./TaskDraftReview";
import {
  canSubmitDraft,
  EMPTY_DRAFT,
  listManualTemplates,
  pickInitialProject,
  REQUIREMENT_TEMPLATES,
  suggestTitle,
  type CommandDraft,
  type CommandStep,
} from "./task-command";

export default function TaskCommandDock({ className = "" }: { className?: string }) {
  const t = useTranslations("taskCommand");
  const router = useRouter();
  const projects = useAppStore((s) => s.projects);
  const templates = useAppStore((s) => s.templates);
  const currentProjectId = useAppStore((s) => s.currentProjectId);
  const setCurrentProjectId = useAppStore((s) => s.setCurrentProjectId);
  const loadProjectData = useAppStore((s) => s.loadProjectData);
  const currentWorkspaceId = useAppStore((s) => s.currentWorkspaceId);
  const { canEdit, isViewer } = usePermission();

  const mobile = useMediaQuery("(max-width: 767px)");
  const [expanded, setExpanded] = useState(false);
  const [step, setStep] = useState<CommandStep>("idle");
  const [draft, setDraft] = useState<CommandDraft>(EMPTY_DRAFT);
  const [errorText, setErrorText] = useState("");
  const [publishedTaskId, setPublishedTaskId] = useState<number | null>(null);

  const manualTemplates = useMemo(() => listManualTemplates(templates), [templates]);
  const hasDraft =
    draft.requirement.trim() !== "" ||
    draft.title.trim() !== "" ||
    draft.projectId !== null ||
    draft.templateId !== null;
  const disabled = !canEdit;

  // 工作区切换后清空上个工作区的未发布草稿,避免跨工作区误发
  const workspaceRef = useRef(currentWorkspaceId);
  useEffect(() => {
    if (workspaceRef.current === currentWorkspaceId) return;
    workspaceRef.current = currentWorkspaceId;
    setStep("idle");
    setDraft(EMPTY_DRAFT);
    setErrorText("");
    setPublishedTaskId(null);
  }, [currentWorkspaceId]);

  const updateDraft = (patch: Partial<CommandDraft>) =>
    setDraft((prev) => ({ ...prev, ...patch }));

  const enterProjectStep = () => {
    setDraft((prev) => ({
      ...prev,
      projectId: prev.projectId ?? pickInitialProject(projects, currentProjectId),
    }));
    setStep("project");
  };

  const enterReview = () => {
    setDraft((prev) => ({ ...prev, title: prev.title.trim() || suggestTitle(prev.requirement) }));
    setStep("review");
  };

  const startNext = () => {
    setDraft(EMPTY_DRAFT);
    setErrorText("");
    setPublishedTaskId(null);
    setStep("requirement");
  };

  const publish = async () => {
    if (disabled || step === "submitting" || !canSubmitDraft(draft)) return;
    // 发布期间用户可能切走工作区:之后所有 Store 回写都必须放弃,
    // 避免把旧工作区的工程与任务灌进新工作区
    const wsAtStart = currentWorkspaceId;
    const wsChanged = () => useAppStore.getState().currentWorkspaceId !== wsAtStart;
    setStep("submitting");
    setErrorText("");
    try {
      const { task_id } = await api.runWorkflowTemplate(draft.projectId!, draft.templateId!, {
        title: draft.title.trim(),
        input: { requirement: draft.requirement.trim() },
      });
      if (wsChanged()) return;
      // 发布工程不是当前工程时先切换,再刷新该工程数据;
      // 工程判断读 live 值,发布期间用户可能在同工作区内切过工程
      if (draft.projectId !== useAppStore.getState().currentProjectId) {
        setCurrentProjectId(draft.projectId!);
      }
      await loadProjectData(draft.projectId!);
      if (wsChanged()) return;
      setPublishedTaskId(task_id);
      setStep("succeeded");
    } catch (err) {
      if (wsChanged()) return;
      setErrorText(err instanceof ApiError ? err.message : t("failed.generic"));
      setStep("failed");
    }
  };

  const stepLabel = {
    requirement: t("steps.requirement"),
    project: t("steps.project"),
    workflow: t("steps.workflow"),
    review: t("steps.review"),
  } as const;

  const renderStepHeader = (key: keyof typeof stepLabel) => (
    <div className="flex items-center gap-2 mb-2">
      <span className="text-[11px] font-medium text-fg-muted">{stepLabel[key]}</span>
      <span className="flex-1 h-px bg-line" />
    </div>
  );

  const renderBody = () => {
    switch (step) {
      case "idle":
        return (
          <div className="space-y-3">
            <div className="text-sm font-medium text-fg">{t("intro.headline")}</div>
            <p className="text-xs text-fg-subtle leading-relaxed">{t("intro.description")}</p>
            <Button variant="primary" size="sm" onClick={() => setStep("requirement")}>
              {t("start")}
            </Button>
          </div>
        );

      case "requirement":
        return (
          <div className="space-y-3">
            {renderStepHeader("requirement")}
            <textarea
              value={draft.requirement}
              onChange={(event) => updateDraft({ requirement: event.target.value })}
              disabled={disabled}
              rows={7}
              placeholder={t("requirement.placeholder")}
              className="w-full py-2 px-2.5 bg-bg border border-line-strong rounded-[var(--radius-control)] text-xs text-fg placeholder-fg-faint focus:outline-none focus:border-primary transition-colors-fast resize-none leading-relaxed disabled:opacity-50 disabled:cursor-not-allowed"
            />
            <div className="flex flex-wrap items-center gap-1.5">
              {(
                [
                  ["fix", t("requirement.quickFix")],
                  ["feature", t("requirement.quickFeature")],
                  ["analysis", t("requirement.quickAnalysis")],
                ] as const
              ).map(([key, label]) => (
                <button
                  key={key}
                  type="button"
                  disabled={disabled}
                  onClick={() =>
                    updateDraft({
                      requirement: draft.requirement.trim()
                        ? `${draft.requirement.trimEnd()}\n${REQUIREMENT_TEMPLATES[key]}`
                        : REQUIREMENT_TEMPLATES[key],
                    })
                  }
                  className="h-7 px-2.5 text-[11px] bg-surface-2 hover:bg-surface-3 border border-line rounded-[var(--radius-control)] text-fg-subtle transition-colors-fast focus-visible:outline-none disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {label}
                </button>
              ))}
            </div>
            <p className="text-[10px] text-fg-faint leading-relaxed">
              {t("requirement.hint")}
            </p>
            <Button
              variant="primary"
              size="sm"
              onClick={enterProjectStep}
              disabled={disabled || !draft.requirement.trim()}
            >
              {t("requirement.continue")}
            </Button>
          </div>
        );

      case "project":
        return (
          <div className="space-y-3">
            {renderStepHeader("project")}
            {projects.length === 0 ? (
              <div className="space-y-2.5">
                <p className="text-xs text-fg-subtle">{t("project.empty")}</p>
                <Button variant="secondary" size="sm" onClick={() => router.push("/settings/project")}>
                  {t("project.emptyAction")}
                </Button>
              </div>
            ) : (
              <>
                <div className="space-y-1.5">
                  {projects.map((project) => (
                    <button
                      key={project.id}
                      type="button"
                      disabled={disabled}
                      onClick={() => updateDraft({ projectId: project.id })}
                      className={`w-full px-2.5 py-2 flex items-center gap-2 rounded-[var(--radius-control)] border text-left transition-colors-fast focus-visible:outline-none disabled:opacity-50 disabled:cursor-not-allowed ${
                        draft.projectId === project.id
                          ? "border-primary/40 bg-primary/10"
                          : "border-line bg-surface-2/40 hover:bg-surface-3/60"
                      }`}
                    >
                      <span className="text-xs text-fg truncate">{project.name}</span>
                      {project.id === currentProjectId && (
                        <span className="ml-auto text-[10px] text-primary-fg shrink-0">
                          {t("project.current")}
                        </span>
                      )}
                    </button>
                  ))}
                </div>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => setStep("workflow")}
                  disabled={disabled || !draft.projectId}
                >
                  {t("project.continue")}
                </Button>
              </>
            )}
          </div>
        );

      case "workflow":
        return (
          <div className="space-y-3">
            {renderStepHeader("workflow")}
            {manualTemplates.length === 0 ? (
              <div className="space-y-2.5">
                <p className="text-xs text-fg-subtle">{t("workflow.empty")}</p>
                <p className="text-[11px] text-fg-faint leading-relaxed">
                  {t("workflow.emptyDescription")}
                </p>
                <Button variant="secondary" size="sm" onClick={() => router.push("/workflows")}>
                  {t("workflow.emptyAction")}
                </Button>
              </div>
            ) : (
              <>
                <div className="space-y-1.5">
                  {manualTemplates.map((item) => (
                    <button
                      key={item.template.id}
                      type="button"
                      disabled={disabled}
                      onClick={() => updateDraft({ templateId: item.template.id })}
                      className={`w-full px-2.5 py-2 rounded-[var(--radius-control)] border text-left transition-colors-fast focus-visible:outline-none disabled:opacity-50 disabled:cursor-not-allowed ${
                        draft.templateId === item.template.id
                          ? "border-primary/40 bg-primary/10"
                          : "border-line bg-surface-2/40 hover:bg-surface-3/60"
                      }`}
                    >
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-fg truncate">{item.template.name}</span>
                        <span className="ml-auto text-[10px] text-fg-faint font-mono shrink-0">
                          v{item.template.version}
                        </span>
                      </div>
                      {item.template.description && (
                        <p className="mt-0.5 text-[10px] text-fg-faint truncate">
                          {item.template.description}
                        </p>
                      )}
                    </button>
                  ))}
                </div>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={enterReview}
                  disabled={disabled || !draft.templateId}
                >
                  {t("workflow.continue")}
                </Button>
              </>
            )}
          </div>
        );

      case "review":
        return (
          <div className="space-y-3">
            {renderStepHeader("review")}
            <TaskDraftReview
              draft={draft}
              projects={projects}
              templates={manualTemplates}
              disabled={disabled}
              onChange={updateDraft}
              onConfirm={publish}
              onBack={() => setStep("workflow")}
            />
          </div>
        );

      case "submitting":
        return (
          <div className="py-8 flex flex-col items-center gap-2.5">
            <div className="w-6 h-6 border-2 border-primary-fg border-t-transparent rounded-full animate-spin" />
            <span className="text-xs text-fg-subtle">{t("submitting")}</span>
          </div>
        );

      case "succeeded":
        return (
          <div className="py-6 flex flex-col items-center gap-3 text-center">
            <CheckCircle2 className="w-7 h-7 text-success-fg" />
            <div className="text-sm font-medium text-fg">{t("succeeded.title")}</div>
            <div className="text-xs text-fg-subtle font-mono">
              {t("succeeded.taskId")}: {publishedTaskId}
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant="primary"
                size="sm"
                onClick={() => router.push(`/workflows/view?taskId=${publishedTaskId}`)}
              >
                {t("succeeded.viewRun")}
              </Button>
              <Button variant="secondary" size="sm" onClick={startNext}>
                {t("succeeded.next")}
              </Button>
            </div>
          </div>
        );

      case "failed":
        return (
          <div className="py-6 flex flex-col items-center gap-3 text-center">
            <AlertTriangle className="w-7 h-7 text-danger-fg" />
            <div className="text-sm font-medium text-fg">{t("failed.title")}</div>
            <div className="px-3 py-2 text-xs text-danger-fg bg-danger/10 border border-danger/20 rounded-[var(--radius-control)] max-w-full break-all">
              {errorText}
            </div>
            <div className="flex items-center gap-2">
              <Button variant="primary" size="sm" onClick={publish}>
                {t("failed.retry")}
              </Button>
              <Button variant="secondary" size="sm" onClick={() => setStep("review")}>
                {t("failed.back")}
              </Button>
            </div>
            <p className="text-[10px] text-fg-faint">{t("failed.draftKept")}</p>
          </div>
        );
    }
  };

  const viewerBanner = isViewer && (
    <div className="mx-3 mt-3 px-2.5 py-1.5 flex items-center gap-1.5 text-[11px] bg-warning/10 border border-warning/20 rounded-[var(--radius-control)] text-warning-fg">
      <AlertTriangle className="w-3 h-3 shrink-0" />
      {t("viewerNotice")}
    </div>
  );

  // ---- 移动端:ContextBar 下方的内联折叠区,展开时推开页面内容 ----
  if (mobile) {
    return (
      <div className={`${className} w-full`}>
        <button
          type="button"
          onClick={() => setExpanded((prev) => !prev)}
          aria-expanded={expanded}
          className="w-full h-11 px-3 flex items-center gap-2 border-b border-line bg-surface-2/40 hover:bg-surface-3/60 transition-colors-fast focus-visible:outline-none text-left"
        >
          <Send className="w-4 h-4 text-primary-fg shrink-0" />
          <span className="text-xs font-medium text-fg">{t("title")}</span>
          {hasDraft && !expanded && (
            <span className="text-[10px] text-warning-fg truncate">{t("draftHint")}</span>
          )}
          <span className="flex-1" />
          {expanded ? (
            <ChevronUp className="w-4 h-4 text-fg-faint shrink-0" />
          ) : (
            <ChevronDown className="w-4 h-4 text-fg-faint shrink-0" />
          )}
        </button>
        {expanded && (
          <div className="border-b border-line bg-surface animate-fade-in">
            {viewerBanner}
            <div className="p-3 max-h-[60vh] overflow-y-auto">{renderBody()}</div>
          </div>
        )}
      </div>
    );
  }

  // ---- 桌面端:布局内右侧栏,收起为 44px 命令栏 ----
  return (
    <div className={className}>
      {expanded ? (
        <div className="w-[360px] h-full border-l border-line bg-surface flex flex-col animate-fade-in">
          <div className="h-11 px-3 flex items-center gap-2 border-b border-line shrink-0">
            <Send className="w-4 h-4 text-primary-fg shrink-0" />
            <span className="text-xs font-medium text-fg">{t("title")}</span>
            <span className="flex-1" />
            <button
              type="button"
              onClick={() => setExpanded(false)}
              aria-label={t("collapse")}
              className="w-7 h-7 flex items-center justify-center rounded-[var(--radius-control)] text-fg-faint hover:text-fg hover:bg-surface-2 transition-colors-fast focus-visible:outline-none"
            >
              <ChevronsRight className="w-4 h-4" />
            </button>
          </div>
          {viewerBanner}
          <div className="flex-1 overflow-y-auto p-3">{renderBody()}</div>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          aria-label={t("title")}
          aria-expanded={false}
          className="w-11 h-full flex flex-col items-center gap-2.5 pt-3 border-l border-line bg-surface-2/40 hover:bg-surface-3/60 transition-colors-fast focus-visible:outline-none"
        >
          <Send className="w-4 h-4 text-primary-fg shrink-0" />
          {hasDraft && <span className="w-1.5 h-1.5 rounded-full bg-warning-fg shrink-0" />}
          <span className="text-[11px] text-fg-muted [writing-mode:vertical-rl] tracking-[0.2em]">
            {t("title")}
          </span>
        </button>
      )}
    </div>
  );
}
