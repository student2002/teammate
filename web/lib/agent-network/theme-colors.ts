// theme-colors.ts: 3D 场景与主题语义变量的桥接。
// 场景不写死色调,所有颜色运行时从 CSS 变量读取并随 data-theme 切换更新。

import * as THREE from "three";

export type ThemeColorKey =
  | "primary"
  | "accent"
  | "success"
  | "warning"
  | "line"
  | "lineStrong"
  | "fg"
  | "fgSubtle"
  | "surface";

const THEME_VAR: Record<ThemeColorKey, string> = {
  primary: "--primary-fg",
  accent: "--accent-fg",
  success: "--success-fg",
  warning: "--warning-fg",
  line: "--line",
  lineStrong: "--line-strong",
  fg: "--fg",
  fgSubtle: "--fg-subtle",
  surface: "--surface",
};

export interface ThemeColors {
  primary: THREE.Color;
  accent: THREE.Color;
  success: THREE.Color;
  warning: THREE.Color;
  line: THREE.Color;
  lineStrong: THREE.Color;
  fg: THREE.Color;
  fgSubtle: THREE.Color;
  surface: THREE.Color;
}

let sharedParseCanvas: HTMLCanvasElement | null = null;

/** 任意 CSS 颜色写法(含自定义主题上传的函数式颜色)统一经 canvas 解析为 sRGB */
function cssColor(value: string): THREE.Color | null {
  if (!sharedParseCanvas) {
    sharedParseCanvas = document.createElement("canvas");
    sharedParseCanvas.width = 1;
    sharedParseCanvas.height = 1;
  }
  const ctx = sharedParseCanvas.getContext("2d");
  if (!ctx) return null;
  ctx.clearRect(0, 0, 1, 1);
  ctx.fillStyle = value;
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data;
  if (a === 0) return null;
  return new THREE.Color(`rgba(${r}, ${g}, ${b}, ${(a / 255).toFixed(3)})`);
}

export function readThemeColors(): ThemeColors {
  const computed = getComputedStyle(document.documentElement);
  const fallback = new THREE.Color("#7f7f7f");
  const result = {} as ThemeColors;
  for (const [key, varName] of Object.entries(THEME_VAR)) {
    const raw = computed.getPropertyValue(varName).trim();
    result[key as ThemeColorKey] = (raw && cssColor(raw)) || fallback;
  }
  return result;
}

/** THREE 色值转带透明度的 CSS 颜色串,供标签/装饰 canvas 使用 */
export function withAlpha(color: THREE.Color, alpha: number): string {
  return color.getStyle().replace("rgb(", "rgba(").replace(")", `, ${alpha})`);
}
