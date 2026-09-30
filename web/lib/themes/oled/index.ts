// 纯黑主题（OLED）：变量块见同目录 theme.css。
import type { ThemeDefinition } from "../types";
import "./theme.css";

const oled: ThemeDefinition = {
  id: "oled",
  name: { "zh-CN": "纯黑", "en-US": "OLED Black" },
  icon: "contrast",
  preview: { bg: "#000000", surface: "#171717", primary: "#3b82f6" },
};

export default oled;
