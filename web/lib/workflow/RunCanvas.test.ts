import { describe, expect, it } from "vitest";
import { layoutNodes } from "./RunCanvas";
import type { TemplateEdge, TemplateNode, WorkflowTypeKey } from "./types";

const node = (id: string, ui_config?: { x: number; y: number }): TemplateNode => ({
  id,
  type_key: "agent.task" as WorkflowTypeKey,
  name: id,
  sort_order: 0,
  config: {},
  ui_config,
});

const edge = (id: string, source_node_id: string, target_node_id: string): TemplateEdge => ({
  id,
  source_node_id,
  source_port: "success",
  target_node_id,
  target_port: "input",
  label: "",
});

describe("layoutNodes", () => {
  // 渲染类缺陷回归:缺 ui_config 的输入兜底 (0,0) 会让全部节点叠在原点且跳过自动布局
  it("assigns distinct dagre coordinates when ui_config is missing", () => {
    const nodes = [node("a"), node("b"), node("c")];
    const edges = [edge("e1", "a", "b"), edge("e2", "a", "c")];
    const laid = layoutNodes(nodes, edges);
    const coords = laid.map((n) => `${n.ui_config?.x},${n.ui_config?.y}`);
    expect(new Set(coords).size).toBe(3);
  });

  it("keeps positions untouched when every node carries ui_config", () => {
    const nodes = [node("a", { x: 12, y: 34 }), node("b", { x: -200, y: 5 })];
    const edges = [edge("e1", "a", "b")];
    expect(layoutNodes(nodes, edges)).toEqual([node("a", { x: 12, y: 34 }), node("b", { x: -200, y: 5 })]);
  });
});
