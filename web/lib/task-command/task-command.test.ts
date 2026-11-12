// task-command.test.ts —— 指挥台纯逻辑单测。
// 覆盖:建议标题截断、手动模板过滤、初始工程选择与提交校验。
import { describe, it, expect } from "vitest";
import type { TemplateListItem } from "@/lib/workflow/types";
import {
  canSubmitDraft,
  listManualTemplates,
  pickInitialProject,
  suggestTitle,
  type CommandDraft,
} from "./task-command";

function makeTemplate(
  id: string,
  options: { enabled?: boolean; nodeTypes?: string[] } = {}
): TemplateListItem {
  return {
    template: {
      id,
      workspace_id: "ws1",
      name: `模板 ${id}`,
      description: "",
      is_builtin: false,
      version: 2,
      enabled: options.enabled ?? true,
      created_at: "",
      updated_at: "",
    },
    nodes: (options.nodeTypes ?? ["trigger.manual", "agent.task"]).map((type_key, index) => ({
      id: `n${index}`,
      type_key: type_key as TemplateListItem["nodes"][number]["type_key"],
      name: type_key,
      sort_order: index,
      config: {},
    })),
    edges: [],
  };
}

describe("suggestTitle", () => {
  it("取首个非空行", () => {
    expect(suggestTitle("\n  \n修复登录页的样式问题\n第二行")).toBe("修复登录页的样式问题");
  });

  it("超长首行截断到 50 个字符", () => {
    const longLine = "长".repeat(80);
    expect(suggestTitle(longLine)).toHaveLength(50);
  });

  it("无可用地返回空串", () => {
    expect(suggestTitle("")).toBe("");
    expect(suggestTitle("   \n\t\n")).toBe("");
  });
});

describe("listManualTemplates", () => {
  it("只返回启用且含 trigger.manual 的模板", () => {
    const templates = [
      makeTemplate("manual-on"),
      makeTemplate("disabled", { enabled: false }),
      makeTemplate("schedule-only", { nodeTypes: ["trigger.schedule", "agent.task"] }),
      makeTemplate("no-trigger", { nodeTypes: ["agent.task"] }),
    ];
    expect(listManualTemplates(templates).map((item) => item.template.id)).toEqual(["manual-on"]);
  });

  it("空列表返回空数组", () => {
    expect(listManualTemplates([])).toEqual([]);
  });
});

describe("pickInitialProject", () => {
  const projects = [
    { id: "p1", name: "工程一", description: "", workspaceId: "ws1" },
    { id: "p2", name: "工程二", description: "", workspaceId: "ws1" },
  ];

  it("当前工程存在时默认选中", () => {
    expect(pickInitialProject(projects, "p2")).toBe("p2");
  });

  it("当前工程不在列表时选择第一项", () => {
    expect(pickInitialProject(projects, "p9")).toBe("p1");
    expect(pickInitialProject(projects, null)).toBe("p1");
  });

  it("无工程时返回 null", () => {
    expect(pickInitialProject([], null)).toBeNull();
    expect(pickInitialProject([], "p1")).toBeNull();
  });
});

describe("canSubmitDraft", () => {
  const full: CommandDraft = {
    requirement: "需求内容",
    title: "标题",
    projectId: "p1",
    templateId: "t1",
  };

  it("全部字段就绪时允许提交", () => {
    expect(canSubmitDraft(full)).toBe(true);
  });

  it("空需求、空标题、空工程、空模板禁止提交", () => {
    expect(canSubmitDraft({ ...full, requirement: "   " })).toBe(false);
    expect(canSubmitDraft({ ...full, title: "" })).toBe(false);
    expect(canSubmitDraft({ ...full, projectId: null })).toBe(false);
    expect(canSubmitDraft({ ...full, templateId: null })).toBe(false);
  });
});
