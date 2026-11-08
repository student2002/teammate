// BrandMark：产品标识。三个连接节点构成抽象字母 T——左右节点代表不同 Agent，
// 中间菱形节点代表交接/审查（与协作链的 review 节点语法一致）。
// 图形用 currentColor，由外层容器决定颜色（通常放在 primary→accent 渐变块内），
// 保证任意主题下与底色的对比关系不变。

export default function BrandMark({
  size = 20,
  className = "",
}: {
  size?: number;
  className?: string;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      className={className}
      aria-hidden="true"
    >
      <path
        d="M7 12h18M16 12v14"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
      <circle cx="7" cy="12" r="3.2" fill="currentColor" />
      <circle cx="25" cy="12" r="3.2" fill="currentColor" />
      <rect
        x="13.4"
        y="9.4"
        width="5.2"
        height="5.2"
        rx="1"
        transform="rotate(45 16 12)"
        fill="currentColor"
      />
    </svg>
  );
}
