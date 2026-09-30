// 主题模组完整性校验：注册表一致性 + 变量契约覆盖，防止新增主题漏定义变量。
import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { THEMES } from "./registry";
import { THEME_VARS } from "./types";

const themesDir = dirname(fileURLToPath(import.meta.url));
const themeDirs = readdirSync(themesDir, { withFileTypes: true })
  .filter((e) => e.isDirectory())
  .map((e) => e.name);

describe("theme registry", () => {
  it("注册表 id 无重复", () => {
    const ids = THEMES.map((t) => t.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("lib/themes 下每个目录都已注册且 id 与目录名一致", () => {
    expect(THEMES.map((t) => t.id).sort()).toEqual([...themeDirs].sort());
  });
});

describe.each(THEMES)("theme module %#", (theme) => {
  it(`${theme.id}: theme.css 覆盖变量契约 THEME_VARS 全部变量`, () => {
    const css = readFileSync(join(themesDir, theme.id, "theme.css"), "utf-8");
    const missing = THEME_VARS.filter((v) => !css.includes(`${v}:`));
    expect(missing, `缺失变量: ${missing.join(", ")}`).toEqual([]);
  });

  it(`${theme.id}: name 提供双语文案，preview 提供三色`, () => {
    expect(theme.name["zh-CN"].length).toBeGreaterThan(0);
    expect(theme.name["en-US"].length).toBeGreaterThan(0);
    expect(theme.preview.bg).toMatch(/^#/);
    expect(theme.preview.surface).toMatch(/^#/);
    expect(theme.preview.primary).toMatch(/^#/);
  });
});
