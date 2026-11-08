import { describe, expect, it } from "vitest";
import {
  IMPORT_DEFAULT_NAME,
  dedupeTemplateName,
  exportTemplateJSON,
  parseImportedTemplate,
  sanitizeFileName,
} from "./transfer";
import type { TemplateGraph } from "./types";

const graph = (): TemplateGraph => ({
  name: 'demo/x:y*z?"<>|',
  description: "示例",
  enabled: false,
  nodes: [
    {
      id: "t",
      type_key: "trigger.manual",
      name: "开始",
      sort_order: 0,
      config: {},
      ui_config: { x: 12, y: 34 },
    },
  ],
  edges: [],
});

describe("sanitizeFileName", () => {
  it("strips Windows-illegal filename characters", () => {
    expect(sanitizeFileName('a/b\\c:d*e?f"g<h>i|j')).toBe("abcdefghij");
  });
  it("falls back to workflow when nothing remains", () => {
    expect(sanitizeFileName('/:*?"<>|')).toBe("workflow");
    expect(sanitizeFileName("   ")).toBe("workflow");
  });
});

describe("exportTemplateJSON", () => {
  it("serializes the full graph pretty-printed with 2-space indent", () => {
    const text = exportTemplateJSON(graph());
    expect(text.startsWith("{\n  \"name\":")).toBe(true);
    const parsed = JSON.parse(text) as TemplateGraph;
    expect(parsed.name).toBe(graph().name);
    expect(parsed.enabled).toBe(false);
    expect(parsed.nodes).toEqual(graph().nodes);
    expect(parsed.edges).toEqual([]);
  });
});

describe("dedupeTemplateName", () => {
  it("returns the name unchanged when unique", () => {
    expect(dedupeTemplateName("X", new Set(["Y", "Z"]))).toBe("X");
  });
  it("appends (导入) then (导入 2), (导入 3) progressively", () => {
    expect(dedupeTemplateName("X", new Set(["X"]))).toBe("X(导入)");
    expect(dedupeTemplateName("X", new Set(["X", "X(导入)"]))).toBe("X(导入 2)");
    expect(dedupeTemplateName("X", new Set(["X", "X(导入)", "X(导入 2)"]))).toBe("X(导入 3)");
  });
});

describe("parseImportedTemplate", () => {
  it("rejects non-JSON text and non-object JSON", () => {
    expect(parseImportedTemplate("not json").ok).toBe(false);
    expect(parseImportedTemplate("[]").ok).toBe(false);
    expect(parseImportedTemplate("null").ok).toBe(false);
  });
  it("rejects objects without nodes/edges arrays", () => {
    expect(parseImportedTemplate('{"name":"x"}').ok).toBe(false);
    expect(parseImportedTemplate('{"nodes":"x","edges":[]}').ok).toBe(false);
  });
  it("parses a valid graph and fills defaults for missing meta fields", () => {
    const text = JSON.stringify({ nodes: [{ id: "t", type_key: "trigger.manual" }], edges: [] });
    const parsed = parseImportedTemplate(text);
    expect(parsed.ok).toBe(true);
    if (parsed.ok) {
      expect(parsed.template.name).toBe("");
      expect(parsed.template.enabled).toBe(true);
      expect(parsed.template.nodes[0].name).toBe("");
      // 缺 ui_config 留空而不是兜底 (0,0):画布层 layoutNodes 会走 dagre 自动布局
      expect(parsed.template.nodes[0].ui_config).toBeUndefined();
    }
  });
  it("round-trips exportTemplateJSON output", () => {
    const parsed = parseImportedTemplate(exportTemplateJSON(graph()));
    expect(parsed.ok).toBe(true);
    if (parsed.ok) {
      expect(parsed.template.name).toBe(graph().name);
      expect(parsed.template.description).toBe(graph().description);
      expect(parsed.template.nodes[0].id).toBe("t");
      expect(parsed.template.nodes[0].ui_config).toEqual({ x: 12, y: 34 });
    }
  });
});

describe("IMPORT_DEFAULT_NAME", () => {
  it("is used as the dedupe base for nameless imports", () => {
    expect(dedupeTemplateName(IMPORT_DEFAULT_NAME, new Set([IMPORT_DEFAULT_NAME]))).toBe(`${IMPORT_DEFAULT_NAME}(导入)`);
  });
});
