// 主题模组类型与变量契约。
// 新增主题：复制 lib/themes/ 下任一目录改 theme.css 变量值与本文件元数据，再到 registry.ts 注册一行。

/** 主题图标名，ThemeSwitcher 映射到 lucide 图标 */
export type ThemeIcon = "moon" | "sun" | "sparkles" | "zap" | "contrast";

/** 主题元数据；变量值位于同目录 theme.css */
export interface ThemeDefinition {
  /** data-theme 属性值，必须与目录名一致，kebab-case */
  id: string;
  /** 双语名称，内嵌于模组内保证自包含 */
  name: { "zh-CN": string; "en-US": string };
  icon: ThemeIcon;
  /** 切换器三色预览卡 */
  preview: { bg: string; surface: string; primary: string };
}

/** 每套主题的 theme.css 必须定义的全部变量；themes.test.ts 据此做完整性校验 */
export const THEME_VARS = [
  "--bg",
  "--surface",
  "--surface-2",
  "--surface-3",
  "--line",
  "--line-strong",
  "--fg",
  "--fg-muted",
  "--fg-subtle",
  "--fg-faint",
  "--primary",
  "--primary-strong",
  "--primary-fg",
  "--accent",
  "--accent-strong",
  "--accent-fg",
  "--success",
  "--success-strong",
  "--success-fg",
  "--warning",
  "--warning-strong",
  "--warning-fg",
  "--danger",
  "--danger-strong",
  "--danger-fg",
  "--shadow",
] as const;
