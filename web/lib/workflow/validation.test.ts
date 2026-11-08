import { describe, expect, it } from "vitest";
import { outputPorts } from "./catalog";
import {
  buildBlankTemplate,
  buildManualTemplate,
  buildScheduleTemplate,
  buildWebhookTemplate,
} from "./presets";
import { validateTemplate } from "./validation";
import type { TemplateEdge, TemplateNode, WorkflowTypeKey } from "./types";

const node = (
  id: string,
  type_key: WorkflowTypeKey,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  config: any = {},
  sort_order = 0
): TemplateNode => ({ id, type_key, name: id, sort_order, config, ui_config: { x: 0, y: 0 } });

const edge = (
  id: string,
  source_node_id: string,
  source_port: string,
  target_node_id: string,
  target_port: string
): TemplateEdge => ({ id, source_node_id, source_port, target_node_id, target_port });

// 多分支示例图:手动触发 → agent → 条件 → 两条汇出路径都到 end
function sampleTemplate(): { nodes: TemplateNode[]; edges: TemplateEdge[] } {
  const nodes = [
    node("t", "trigger.manual", {}, 0),
    node("a", "agent.task", { instruction: "do", retry: { max_attempts: 1 } }, 1),
    node("c", "flow.condition", { condition: { all: [{ path: "nodes.a.output.summary", op: "exists" }] } }, 2),
    node("w", "flow.wait", { mode: "duration", minutes: 5 }, 3),
    node("e", "flow.end", {}, 4),
  ];
  const edges = [
    edge("e1", "t", "success", "a", "input"),
    edge("e2", "a", "success", "c", "input"),
    edge("e3", "c", "true", "e", "completed"),
    edge("e4", "c", "false", "w", "input"),
    edge("e5", "w", "resumed", "e", "completed"),
    edge("e6", "w", "timeout", "e", "failed"),
    edge("e7", "a", "failed", "e", "failed"),
  ];
  return { nodes, edges };
}

// trigger.manual → agent.task → flow.end(completed) 的最小合法图
function minimalGraph() {
  const nodes = [
    node("t", "trigger.manual", {}, 0),
    node("a", "agent.task", { instruction: "do", retry: { max_attempts: 1 } }, 1),
    node("e", "flow.end", {}, 2),
  ];
  const edges = [
    edge("e1", "t", "success", "a", "input"),
    edge("e2", "a", "success", "e", "completed"),
  ];
  return { nodes, edges };
}

describe("validateTemplate", () => {
  it("多分支示例图通过全部校验", () => {
    const { nodes, edges } = sampleTemplate();
    expect(validateTemplate(nodes, edges)).toEqual([]);
  });

  it("空白骨架与全部预制模板除定时触发待选工程外都通过校验", () => {
    for (const build of [buildBlankTemplate, buildManualTemplate, buildWebhookTemplate]) {
      const graph = build();
      expect(validateTemplate(graph.nodes, graph.edges)).toEqual([]);
    }
    // 定时触发预制模板的运行工程留空待选,唯一缺项就是工程
    const schedule = buildScheduleTemplate();
    expect(validateTemplate(schedule.nodes, schedule.edges).map((i) => i.key)).toEqual(["cfg.scheduleProject"]);
  });

  it("缺少 trigger 与 end 分别报错", () => {
    const { nodes, edges } = minimalGraph();
    expect(validateTemplate(nodes.slice(1), edges).map((i) => i.key)).toContain("triggerMissing");
    expect(validateTemplate(nodes.slice(0, 2), edges.slice(0, 1)).map((i) => i.key)).toContain("endMissing");
  });

  it("多个 trigger 报错", () => {
    const { nodes, edges } = minimalGraph();
    const extra = node("t2", "trigger.manual", {}, 3);
    const more = [...edges, edge("e3", "t2", "success", "a", "input")];
    expect(validateTemplate([...nodes, extra], more).map((i) => i.key)).toContain("triggerMultiple");
  });

  it("重复 (source,port,target) 与同端口多条出边均报错", () => {
    const { nodes, edges } = minimalGraph();
    const dup = [...edges, edge("dup", "t", "success", "a", "input")];
    expect(validateTemplate(nodes, dup).map((i) => i.key)).toContain("edgeDuplicate");
    const multi = [...edges, edge("multi", "t", "success", "e", "failed")];
    expect(validateTemplate(nodes, multi).map((i) => i.key)).toContain("portMultiEdge");
  });

  it("端口不属于节点声明的端口时报错", () => {
    const { nodes, edges } = minimalGraph();
    const bad = [...edges, edge("bad", "a", "nope", "e", "failed")];
    const keys = validateTemplate(nodes, bad).map((i) => i.key);
    expect(keys).toContain("sourcePortInvalid");
  });

  it("检测到环", () => {
    const nodes = [
      node("t", "trigger.manual", {}, 0),
      node("a", "flow.join", { mode: "all" }, 1),
      node("b", "flow.join", { mode: "all" }, 2),
      node("e", "flow.end", {}, 3),
    ];
    const edges = [
      edge("e1", "t", "success", "a", "input"),
      edge("e2", "a", "success", "b", "input"),
      edge("e3", "b", "success", "a", "input"),
      edge("e4", "b", "success", "e", "completed"),
    ];
    expect(validateTemplate(nodes, edges).map((i) => i.key)).toContain("cycle");
  });

  it("节点必须从 trigger 可达且可到达 end", () => {
    const { nodes, edges } = minimalGraph();
    const orphan = node("x", "flow.join", { mode: "all" }, 3);
    expect(validateTemplate([...nodes, orphan], edges).map((i) => i.key)).toContain("unreachableFromTrigger");
  });

  it("非 trigger 节点缺入边、非 end 节点缺出边报错", () => {
    const { nodes } = minimalGraph();
    const loose = validateTemplate(nodes, []).map((i) => i.key);
    expect(loose).toContain("noInput");
    expect(loose).toContain("noOutput");
  });

  it("必填配置缺失报错", () => {
    const { nodes, edges } = minimalGraph();
    const noInstruction = nodes.map((n) =>
      n.type_key === "agent.task" ? { ...n, config: {} } : n
    );
    expect(validateTemplate(noInstruction, edges).map((i) => i.key)).toContain("cfg.instruction");
  });

  it("非空节点名重复报 duplicateName,重名节点各标一条,空名不参与", () => {
    const { nodes, edges } = minimalGraph();
    const dup = nodes.map((n) => (n.id === "a" || n.id === "e" ? { ...n, name: "同名节点" } : n));
    const dupIssues = validateTemplate(dup, edges).filter((i) => i.key === "cfg.duplicateName");
    expect(dupIssues.map((i) => i.node_id).sort()).toEqual(["a", "e"]);

    // 空名可以有任意多个,不算重名
    const blanks = nodes.map((n) => ({ ...n, name: "" }));
    expect(validateTemplate(blanks, edges).map((i) => i.key)).not.toContain("cfg.duplicateName");
  });
});

describe("outputPorts", () => {
  it("switch 分支端口 = rules 分支 + default", () => {
    const ports = outputPorts("flow.switch", { rules: [{ branch: "a" }, { branch: "b" }] }).map((p) => p.key);
    expect(ports).toEqual(["a", "b", "default"]);
  });

  it("switch 空 rules 只剩 default 端口", () => {
    expect(outputPorts("flow.switch", { rules: [] }).map((p) => p.key)).toEqual(["default"]);
  });

  it("switch 端口派生剔除空名/重名/保留名", () => {
    const ports = outputPorts("flow.switch", {
      rules: [{ branch: " a " }, { branch: "a" }, { branch: "" }, { branch: "default" }, { branch: "b" }],
    }).map((p) => p.key);
    expect(ports).toEqual(["a", "b", "default"]);
  });

  it("parallel 分支端口 = 全部配置分支", () => {
    const ports = outputPorts("flow.parallel", { branches: ["x", "y"] }).map((p) => p.key);
    expect(ports).toEqual(["x", "y"]);
  });

  it("固定端口类型不受配置影响", () => {
    expect(outputPorts("agent.task", {}).map((p) => p.key)).toEqual(["success", "failed"]);
    expect(outputPorts("flow.end", {}).map((p) => p.key)).toEqual([]);
  });
});

describe("flow.switch 配置校验", () => {
  const switchNode = (config: unknown) => [
    node("t", "trigger.manual", {}, 0),
    node("s", "flow.switch", config, 1),
    node("e", "flow.end", {}, 2),
  ];
  const switchEdges = [
    edge("e1", "t", "success", "s", "input"),
    edge("e2", "s", "approved", "e", "completed"),
    edge("e3", "s", "default", "e", "failed"),
  ];
  const validConfig = {
    rules: [
      { branch: "approved", condition: { all: [{ path: "nodes.t.output.ok", op: "eq", value: "yes" }] } },
      { branch: "rejected", condition: { any: [{ path: "nodes.t.output.ok", op: "eq", value: "no" }] } },
    ],
  };

  it("合法 rules 通过校验", () => {
    expect(validateTemplate(switchNode(validConfig), switchEdges)).toEqual([]);
  });

  it("rules 为空或缺条件组报错", () => {
    expect(validateTemplate(switchNode({ rules: [] }), switchEdges).map((i) => i.key)).toContain("cfg.branches");
    const noCond = { rules: [{ branch: "approved", condition: { all: [] } }] };
    expect(validateTemplate(switchNode(noCond), switchEdges).map((i) => i.key)).toContain("cfg.switchCondition");
  });

  it("条件组 all 与 any 同时配置报冲突", () => {
    const conflict = { rules: [{ branch: "approved", condition: { all: [{ path: "p", op: "eq", value: "1" }], any: [{ path: "p", op: "eq", value: "2" }] } }] };
    expect(validateTemplate(switchNode(conflict), switchEdges).map((i) => i.key)).toContain("cfg.conditionConflict");
  });

  it("分支名为空/保留名/重复分别报错", () => {
    const emptyName = { rules: [{ branch: "", condition: validConfig.rules[0].condition }] };
    expect(validateTemplate(switchNode(emptyName), switchEdges).map((i) => i.key)).toContain("cfg.reservedBranchName");
    const reserved = { rules: [{ branch: "default", condition: validConfig.rules[0].condition }] };
    expect(validateTemplate(switchNode(reserved), switchEdges).map((i) => i.key)).toContain("cfg.reservedBranchName");
    const dup = { rules: [
      { branch: "approved", condition: validConfig.rules[0].condition },
      { branch: "approved", condition: validConfig.rules[1].condition },
    ] };
    expect(validateTemplate(switchNode(dup), switchEdges).map((i) => i.key)).toContain("cfg.duplicateBranch");
  });
});
