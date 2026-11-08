// mappers.test.ts —— 关键纯映射函数的最小单测。
import { describe, it, expect } from "vitest";
import {
  formatTaskRef,
  mapSkillFromApi,
  mapMcpServerFromApi,
  mapAgentFromApi,
  mapTaskFromApi,
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
  it("defaults type/auth and keeps env_vars object", () => {
    const mcp = mapMcpServerFromApi({
      id: "m1",
      name: "mcp",
      env_vars: { K: "V" },
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
  it("computes taskRef and current node status for workflow nodes", () => {
    const task = mapTaskFromApi({
      task: { id: 7, title: "hi", status: "active" },
      nodes: [{ status: "waiting", type_key: "agent.task", name: "n1" }],
    });
    expect(task.id).toBe(7);
    expect(task.taskRef).toBe("T-7");
    expect(task.title).toBe("hi");
    expect(task.nodesStatus).toEqual(["waiting"]);
    expect(task.currentNodeType).toBe("agent.task");
    expect(task.priorityState).toBe("waiting");
  });

  it("maps the current node's reservation to task level with the agent name", () => {
    const task = mapTaskFromApi(
      {
        task: { id: 8, title: "reserved", status: "active" },
        nodes: [
          { status: "succeeded", type_key: "agent.task", name: "n1" },
          {
            status: "running",
            type_key: "agent.task",
            name: "n2",
            reserved_for_agent_id: "agent-uuid-1",
          },
        ],
      },
      [{ id: "agent-uuid-1", name: "claude-01" }]
    );
    expect(task.reservedForAgent).toBe("claude-01");
    expect(task.currentNode).toBe(1);
    expect(task.priorityState).toBe("running");
  });

  it("falls back to the raw agent id when the agent list lacks it", () => {
    const task = mapTaskFromApi({
      task: { id: 9, title: "reserved", status: "active" },
      nodes: [
        {
          status: "waiting",
          type_key: "agent.task",
          name: "n1",
          reserved_for_agent_id: "agent-uuid-2",
        },
      ],
    });
    expect(task.reservedForAgent).toBe("agent-uuid-2");
  });

  it("marks manual_intervention when any node needs a human", () => {
    const task = mapTaskFromApi({
      task: { id: 10, title: "manual", status: "active" },
      nodes: [
        { status: "succeeded", type_key: "agent.task", name: "n1" },
        { status: "manual_intervention", type_key: "human.input", name: "n2" },
      ],
    });
    expect(task.priorityState).toBe("manual_intervention");
  });

  it("maps terminal task statuses directly", () => {
    expect(
      mapTaskFromApi({
        task: { id: 11, status: "cancelled" },
        nodes: [{ status: "cancelled", type_key: "agent.task", name: "n1" }],
      }).priorityState
    ).toBe("cancelled");
    expect(
      mapTaskFromApi({
        task: { id: 12, status: "failed" },
        nodes: [{ status: "failed", type_key: "agent.task", name: "n1" }],
      }).priorityState
    ).toBe("failed");
  });

  it("treats the last node as current when every node is terminal", () => {
    const task = mapTaskFromApi({
      task: { id: 13, status: "completed" },
      nodes: [
        { status: "succeeded", type_key: "agent.task", name: "n1" },
        { status: "skipped", type_key: "data.set", name: "n2" },
      ],
    });
    expect(task.currentNode).toBe(1);
    expect(task.priorityState).toBe("succeeded");
  });
});
