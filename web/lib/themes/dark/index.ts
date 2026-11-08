// 暗色主题（默认）：变量块见同目录 theme.css。
import type { ThemeDefinition } from "../types";
import "./theme.css";

const dark: ThemeDefinition = {
  id: "dark",
  name: { "zh-CN": "深色", "en-US": "Dark" },
  icon: "moon",
  preview: { bg: "#080a0f", surface: "#131a25", primary: "#7c6cf2" },
};

export default dark;
