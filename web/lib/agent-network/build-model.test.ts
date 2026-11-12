// build-model.test.ts —— 协作网络数据模型的纯逻辑单测。
// 覆盖:确定性布局、空数据、自环、边合并、工程过滤与流光判定。
import { describe, it, expect } from "vitest";
import type { MappedAgent, MappedTask } from "@/lib/types";
import { buildAgentNetworkModel } from "./build-model";

function makeAgent(id: string, name = id, overrides: Partial<MappedAgent> = {}): MappedAgent {
  return {
    id,
    name,
    tool: "Claude Code",
    provider: "claude",
    status: "online",
    task: null,
    identityInstruction: "",
    gitName: "",
    gitEmail: "",
    skills: [],
    mcpServers: [],
    tokenUsage: { input: 0, output: 0 },
    history: [],
    ...overrides,
  };
}

function makeTask(
  id: number,
  projectId: string,
  overrides: Partial<MappedTask> = {}
): MappedTask {
  return {
    id,
    taskRef: `T-${id}`,
    title: `task ${id}`,
    type: "task",
    priority: "medium",
    priorityState: "waiting",
    description: "",
    constraints: "",
    currentNode: 0,
    currentNodeType: "agent.task",
    agent: null,
    message: "",
    nodesStatus: ["waiting"],
    nodeAgents: [],
    nodeNames: [],
    nodeTokens: {},
    dueDate: "",
    labels: [],
    logs: {},
    comments: [],
    subTasks: [],
    gitBranch: "",
    parentTask: "",
    reservedForAgent: null,
    nodeSummaries: {},
    workflowName: "",
    _rawNodes: [],
    _projectId: projectId,
    ...overrides,
  };
}

describe("buildAgentNetworkModel 布局", () => {
  it("Agent ID 顺序变化不改变同一 Agent 的位置", () => {
    const agents = [makeAgent("a1"), makeAgent("a2"), makeAgent("a3")];
    const shuffled = [agents[2], agents[0], agents[1]];
    const a = buildAgentNetworkModel(agents, [], "p1");
    const b = buildAgentNetworkModel(shuffled, [], "p1");
    for (const node of a.nodes) {
      const other = b.nodes.find((n) => n.id === node.id);
      expect(other?.position).toEqual(node.position);
    }
  });

  it("多 Agent 位置互不重叠且无 NaN", () => {
    const agents = Array.from({ length: 9 }, (_, i) => makeAgent(`agent-${i}`));
    const model = buildAgentNetworkModel(agents, [], "p1");
    const positions = new Set(model.nodes.map((n) => n.position.join(",")));
    expect(positions.size).toBe(model.nodes.length);
    for (const [x, y, z] of model.nodes.map((n) => n.position)) {
      expect(Number.isFinite(x)).toBe(true);
      expect(Number.isFinite(y)).toBe(true);
      expect(Number.isFinite(z)).toBe(true);
    }
  });
});

describe("buildAgentNetworkModel 数据规则", () => {
  it("空 agents 返回空模型", () => {
    const model = buildAgentNetworkModel([], [makeTask(1, "p1")], "p1");
    expect(model.nodes).toEqual([]);
    expect(model.edges).toEqual([]);
  });

  it("有 Agent 无任务时节点存在、边为空", () => {
    const model = buildAgentNetworkModel([makeAgent("a1"), makeAgent("a2")], [], "p1");
    expect(model.nodes).toHaveLength(2);
    expect(model.edges).toEqual([]);
  });

  it("连续相同 Agent 不产生自环", () => {
    const agents = [makeAgent("a1"), makeAgent("a2")];
    const tasks = [makeTask(1, "p1", { nodeAgents: ["a1", "a1", "a2"] })];
    const model = buildAgentNetworkModel(agents, tasks, "p1");
    expect(model.edges).toHaveLength(1);
    expect(model.edges[0].source).not.toBe(model.edges[0].target);
    expect(model.edges[0].taskIds).toEqual([1]);
  });

  it("相邻不同 Agent 产生一条边,多任务合并 taskIds", () => {
    const agents = [makeAgent("a1"), makeAgent("a2")];
    const tasks = [
      makeTask(1, "p1", { nodeAgents: ["a1", "a2"] }),
      makeTask(2, "p1", { nodeAgents: ["a2", "a1"] }),
    ];
    const model = buildAgentNetworkModel(agents, tasks, "p1");
    expect(model.edges).toHaveLength(1);
    expect(model.edges[0].taskIds.sort()).toEqual([1, 2]);
  });

  it("nodeAgents 引用不存在的 Agent 时跳过,不产生悬空边", () => {
    const agents = [makeAgent("a1")];
    const tasks = [
      makeTask(1, "p1", { nodeAgents: ["a1", "ghost"] }),
      makeTask(2, "p1", { nodeAgents: [""] }),
    ];
    const model = buildAgentNetworkModel(agents, tasks, "p1");
    expect(model.edges).toEqual([]);
    expect(model.nodes).toHaveLength(1);
  });

  it("nodeAgents 缺失或为空数组时不产生边,节点仍有确定性坐标", () => {
    const agents = [makeAgent("a1"), makeAgent("a2"), makeAgent("a3")];
    const tasks = [
      makeTask(1, "p1"),
      makeTask(2, "p1", { nodeAgents: [] }),
    ];
    const model = buildAgentNetworkModel(agents, tasks, "p1");
    expect(model.edges).toEqual([]);
    const positions = new Set(model.nodes.map((n) => n.position.join(",")));
    expect(positions.size).toBe(3);
    for (const node of model.nodes) {
      expect(node.position).not.toEqual([0, 0, 0]);
    }
  });
});

describe("buildAgentNetworkModel 工程与流光", () => {
  it("工程切换只使用当前工程任务", () => {
    const agents = [makeAgent("a1"), makeAgent("a2")];
    const tasks = [
      makeTask(1, "p1", { nodeAgents: ["a1", "a2"] }),
      makeTask(2, "p2", { nodeAgents: ["a1", "a2"] }),
    ];
    const model = buildAgentNetworkModel(agents, tasks, "p1");
    expect(model.edges).toHaveLength(1);
    expect(model.edges[0].taskIds).toEqual([1]);
  });

  it("projectId 为空时不统计任何任务", () => {
    const agents = [makeAgent("a1"), makeAgent("a2")];
    const tasks = [makeTask(1, "p1", { nodeAgents: ["a1", "a2"] })];
    const model = buildAgentNetworkModel(agents, tasks, null);
    expect(model.edges).toEqual([]);
  });

  it("running 任务驱动节点 activeTaskIds 与边流光", () => {
    const agents = [makeAgent("a1"), makeAgent("a2")];
    const tasks = [makeTask(1, "p1", { priorityState: "running", nodeAgents: ["a1", "a2"] })];
    const model = buildAgentNetworkModel(agents, tasks, "p1");
    expect(model.edges[0].active).toBe(true);
    expect(model.nodes.find((n) => n.id === "a1")?.activeTaskIds).toEqual([1]);
    expect(model.nodes.find((n) => n.id === "a2")?.activeTaskIds).toEqual([1]);
  });

  it("已结束任务保留在边的 taskIds 中但不产生流光", () => {
    const agents = [makeAgent("a1"), makeAgent("a2")];
    for (const state of ["succeeded", "failed", "cancelled", "manual_intervention"] as const) {
      const tasks = [makeTask(1, "p1", { priorityState: state, nodeAgents: ["a1", "a2"] })];
      const model = buildAgentNetworkModel(agents, tasks, "p1");
      expect(model.edges).toHaveLength(1);
      expect(model.edges[0].active).toBe(false);
      expect(model.edges[0].taskIds).toEqual([1]);
      expect(model.nodes.every((n) => n.activeTaskIds.length === 0)).toBe(true);
    }
  });
});
