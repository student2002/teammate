// 预制起始模板与数据路径候选。预制模板仅用于编辑器"起始模板"选择器,
// UI 上带"预制"标识;字段选择器路径是编辑器的真实能力,与预制数据无关。

import type { TemplateEdge, TemplateGraph, TemplateNode } from "./types";

const N = (
  id: string,
  type_key: TemplateNode["type_key"],
  name: string,
  sort_order: number,
  config: Record<string, unknown>,
  x: number,
  y: number
): TemplateNode => ({ id, type_key, name, sort_order, config, ui_config: { x, y } });

const E = (
  id: string,
  source_node_id: string,
  source_port: string,
  target_node_id: string,
  target_port: string
): TemplateEdge => ({ id, source_node_id, source_port, target_node_id, target_port });

// 新建模板的空白骨架:固定开始(手动触发)与完成节点,成功端口连到完成
export function buildBlankTemplate(): TemplateGraph {
  return {
    name: "",
    description: "",
    enabled: true,
    nodes: [
      N("n-start", "trigger.manual", "开始", 0, {}, 160, 200),
      N("n-end", "flow.end", "完成", 1, {}, 520, 200),
    ],
    edges: [E("e-start-end", "n-start", "success", "n-end", "completed")],
  };
}

// 预制 Agent 任务节点的通用配置
const agentConfig = (instruction: string) => ({
  instruction,
  assignee: { mode: "any_agent", agent_id: null },
  input_map: {},
  timeout_minutes: 60,
  retry: { max_attempts: 1, backoff_seconds: 30 },
  readonly_dirs: [],
  full_control_dirs: ["."],
});

// 预制模板:每个触发类型一个最小可运行样例,新增触发类型时在此补一个构造器
export function buildManualTemplate(): TemplateGraph {
  return {
    name: "手动触发的 Agent 任务",
    description: "手动启动,由 Agent 按指令完成工作",
    enabled: true,
    nodes: [
      N("m-start", "trigger.manual", "开始", 0, {}, 160, 200),
      N("m-agent", "agent.task", "执行 Agent 指令", 1, agentConfig("根据任务输入完成工作"), 440, 200),
      N("m-end", "flow.end", "完成", 2, {}, 720, 200),
    ],
    edges: [
      E("m-e1", "m-start", "success", "m-agent", "input"),
      E("m-e2", "m-agent", "success", "m-end", "completed"),
      E("m-e3", "m-agent", "failed", "m-end", "failed"),
    ],
  };
}

export function buildScheduleTemplate(): TemplateGraph {
  return {
    name: "定时巡检与自动修复",
    description: "按间隔执行巡检命令,失败时交给 Agent 修复",
    enabled: true,
    nodes: [
      N("s-start", "trigger.schedule", "开始", 0, { interval_minutes: 30, project_id: "" }, 160, 200),
      N("s-cmd", "action.command", "运行巡检命令", 1, { command: { program: "go", args: ["test", "./..."], workdir: "." }, timeout_minutes: 30, retry: { max_attempts: 1 } }, 440, 200),
      N("s-cond", "flow.condition", "命令是否成功", 2, { condition: { all: [{ path: "nodes.s-cmd.output.exit_code", op: "eq", value: "0" }] } }, 720, 200),
      N("s-fix", "agent.task", "修复异常", 3, agentConfig("根据命令输出排查并修复问题"), 1000, 360),
      N("s-end", "flow.end", "完成", 4, {}, 1280, 200),
    ],
    edges: [
      E("s-e1", "s-start", "success", "s-cmd", "input"),
      E("s-e2", "s-cmd", "success", "s-cond", "input"),
      E("s-e3", "s-cond", "true", "s-end", "completed"),
      E("s-e4", "s-cond", "false", "s-fix", "input"),
      E("s-e5", "s-fix", "success", "s-end", "completed"),
      E("s-e6", "s-fix", "failed", "s-end", "failed"),
      E("s-e7", "s-cmd", "failed", "s-end", "failed"),
    ],
  };
}

export function buildWebhookTemplate(): TemplateGraph {
  return {
    name: "Webhook 请求处理",
    description: "接收 Webhook 请求,由 Agent 处理后结束",
    enabled: true,
    nodes: [
      N("w-start", "trigger.webhook", "开始", 0, {}, 160, 200),
      N("w-agent", "agent.task", "处理请求", 1, agentConfig("根据 Webhook 输入完成处理"), 440, 200),
      N("w-end", "flow.end", "完成", 2, {}, 720, 200),
    ],
    edges: [
      E("w-e1", "w-start", "success", "w-agent", "input"),
      E("w-e2", "w-agent", "success", "w-end", "completed"),
      E("w-e3", "w-agent", "failed", "w-end", "failed"),
    ],
  };
}

export const ISSUE_FIX_TEMPLATE: TemplateGraph = {
  name: "GitHub Issue 自动修复",
  description: "Issue 带 bug 标签时自动分析修复,跑测试后创建 PR 并等待 Review",
  enabled: true,
  nodes: [
    N("n-trigger", "trigger.github", "开始", 0, { integration_id: "", event: "issues", actions: ["opened"] }, 40, 260),
    N("n-cond-label", "flow.condition", "Issue 是否带 bug 标签", 1, { condition: { all: [{ path: "trigger.issue.labels", op: "contains", value: "bug" }] } }, 320, 260),
    N("n-agent", "agent.task", "分析并修复", 2, {
      instruction: "分析 Issue 并完成修复",
      assignee: { mode: "any_agent", agent_id: null },
      input_map: { title: "{{ trigger.issue.title }}", body: "{{ trigger.issue.body }}", repository: "{{ trigger.repository.full_name }}" },
      timeout_minutes: 60,
      retry: { max_attempts: 2, backoff_seconds: 30 },
      readonly_dirs: [],
      full_control_dirs: ["."],
    }, 600, 120),
    N("n-test", "action.command", "运行测试", 3, { command: { program: "go", args: ["test", "./..."], workdir: "." }, timeout_minutes: 30, retry: { max_attempts: 1 } }, 880, 120),
    N("n-cond-test", "flow.condition", "测试是否通过", 4, { condition: { all: [{ path: "nodes.n-test.output.exit_code", op: "eq", value: "0" }] } }, 1160, 120),
    N("n-pr", "action.github", "创建 PR 并评论 Issue", 5, {
      operation: "create_pull_request",
      title: "Fix Issue #{{ trigger.issue.number }}: {{ trigger.issue.title }}",
      head: "teammate/task-{{ task.id }}",
      base: "{{ trigger.repository.default_branch }}",
      body: "由工作流自动创建",
      comment_issue: "{{ trigger.issue.number }}",
    }, 1440, 40),
    N("n-wait", "flow.wait", "等待 PR Review", 6, { mode: "github_event", event: "pull_request_review", filter: {} }, 1720, 40),
    N("n-cond-review", "flow.condition", "Review 是否通过", 7, { condition: { all: [{ path: "nodes.n-wait.output.review.state", op: "eq", value: "approved" }] } }, 2000, 40),
    N("n-task", "action.create_task", "处理 Review 意见", 8, { title: "处理 Review 意见:{{ nodes.n-wait.output.review.body }}", description: "" }, 2280, 200),
    N("n-add-label", "action.github", "添加 triage 标签", 9, { operation: "add_labels", issue: "{{ trigger.issue.number }}", labels: "triage" }, 600, 440),
    N("n-end", "flow.end", "完成", 10, {}, 2560, 260),
  ],
  edges: [
    E("e01", "n-trigger", "success", "n-cond-label", "input"),
    E("e02", "n-cond-label", "true", "n-agent", "input"),
    E("e03", "n-cond-label", "false", "n-add-label", "input"),
    E("e04", "n-add-label", "success", "n-end", "completed"),
    E("e05", "n-add-label", "failed", "n-end", "failed"),
    E("e06", "n-agent", "success", "n-test", "input"),
    E("e07", "n-agent", "failed", "n-end", "failed"),
    E("e08", "n-test", "success", "n-cond-test", "input"),
    E("e09", "n-test", "failed", "n-end", "failed"),
    E("e10", "n-cond-test", "true", "n-pr", "input"),
    E("e11", "n-cond-test", "false", "n-end", "failed"),
    E("e12", "n-pr", "success", "n-wait", "input"),
    E("e13", "n-pr", "failed", "n-end", "failed"),
    E("e14", "n-wait", "resumed", "n-cond-review", "input"),
    E("e16", "n-cond-review", "true", "n-end", "completed"),
    E("e17", "n-cond-review", "false", "n-task", "input"),
    E("e18", "n-task", "success", "n-end", "completed"),
    E("e19", "n-task", "failed", "n-end", "failed"),
  ],
};

// instantiateTemplate 把一份预制图实例化为可保存的整图:
// 节点/边 id 换成全新 UUID(不同工作区可各自保存同一预制而不撞全局主键),
// 条件表达式里 nodes.<旧id>.output 的引用同步改写为新 id(context.nodes 以
// 模板节点 id 为键)。
export function instantiateTemplate(source: TemplateGraph): TemplateGraph {
  const graph = structuredClone(source);
  const idMap = new Map<string, string>();
  for (const node of graph.nodes) {
    idMap.set(node.id, freshUUID());
  }
  const rewrite = (text: string): string => {
    let out = text;
    for (const [oldID, newID] of idMap) {
      out = out.split(`nodes.${oldID}.`).join(`nodes.${newID}.`);
    }
    return out;
  };
  for (const node of graph.nodes) {
    node.id = idMap.get(node.id)!;
    node.config = JSON.parse(rewrite(JSON.stringify(node.config)));
  }
  for (const edge of graph.edges) {
    edge.id = freshUUID();
    edge.source_node_id = idMap.get(edge.source_node_id)!;
    edge.target_node_id = idMap.get(edge.target_node_id)!;
  }
  return graph;
}

function freshUUID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `id-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

// ---- 字段选择器路径候选(§13.3) ----

const TRIGGER_PATHS = [
  "trigger.event",
  "trigger.action",
  "trigger.delivery_id",
  "trigger.repository.id",
  "trigger.repository.full_name",
  "trigger.repository.default_branch",
  "trigger.sender.login",
  "trigger.issue.number",
  "trigger.issue.title",
  "trigger.issue.body",
  "trigger.issue.labels",
  "trigger.issue.url",
  "trigger.pull_request.number",
  "trigger.pull_request.title",
  "trigger.pull_request.state",
  "task.id",
  "task.title",
  "task.project_id",
];

const OUTPUT_SCHEMA: Partial<Record<TemplateNode["type_key"], string[]>> = {
  "agent.task": ["summary", "git.branch", "git.commit"],
  "action.command": ["exit_code", "stdout_tail", "stderr_tail"],
  "action.http": ["status", "body", "headers"],
  "action.create_task": ["task_id", "task_title"],
  "action.github": ["pr_number", "pr_url", "comment_url"],
  "flow.wait": ["review.state", "review.body", "pull_request.number"],
  "data.set": [],
  "data.map": [],
};

// 给定全图与当前节点,返回可插入的 {{path}} 候选:trigger/task 全量 + 上游节点输出 + vars
export function contextPaths(nodes: TemplateNode[], edges: TemplateEdge[], currentNodeId: string | null): string[] {
  const upstream = new Set<string>();
  if (currentNodeId) {
    const seen = new Set<string>();
    const stack = [currentNodeId];
    while (stack.length) {
      const cur = stack.pop()!;
      if (seen.has(cur)) continue;
      seen.add(cur);
      for (const e of edges) {
        if (e.target_node_id === cur && !seen.has(e.source_node_id)) {
          upstream.add(e.source_node_id);
          stack.push(e.source_node_id);
        }
      }
    }
    upstream.delete(currentNodeId);
  }

  const paths = [...TRIGGER_PATHS];
  for (const id of upstream) {
    const node = nodes.find((x) => x.id === id);
    if (!node) continue;
    const schema = OUTPUT_SCHEMA[node.type_key] ?? [];
    // 名字式引用(对齐 n8n):运行上下文按节点名暴露别名;名字为空回退 id
    const ref = node.name?.trim() || node.id;
    for (const p of schema) paths.push(`nodes.${ref}.output.${p}`);
  }
  for (const node of nodes) {
    if (node.type_key === "data.set" && node.id !== currentNodeId) {
      const name = node.config?.name;
      if (typeof name === "string" && name.trim()) paths.push(`vars.${name.trim()}`);
    }
  }
  return paths;
}
