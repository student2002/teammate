// catalog.test.ts —— 触发摘要推导的最小单测。
import { describe, it, expect } from "vitest";
import { triggerSummary } from "./catalog";
import { buildBlankTemplate, buildManualTemplate, buildScheduleTemplate, buildWebhookTemplate, ISSUE_FIX_TEMPLATE } from "./presets";

describe("triggerSummary", () => {
  it("derives manual from the preset manual template", () => {
    expect(triggerSummary(buildManualTemplate().nodes)).toEqual({ key: "manual" });
  });

  it("derives schedule minutes from the trigger config", () => {
    expect(triggerSummary(buildScheduleTemplate().nodes)).toEqual({
      key: "schedule",
      params: { minutes: 30 },
    });
  });

  it("derives github event and actions", () => {
    expect(triggerSummary(ISSUE_FIX_TEMPLATE.nodes)).toEqual({
      key: "github",
      params: { event: "issues", actions: "opened" },
    });
  });

  it("derives webhook", () => {
    expect(triggerSummary(buildWebhookTemplate().nodes)).toEqual({ key: "webhook" });
  });

  it("returns null when no trigger node exists", () => {
    const blank = buildBlankTemplate();
    expect(triggerSummary(blank.nodes.filter((n) => n.type_key !== "trigger.manual"))).toBeNull();
  });
});
