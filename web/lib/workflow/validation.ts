// 模板整图即时校验(§5.1 的前端镜像)。Server 是唯一信任边界并重复完整校验,
// 这里只做编辑过程中的即时反馈;issue.key 对应 workflows.editor.issues 下的文案。
import { NODE_CATALOG, def, outputPorts, readBranches, readMappings, readSwitchRules } from "./catalog";
import type { ConditionAst, TemplateEdge, TemplateNode } from "./types";

export interface ValidationIssue {
  node_id?: string;
  edge_id?: string;
  key: string;
  params?: Record<string, string | number>;
}

const EXTRA_OPS = new Set(["eq", "neq", "exists", "contains", "in", "gt", "gte", "lt", "lte", "matches"]);

export function validateTemplate(nodes: TemplateNode[], edges: TemplateEdge[]): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const triggers = nodes.filter((n) => n.type_key.startsWith("trigger."));
  const ends = nodes.filter((n) => n.type_key === "flow.end");

  if (triggers.length === 0) issues.push({ key: "triggerMissing" });
  if (triggers.length > 1) issues.push({ key: "triggerMultiple", params: { count: triggers.length } });
  if (ends.length === 0) issues.push({ key: "endMissing" });
  if (ends.length > 1) issues.push({ key: "endMultiple", params: { count: ends.length } });

  for (const node of nodes) {
    if (!Object.prototype.hasOwnProperty.call(NODE_CATALOG, node.type_key)) {
      issues.push({ node_id: node.id, key: "typeUnknown", params: { type: node.type_key } });
    }
  }

  // 非空节点名须唯一:运行上下文按名字暴露别名(nodes.<名字>.output),
  // 重名会让按名字引用有歧义;空名不参与,重名的每个节点各报一条
  const nameCount = new Map<string, number>();
  for (const node of nodes) {
    const name = (node.name ?? "").trim();
    if (!name) continue;
    nameCount.set(name, (nameCount.get(name) ?? 0) + 1);
  }
  for (const node of nodes) {
    const name = (node.name ?? "").trim();
    if (name && (nameCount.get(name) ?? 0) > 1) {
      issues.push({ node_id: node.id, key: "cfg.duplicateName" });
    }
  }

  for (const edge of edges) {
    const source = byId.get(edge.source_node_id);
    const target = byId.get(edge.target_node_id);
    if (!source || !target) {
      issues.push({ edge_id: edge.id, key: "edgeDangling" });
      continue;
    }
    const sourcePorts = outputPorts(source.type_key, source.config);
    const targetPorts = def(target.type_key).input_ports;
    if (!sourcePorts.some((p) => p.key === edge.source_port)) {
      issues.push({ edge_id: edge.id, key: "sourcePortInvalid", params: { port: edge.source_port } });
    }
    if (!targetPorts.some((p) => p.key === edge.target_port)) {
      issues.push({ edge_id: edge.id, key: "targetPortInvalid", params: { port: edge.target_port } });
    }
  }

  // 同一 (source, port, target) 不重复;同一输出端口最多一条出边(§5.1 规则 5/8)
  const seen = new Set<string>();
  const portUsed = new Set<string>();
  for (const edge of edges) {
    const dupKey = `${edge.source_node_id}|${edge.source_port}|${edge.target_node_id}`;
    if (seen.has(dupKey)) issues.push({ edge_id: edge.id, key: "edgeDuplicate" });
    seen.add(dupKey);
    const portKey = `${edge.source_node_id}|${edge.source_port}`;
    if (portUsed.has(portKey)) issues.push({ edge_id: edge.id, key: "portMultiEdge" });
    portUsed.add(portKey);
  }

  if (hasCycle(nodes, edges)) issues.push({ key: "cycle" });

  const triggerId = triggers[0]?.id;
  const endIds = new Set(ends.map((e) => e.id));
  const reachableFromTrigger = triggerId ? reachable(triggerId, edges, true) : new Set<string>();
  const reachingEnd = reachable(endIds, edges, false);
  for (const node of nodes) {
    if (!triggerId) break;
    if (node.id !== triggerId && !reachableFromTrigger.has(node.id)) {
      issues.push({ node_id: node.id, key: "unreachableFromTrigger" });
      continue;
    }
    if (!endIds.has(node.id) && !reachingEnd.has(node.id)) {
      issues.push({ node_id: node.id, key: "cannotReachEnd" });
    }
  }

  for (const node of nodes) {
    const hasIn = edges.some((e) => e.target_node_id === node.id);
    const hasOut = edges.some((e) => e.source_node_id === node.id);
    if (!node.type_key.startsWith("trigger.") && !hasIn) issues.push({ node_id: node.id, key: "noInput" });
    if (node.type_key !== "flow.end" && !hasOut) issues.push({ node_id: node.id, key: "noOutput" });
    issues.push(...configIssues(node));
  }

  return issues;
}

function hasCycle(nodes: TemplateNode[], edges: TemplateEdge[]): boolean {
  const adj = new Map<string, string[]>();
  for (const e of edges) adj.set(e.source_node_id, [...(adj.get(e.source_node_id) ?? []), e.target_node_id]);
  const state = new Map<string, 1 | 2>();
  const visit = (id: string): boolean => {
    const st = state.get(id);
    if (st === 1) return true;
    if (st === 2) return false;
    state.set(id, 1);
    for (const next of adj.get(id) ?? []) if (visit(next)) return true;
    state.set(id, 2);
    return false;
  };
  return nodes.some((n) => visit(n.id));
}

// forward=true:从 trigger 正向可达集合;false:从 end 反向可达集合
function reachable(roots: string | Set<string>, edges: TemplateEdge[], forward: boolean): Set<string> {
  const adj = new Map<string, string[]>();
  for (const e of edges) {
    const from = forward ? e.source_node_id : e.target_node_id;
    const to = forward ? e.target_node_id : e.source_node_id;
    adj.set(from, [...(adj.get(from) ?? []), to]);
  }
  const rootSet = typeof roots === "string" ? [roots] : [...roots];
  const seen = new Set<string>();
  const stack = rootSet.filter(Boolean);
  while (stack.length) {
    const cur = stack.pop()!;
    if (seen.has(cur)) continue;
    seen.add(cur);
    stack.push(...(adj.get(cur) ?? []));
  }
  return seen;
}

// 各类型必填配置(§2.1/§3/§4);缺省项由 Inspector 引导补齐
function configIssues(node: TemplateNode): ValidationIssue[] {
  const c = (node.config ?? {}) as Record<string, unknown>;
  const str = (v: unknown) => typeof v === "string" && v.trim() !== "";
  const num = (v: unknown) => typeof v === "number" && Number.isFinite(v) && v > 0;
  const push = (key: string): ValidationIssue => ({ node_id: node.id, key });
  const issues: ValidationIssue[] = [];

  switch (node.type_key) {
    case "trigger.schedule":
      if (!num(c.interval_minutes)) issues.push(push("cfg.scheduleInterval"));
      // 触发的运行必须落在具体工程(后端校验器强制 project_id 为 UUID)
      if (!str(c.project_id)) issues.push(push("cfg.scheduleProject"));
      break;
    case "trigger.github": {
      if (!str(c.event)) issues.push(push("cfg.githubEvent"));
      if (readBranches({ branches: c.actions }).length === 0) issues.push(push("cfg.githubActions"));
      if (!str(c.integration_id)) issues.push(push("cfg.githubIntegration"));
      break;
    }
    case "agent.task":
      if (!str(c.instruction)) issues.push(push("cfg.instruction"));
      break;
    case "action.github":
      if (!str(c.operation)) issues.push(push("cfg.githubOperation"));
      break;
    case "action.http":
      if (!str(c.url)) issues.push(push("cfg.httpUrl"));
      break;
    case "action.command": {
      const cmd = (c.command ?? {}) as Record<string, unknown>;
      if (!str(cmd.program)) issues.push(push("cfg.commandProgram"));
      break;
    }
    case "action.create_task":
      if (!str(c.title)) issues.push(push("cfg.taskTitle"));
      break;
    case "workflow.call":
      if (!str(c.template_id)) issues.push(push("cfg.callTemplate"));
      break;
    case "data.set":
      if (!str(c.name)) issues.push(push("cfg.varName"));
      break;
    case "data.map":
      if (readMappings(c.mappings).length === 0) issues.push(push("cfg.mappingRows"));
      break;
    case "flow.condition": {
      const cond = (c.condition ?? {}) as ConditionAst;
      const all = Array.isArray(cond.all) ? cond.all : [];
      const any = Array.isArray(cond.any) ? cond.any : [];
      if (all.length === 0 && any.length === 0) issues.push(push("cfg.conditionRules"));
      if (all.length > 0 && any.length > 0) issues.push(push("cfg.conditionConflict"));
      break;
    }
    case "flow.switch": {
      const rules = readSwitchRules(c);
      if (rules.length === 0) issues.push(push("cfg.branches"));
      const names = rules.map((r) => r.branch.trim());
      // switch 会自动追加 default 端口,分支名不得占用;空分支名同样报错
      if (names.some((n) => n === "" || n === "default")) issues.push(push("cfg.reservedBranchName"));
      if (new Set(names).size !== names.length) issues.push(push("cfg.duplicateBranch"));
      // 每条分支的条件组(all/any)至少要有一条规则,且两者不能同时配置
      for (const r of rules) {
        const cond = (typeof r.condition === "object" && r.condition !== null ? r.condition : {}) as { all?: unknown; any?: unknown };
        const all = Array.isArray(cond.all) ? cond.all : [];
        const any = Array.isArray(cond.any) ? cond.any : [];
        if (all.length === 0 && any.length === 0) {
          issues.push(push("cfg.switchCondition"));
          break;
        }
        if (all.length > 0 && any.length > 0) issues.push(push("cfg.conditionConflict"));
      }
      break;
    }
    case "flow.parallel": {
      const branches = readBranches(c);
      if (branches.length === 0) issues.push(push("cfg.branches"));
      if (new Set(branches).size !== branches.length) issues.push(push("cfg.duplicateBranch"));
      break;
    }
    case "flow.join":
      if (c.mode !== "all" && c.mode !== "any") issues.push(push("cfg.joinMode"));
      break;
    case "flow.wait": {
      if (c.mode === "duration") {
        if (!num(c.minutes)) issues.push(push("cfg.waitMinutes"));
      } else if (c.mode === "github_event") {
        if (!str(c.event)) issues.push(push("cfg.waitEvent"));
      } else {
        issues.push(push("cfg.waitMode"));
      }
      break;
    }
    case "human.input":
      if (!str(c.prompt)) issues.push(push("cfg.humanPromptRequired"));
      break;
  }
  if (node.type_key === "agent.task") {
    const retry = (c.retry ?? {}) as Record<string, unknown>;
    if (!num(retry.max_attempts) || Number(retry.max_attempts) < 1) {
      issues.push({ node_id: node.id, key: "cfg.retryAttempts" });
    }
  }
  if (node.type_key === "flow.condition") {
    const cond = (c.condition ?? {}) as { all?: Array<Record<string, unknown>>; any?: Array<Record<string, unknown>> };
    for (const rule of [...(cond.all ?? []), ...(cond.any ?? [])]) {
      if (!str(rule.path) || !EXTRA_OPS.has(String(rule.op))) {
        issues.push({ node_id: node.id, key: "cfg.conditionRule" });
        break;
      }
    }
  }
  return issues;
}
