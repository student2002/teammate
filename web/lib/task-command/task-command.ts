// task-command.ts: 任务指挥台纯逻辑。
// 状态机类型、建议标题、手动模板过滤、初始工程选择与提交校验;
// 全部为无副作用纯函数,便于单测覆盖。

import type { Project } from "@/lib/types";
import type { TemplateListItem } from "@/lib/workflow/types";

export type CommandStep =
  | "idle"
  | "requirement"
  | "project"
  | "workflow"
  | "review"
  | "submitting"
  | "succeeded"
  | "failed";

export interface CommandDraft {
  requirement: string;
  title: string;
  projectId: string | null;
  templateId: string | null;
}

export const EMPTY_DRAFT: CommandDraft = {
  requirement: "",
  title: "",
  projectId: null,
  templateId: null,
};

/** 建议标题:首个非空行截取前 50 个字符;无可用地返回空串 */
export function suggestTitle(requirement: string): string {
  const firstLine = requirement.split(/\r?\n/).find((line) => line.trim()) ?? "";
  return firstLine.trim().slice(0, 50);
}

/** 可手动运行的工作流:启用且包含 trigger.manual 触发节点 */
export function listManualTemplates(templates: TemplateListItem[]): TemplateListItem[] {
  return templates.filter(
    (item) =>
      item.template.enabled && item.nodes.some((node) => node.type_key === "trigger.manual")
  );
}

/** 初始工程:当前工程仍在列表时用它,否则取列表第一项 */
export function pickInitialProject(
  projects: Project[],
  currentProjectId: string | null
): string | null {
  if (currentProjectId && projects.some((project) => project.id === currentProjectId)) {
    return currentProjectId;
  }
  return projects[0]?.id ?? null;
}

/** 空需求、空标题、空工程、空模板任一缺失都禁止提交 */
export function canSubmitDraft(draft: CommandDraft): boolean {
  return Boolean(
    draft.requirement.trim() && draft.title.trim() && draft.projectId && draft.templateId
  );
}

/** 快捷入口:只插入文本结构骨架,不自动发布、不虚构字段 */
export const REQUIREMENT_TEMPLATES = {
  fix: "问题现象:\n复现步骤:\n期望结果:\n相关位置:",
  feature: "功能目标:\n验收标准:\n边界与约束:",
  analysis: "分析对象:\n关注点:\n产出要求:",
} as const;
