// mappers.test.ts —— 关键纯映射函数的最小单测。
import { describe, it, expect } from "vitest";
import {
  formatTaskRef,
  mapSkillFromApi,
  mapMcpServerFromApi,
  mapAgentFromApi,
  mapTaskFromApi,
  mapTemplateFromApi,
} from "./mappers";

describe("formatTaskRef", () => {
  it("formats numeric id with T- prefix", () => {
    expect(formatTaskRef(42)).toBe("T-42");
  });

  it("formats string id with T- prefix", () => {
    expect(formatTaskRef("abc")).toBe("T-abc");
  });
});

describe("mapSkillFromApi", () => {
  it("maps snake_case fields and handles null description", () => {
    const skill = mapSkillFromApi({
      id: "s1",
      name: "dev",
      description: null,
      prompt_template: "prompt",
      workspace_id: "ws1",
    });
    expect(skill.id).toBe("s1");
    expect(skill.name).toBe("dev");
    expect(skill.description).toBe("");
    expect(skill.promptTemplate).toBe("prompt");
    expect(skill.workspaceId).toBe("ws1");
  });
});

describe("mapMcpServerFromApi", () => {
  it("defaults type/auth and parses env_vars JSON string", () => {
    const mcp = mapMcpServerFromApi({
      id: "m1",
      name: "mcp",
      env_vars: '{"K":"V"}',
    });
    expect(mcp.type).toBe("sse");
    expect(mcp.authType).toBe("none");
    expect(mcp.envVars).toEqual({ K: "V" });
  });
});

describe("mapAgentFromApi", () => {
  it("maps provider to display name and token counts", () => {
    const agent = mapAgentFromApi({
      id: "a1",
      name: "agent",
      provider: "claude",
      input_tokens: 10,
      output_tokens: 20,
    });
    expect(agent.tool).toBe("Claude Code");
    expect(agent.tokenUsage).toEqual({ input: 10, output: 20 });
  });

  it("falls back to raw provider when not in map", () => {
    expect(mapAgentFromApi({ id: "a2", provider: "custom" }).tool).toBe("custom");
  });
});

describe("mapTaskFromApi", () => {
  it("computes taskRef and current node status", () => {
    const task = mapTaskFromApi({
      task: { id: 7, title: "hi", status: "active" },
      nodes: [{ status: "pending", node_type: "standard", name: "n1" }],
    });
    expect(task.id).toBe(7);
    expect(task.taskRef).toBe("T-7");
    expect(task.title).toBe("hi");
    expect(task.nodesStatus).toEqual(["pending"]);
  });
});

describe("mapTemplateFromApi", () => {
  it("maps trigger config snake_case keys", () => {
    const tpl = mapTemplateFromApi({
      template: {
        id: "t1",
        name: "tpl",
        trigger_config: { project_id: "p1", interval_minutes: 15, repo_name: "r" },
      },
      nodes: [],
    });
    expect(tpl.id).toBe("t1");
    expect(tpl.triggerConfig.projectId).toBe("p1");
    expect(tpl.triggerConfig.intervalMinutes).toBe(15);
    expect(tpl.triggerConfig.repoName).toBe("r");
  });
});