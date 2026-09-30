// 浅色主题：变量块见同目录 theme.css。
import type { ThemeDefinition } from "../types";
import "./theme.css";

const light: ThemeDefinition = {
  id: "light",
  name: { "zh-CN": "浅色", "en-US": "Light" },
  icon: "sun",
  preview: { bg: "#f8fafc", surface: "#ffffff", primary: "#3b82f6" },
};

export default light;
