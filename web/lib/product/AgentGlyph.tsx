// AgentGlyph：Agent 智能身份图形。按名称字符 code 加权累加得到稳定 hash，确定性
// 选择 4 组几何纹理之一（同一名称永远同一纹理，禁止随机值）；颜色只来自
// var(--primary)/var(--accent) 及其透明度，头像底色不承担状态语义。
// 状态由右下角独立小圆点表达：online=success / busy=accent /
// paused+pending=warning / offline=中性；具体状态含义由相邻文字徽章承载。

import type { AgentStatus } from "@/lib/types";

export type AgentGlyphProps = {
  name: string;
  status?: AgentStatus;
  size?: "sm" | "md" | "lg";
};

/** 名称 → 0..3 的稳定纹理编号（字符 code 加权累加；导出供确定性测试断言） */
export function agentGlyphVariant(name: string): number {
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash + name.charCodeAt(i) * (i + 1)) >>> 0;
  }
  return hash % 4;
}

type GlyphSize = NonNullable<AgentGlyphProps["size"]>;

const BOX: Record<GlyphSize, string> = {
  sm: "w-7 h-7",
  md: "w-9 h-9",
  lg: "w-12 h-12",
};

const RADIUS: Record<GlyphSize, string> = {
  sm: "rounded-[var(--radius-control)]",
  md: "rounded-[var(--radius-control)]",
  lg: "rounded-[var(--radius-card)]",
};

const DOT: Record<GlyphSize, string> = {
  sm: "w-2 h-2 -right-0.5 -bottom-0.5",
  md: "w-2.5 h-2.5 -right-1 -bottom-1",
  lg: "w-3 h-3 -right-1 -bottom-1",
};

/** 状态点底色：独立于头像纹理色调 */
const STATUS_DOT: Record<string, string> = {
  online: "bg-success-fg",
  busy: "bg-accent-fg",
  paused: "bg-warning-fg",
  pending: "bg-warning-fg",
  offline: "bg-fg-faint",
};

/** 四组几何纹理：主形用当前色调，细节点缀用另一色调；SVG 颜色经 currentColor 继承 */
function Texture({ variant }: { variant: number }) {
  const base = variant % 2 === 0 ? "var(--primary)" : "var(--accent)";
  const detail = variant % 2 === 0 ? "var(--accent)" : "var(--primary)";
  return (
    <svg
      viewBox="0 0 36 36"
      fill="none"
      aria-hidden="true"
      className="w-full h-full"
      style={{ color: base }}
    >
      {variant === 0 && (
        <>
          <circle cx="18" cy="18" r="11.5" stroke="currentColor" strokeWidth="2.5" />
          <circle cx="18" cy="18" r="5" stroke="currentColor" strokeWidth="2" strokeOpacity="0.5" />
          <g style={{ color: detail }}>
            <circle cx="27.5" cy="8.5" r="3" fill="currentColor" />
          </g>
        </>
      )}
      {variant === 1 && (
        <>
          <path d="M7 25L25 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
          <path d="M13 30L30 13" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeOpacity="0.45" />
          <path d="M6 15L15 6" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeOpacity="0.45" />
          <g style={{ color: detail }}>
            <circle cx="28.5" cy="28.5" r="3" fill="currentColor" />
          </g>
        </>
      )}
      {variant === 2 && (
        <>
          <path
            d="M18 6.5L29.5 27.5L6.5 27.5Z"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinejoin="round"
          />
          <g style={{ color: detail }}>
            <circle cx="18" cy="20" r="3.2" fill="currentColor" />
          </g>
        </>
      )}
      {variant === 3 && (
        <>
          <rect x="6.5" y="6.5" width="10" height="10" rx="2" stroke="currentColor" strokeWidth="2.5" />
          <rect x="19.5" y="19.5" width="10" height="10" rx="2" stroke="currentColor" strokeWidth="2.5" strokeOpacity="0.45" />
          <rect x="19.5" y="6.5" width="10" height="10" rx="2" fill="currentColor" fillOpacity="0.25" />
          <g style={{ color: detail }}>
            <rect x="6.5" y="19.5" width="10" height="10" rx="2" fill="currentColor" fillOpacity="0.8" />
          </g>
        </>
      )}
    </svg>
  );
}

export default function AgentGlyph({ name, status, size = "md" }: AgentGlyphProps) {
  const variant = agentGlyphVariant(name);
  const toneCls = variant % 2 === 0 ? "bg-primary/10 border-primary/25" : "bg-accent/10 border-accent/25";
  return (
    <span
      aria-hidden="true"
      className={`relative inline-flex items-center justify-center shrink-0 border ${BOX[size]} ${RADIUS[size]} ${toneCls}`}
    >
      <Texture variant={variant} />
      {status && (
        <span
          className={`absolute rounded-full ring-2 ring-surface ${DOT[size]} ${STATUS_DOT[status] || STATUS_DOT.offline}`}
        />
      )}
    </span>
  );
}
