"use client";
// 节点卡:端口是卡片正式组成部分,端口旁显示结果名(§13.2)。
// 布局参照 ComfyUI:端口行是标题栏下的专用横条,输入名在圆点右侧、
// 输出名在圆点左侧,正文内容排在端口行之下,文字互不重叠。
// 开始/完成主标题固定,卡片摘要由 catalog.summaryItems 提供。
import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { useTranslations } from "next-intl";
import { CATEGORY_TONE, HANDLE_BASE, KNOWN_PORT_KEYS, TONE_VAR, def, outputPorts, portBandHeight, portCenterY, summaryItems, type PortDef } from "./catalog";
import { hasCardWidgets, CardWidgets } from "./node_widgets";
import { setIn } from "./inspectors";
import type { TemplateNode } from "./types";

export interface EditorNodeData extends Record<string, unknown> {
  node: TemplateNode;
  invalid: boolean;
  // 当前节点可引用的数据路径候选(trigger/上游输出/vars),供卡上字段选择器使用
  paths?: string[];
  // 传入即启用卡上内联编辑(编辑器画布);运行视图不传,卡片只读
  onConfig?: (config: Record<string, unknown>) => void;
  // 为 true 时卡片控件让位给摘要(Inspector 面板打开期间,防双实例草稿互踩)
  widgetsHidden?: boolean;
}

export type EditorRFNode = Node<EditorNodeData, "v2">;

function PortHandle({ port, top, type }: { port: PortDef; top: number; type: "source" | "target" }) {
  return (
    <Handle
      id={port.key}
      type={type}
      position={type === "source" ? Position.Right : Position.Left}
      style={{ ...HANDLE_BASE, top, background: TONE_VAR[port.tone] }}
    />
  );
}

export function NodeCard({ data, selected }: NodeProps<EditorRFNode>) {
  const t = useTranslations("workflows.editor");
  const node = data.node;
  const d = def(node.type_key);
  const Icon = d.icon;
  const outPorts = outputPorts(node.type_key, node.config);
  const inPorts = d.input_ports;
  const portLabel = (key: string) => (KNOWN_PORT_KEYS.has(key) ? t(`ports.${key}`) : key);
  const fixedTitle = node.type_key.startsWith("trigger.") ? t("startTitle") : node.type_key === "flow.end" ? t("endTitle") : null;
  const bandHeight = portBandHeight(inPorts.length, outPorts.length);

  return (
    <div
      className={`relative bg-surface-2 border rounded-[var(--radius-control)] min-w-[220px] max-w-[260px] shadow-sm ${
        data.invalid ? "border-danger" : selected ? "border-primary" : "border-line-strong"
      }`}
    >
      <div
        className="px-3 h-7 flex items-center gap-1.5 rounded-t-[var(--radius-control)] border-b border-line"
        style={{ background: `color-mix(in srgb, ${TONE_VAR[CATEGORY_TONE[d.category]]} 12%, transparent)` }}
      >
        <Icon className="w-3.5 h-3.5 shrink-0" style={{ color: TONE_VAR[CATEGORY_TONE[d.category]] }} />
        <span className="text-xs font-bold truncate">{fixedTitle ?? node.name}</span>
        {data.invalid && <span className="ml-auto w-2 h-2 rounded-full bg-danger shrink-0" aria-label={t("invalidBadge")} />}
      </div>
      {bandHeight > 0 && (
        <div className="relative" style={{ height: bandHeight }}>
          {inPorts.map((p, i) => (
            <PortHandle key={p.key} port={p} top={portCenterY(i)} type="target" />
          ))}
          {outPorts.map((p, i) => (
            <PortHandle key={p.key} port={p} top={portCenterY(i)} type="source" />
          ))}
          {inPorts.map((p, i) => (
            <span
              key={`lb-${p.key}`}
              className="absolute left-2.5 -translate-y-1/2 text-[9px] text-fg-faint pointer-events-none whitespace-nowrap max-w-[120px] truncate"
              style={{ top: portCenterY(i) }}
            >
              {portLabel(p.key)}
            </span>
          ))}
          {outPorts.map((p, i) => (
            <span
              key={`lb-${p.key}`}
              className="absolute right-2.5 -translate-y-1/2 text-[9px] text-fg-faint pointer-events-none whitespace-nowrap max-w-[120px] truncate"
              style={{ top: portCenterY(i) }}
            >
              {portLabel(p.key)}
            </span>
          ))}
        </div>
      )}
      <div className="px-2 py-1.5 space-y-1 min-h-[46px]">
        <div className="text-[10px] text-fg-subtle px-1">{t(`types.${node.type_key}.label`)}</div>
        {fixedTitle !== null && node.name && node.name !== fixedTitle && (
          <div className="text-[10px] text-fg-faint truncate max-w-[170px] px-1">{node.name}</div>
        )}
        {data.onConfig && !data.widgetsHidden && hasCardWidgets(node.type_key) ? (
          <CardWidgets
            node={node}
            paths={data.paths ?? []}
            onSet={(path, value) => data.onConfig?.(setIn(node.config, path, value) as Record<string, unknown>)}
          />
        ) : (
          summaryItems(node.type_key, node.config).map((item, i) => (
            <div key={i} className="text-[10px] text-fg-faint truncate max-w-[170px] px-1">
              {t(item.key, item.params)}
            </div>
          ))
        )}
      </div>
    </div>
  );
}
