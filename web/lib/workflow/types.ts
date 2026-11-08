// 工作流前端契约类型。字段名与工作流 API 契约(§12.2)保持 snake_case 一致,
// 与真实接口共用同一形状,不经过 mappers 转换层。

export type WorkflowTypeKey =
  | "trigger.manual"
  | "trigger.schedule"
  | "trigger.github"
  | "trigger.webhook"
  | "agent.task"
  | "action.github"
  | "action.http"
  | "action.command"
  | "action.create_task"
  | "workflow.call"
  | "data.set"
  | "data.map"
  | "flow.condition"
  | "flow.switch"
  | "flow.parallel"
  | "flow.join"
  | "flow.wait"
  | "human.input"
  | "flow.end";

export type NodeCategory =
  | "trigger"
  | "agent"
  | "integration"
  | "action"
  | "task"
  | "reuse"
  | "data"
  | "logic"
  | "wait"
  | "human"
  | "end";

// 画布节点位置与折叠状态,保存时随模板整图提交
export interface UiConfig {
  x: number;
  y: number;
  collapsed?: boolean;
}

export interface TemplateNode {
  id: string;
  type_key: WorkflowTypeKey;
  name: string;
  sort_order: number;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  config: any;
  // 运行节点无画布位置;缺省时视图层用 dagre 自动布局
  ui_config?: UiConfig;
}

export interface TemplateEdge {
  id: string;
  source_node_id: string;
  source_port: string;
  target_node_id: string;
  target_port: string;
  label?: string;
}

// 整图写入载荷(POST/PUT /workspaces/{ws}/workflows 共用)
export interface TemplateGraph {
  name: string;
  description: string;
  enabled: boolean;
  nodes: TemplateNode[];
  edges: TemplateEdge[];
}

// 模板元信息(GET 列表/详情的 template 字段)
export interface TemplateMeta {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  is_builtin: boolean;
  version: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

// 列表/详情响应条目:{template, nodes, edges}
export interface TemplateListItem {
  template: TemplateMeta;
  nodes: TemplateNode[];
  edges: TemplateEdge[];
}

// ---- 条件 AST(§4.3) ----

export type ConditionOp =
  | "eq"
  | "neq"
  | "exists"
  | "contains"
  | "in"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "matches";

export interface ConditionRule {
  path: string;
  op: ConditionOp;
  value?: string;
}

export interface ConditionAst {
  all?: ConditionRule[];
  any?: ConditionRule[];
}

// ---- 运行视图(§13.5,数据来自 GET /tasks/{id}/graph) ----

export type RunNodeStatus =
  | "waiting"
  | "ready"
  | "running"
  | "succeeded"
  | "failed"
  | "skipped"
  | "cancelled"
  | "manual_intervention";

export type EdgeResolution = "pending" | "activated" | "skipped";

export interface RunNodeAttempt {
  attempt_no: number;
  status: "running" | "succeeded" | "failed" | "cancelled";
  started_at: string;
  completed_at?: string;
  error?: { code: string; message: string } | null;
}

export interface RunNode extends TemplateNode {
  status: RunNodeStatus;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  input?: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  output?: any;
  error?: { code: string; message: string } | null;
  attempts: RunNodeAttempt[];
  // 等待信息:kind 由 wait_key 前缀推导(duration/event/human)
  wait?: { kind: "duration" | "event" | "human"; expires_at?: string } | null;
}

export interface RunEdge extends TemplateEdge {
  resolution: EdgeResolution;
}

export interface RunSample {
  task_title: string;
  task_status: "active" | "completed" | "failed" | "cancelled";
  workflow_name: string;
  workflow_version: number;
  started_at: string;
  nodes: RunNode[];
  edges: RunEdge[];
}
