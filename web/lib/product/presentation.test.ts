// presentation 纯函数测试：紧凑协作链节点选择、状态→语义映射、注意级别。
import { describe, expect, it } from "vitest";
import {
  attentionLevel,
  compactChainIndices,
  nodeVisual,
  priorityTextCls,
  taskStateLabelKey,
  taskStateTone,
} from "./presentation";

describe("compactChainIndices", () => {
  it("0 节点返回空链", () => {
    expect(compactChainIndices(0, 0)).toEqual({ indices: [], omitted: 0 });
  });

  it("1 节点全展示", () => {
    expect(compactChainIndices(1, 0)).toEqual({ indices: [0], omitted: 0 });
  });

  it("5 节点恰好全展示", () => {
    expect(compactChainIndices(5, 2)).toEqual({ indices: [0, 1, 2, 3, 4], omitted: 0 });
  });

  it("6 节点收敛为首/当前/末 + 3 个省略", () => {
    expect(compactChainIndices(6, 2)).toEqual({ indices: [0, 2, 5], omitted: 3 });
  });

  it("当前节点与首或末重合时去重", () => {
    expect(compactChainIndices(6, 0)).toEqual({ indices: [0, 5], omitted: 4 });
    expect(compactChainIndices(6, 5)).toEqual({ indices: [0, 5], omitted: 4 });
  });

  it("当前节点越界时收敛到合法范围", () => {
    expect(compactChainIndices(6, 99)).toEqual({ indices: [0, 5], omitted: 4 });
    expect(compactChainIndices(6, -1)).toEqual({ indices: [0, 5], omitted: 4 });
  });
});

describe("taskStateTone / taskStateLabelKey", () => {
  it("需人工 warning、失败 danger", () => {
    expect(taskStateTone("manual_intervention")).toBe("warning");
    expect(taskStateTone("failed")).toBe("danger");
  });

  it("执行/就绪 accent、成功 success、等待/跳过/取消 neutral", () => {
    expect(taskStateTone("running")).toBe("accent");
    expect(taskStateTone("ready")).toBe("accent");
    expect(taskStateTone("succeeded")).toBe("success");
    expect(taskStateTone("waiting")).toBe("neutral");
    expect(taskStateTone("skipped")).toBe("neutral");
    expect(taskStateTone("cancelled")).toBe("neutral");
    expect(taskStateTone("unknown")).toBe("neutral");
  });

  it("label key 覆盖全部运行八态", () => {
    expect(taskStateLabelKey("manual_intervention")).toBe("manualIntervention");
    expect(taskStateLabelKey("waiting")).toBe("waiting");
    expect(taskStateLabelKey("ready")).toBe("ready");
    expect(taskStateLabelKey("running")).toBe("running");
    expect(taskStateLabelKey("succeeded")).toBe("succeeded");
    expect(taskStateLabelKey("failed")).toBe("failed");
    expect(taskStateLabelKey("skipped")).toBe("skipped");
    expect(taskStateLabelKey("cancelled")).toBe("cancelled");
  });
});

describe("nodeVisual", () => {
  it("human.input 节点是菱形，其余圆点", () => {
    expect(nodeVisual("waiting", false, "human.input").shape).toBe("diamond");
    expect(nodeVisual("waiting", false, "agent.task").shape).toBe("dot");
    expect(nodeVisual("waiting", false, undefined).shape).toBe("dot");
  });

  it("成功=success 实心；执行/当前=accent 外环脉冲；等待=中性描边", () => {
    expect(nodeVisual("succeeded", false, "agent.task").className).toContain("bg-success-fg");
    const active = nodeVisual("running", false, "agent.task").className;
    expect(active).toContain("bg-accent-fg");
    expect(active).toContain("animate-pulse-soft");
    expect(nodeVisual("waiting", false, "agent.task").className).toContain("border-line-strong");
  });

  it("失败 danger、需人工 warning；当前节点即使状态滞后也用 accent", () => {
    expect(nodeVisual("failed", false, "agent.task").className).toContain("bg-danger-fg");
    expect(nodeVisual("manual_intervention", false, "human.input").className).toContain("bg-warning-fg");
    expect(nodeVisual("waiting", true, "agent.task").className).toContain("bg-accent-fg");
  });
});

describe("priorityTextCls", () => {
  it("urgent/high 用 warning 文字，紧急加粗；medium/low 中性", () => {
    expect(priorityTextCls("urgent")).toContain("text-warning-fg");
    expect(priorityTextCls("urgent")).toContain("font-semibold");
    expect(priorityTextCls("high")).toContain("text-warning-fg");
    expect(priorityTextCls("medium")).toBe("text-fg-muted");
    expect(priorityTextCls("low")).toBe("text-fg-subtle");
  });
});

describe("attentionLevel", () => {
  const base = { priorityState: "running", dueDate: "" };

  it("未结束且逾期 → danger", () => {
    expect(attentionLevel({ ...base, dueDate: "2000-01-01" })).toBe("danger");
  });

  it("需人工/失败 → warning", () => {
    expect(attentionLevel({ ...base, priorityState: "manual_intervention" })).toBe("warning");
    expect(attentionLevel({ ...base, priorityState: "failed" })).toBe("warning");
  });

  it("已成功/已取消即使日期过期也不报警", () => {
    expect(attentionLevel({ ...base, priorityState: "succeeded", dueDate: "2000-01-01" })).toBe("none");
    expect(attentionLevel({ ...base, priorityState: "cancelled", dueDate: "2000-01-01" })).toBe("none");
  });

  it("正常执行 → none", () => {
    expect(attentionLevel(base)).toBe("none");
  });
});
