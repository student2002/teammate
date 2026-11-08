// 节点目录:19 种 type_key 的结构契约(§2.1)。type_key、端口与分类和后端
// GET /api/workflow-node-types 契约一致,S7 接真实接口后此文件仅保留 UI 元数据。
import type { LucideIcon } from "lucide-react";
import {
  Bot,
  CircleCheck,
  Clock,
  Database,
  GitBranch,
  ListPlus,
  Play,
  Plug,
  Repeat,
  Terminal,
  User,
} from "lucide-react";
import type { NodeCategory, TemplateNode, WorkflowTypeKey } from "./types";

export type Tone = "neutral" | "primary" | "accent" | "success" | "warning" | "danger";

export interface PortDef {
  key: string;
  tone: Tone;
}

export interface NodeTypeDefinition {
  type_key: WorkflowTypeKey;
  category: NodeCategory;
  executor: "server" | "agentd" | "human";
  input_ports: PortDef[];
  fixed_output_ports: PortDef[];
  // switch 输出端口由 config.rules[].branch 派生,parallel 由 config.branches 派生
  output_ports_kind: "fixed" | "switch_branches" | "parallel_branches";
  // 开始/完成固定存在于模板,不进节点库
  in_library: boolean;
  icon: LucideIcon;
}

const INPUT: PortDef = { key: "input", tone: "neutral" };
const COMPLETED: PortDef = { key: "completed", tone: "success" };
const FAILED_IN: PortDef = { key: "failed", tone: "danger" };
const SUCCESS: PortDef = { key: "success", tone: "success" };
const FAILED: PortDef = { key: "failed", tone: "danger" };
const TRUE: PortDef = { key: "true", tone: "success" };
const FALSE: PortDef = { key: "false", tone: "danger" };

export const NODE_CATALOG: Record<WorkflowTypeKey, NodeTypeDefinition> = {
  "trigger.manual": { type_key: "trigger.manual", category: "trigger", executor: "server", input_ports: [], fixed_output_ports: [SUCCESS], output_ports_kind: "fixed", in_library: false, icon: Play },
  "trigger.schedule": { type_key: "trigger.schedule", category: "trigger", executor: "server", input_ports: [], fixed_output_ports: [SUCCESS], output_ports_kind: "fixed", in_library: false, icon: Clock },
  "trigger.github": { type_key: "trigger.github", category: "trigger", executor: "server", input_ports: [], fixed_output_ports: [SUCCESS], output_ports_kind: "fixed", in_library: false, icon: Plug },
  "trigger.webhook": { type_key: "trigger.webhook", category: "trigger", executor: "server", input_ports: [], fixed_output_ports: [SUCCESS], output_ports_kind: "fixed", in_library: false, icon: Plug },
  "agent.task": { type_key: "agent.task", category: "agent", executor: "agentd", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Bot },
  "action.github": { type_key: "action.github", category: "integration", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Plug },
  "action.http": { type_key: "action.http", category: "integration", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Plug },
  "action.command": { type_key: "action.command", category: "action", executor: "agentd", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Terminal },
  "action.create_task": { type_key: "action.create_task", category: "task", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: ListPlus },
  "workflow.call": { type_key: "workflow.call", category: "reuse", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Repeat },
  "data.set": { type_key: "data.set", category: "data", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Database },
  "data.map": { type_key: "data.map", category: "data", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS, FAILED], output_ports_kind: "fixed", in_library: true, icon: Database },
  "flow.condition": { type_key: "flow.condition", category: "logic", executor: "server", input_ports: [INPUT], fixed_output_ports: [TRUE, FALSE], output_ports_kind: "fixed", in_library: true, icon: GitBranch },
  "flow.switch": { type_key: "flow.switch", category: "logic", executor: "server", input_ports: [INPUT], fixed_output_ports: [], output_ports_kind: "switch_branches", in_library: true, icon: GitBranch },
  "flow.parallel": { type_key: "flow.parallel", category: "logic", executor: "server", input_ports: [INPUT], fixed_output_ports: [], output_ports_kind: "parallel_branches", in_library: true, icon: GitBranch },
  "flow.join": { type_key: "flow.join", category: "logic", executor: "server", input_ports: [INPUT], fixed_output_ports: [SUCCESS], output_ports_kind: "fixed", in_library: true, icon: GitBranch },
  "flow.wait": { type_key: "flow.wait", category: "wait", executor: "server", input_ports: [INPUT], fixed_output_ports: [{ key: "resumed", tone: "accent" }, { key: "timeout", tone: "warning" }], output_ports_kind: "fixed", in_library: true, icon: Clock },
  "human.input": { type_key: "human.input", category: "human", executor: "human", input_ports: [INPUT], fixed_output_ports: [{ key: "submitted", tone: "success" }, { key: "cancelled", tone: "neutral" }, { key: "timeout", tone: "warning" }], output_ports_kind: "fixed", in_library: true, icon: User },
  "flow.end": { type_key: "flow.end", category: "end", executor: "server", input_ports: [COMPLETED, FAILED_IN], fixed_output_ports: [], output_ports_kind: "fixed", in_library: false, icon: CircleCheck },
};

// 节点库分组顺序(§13.1)。trigger 不列具体类型:开始节点固定存在,
// 触发方式(手动/定时/GitHub/Webhook)在 Inspector 中切换。
export const LIBRARY_GROUPS: Array<{ category: NodeCategory; typeKeys: WorkflowTypeKey[] }> = [
  { category: "agent", typeKeys: ["agent.task"] },
  { category: "integration", typeKeys: ["action.github", "action.http"] },
  { category: "action", typeKeys: ["action.command"] },
  { category: "task", typeKeys: ["action.create_task"] },
  { category: "reuse", typeKeys: ["workflow.call"] },
  { category: "data", typeKeys: ["data.set", "data.map"] },
  { category: "logic", typeKeys: ["flow.condition", "flow.switch", "flow.parallel", "flow.join"] },
  { category: "wait", typeKeys: ["flow.wait"] },
  { category: "human", typeKeys: ["human.input"] },
];

export function def(typeKey: WorkflowTypeKey): NodeTypeDefinition {
  return NODE_CATALOG[typeKey];
}

function branchPort(key: string): PortDef {
  return { key, tone: "accent" };
}

// switch/parallel 的输出端口由配置派生;其他类型返回固定端口
export function outputPorts(typeKey: WorkflowTypeKey, config: unknown): PortDef[] {
  const d = def(typeKey);
  if (d.output_ports_kind === "switch_branches") {
    const names: string[] = [];
    for (const rule of readSwitchRules(config)) {
      const name = rule.branch.trim();
      // 空名/重名/保留名不生成端口,交由校验提示;末尾固定追加 default 兜底端口
      if (name === "" || name === "default" || names.includes(name)) continue;
      names.push(name);
    }
    return [...names.map(branchPort), branchPort("default")];
  }
  if (d.output_ports_kind === "parallel_branches") {
    return readBranches(config).map(branchPort);
  }
  return d.fixed_output_ports;
}

export function readBranches(config: unknown): string[] {
  const raw = (config as { branches?: unknown } | null)?.branches;
  if (!Array.isArray(raw)) return [];
  return raw.filter((b): b is string => typeof b === "string" && b.trim() !== "");
}

// flow.switch 的分支规则(config.rules):安全解析,非数组返回空
export interface SwitchRule {
  branch: string;
  condition: unknown;
}

export function readSwitchRules(config: unknown): SwitchRule[] {
  const raw = (config as { rules?: unknown } | null)?.rules;
  if (!Array.isArray(raw)) return [];
  return raw
    .filter((r): r is Record<string, unknown> => typeof r === "object" && r !== null)
    .map((r) => ({ branch: String(r.branch ?? ""), condition: r.condition }));
}

// 新增节点时的默认配置(§3.2/§3.3/§4.3 形状);空值字段由 Inspector 引导填写
export function defaultConfig(typeKey: WorkflowTypeKey): Record<string, unknown> {
  switch (typeKey) {
    case "trigger.schedule":
      // project_id pins the project the fired runs land in; the Inspector
      // offers the workspace real project list.
      return { interval_minutes: 30, project_id: "" };
    case "trigger.github":
      return { integration_id: "", event: "issues", actions: ["opened"] };
    case "agent.task":
      return {
        instruction: "",
        assignee: { mode: "any_agent", agent_id: null },
        input_map: {},
        timeout_minutes: 60,
        retry: { max_attempts: 1, backoff_seconds: 30 },
        readonly_dirs: [],
        full_control_dirs: ["."],
      };
    case "action.github":
      return { operation: "add_comment", issue: "{{ trigger.issue.number }}", body: "" };
    case "action.http":
      return { method: "GET", url: "", headers: {}, query: {}, body: "" };
    case "action.command":
      return { command: { program: "", args: [], workdir: "." }, timeout_minutes: 30, retry: { max_attempts: 1 } };
    case "action.create_task":
      return { title: "", description: "" };
    case "workflow.call":
      return { template_id: "" };
    case "data.set":
      return { name: "", value: "" };
    case "data.map":
      return { mappings: [] };
    case "flow.condition":
      return { condition: { all: [] } };
    case "flow.switch":
      return { rules: [] };
    case "flow.parallel":
      return { branches: [] };
    case "flow.join":
      return { mode: "all" };
    case "flow.wait":
      return { mode: "duration", minutes: 10 };
    case "human.input":
      return { prompt: "", fields: [] };
    case "trigger.manual":
    case "trigger.webhook":
    case "flow.end":
      return {};
  }
}

// 卡片摘要行(§13.2):返回 i18n key 与参数,由节点卡组件渲染
export interface SummaryItem {
  key: string;
  params?: Record<string, string | number>;
}

export function summaryItems(typeKey: WorkflowTypeKey, config: unknown): SummaryItem[] {
  const c = (config ?? {}) as Record<string, unknown>;
  switch (typeKey) {
    case "trigger.manual":
      return [{ key: "sum.triggerManual" }];
    case "trigger.schedule":
      return [{ key: "sum.triggerSchedule", params: { minutes: Number(c.interval_minutes) || 0 } }];
    case "trigger.github":
      return [
        { key: "sum.triggerGithubEvent", params: { event: String(c.event ?? "") } },
        { key: "sum.triggerGithubActions", params: { actions: readStringArray(c.actions).join(", ") } },
      ];
    case "trigger.webhook":
      return [{ key: "sum.triggerWebhook" }];
    case "agent.task": {
      const items: SummaryItem[] = [];
      const instruction = String(c.instruction ?? "").trim();
      if (instruction) items.push({ key: "sum.instruction", params: { text: instruction } });
      const mode = (c.assignee as { mode?: string } | null)?.mode;
      if (mode === "specific_agent") items.push({ key: "sum.assigneeSpecific" });
      else items.push({ key: "sum.assigneeAny" });
      items.push({
        key: "sum.timeoutRetry",
        params: { timeout: Number(c.timeout_minutes) || 0, attempts: Number((c.retry as { max_attempts?: number } | null)?.max_attempts) || 1 },
      });
      return items;
    }
    case "action.github": {
      const items: SummaryItem[] = [{ key: "sum.githubOperation", params: { operation: String(c.operation ?? "") } }];
      const issue = String(c.issue ?? "").trim();
      if (issue) items.push({ key: "sum.targetExpr", params: { text: issue } });
      return items;
    }
    case "action.http":
      return [
        { key: "sum.httpRequest", params: { method: String(c.method ?? "GET"), url: String(c.url ?? "") } },
      ];
    case "action.command": {
      const cmd = (c.command ?? {}) as { program?: unknown; args?: unknown };
      return [
        {
          key: "sum.command",
          params: { program: String(cmd.program ?? ""), args: readStringArray(cmd.args).join(" ") },
        },
      ];
    }
    case "action.create_task":
      return [{ key: "sum.createTask", params: { text: String(c.title ?? "") } }];
    case "workflow.call":
      return [{ key: "sum.callWorkflow", params: { text: String(c.template_id ?? "") } }];
    case "data.set":
      return [{ key: "sum.setVar", params: { name: String(c.name ?? "") } }];
    case "data.map":
      return [{ key: "sum.mapFields", params: { count: readMappings(c.mappings).length } }];
    case "flow.condition":
      return [{ key: "sum.condition", params: { text: conditionSummary(c.condition) } }];
    case "flow.switch":
      return [{ key: "sum.branches", params: { names: readSwitchRules(c).map((r) => r.branch.trim()).filter(Boolean).join(", ") } }];
    case "flow.parallel":
      return [{ key: "sum.branches", params: { names: readBranches(c).join(", ") } }];
    case "flow.join":
      return [String(c.mode) === "any" ? { key: "sum.joinAny" } : { key: "sum.joinAll" }];
    case "flow.wait":
      return String(c.mode) === "github_event"
        ? [{ key: "sum.waitEvent", params: { event: String(c.event ?? "") } }]
        : [{ key: "sum.waitDuration", params: { minutes: Number(c.minutes) || 0 } }];
    case "human.input":
      return [{ key: "sum.humanPrompt", params: { text: String(c.prompt ?? "") } }];
    case "flow.end":
      return [];
  }
}

export function readStringArray(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  return v.filter((x): x is string => typeof x === "string");
}

// 模板卡的触发摘要:从 trigger 节点的 type_key 与 config 推导(i18n key + 参数),
// 无触发节点(理论不可发生)返回 null
export interface TriggerSummary {
  key: "manual" | "schedule" | "github" | "webhook";
  params?: Record<string, string | number>;
}

export function triggerSummary(nodes: Array<Pick<TemplateNode, "type_key" | "config">>): TriggerSummary | null {
  const trigger = nodes.find((n) => typeof n.type_key === "string" && n.type_key.startsWith("trigger."));
  if (!trigger) return null;
  const c = (trigger.config ?? {}) as Record<string, unknown>;
  switch (trigger.type_key) {
    case "trigger.schedule":
      return { key: "schedule", params: { minutes: Number(c.interval_minutes) || 0 } };
    case "trigger.github":
      return {
        key: "github",
        params: {
          event: String(c.event ?? ""),
          actions: readStringArray(c.actions).join(", "),
        },
      };
    case "trigger.webhook":
      return { key: "webhook" };
    default:
      return { key: "manual" };
  }
}

export interface MappingRow {
  target: string;
  source: string;
}

export function readMappings(v: unknown): MappingRow[] {
  if (!Array.isArray(v)) return [];
  return v
    .filter((x): x is Record<string, unknown> => typeof x === "object" && x !== null)
    .map((x) => ({ target: String(x.target ?? ""), source: String(x.source ?? "") }));
}

// 条件 AST → 单行摘要文案;操作符与路径保持原文,值按字符串展示;any 组用 || 连接
export function conditionSummary(ast: unknown): string {
  if (typeof ast !== "object" || ast === null) return "";
  const obj = ast as { all?: unknown; any?: unknown };
  const isAny = Array.isArray(obj.any);
  const rules = (Array.isArray(obj.all) ? obj.all : isAny ? obj.any : []) as Array<Record<string, unknown>>;
  return rules
    .map((r) => `${String(r.path ?? "")} ${String(r.op ?? "")} ${r.value === undefined ? "" : String(r.value)}`)
    .join(isAny ? " || " : " && ");
}

// 生成节点/连线 ID;crypto.randomUUID 仅在安全上下文(https/localhost)可用,此处提供降级
export function newId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `id-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

// 有 i18n 文案的端口键集合;其余端口(分支名)原样展示
export const KNOWN_PORT_KEYS = new Set([
  "input", "success", "failed", "true", "false", "resumed", "timeout", "submitted", "cancelled", "completed", "default",
]);

export const HANDLE_BASE = { width: 9, height: 9, border: "1.5px solid var(--surface)" } as const;

// 端口行几何(ComfyUI 式布局):端口条是标题栏下的专用横条,每端口占一行,
// 输入名在圆点右侧、输出名在圆点左侧,正文排在端口条之下,文字互不重叠
export const PORT_ROW_H = 15;
const PORT_BAND_PAD = 3;

export function portCenterY(index: number): number {
  return PORT_BAND_PAD + index * PORT_ROW_H + PORT_ROW_H / 2;
}

export function portBandHeight(inCount: number, outCount: number): number {
  const rows = Math.max(inCount, outCount);
  return rows === 0 ? 0 : rows * PORT_ROW_H + PORT_BAND_PAD * 2;
}

export const CATEGORY_TONE: Record<NodeCategory, Tone> = {
  trigger: "primary",
  agent: "accent",
  integration: "success",
  action: "warning",
  task: "accent",
  reuse: "neutral",
  data: "neutral",
  logic: "primary",
  wait: "warning",
  human: "danger",
  end: "success",
};

// 语气 → 主题 CSS 变量,用于画布内联样式(端口圆点、类型色)
export const TONE_VAR: Record<Tone, string> = {
  neutral: "var(--fg-faint)",
  primary: "var(--primary)",
  accent: "var(--accent)",
  success: "var(--success)",
  warning: "var(--warning)",
  danger: "var(--danger)",
};
