// 浅色主题：变量块见同目录 theme.css。
import type { ThemeDefinition } from "../types";
import "./theme.css";

const light: ThemeDefinition = {
  id: "light",
  name: { "zh-CN": "浅色", "en-US": "Light" },
  icon: "sun",
  preview: { bg: "#f4f6f9", surface: "#edf1f6", primary: "#6556d8" },
};

export default light;
