"use client";
// ModalPortal — 将弹窗挂载到 document.body,使其 position:fixed 以视口为基准。
// 页面容器的进场动画(page-enter)与 backdrop-filter 会让祖先成为 fixed 元素的
// 定位基准,滚动后弹窗会定位到容器顶部而非视口居中;所有弹窗统一经此 Portal 渲染。
import { createPortal } from "react-dom";

export default function ModalPortal({ children }: { children: React.ReactNode }) {
  if (typeof document === "undefined") return null;
  return createPortal(children, document.body);
}
