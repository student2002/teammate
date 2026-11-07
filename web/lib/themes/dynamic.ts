// dynamic.ts:运行时自定义主题(上传安装,部署级共享)的前端支撑。
// CSS 注入(生成 [data-theme] 变量块)、上传文件校验(与后端契约一致)、类型定义。
// 校验在前端做一遍是为了上传前即时报错;服务端仍独立校验(入口各校一次)。

import { THEME_VARS } from "./types";

/** 后端自定义主题响应(见 internal/server/handler/themes.go) */
export interface CustomThemeInfo {
  id: string;
  name_zh: string;
  name_en: string;
  icon: string;
  vars: Record<string, string>;
}

const ID_RE = /^[a-z0-9][a-z0-9-]{1,40}[a-z0-9]$/;
const HEX_RE = /^#[0-9a-fA-F]{6}$/;
const SHADOW_RE = /^rgba\(\s*(25[0-5]|2[0-4]\d|1?\d?\d)\s*,\s*(25[0-5]|2[0-4]\d|1?\d?\d)\s*,\s*(25[0-5]|2[0-4]\d|1?\d?\d)\s*,\s*(0|1|0\.\d+)\s*\)$/;
const CONTRACT_KEYS = new Set<string>(THEME_VARS);
const ICONS = ["moon", "sun", "sparkles", "zap", "contrast"] as const;
const BUILTIN_IDS = ["dark", "light", "oled"];

/** 主题上传文件(theme.json)的顶层结构 */
export interface ThemeFile {
  id: string;
  name: { "zh-CN": string; "en-US": string };
  icon: string;
  vars: Record<string, string>;
}

/** 校验结果:成功时携带规整后的上传体(与后端 createCustomThemeRequest 对齐) */
export interface ThemeFileValidation {
  ok: boolean;
  error?: string;
  payload?: { id: string; name_zh: string; name_en: string; icon: string; vars: Record<string, string> };
  preview?: { bg: string; surface: string; primary: string };
}

export function validateThemeFile(raw: unknown): ThemeFileValidation {
  const fail = (error: string): ThemeFileValidation => ({ ok: false, error });
  if (typeof raw !== "object" || raw === null) return fail("文件必须是 JSON 对象");
  const f = raw as Partial<ThemeFile>;
  if (typeof f.id !== "string" || !ID_RE.test(f.id)) return fail("id 须为小写字母/数字/连字符(kebab-case)");
  if (BUILTIN_IDS.includes(f.id)) return fail(`id "${f.id}" 与内置主题冲突`);
  const name = f.name;
  if (
    typeof name !== "object" || name === null ||
    typeof name["zh-CN"] !== "string" || !name["zh-CN"].trim() ||
    typeof name["en-US"] !== "string" || !name["en-US"].trim()
  ) {
    return fail("name 需含非空的 zh-CN 与 en-US 两个名称");
  }
  if (name["zh-CN"].length > 100 || name["en-US"].length > 100) {
    return fail("名称长度不得超过 100 字符");
  }
  const icon = typeof f.icon === "string" && f.icon ? f.icon : "sparkles";
  if (!ICONS.includes(icon as (typeof ICONS)[number])) {
    return fail(`icon 须为 ${ICONS.join("/")} 之一`);
  }
  const vars = f.vars;
  if (typeof vars !== "object" || vars === null) return fail("vars 必须是变量名到颜色值的对象");
  const seen = new Set<string>();
  for (const [k, v] of Object.entries(vars)) {
    // 键必须属于契约集合:注入面在键维度同样封死(值会被拼入全局样式)
    if (!CONTRACT_KEYS.has(k)) return fail(`vars 含未知变量 "${k}"`);
    if (typeof v !== "string") return fail(`vars["${k}"] 必须是字符串`);
    if (k === "--shadow") {
      if (!SHADOW_RE.test(v)) return fail('vars["--shadow"] 须为 rgba() 格式');
    } else if (!HEX_RE.test(v)) {
      return fail(`vars["${k}"] 须为 6 位十六进制颜色(如 #09090b)`);
    }
    seen.add(k);
  }
  const missing = THEME_VARS.filter((v) => !seen.has(v));
  if (missing.length > 0) return fail(`缺少必需变量: ${missing.join(", ")}`);
  return {
    ok: true,
    payload: { id: f.id, name_zh: name["zh-CN"].trim(), name_en: name["en-US"].trim(), icon, vars: vars as Record<string, string> },
    preview: { bg: vars["--bg"]!, surface: vars["--surface"]!, primary: vars["--primary"]! },
  };
}

const STYLE_ID = "custom-themes-css";

/** 把自定义主题变量块写入 <style>,与内置主题一样经 data-theme 属性生效 */
export function injectCustomThemesCss(themes: CustomThemeInfo[]): void {
  if (typeof document === "undefined") return;
  let style = document.getElementById(STYLE_ID);
  if (!style) {
    style = document.createElement("style");
    style.id = STYLE_ID;
    document.head.appendChild(style);
  }
  const css = themes
    .map((t) => {
      // 只注入契约键:即使服务端数据异常也不引入非契约内容进全局样式
      const decls = THEME_VARS.filter((k) => typeof t.vars[k] === "string")
        .map((k) => `${k}:${t.vars[k]};`)
        .join("");
      return `[data-theme="${t.id}"]{${decls}}`;
    })
    .join("\n");
  style.textContent = css;
}
