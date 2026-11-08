// 工作流模板整图的导入导出:导出序列化为 pretty JSON 并触发浏览器下载;
// 导入只做形状规整,内容合法性由 validateTemplate(与编辑器同一套)把关。
import type { TemplateEdge, TemplateGraph, TemplateNode, WorkflowTypeKey } from "./types";

// 导入文件缺 name 时的默认模板名;重名后缀同款字面量
export const IMPORT_DEFAULT_NAME = "导入的工作流";

// Windows 文件名非法字符 \/:*?"<>|;清洗后为空时回退 workflow,避免只剩 ".json"
export function sanitizeFileName(name: string): string {
  const cleaned = name.replace(/[\\/:*?"<>|]/g, "").trim();
  return cleaned || "workflow";
}

// 整图 → 导出 JSON 字符串(2 空格缩进,只含交换字段)
export function exportTemplateJSON(template: TemplateGraph): string {
  return JSON.stringify(
    {
      name: template.name,
      description: template.description,
      enabled: template.enabled,
      nodes: template.nodes.map((n) => ({
        id: n.id,
        type_key: n.type_key,
        name: n.name,
        sort_order: n.sort_order,
        config: n.config,
        ui_config: n.ui_config,
      })),
      edges: template.edges.map((e) => ({
        id: e.id,
        source_node_id: e.source_node_id,
        source_port: e.source_port,
        target_node_id: e.target_node_id,
        target_port: e.target_port,
      })),
    },
    null,
    2
  );
}

// Blob + 隐藏 a[download] 触发浏览器下载,文件名为清洗后的模板名
export function downloadTemplateFile(template: TemplateGraph): void {
  const blob = new Blob([exportTemplateJSON(template)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `${sanitizeFileName(template.name)}.json`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

// 与现有模板名集合重名时递进加后缀:(导入) → (导入 2) → (导入 3) …,直至不重名
export function dedupeTemplateName(name: string, existing: Set<string>): string {
  if (!existing.has(name)) return name;
  let n = 1;
  let candidate = `${name}(导入)`;
  while (existing.has(candidate)) {
    n += 1;
    candidate = `${name}(导入 ${n})`;
  }
  return candidate;
}

export type ParsedImport = { ok: true; template: TemplateGraph } | { ok: false };

// 导入文件文本 → 整图:nodes/edges 必须是数组(否则视为无效文件),
// 其余字段缺省兜底;节点/边的每个字段统一收敛为字符串/数字,保证后续校验不崩
export function parseImportedTemplate(text: string): ParsedImport {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { ok: false };
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return { ok: false };
  const raw = parsed as Record<string, unknown>;
  if (!Array.isArray(raw.nodes) || !Array.isArray(raw.edges)) return { ok: false };
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const nodes: TemplateNode[] = (raw.nodes as any[]).map((n) => ({
    id: String(n?.id ?? ""),
    type_key: String(n?.type_key ?? "") as WorkflowTypeKey,
    name: String(n?.name ?? ""),
    sort_order: Number(n?.sort_order) || 0,
    config: n?.config ?? {},
    // ui_config 缺失/形态不对时留空,让画布层 layoutNodes 走 dagre 自动布局
    ui_config:
      typeof n?.ui_config?.x === "number" && typeof n?.ui_config?.y === "number" ? { x: n.ui_config.x, y: n.ui_config.y } : undefined,
  }));
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const edges: TemplateEdge[] = (raw.edges as any[]).map((e) => ({
    id: String(e?.id ?? ""),
    source_node_id: String(e?.source_node_id ?? ""),
    source_port: String(e?.source_port ?? ""),
    target_node_id: String(e?.target_node_id ?? ""),
    target_port: String(e?.target_port ?? ""),
    label: String(e?.label ?? ""),
  }));
  return {
    ok: true,
    template: {
      name: String(raw.name ?? ""),
      description: String(raw.description ?? ""),
      enabled: typeof raw.enabled === "boolean" ? raw.enabled : true,
      nodes,
      edges,
    },
  };
}
