// 暗色主题（默认）：变量块见同目录 theme.css。
import type { ThemeDefinition } from "../types";
import "./theme.css";

const dark: ThemeDefinition = {
  id: "dark",
  name: { "zh-CN": "深色", "en-US": "Dark" },
  icon: "moon",
  preview: { bg: "#020617", surface: "#1e293b", primary: "#3b82f6" },
};

export default dark;
