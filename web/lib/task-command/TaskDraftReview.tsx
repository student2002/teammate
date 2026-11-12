"use client";
// TaskDraftReview.tsx: 发布前的真实预览卡片(内联,无弹窗)。
// 展示并允许修改标题/目标工程/工作流/需求全文;
// 确认发布调用真实接口,禁用态由权限与字段完整性共同决定。

import { useTranslations } from "next-intl";
import type { Project } from "@/lib/types";
import type { TemplateListItem } from "@/lib/workflow/types";
import Button from "@/lib/ui/Button";
import { canSubmitDraft, type CommandDraft } from "./task-command";

interface TaskDraftReviewProps {
  draft: CommandDraft;
  projects: Project[];
  /** 已经过手动触发过滤的可选工作流 */
  templates: TemplateListItem[];
  disabled: boolean;
  onChange: (patch: Partial<CommandDraft>) => void;
  onConfirm: () => void;
  onBack: () => void;
}

const inputClasses =
  "w-full bg-bg border border-line-strong rounded-[var(--radius-control)] px-2.5 text-xs text-fg focus:outline-none focus:border-primary transition-colors-fast disabled:opacity-50 disabled:cursor-not-allowed";

export default function TaskDraftReview({
  draft,
  projects,
  templates,
  disabled,
  onChange,
  onConfirm,
  onBack,
}: TaskDraftReviewProps) {
  const t = useTranslations("taskCommand.review");
  const wt = useTranslations("taskCommand.workflow");
  const selectedTemplate = templates.find(
    (item) => item.template.id === draft.templateId
  );
  const ready = canSubmitDraft(draft);

  return (
    <div className="space-y-3">
      <div>
        <label className="block text-[11px] font-medium text-fg-muted mb-1">
          {t("titleLabel")}
        </label>
        <input
          value={draft.title}
          onChange={(event) => onChange({ title: event.target.value })}
          disabled={disabled}
          className={`${inputClasses} h-8`}
        />
      </div>

      <div>
        <label className="block text-[11px] font-medium text-fg-muted mb-1">
          {t("projectLabel")}
        </label>
        <select
          value={draft.projectId ?? ""}
          onChange={(event) => onChange({ projectId: event.target.value || null })}
          disabled={disabled}
          className={`${inputClasses} h-8`}
        >
          {!draft.projectId && <option value="" />}
          {projects.map((project) => (
            <option key={project.id} value={project.id}>
              {project.name}
            </option>
          ))}
        </select>
      </div>

      <div>
        <label className="block text-[11px] font-medium text-fg-muted mb-1">
          {t("workflowLabel")}
        </label>
        <select
          value={draft.templateId ?? ""}
          onChange={(event) => onChange({ templateId: event.target.value || null })}
          disabled={disabled}
          className={`${inputClasses} h-8`}
        >
          {!draft.templateId && <option value="" />}
          {templates.map((item) => (
            <option key={item.template.id} value={item.template.id}>
              {item.template.name} · {wt("version")} v{item.template.version}
            </option>
          ))}
        </select>
        {selectedTemplate?.template.description && (
          <p className="mt-1 text-[10px] text-fg-faint leading-relaxed">
            {selectedTemplate.template.description}
          </p>
        )}
      </div>

      <div>
        <label className="block text-[11px] font-medium text-fg-muted mb-1">
          {t("requirementLabel")}
        </label>
        <textarea
          value={draft.requirement}
          onChange={(event) => onChange({ requirement: event.target.value })}
          disabled={disabled}
          rows={6}
          className={`${inputClasses} py-2 resize-none leading-relaxed`}
        />
      </div>

      <div className="px-2.5 py-1.5 text-[11px] bg-warning/10 border border-warning/20 rounded-[var(--radius-control)] text-warning-fg">
        {t("warning")}
      </div>

      <div className="flex items-center gap-2">
        <Button variant="primary" size="sm" onClick={onConfirm} disabled={disabled || !ready}>
          {t("confirm")}
        </Button>
        <Button variant="secondary" size="sm" onClick={onBack} disabled={disabled}>
          {t("back")}
        </Button>
      </div>
    </div>
  );
}
