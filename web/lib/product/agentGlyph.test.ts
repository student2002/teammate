import { describe, expect, it } from "vitest";
import { agentGlyphVariant } from "./AgentGlyph";

describe("agentGlyphVariant", () => {
  it("同一名称永远映射到同一纹理", () => {
    expect(agentGlyphVariant("coder-1")).toBe(agentGlyphVariant("coder-1"));
    expect(agentGlyphVariant("审核员")).toBe(agentGlyphVariant("审核员"));
  });

  it("结果始终是 0..3 的整数（含空名与长名）", () => {
    const names = ["", "a", "bot", "agent-42", "中文名-审核员", "x".repeat(256)];
    for (const name of names) {
      const variant = agentGlyphVariant(name);
      expect(Number.isInteger(variant)).toBe(true);
      expect(variant).toBeGreaterThanOrEqual(0);
      expect(variant).toBeLessThanOrEqual(3);
    }
  });

  it("一批名称能覆盖到多种纹理（不是恒定值）", () => {
    const variants = new Set(
      Array.from({ length: 40 }, (_, i) => agentGlyphVariant(`agent-${i}`))
    );
    expect(variants.size).toBeGreaterThan(1);
  });
});
